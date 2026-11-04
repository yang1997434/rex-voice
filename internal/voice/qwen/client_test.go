package qwen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
	"slices"
	"sort"
)

const testModel = "qwen3.8-omni-flash-realtime"

// fake is a scripted Qwen-Omni Realtime server: it records every client event and lets the test push server events.
type fake struct {
	srv    *httptest.Server
	got    chan map[string]any
	conn   chan *websocket.Conn
	header http.Header
	query  string
	ws     *websocket.Conn
}

// newFake answers session.update with ready (a session.updated or an error event), or not at all when ready is "".
func newFake(t *testing.T, ready string) *fake {
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
				if ev["type"] == "session.update" && ready != "" {
					websocket.Message.Send(ws, ready)
				}
				f.got <- ev
			}
		},
	}
	f.srv = httptest.NewServer(h)
	t.Cleanup(f.srv.Close)
	return f
}

const updated = `{"type":"session.updated","session":{}}`

func (f *fake) cfg(cfg voice.Config) voice.Config {
	cfg.Extra = map[string]string{"url": "ws" + strings.TrimPrefix(f.srv.URL, "http")}
	cfg.Key = "test-key"
	if cfg.Model == "" {
		cfg.Model = testModel
	}
	return cfg
}

func (f *fake) dial(t *testing.T, cfg voice.Config) *Session {
	t.Helper()
	s, err := dial(context.Background(), f.cfg(cfg))
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
		t.Fatalf("unexpected client event %v", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

func wait[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatalf("%s not called", what)
		var zero T
		return zero
	}
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

var testTools = []tools.ToolDef{{Type: "function", Name: "list_panes", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}

// sessionUpdate dials and returns the session object of the client's session.update.
func sessionUpdate(t *testing.T, cfg voice.Config) map[string]any {
	t.Helper()
	f := newFake(t, updated)
	s, err := Dial(context.Background(), f.cfg(cfg))
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
	if want := "model=" + cfg.Model; cfg.Model != "" && f.query != want || cfg.Model == "" && f.query != "model="+testModel {
		t.Errorf("query = %q", f.query)
	}
	return ev["session"].(map[string]any)
}

func TestSessionUpdateTextOnly(t *testing.T) {
	sess := sessionUpdate(t, voice.Config{TextOnly: true, Instructions: "hi", Tools: testTools})
	if sess["instructions"] != "hi" || js(sess["modalities"]) != `["text"]` {
		t.Errorf("session = %s", js(sess))
	}
	// The voice is required even in text mode: the server default is rejected.
	if got := js(sess["audio"]); got != `{"input":{"format":{"sample_rate":16000,"type":"pcm"}},"output":{"voice":"Tina"}}` {
		t.Errorf("audio = %s", got)
	}
	if js(sess["tools"]) != `[{"description":"d","name":"list_panes","parameters":{"type":"object"},"type":"function"}]` {
		t.Errorf("tools = %s", js(sess["tools"]))
	}
	if _, ok := sess["turn_detection"]; ok {
		t.Errorf("text-only session sets turn_detection: %s", js(sess))
	}
}

func TestSessionUpdateVoice(t *testing.T) {
	sess := sessionUpdate(t, voice.Config{Transcribe: true, Voice: "Cici"})
	if _, ok := sess["modalities"]; ok {
		t.Errorf("voice session sets modalities: %s", js(sess))
	}
	if _, ok := sess["tools"]; ok {
		t.Errorf("tools sent without tools: %s", js(sess))
	}
	if got := js(sess["turn_detection"]); got != `{"type":"semantic_vad"}` {
		t.Errorf("turn_detection = %s", got)
	}
	if got := js(sess["input_audio_transcription"]); got != `{"model":"qwen3-asr-flash-realtime"}` {
		t.Errorf("input_audio_transcription = %s", got)
	}
	if got := turnDetection("qwen-audio-3.1-realtime-plus"); got != "smart_turn_v2" {
		t.Errorf("audio-3.1 turn_detection = %s", got)
	}
	if got := js(sessionUpdate(t, voice.Config{Model: "qwen-audio-3.1-realtime-plus", Voice: "Tina"})["audio"]); !strings.Contains(got, `"voice":"longanqian"`) {
		t.Errorf("audio-3.1 default voice: %s", got)
	}
	if got := js(sess["audio"]); !strings.Contains(got, `"voice":"Cici"`) {
		t.Errorf("audio = %s", got)
	}
	sess = sessionUpdate(t, voice.Config{})
	if v, ok := sess["input_audio_transcription"]; !ok || v != nil {
		t.Errorf("Transcribe off must send null, got %s (present %v)", js(v), ok)
	}
}

func TestDialFails(t *testing.T) {
	old := readyTimeout
	readyTimeout = 200 * time.Millisecond
	defer func() { readyTimeout = old }()
	if _, err := Dial(context.Background(), newFake(t, "").cfg(voice.Config{})); err == nil || !strings.Contains(err.Error(), "session.updated") {
		t.Errorf("no session.updated: err = %v", err)
	}
	rejected := newFake(t, `{"type":"error","error":{"code":"COMMON_ERROR","message":"Voice 'x' is not supported."}}`)
	if _, err := Dial(context.Background(), rejected.cfg(voice.Config{})); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("rejected: err = %v", err)
	}
	if _, err := Dial(context.Background(), voice.Config{Key: "k", Model: "m", Extra: map[string]string{"workspace": "evil.example.com/"}}); err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Errorf("bad workspace: err = %v", err)
	}
}

// Qwen 3.8 only takes 16 kHz: every 24 kHz frame goes out resampled, 2/3 of its bytes.
func TestAppendAudioIs16k(t *testing.T) {
	f := newFake(t, updated)
	s := f.dial(t, voice.Config{})
	const frames, frame = 10, 960 // 20 ms at 24 kHz
	for range frames {
		if err := s.AppendAudio(make([]byte, frame)); err != nil {
			t.Fatal(err)
		}
	}
	var sent int
	for range frames {
		ev := f.next(t)
		if ev["type"] != "input_audio_buffer.append" {
			t.Fatalf("got %s", js(ev))
		}
		b, err := base64.StdEncoding.DecodeString(ev["audio"].(string))
		if err != nil {
			t.Fatal(err)
		}
		sent += len(b)
	}
	// The Downsampler holds back firHalf (16) output samples of look-ahead.
	if want := frames * frame * 2 / 3; sent > want || sent < want-64 {
		t.Errorf("sent %d bytes for %d in, want about %d", sent, frames*frame, want)
	}
	if err := s.AppendAudio([]byte{1}); err == nil {
		t.Error("odd byte count accepted")
	}
}

func TestSpeechStartedCancelsAndDropsLateAudio(t *testing.T) {
	f := newFake(t, updated)
	audio := make(chan []byte, 10)
	speech := make(chan struct{}, 1)
	f.dial(t, voice.Config{Handlers: voice.Handlers{AudioDelta: func(p []byte) { audio <- p }, SpeechStarted: func() { speech <- struct{}{} }}})
	pcm := func(b byte) string { return base64.StdEncoding.EncodeToString([]byte{b, b}) }
	f.push(t, `{"type":"response.created","response":{"id":"r1"}}`,
		`{"type":"response.audio.delta","response_id":"r1","delta":"`+pcm(1)+`"}`,
		`{"type":"input_audio_buffer.speech_started"}`)
	if got := wait(t, audio, "AudioDelta"); got[0] != 1 {
		t.Errorf("audio = %v", got)
	}
	wait(t, speech, "SpeechStarted")
	if ev := f.next(t); ev["type"] != "response.cancel" {
		t.Fatalf("want response.cancel, got %s", js(ev))
	}
	f.push(t, `{"type":"response.audio.delta","response_id":"r1","delta":"`+pcm(2)+`"}`,
		`{"type":"response.created","response":{"id":"r2"}}`,
		`{"type":"response.audio.delta","response_id":"r2","delta":"`+pcm(3)+`"}`)
	if got := wait(t, audio, "AudioDelta"); got[0] != 3 {
		t.Errorf("late audio of the interrupted answer was played: %v", got)
	}
}

func TestSpeechStartedWithoutAnswerSendsNoCancel(t *testing.T) {
	f := newFake(t, updated)
	speech := make(chan struct{}, 1)
	f.dial(t, voice.Config{Handlers: voice.Handlers{SpeechStarted: func() { speech <- struct{}{} }}})
	f.push(t, `{"type":"input_audio_buffer.speech_started"}`)
	wait(t, speech, "SpeechStarted")
	f.quiet(t) // audio-3.1 errors on a cancel with nothing active
}

func TestToolBatchOneCreate(t *testing.T) {
	f := newFake(t, updated)
	var both sync.WaitGroup
	both.Add(2)
	call := func(ctx context.Context, name, args string) string {
		both.Done()
		both.Wait() // only returns if both calls run at the same time
		return `{"tool":"` + name + args + `"}`
	}
	turn := make(chan struct{}, 2)
	f.dial(t, voice.Config{TextOnly: true, Call: call, Handlers: voice.Handlers{TurnDone: func() { turn <- struct{}{} }}})
	f.push(t, `{"type":"response.function_call_arguments.done","response_id":"r1","call_id":"c1","name":"a","arguments":"{}"}`,
		`{"type":"response.function_call_arguments.done","response_id":"r1","call_id":"c2","name":"b","arguments":"{\"x\":1}"}`,
		`{"type":"response.done","response":{"id":"r1","status":"completed"}}`)
	outs := map[string]string{}
	for range 2 {
		ev := f.next(t)
		item, _ := ev["item"].(map[string]any)
		if ev["type"] != "conversation.item.create" || item["type"] != "function_call_output" {
			t.Fatalf("want function_call_output first, got %s", js(ev))
		}
		outs[item["call_id"].(string)] = item["output"].(string)
	}
	if outs["c1"] != `{"tool":"a{}"}` || outs["c2"] != `{"tool":"b{"x":1}"}` {
		t.Errorf("outputs = %v", outs)
	}
	if ev := f.next(t); ev["type"] != "response.create" {
		t.Fatalf("want response.create, got %s", js(ev))
	}
	f.quiet(t)
	select {
	case <-turn:
		t.Fatal("TurnDone before the answer to the tool results")
	default:
	}
	f.push(t, `{"type":"response.done","response":{"id":"r2","status":"completed"}}`)
	wait(t, turn, "TurnDone")
}

// 11-03 真机（qwen3.8）：arguments.done 带截断的 JSON、或者干脆没有 arguments.done，只有 response.done 的
// output 里有 function_call；两种都要照 response.done 里的完整参数调一次、不多调。
func TestToolFromResponseDoneOutput(t *testing.T) {
	f := newFake(t, updated)
	var mu sync.Mutex
	var got []string
	call := func(ctx context.Context, name, args string) string {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, name+" "+args)
		return "ok"
	}
	f.dial(t, voice.Config{TextOnly: true, Call: call})
	f.push(t, `{"type":"response.function_call_arguments.done","response_id":"r1","call_id":"c1","name":"read_claude","arguments":"{\"block\": \"block:01a1"}`,
		`{"type":"response.done","response":{"id":"r1","status":"completed","output":[`+
			`{"type":"message","id":"m1"},`+
			`{"type":"function_call","call_id":"c1","name":"read_claude","arguments":"{\"block\":\"block:01a19b75\"}"},`+
			`{"type":"function_call","call_id":"c2","name":"close_pane","arguments":"{\"block\":\"block:x\"}"}]}}`)
	outs := map[string]bool{}
	for range 2 {
		ev := f.next(t)
		item, _ := ev["item"].(map[string]any)
		if ev["type"] != "conversation.item.create" || item["type"] != "function_call_output" {
			t.Fatalf("want function_call_output, got %s", js(ev))
		}
		outs[item["call_id"].(string)] = true
	}
	if ev := f.next(t); ev["type"] != "response.create" {
		t.Fatalf("want response.create, got %s", js(ev))
	}
	mu.Lock()
	defer mu.Unlock()
	sort.Strings(got)
	want := []string{`close_pane {"block":"block:x"}`, `read_claude {"block":"block:01a19b75"}`}
	if !outs["c1"] || !outs["c2"] || !slices.Equal(got, want) {
		t.Fatalf("calls = %q outs = %v", got, outs)
	}
}

