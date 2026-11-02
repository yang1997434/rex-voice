package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
)

// fake is a scripted OpenAI Realtime server: it records every client event and lets the test push server events.
type fake struct {
	srv    *httptest.Server
	got    chan map[string]any
	conn   chan *websocket.Conn
	header http.Header
	ws     *websocket.Conn
}

func newFake(t *testing.T, ready bool) *fake {
	t.Helper()
	f := &fake{got: make(chan map[string]any, 100), conn: make(chan *websocket.Conn, 1)}
	h := websocket.Server{
		Handshake: func(_ *websocket.Config, r *http.Request) error { f.header = r.Header.Clone(); return nil },
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
				if ev["type"] == "session.update" && ready {
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

func (f *fake) url() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") }

// withURL points cfg at the fake, keeping other Extra entries.
func (f *fake) withURL(cfg voice.Config) voice.Config {
	extra := map[string]string{"url": f.url()}
	for k, v := range cfg.Extra {
		extra[k] = v
	}
	cfg.Extra, cfg.Key = extra, "test-key"
	return cfg
}

func (f *fake) dial(t *testing.T, cfg voice.Config) *Session {
	t.Helper()
	cfg = f.withURL(cfg)
	if cfg.Model == "" {
		cfg.Model = "gpt-realtime-2.1"
	}
	s, err := dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f.ws = <-f.conn
	if ev := f.next(t); ev["type"] != "session.update" {
		t.Fatalf("first event %v", ev["type"])
	}
	return s
}

func (f *fake) push(t *testing.T, ev string) {
	t.Helper()
	if err := websocket.Message.Send(f.ws, ev); err != nil {
		t.Fatal(err)
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
		t.Fatalf("unexpected client event %v", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

func sessionOf(t *testing.T, f *fake, cfg voice.Config) map[string]any {
	t.Helper()
	cfg = f.withURL(cfg)
	cfg.Model = "gpt-realtime-2.1"
	s, err := Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ev := f.next(t)
	if ev["type"] != "session.update" {
		t.Fatalf("got %v", ev["type"])
	}
	if got := f.header.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization = %q", got)
	}
	if got := f.header.Get("User-Agent"); got != "rex-voiced/dev" {
		t.Errorf("User-Agent = %q", got)
	}
	return ev["session"].(map[string]any)
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

var testTools = []tools.ToolDef{{Type: "function", Name: "list_panes", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}

func TestSessionUpdateTextOnly(t *testing.T) {
	sess := sessionOf(t, newFake(t, true), voice.Config{TextOnly: true, Instructions: "hi", Tools: testTools})
	if sess["type"] != "realtime" || sess["instructions"] != "hi" || sess["tool_choice"] != "auto" {
		t.Errorf("session = %s", js(sess))
	}
	if js(sess["output_modalities"]) != `["text"]` {
		t.Errorf("output_modalities = %s", js(sess["output_modalities"]))
	}
	if _, ok := sess["audio"]; ok {
		t.Errorf("text-only session has audio: %s", js(sess))
	}
	if js(sess["tools"]) != `[{"description":"d","name":"list_panes","parameters":{"type":"object"},"type":"function"}]` {
		t.Errorf("tools = %s", js(sess["tools"]))
	}
}

func TestSessionUpdateVoice(t *testing.T) {
	sess := sessionOf(t, newFake(t, true), voice.Config{Transcribe: true})
	if _, ok := sess["output_modalities"]; ok {
		t.Errorf("voice session sets output_modalities: %s", js(sess))
	}
	want := `{"input":{"format":{"rate":24000,"type":"audio/pcm"},"noise_reduction":{"type":"near_field"},` +
		`"transcription":{"language":"zh","model":"gpt-4o-mini-transcribe"},"turn_detection":{"interrupt_response":true,"type":"semantic_vad"}},` +
		`"output":{"voice":"marin"}}`
	if got := js(sess["audio"]); got != want {
		t.Errorf("audio =\n%s\nwant\n%s", got, want)
	}
}

func TestDialReadyTimeout(t *testing.T) {
	old := readyTimeout
	readyTimeout = 200 * time.Millisecond
	defer func() { readyTimeout = old }()
	f := newFake(t, false)
	_, err := Dial(context.Background(), voice.Config{Key: "k", Model: "m", Extra: map[string]string{"url": f.url()}})
	if err == nil || !strings.Contains(err.Error(), "session.updated") {
		t.Fatalf("err = %v", err)
	}
}

const twoCalls = `{"type":"response.output_item.done","response_id":"r1","item":{"id":"fc1","type":"function_call","call_id":"c1","name":"a","arguments":"{}"}}
{"type":"response.output_item.done","response_id":"r1","item":{"id":"fc2","type":"function_call","call_id":"c2","name":"b","arguments":"{\"x\":1}"}}
{"type":"response.done","response":{"id":"r1","status":"completed"}}`

func pushLines(t *testing.T, f *fake, lines string) {
	for _, l := range strings.Split(lines, "\n") {
		f.push(t, l)
	}
}

func TestToolBatchOneCreate(t *testing.T) {
	f := newFake(t, true)
	var both sync.WaitGroup
	both.Add(2)
	call := func(ctx context.Context, name, args string) string {
		both.Done()
		both.Wait() // only returns if both calls run at the same time
		return `{"tool":"` + name + `"}`
	}
	f.dial(t, voice.Config{TextOnly: true, Call: call})
	pushLines(t, f, twoCalls)
	outs := map[string]string{}
	for range 2 {
		ev := f.next(t)
		item, _ := ev["item"].(map[string]any)
		if ev["type"] != "conversation.item.create" || item["type"] != "function_call_output" {
			t.Fatalf("want function_call_output first, got %s", js(ev))
		}
		outs[item["call_id"].(string)] = item["output"].(string)
	}
	if outs["c1"] != `{"tool":"a"}` || outs["c2"] != `{"tool":"b"}` {
		t.Errorf("outputs = %v", outs)
	}
	if ev := f.next(t); ev["type"] != "response.create" {
		t.Fatalf("want response.create, got %s", js(ev))
	}
	f.quiet(t)
}

func TestToolTimeout(t *testing.T) {
	old := toolTimeout
	toolTimeout = 100 * time.Millisecond
	defer func() { toolTimeout = old }()
	f := newFake(t, true)
	block := make(chan struct{})
	defer close(block)
	f.dial(t, voice.Config{TextOnly: true, Call: func(ctx context.Context, _, _ string) string { <-block; return "late" }})
	f.push(t, `{"type":"response.output_item.done","response_id":"r1","item":{"id":"fc1","type":"function_call","call_id":"c1","name":"a","arguments":"{}"}}`)
	f.push(t, `{"type":"response.done","response":{"id":"r1"}}`)
	ev := f.next(t)
	if item, _ := ev["item"].(map[string]any); item["output"] != `{"error":"工具超时"}` {
		t.Errorf("want timeout output, got %s", js(ev))
	}
	if ev := f.next(t); ev["type"] != "response.create" {
		t.Fatalf("want response.create, got %s", js(ev))
	}
}

func runRound(t *testing.T, keep bool) (*fake, []string) {
	f := newFake(t, true)
	f.dial(t, voice.Config{TextOnly: true, Extra: map[string]string{"keep_tool_outputs": strconv.FormatBool(keep)}, Call: func(context.Context, string, string) string { return "{}" }})
	pushLines(t, f, twoCalls)
	var outIDs []string
	for range 2 {
		ev := f.next(t)
		item, _ := ev["item"].(map[string]any)
		if ev["type"] != "conversation.item.create" || item["type"] != "function_call_output" {
			t.Fatalf("want function_call_output, got %s", js(ev))
		}
		outIDs = append(outIDs, item["id"].(string))
	}
	if ev := f.next(t); ev["type"] != "response.create" {
		t.Fatalf("want response.create, got %s", js(ev))
	}
	f.push(t, `{"type":"response.done","response":{"id":"r2","status":"completed"}}`)
	return f, outIDs
}

func TestPruneAfterFinalAnswer(t *testing.T) {
	f, outIDs := runRound(t, false)
	got := map[string]bool{}
	for range 4 {
		ev := f.next(t)
		if ev["type"] != "conversation.item.delete" {
			t.Fatalf("want delete, got %s", js(ev))
		}
		got[ev["item_id"].(string)] = true
	}
	for _, id := range append([]string{"fc1", "fc2"}, outIDs...) {
		if !got[id] {
			t.Errorf("item %s not deleted; deleted %v", id, got)
		}
	}
	f.quiet(t)
}

func TestKeepToolOutputs(t *testing.T) {
	f, _ := runRound(t, true)
	f.quiet(t)
}

func TestSpeechStartedAndErrors(t *testing.T) {
	f := newFake(t, true)
	speech := make(chan struct{}, 1)
	errs := make(chan error, 3)
	s := f.dial(t, voice.Config{Handlers: voice.Handlers{SpeechStarted: func() { speech <- struct{}{} }, Error: func(e error) { errs <- e }}})
	f.push(t, `{"type":"input_audio_buffer.speech_started"}`)
	f.push(t, `{"type":"error","error":{"code":"conversation_already_has_active_response","message":"busy"}}`)
	f.push(t, `{"type":"error","error":{"code":"x","message":"gone","event_id":"rv_del_3"}}`)
	f.push(t, `{"type":"error","error":{"code":"invalid_value","message":"bad"}}`)
	select {
	case <-speech:
	case <-time.After(3 * time.Second):
		t.Fatal("SpeechStarted not called")
	}
	if e := <-errs; !strings.Contains(e.Error(), "invalid_value") {
		t.Errorf("first reported error = %v (benign ones must be ignored)", e)
	}
	f.ws.Close()
	select {
	case e := <-errs:
		if !strings.Contains(e.Error(), "read") {
			t.Errorf("read error = %v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read error not reported")
	}
	<-s.Done()
}

func TestUsage(t *testing.T) {
	f := newFake(t, true)
	var log bytes.Buffer
	got := make(chan voice.Usage, 1)
	f.dial(t, voice.Config{TextOnly: true, UsageLog: &log, Handlers: voice.Handlers{ResponseDone: func(u voice.Usage) { got <- u }}})
	f.push(t, `{"type":"response.done","response":{"id":"r1","usage":{"input_tokens":1000,"output_tokens":200,`+
		`"input_token_details":{"cached_tokens":400,"text_tokens":1000,"audio_tokens":0,"cached_tokens_details":{"text_tokens":400,"audio_tokens":0}},`+
		`"output_token_details":{"text_tokens":200,"audio_tokens":0}}}}`)
	var u voice.Usage
	select {
	case u = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("ResponseDone not called")
	}
	want := voice.Usage{Input: 1000, Cached: 400, TextIn: 1000, Output: 200, TextOut: 200, USD: u.USD}
	if u != want {
		t.Errorf("usage = %+v", u)
	}
	// (600*4 + 400*0.4 + 200*16) / 1e6
	if d := u.USD - 0.00576; d > 1e-12 || d < -1e-12 {
		t.Errorf("usd = %v", u.USD)
	}
	line := log.String() // written before ResponseDone, so the channel receive orders it
	var l usageLine
	if err := json.Unmarshal([]byte(line), &l); err != nil || !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("usage log %q: %v", line, err)
	}
	if l.Model != "gpt-realtime-2.1" || l.Input != 1000 || l.Cached != 400 || l.Output != 200 || l.USD != u.USD || l.Unpriced {
		t.Errorf("usage line = %+v", l)
	}
}

// x/net/websocket answers a server ping from inside Receive, so a write deadline left over from the
// last send must not outlive that send; otherwise the first ping after a quiet spell kills the session.
func TestPingAfterQuietSpell(t *testing.T) {
	old := writeTimeout
	writeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { writeTimeout = old })
	f := newFake(t, true)
	errs := make(chan error, 1)
	s := f.dial(t, voice.Config{Handlers: voice.Handlers{Error: func(e error) { errs <- e }}})
	if err := s.AppendAudio([]byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	f.next(t)
	time.Sleep(150 * time.Millisecond) // past the deadline of that send
	f.ws.PayloadType = websocket.PingFrame
	if _, err := f.ws.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	f.ws.PayloadType = websocket.TextFrame
	f.push(t, `{"type":"input_audio_buffer.speech_started"}`)
	select {
	case err := <-errs:
		t.Fatalf("session died on a ping: %v", err)
	case <-s.Done():
		t.Fatal("session closed on a ping")
	case <-time.After(300 * time.Millisecond):
	}
}
