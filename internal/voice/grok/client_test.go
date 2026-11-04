package grok

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
)

const testModel = "grok-voice-3.0"

// fake is a scripted xAI realtime server: it records every client event and lets the test push server events.
type fake struct {
	srv    *httptest.Server
	got    chan map[string]any
	conn   chan *websocket.Conn
	header http.Header
	query  string
	ws     *websocket.Conn
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{got: make(chan map[string]any, 100), conn: make(chan *websocket.Conn, 1)}
	h := websocket.Server{
		Handshake: func(_ *websocket.Config, r *http.Request) error {
			f.header, f.query = r.Header.Clone(), r.URL.RawQuery
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			f.conn <- ws
			for {
				var raw string
				if err := websocket.Message.Receive(ws, &raw); err != nil {
					return
				}
				var ev map[string]any
				if err := json.Unmarshal([]byte(raw), &ev); err != nil {
					t.Errorf("client sent bad JSON: %s", raw)
					return
				}
				if ev["type"] == "session.update" {
					websocket.Message.Send(ws, `{"type":"session.updated"}`)
				}
				f.got <- ev
			}
		},
	}
	f.srv = httptest.NewServer(h)
	t.Cleanup(f.srv.Close)
	return f
}

// dial connects through voice.Dial (so Defaults fill Model / Voice) and returns the session and its session.update.
func (f *fake) dial(t *testing.T, cfg voice.Config) (*Session, map[string]any) {
	t.Helper()
	cfg.Provider, cfg.Key = voice.Grok, "test-key"
	cfg.Extra = map[string]string{"url": "ws" + strings.TrimPrefix(f.srv.URL, "http")}
	vs, err := voice.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := vs.(*Session)
	t.Cleanup(func() { s.Close() })
	f.ws = <-f.conn
	ev := f.next(t)
	if ev["type"] != "session.update" {
		t.Fatalf("first event %v", ev["type"])
	}
	return s, ev["session"].(map[string]any)
}

func (f *fake) push(t *testing.T, evs ...string) {
	t.Helper()
	for _, ev := range evs {
		if err := websocket.Message.Send(f.ws, ev); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fake) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case ev := <-f.got:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a client event")
		return nil
	}
}