// 11-02 实测：工具结果后的 response.create 撞上用户在说话，服务端回 error；会话不能断，等用户说完再发。
func TestCreateRefusedWhileSpeaking(t *testing.T) {
	const refused = `{"type":"error","error":{"type":"invalid_request_error","code":"invalid_value","message":"Cannot create response while user is speaking."}}`
	toolBatch := func(t *testing.T, f *fake) {
		f.push(t, `{"type":"response.function_call_arguments.done","response_id":"r1","call_id":"c1","name":"a","arguments":"{}"}`,
			`{"type":"response.done","response":{"id":"r1","status":"completed"}}`)
		f.next(t) // function_call_output
		if ev := f.next(t); ev["type"] != "response.create" {
			t.Fatalf("want response.create, got %s", js(ev))
		}
		f.push(t, refused)
	}
	call := func(context.Context, string, string) string { return "{}" }
	t.Run("resend on speech_stopped", func(t *testing.T) {
		f := newFake(t, updated)
		errs := make(chan error, 1)
		s := f.dial(t, voice.Config{Call: call, Handlers: voice.Handlers{Error: func(e error) { errs <- e }}})
		toolBatch(t, f)
		f.quiet(t)
		select {
		case <-s.Done():
			t.Fatal("session ended on a create refused while the user speaks")
		case e := <-errs:
			t.Fatalf("benign error reported: %v", e)
		default:
		}
		f.push(t, `{"type":"input_audio_buffer.speech_stopped"}`)
		if ev := f.next(t); ev["type"] != "response.create" {
			t.Fatalf("want response.create after speech_stopped, got %s", js(ev))
		}
		f.push(t, `{"type":"input_audio_buffer.speech_stopped"}`)
		f.quiet(t) // only once
		f.push(t, `{"type":"error","error":{"code":"invalid_request_error","message":"Conversation already has an active response in progress: r9"}}`)
		f.quiet(t)
		select {
		case <-s.Done():
			t.Fatal("session ended on a create racing the server's own answer")
		case e := <-errs:
			t.Fatalf("benign error reported: %v", e)
		default:
		}
	})
	t.Run("server answers first", func(t *testing.T) {
		f := newFake(t, updated)
		f.dial(t, voice.Config{Call: call})
		toolBatch(t, f)
		f.push(t, `{"type":"response.created","response":{"id":"r2"}}`, `{"type":"input_audio_buffer.speech_stopped"}`)
		f.quiet(t)
	})
}