// quiet asserts the client sends nothing more for a while.
func (f *fake) quiet(t *testing.T) {
	t.Helper()
	select {
	case ev := <-f.got:
		t.Fatalf("unexpected client event %s", js(ev))
	case <-time.After(300 * time.Millisecond):
	}
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func wait[T any](t *testing.T, c <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

var testTools = []tools.ToolDef{{Type: "function", Name: "list_panes", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}

func TestSessionUpdateVoice(t *testing.T) {
	f := newFake(t)
	_, sess := f.dial(t, voice.Config{Instructions: "hi", Tools: testTools, Transcribe: true})
	if f.header.Get("Authorization") != "Bearer test-key" || f.header.Get("User-Agent") != "rex-voiced/dev" {
		t.Errorf("headers = %v", f.header)
	}
	if f.query != "model=grok-voice-3.0&reasoning.effort=none" {
		t.Errorf("query = %q", f.query)
	}
	for _, k := range []string{"type", "tool_choice", "output_modalities"} {
		if _, ok := sess[k]; ok {
			t.Errorf("session has %q: %s", k, js(sess))
		}
	}
	if sess["instructions"] != "hi" || sess["voice"] != "Ara" {
		t.Errorf("session = %s", js(sess))
	}
	if got := js(sess["turn_detection"]); got != `{"idle_timeout_ms":60000,"type":"server_vad"}` {
		t.Errorf("turn_detection = %s", got)
	}
	want := `{"input":{"format":{"rate":24000,"type":"audio/pcm"},"transcription":{"language_hint":"zh","model":"grok-transcribe"}},` +
		`"output":{"format":{"rate":24000,"type":"audio/pcm"}}}`
	if got := js(sess["audio"]); got != want {
		t.Errorf("audio =\n%s\nwant\n%s", got, want)
	}
	if js(sess["tools"]) != `[{"description":"d","name":"list_panes","parameters":{"type":"object"},"type":"function"}]` {
		t.Errorf("tools = %s", js(sess["tools"]))
	}
}

func TestSessionUpdateTextOnly(t *testing.T) {
	f := newFake(t)
	cfg := voice.Config{TextOnly: true, Voice: "eve", Model: "grok-voice-latest"}
	cfg.Provider, cfg.Key = voice.Grok, "test-key"
	cfg.Extra = map[string]string{"url": "ws" + strings.TrimPrefix(f.srv.URL, "http"), "reasoning": "high"}
	s, err := voice.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sess := f.next(t)["session"].(map[string]any)
	if f.query != "model=grok-voice-latest&reasoning.effort=high" {
		t.Errorf("query = %q", f.query)
	}
	if js(sess["output_modalities"]) != `["text"]` || sess["voice"] != "eve" {
		t.Errorf("session = %s", js(sess))
	}
	for _, k := range []string{"audio", "turn_detection", "tools"} {
		if _, ok := sess[k]; ok {
			t.Errorf("text-only session has %q: %s", k, js(sess))
		}
	}
}

// 24 kHz both ways: what AppendAudio gets goes on the wire unchanged, and so do the answer's deltas.
func TestAudioPassthrough24k(t *testing.T) {
	f := newFake(t)
	got := make(chan []byte, 1)
	s, _ := f.dial(t, voice.Config{Handlers: voice.Handlers{AudioDelta: func(b []byte) { got <- b }}})
	pcm := make([]byte, 960) // 20 ms at 24k
	for i := range pcm {
		pcm[i] = byte(i)
	}
	if err := s.AppendAudio(pcm); err != nil {
		t.Fatal(err)
	}
	ev := f.next(t)
	if b, _ := base64.StdEncoding.DecodeString(ev["audio"].(string)); ev["type"] != "input_audio_buffer.append" || !bytes.Equal(b, pcm) {
		t.Errorf("append = %s (%d bytes)", ev["type"], len(b))
	}
	f.push(t, `{"type":"response.output_audio.delta","response_id":"r1","item_id":"a1","delta":"`+base64.StdEncoding.EncodeToString(pcm)+`"}`)
	if b := wait(t, got, "AudioDelta"); !bytes.Equal(b, pcm) {
		t.Errorf("AudioDelta got %d bytes, want the 960 sent", len(b))
	}
}

func TestTranscriptUpdated(t *testing.T) {
	f := newFake(t)
	users := make(chan string, 4)
	s, _ := f.dial(t, voice.Config{Transcribe: true, Handlers: voice.Handlers{UserText: func(u string) { users <- u }}})
	// No .completed before the answer: the last cumulative text is reported at response.created, once.
	f.push(t,
		`{"type":"conversation.item.input_audio_transcription.updated","item_id":"u1","transcript":"现在"}`,
		`{"type":"conversation.item.input_audio_transcription.updated","item_id":"u1","transcript":"现在几点"}`,
		`{"type":"response.created","response":{"id":"r1"}}`,
		`{"type":"conversation.item.input_audio_transcription.completed","item_id":"u1","transcript":"现在几点"}`,
		// .completed first: it wins and response.created adds nothing.
		`{"type":"conversation.item.input_audio_transcription.updated","item_id":"u2","transcript":"挂"}`,
		`{"type":"conversation.item.input_audio_transcription.completed","item_id":"u2","transcript":"挂断吧"}`,
		`{"type":"response.created","response":{"id":"r2"}}`)
	if u := wait(t, users, "UserText 1"); u != "现在几点" {
		t.Errorf("first UserText = %q", u)
	}
	if u := wait(t, users, "UserText 2"); u != "挂断吧" {
		t.Errorf("second UserText = %q", u)
	}
	select {
	case u := <-users:
		t.Errorf("extra UserText %q", u)
	case <-time.After(300 * time.Millisecond):
	}
	if turns := s.RecentTurns(5); js(turns) != `[{"Role":"user","Text":"现在几点"},{"Role":"user","Text":"挂断吧"}]` {
		t.Errorf("turns = %s", js(turns))
	}
}

// xAI: a response.create sent while the answer's audio is still arriving makes two answers overlap.
func TestToolsWaitForAudioDone(t *testing.T) {
	f := newFake(t)
	f.dial(t, voice.Config{Call: func(context.Context, string, string) string { return `{"ok":1}` }})
	f.push(t,
		`{"type":"response.output_audio.delta","response_id":"r1","item_id":"a1","delta":"AAAA"}`,
		`{"type":"response.output_item.done","response_id":"r1","item":{"id":"fc1","type":"function_call","call_id":"c1","name":"a","arguments":"{}"}}`,
		`{"type":"response.done","response":{"id":"r1","status":"completed"}}`)
	ev := f.next(t)
	if item, _ := ev["item"].(map[string]any); ev["type"] != "conversation.item.create" || item["type"] != "function_call_output" || item["call_id"] != "c1" {
		t.Fatalf("want function_call_output, got %s", js(ev))
	}
	f.quiet(t) // audio of r1 not done yet: no response.create
	f.push(t, `{"type":"response.output_audio.done","response_id":"r1","item_id":"a1"}`)
	if ev := f.next(t); ev["type"] != "response.create" {
		t.Fatalf("want response.create, got %s", js(ev))
	}
	// Final answer done: the tool items are pruned.
	f.push(t, `{"type":"response.done","response":{"id":"r2","status":"completed"}}`)
	for range 2 {
		if ev := f.next(t); ev["type"] != "conversation.item.delete" {
			t.Fatalf("want delete, got %s", js(ev))
		}
	}
	f.quiet(t)
}

func TestSpeechStartedTruncatesNoCancel(t *testing.T) {
	f := newFake(t)
	speech := make(chan struct{}, 1)
	f.dial(t, voice.Config{Handlers: voice.Handlers{SpeechStarted: func() { speech <- struct{}{} }}})
	f.push(t,
		`{"type":"response.output_audio.delta","response_id":"r1","item_id":"a1","delta":"`+base64.StdEncoding.EncodeToString(make([]byte, 960))+`"}`,
		`{"type":"input_audio_buffer.speech_started"}`)
	wait(t, speech, "SpeechStarted")
	ev := f.next(t)
	if ev["type"] != "conversation.item.truncate" || ev["item_id"] != "a1" || ev["audio_end_ms"] != float64(20) {
		t.Fatalf("want truncate a1 at 20 ms, got %s", js(ev))
	}
	f.quiet(t) // the server VAD cancels by itself: no response.cancel
}

func TestUsagePerMinute(t *testing.T) {
	f := newFake(t)
	var log bytes.Buffer
	got := make(chan voice.Usage, 2)
	s, _ := f.dial(t, voice.Config{TextOnly: true, UsageLog: &log, Handlers: voice.Handlers{ResponseDone: func(u voice.Usage) { got <- u }}})
	for range 2 {
		if err := s.SendText("hi"); err != nil {
			t.Fatal(err)
		}
		f.next(t)
		f.next(t)
		f.push(t, `{"type":"response.done","response":{"id":"r","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}}`)
		if u := wait(t, got, "ResponseDone"); u != (voice.Usage{Input: 100, Output: 20}) {
			t.Errorf("ResponseDone usage = %+v (per-response cost must be 0)", u)
		}
	}
	if log.Len() != 0 {
		t.Fatalf("usage written before Close: %q", log.String())
	}
	time.Sleep(50 * time.Millisecond)
	s.Close()
	s.Close() // second Close writes nothing
	line := log.String()
	var l usageLine
	if err := json.Unmarshal([]byte(line), &l); err != nil || strings.Count(line, "\n") != 1 {
		t.Fatalf("usage log %q: %v", line, err)
	}
	if l.Model != testModel || l.Seconds < 0.05 || l.Input != 200 || l.Output != 40 || l.Unpriced || strings.Contains(line, "unpriced") {
		t.Errorf("usage line = %s", line)
	}
	if want := l.Seconds/60*0.08 + 2*0.004; math.Abs(l.USD-want) > 1e-12 {
		t.Errorf("usd = %v, want %v", l.USD, want)
	}
}

// Cost is billed by connection time, so it grows with Age between responses.
func TestCostGrowsWithAge(t *testing.T) {
	f := newFake(t)
	s, _ := f.dial(t, voice.Config{TextOnly: true})
	c1 := s.Cost()
	time.Sleep(30 * time.Millisecond)
	c2, age := s.Cost(), s.Age()
	if c1 <= 0 || c2 <= c1 {
		t.Errorf("Cost %v then %v, want growing", c1, c2)
	}
	if want := age.Seconds() / 60 * 0.08; c2 > want || want-c2 > 1e-6 {
		t.Errorf("Cost = %v, want about %v (age %v)", c2, want, age)
	}
}

func TestDialRejected(t *testing.T) {
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var raw string
		websocket.Message.Receive(ws, &raw)
		websocket.Message.Send(ws, `{"type":"error","error":{"type":"invalid_request_error","message":"bad field"}}`)
		websocket.Message.Receive(ws, &raw)
	}))
	defer srv.Close()
	var log bytes.Buffer
	_, err := voice.Dial(context.Background(), voice.Config{Provider: voice.Grok, Key: "k", UsageLog: &log,
		Extra: map[string]string{"url": "ws" + strings.TrimPrefix(srv.URL, "http")}})
	if err == nil || !strings.Contains(err.Error(), "invalid_request_error: bad field") {
		t.Fatalf("err = %v", err)
	}
	if strings.Count(log.String(), "\n") != 1 {
		t.Errorf("a failed dial still bills its seconds; usage log = %q", log.String())
	}
}