func TestToolTimeout(t *testing.T) {
	old := toolTimeout
	toolTimeout = 100 * time.Millisecond
	defer func() { toolTimeout = old }()
	f := newFake(t, updated)
	block := make(chan struct{})
	defer close(block)
	f.dial(t, voice.Config{TextOnly: true, Call: func(ctx context.Context, _, _ string) string { <-block; return "late" }})
	f.push(t, `{"type":"response.function_call_arguments.done","response_id":"r1","call_id":"c1","name":"a","arguments":"{}"}`,
		`{"type":"response.done","response":{"id":"r1"}}`)
	if item, _ := f.next(t)["item"].(map[string]any); item["output"] != timeoutOutput {
		t.Errorf("want timeout output, got %v", item)
	}
	if ev := f.next(t); ev["type"] != "response.create" {
		t.Fatalf("want response.create, got %s", js(ev))
	}
}

func TestTextTurn(t *testing.T) {
	f := newFake(t, updated)
	var deltas []string
	final := make(chan string, 1)
	turn := make(chan struct{}, 1)
	s := f.dial(t, voice.Config{TextOnly: true, Handlers: voice.Handlers{
		TextDelta: func(d string) { deltas = append(deltas, d) }, AssistantText: func(x string) { final <- x }, TurnDone: func() { turn <- struct{}{} }}})
	if err := s.SendText("你好"); err != nil {
		t.Fatal(err)
	}
	if ev := f.next(t); js(ev["item"]) != `{"content":[{"text":"你好","type":"input_text"}],"role":"user","type":"message"}` {
		t.Errorf("SendText item = %s", js(ev))
	}
	if ev := f.next(t); ev["type"] != "response.create" {
		t.Errorf("want response.create, got %s", js(ev))
	}
	f.push(t, `{"type":"response.text.delta","delta":"我是"}`, `{"type":"response.text.delta","delta":"千问"}`,
		`{"type":"response.text.done","text":"我是千问"}`, `{"type":"response.done","response":{"id":"r1","status":"completed"}}`)
	if got := wait(t, final, "AssistantText"); got != "我是千问" {
		t.Errorf("AssistantText = %q", got)
	}
	wait(t, turn, "TurnDone")
	if strings.Join(deltas, "|") != "我是|千问" {
		t.Errorf("deltas = %v", deltas)
	}
	if got := s.RecentTurns(5); js(got) != `[{"Role":"user","Text":"你好"},{"Role":"assistant","Text":"我是千问"}]` {
		t.Errorf("turns = %s", js(got))
	}
}

func TestErrorEndsSession(t *testing.T) {
	f := newFake(t, updated)
	errs := make(chan error, 3)
	s := f.dial(t, voice.Config{Handlers: voice.Handlers{Error: func(e error) { errs <- e }}})
	f.push(t, `{"type":"error","error":{"type":"invalid_request_error","message":"Cannot cancel response before response.created","param":"response"}}`,
		`{"type":"error","error":{"type":"invalid_value","message":"Conversation has no active response."}}`)
	f.quiet(t)
	select {
	case <-s.Done():
		t.Fatal("a cancel with nothing to cancel ended the session")
	case e := <-errs:
		t.Fatalf("benign cancel error reported: %v", e)
	default:
	}
	f.push(t, `{"type":"error","error":{"code":"COMMON_ERROR","message":"InternalError.Algo.InvalidParameter"}}`)
	if e := wait(t, errs, "Error"); !strings.Contains(e.Error(), "COMMON_ERROR: InternalError.Algo.InvalidParameter") {
		t.Errorf("error = %v", e)
	}
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("session not done after a server error")
	}
	select {
	case e := <-errs:
		t.Errorf("second error reported after the session ended: %v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUsageLine(t *testing.T) {
	f := newFake(t, updated)
	var log bytes.Buffer
	got := make(chan voice.Usage, 1)
	s := f.dial(t, voice.Config{UsageLog: &log, Handlers: voice.Handlers{ResponseDone: func(u voice.Usage) { got <- u }}})
	// Real response.done usage shape, 11-01.
	f.push(t, `{"type":"response.done","response":{"id":"r1","status":"completed","usage":{"total_tokens":804,"input_tokens":693,"output_tokens":111,`+
		`"input_tokens_details":{"text_tokens":693},"output_tokens_details":{"text_tokens":25,"audio_tokens":86}}}}`)
	u := wait(t, got, "ResponseDone")
	if want := (voice.Usage{Input: 693, TextIn: 693, Output: 111, TextOut: 25, AudioOut: 86, USD: u.USD}); u != want || u.USD == 0 {
		t.Errorf("usage = %+v", u)
	}
	// (693*1.5 + 25*4.5 + 86*12) / 7.2 / 1e6, CNY price table
	if c, want := s.Cost(), (693*1.5+25*4.5+86*12)/7.2/1e6; math.Abs(c-want) > 1e-12 || c != u.USD {
		t.Errorf("Cost = %v, want %v", c, want)
	}
	line := log.String() // written before ResponseDone, so the channel receive orders it
	var l usageLine
	if err := json.Unmarshal([]byte(line), &l); err != nil || strings.Count(line, "\n") != 1 {
		t.Fatalf("usage log %q: %v", line, err)
	}
	if l.Model != testModel || l.Input != 693 || l.AudioOut != 86 || l.TextOut != 25 || l.USD != u.USD || l.Unpriced {
		t.Errorf("usage line = %+v", l)
	}
}

// A model without a price entry still reports usage, and Cost stays 0.
func TestCostUnpriced(t *testing.T) {
	f := newFake(t, updated)
	got := make(chan voice.Usage, 1)
	s := f.dial(t, voice.Config{Model: "qwen-nope-realtime", Handlers: voice.Handlers{ResponseDone: func(u voice.Usage) { got <- u }}})
	f.push(t, `{"type":"response.done","response":{"id":"r1","status":"completed","usage":{"input_tokens":693,"output_tokens":111}}}`)
	if u := wait(t, got, "ResponseDone"); !u.Unpriced || u.USD != 0 {
		t.Errorf("usage = %+v", u)
	}
	if c := s.Cost(); c != 0 {
		t.Errorf("Cost = %v, want 0", c)
	}
}

// Without Extra region the international endpoint is the only one dialed (Beijing never, even when
// it is reachable); region "cn" picks Beijing alone.
func TestDialDefaultsToInternationalOnly(t *testing.T) {
	bj := newFake(t, `{"type":"session.updated"}`)
	intl := newFake(t, `{"type":"session.updated"}`)
	saved := endpoints
	endpoints = []string{"ws" + strings.TrimPrefix(bj.srv.URL, "http"), "ws" + strings.TrimPrefix(intl.srv.URL, "http")}
	t.Cleanup(func() { endpoints = saved })
	cfg := intl.cfg(voice.Config{})
	delete(cfg.Extra, "url")
	s, err := dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if bj.header != nil || intl.header == nil {
		t.Fatalf("default dial: beijing reached=%v international reached=%v", bj.header != nil, intl.header != nil)
	}
	cfg.Extra["region"] = "cn"
	if s, err = dial(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if bj.header == nil {
		t.Fatal("region cn did not reach beijing")
	}
	endpoints = []string{"ws://127.0.0.1:1/api-ws/v1/realtime", "ws://127.0.0.1:2/api-ws/v1/realtime"}
	delete(cfg.Extra, "region")
	if _, err := dial(context.Background(), cfg); err == nil {
		t.Fatal("dial succeeded with no reachable endpoint")
	}
}
