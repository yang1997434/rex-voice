package doubao

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
)

// fake is a scripted Doubao Realtime 3.0 server: it records every client event and lets the test push server events.
type fake struct {
	srv      *httptest.Server
	got      chan map[string]any
	conn     chan *websocket.Conn
	header   http.Header
	ws       *websocket.Conn
	create   string // reply to session.create; "" = none
	ackClose bool   // reply session.closed to session.close
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{got: make(chan map[string]any, 200), conn: make(chan *websocket.Conn, 1),
		create: `{"type":"session.created","session":{"id":"sess_1"}}`, ackClose: true}
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
				switch {
				case ev["type"] == "session.create" && f.create != "":
					websocket.Message.Send(ws, f.create)
				case ev["type"] == "session.close" && f.ackClose:
					websocket.Message.Send(ws, `{"type":"session.closed"}`)
				}
				f.got <- ev
			}
		},
	}
	f.srv = httptest.NewServer(h)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) cfg(cfg voice.Config) voice.Config {
	cfg.Extra = map[string]string{"url": "ws" + strings.TrimPrefix(f.srv.URL, "http")}
	cfg.Key, cfg.Model = "test-key", "1.2.6.1"
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
	if ev := f.next(t); ev["type"] != "session.create" {
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

func set[T any](t *testing.T, p *T, v T) {
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func wait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for " + what)
	}
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestSessionCreate(t *testing.T) {
	f := newFake(t)
	cfg := f.cfg(voice.Config{Instructions: "be brief", Tools: []tools.ToolDef{{Type: "function", Name: "list_panes",
		Description: "d", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}})
	s, err := Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ev := f.next(t)
	if ev["type"] != "session.create" {
		t.Fatalf("got %v", ev["type"])
	}
	if got := f.header.Get("X-Api-Key"); got != "test-key" {
		t.Errorf("X-Api-Key = %q", got)
	}
	if got := f.header.Get("User-Agent"); got != "rex-voiced/dev" {
		t.Errorf("User-Agent = %q", got)
	}
	sess := ev["session"].(map[string]any)
	if sess["model"] != "1.2.6.1" || sess["instructions"] != "be brief" {
		t.Errorf("session = %v", js(sess))
	}
	audio := js(sess["audio"])
	want := `{"input":{"format":{"rate":16000,"type":"pcm"}},"output":{"format":{"rate":24000,"type":"pcm_s16le"},"voice":"zh_female_vv_uranus_bigtts"}}`
	if audio != want {
		t.Errorf("audio = %s\nwant    %s", audio, want)
	}
	if got := js(sess["tools"]); !strings.Contains(got, `"name":"list_panes"`) || strings.Contains(got, `"function":{`) {
		t.Errorf("tools = %s", got)
	}
	if got := js(ev["extension"]); got != `{"asr":{},"dialog":{},"tts":{}}` {
		t.Errorf("extension = %s", got)
	}
	if id := s.(*Session).ID(); id != "sess_1" {
		t.Errorf("session id = %q", id)
	}
	if s.MaxAge() != 0 {
		t.Errorf("MaxAge = %v", s.MaxAge())
	}
}

func TestDialFails(t *testing.T) {
	t.Run("rejected", func(t *testing.T) {
		f := newFake(t)
		f.create = `{"type":"error","error":{"type":"invalid_request_error","code":"45000001","message":"InvalidSpeaker"}}`
		_, err := Dial(context.Background(), f.cfg(voice.Config{}))
		if err == nil || !strings.Contains(err.Error(), "45000001: InvalidSpeaker") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no session.created", func(t *testing.T) {
		set(t, &readyTimeout, 200*time.Millisecond)
		f := newFake(t)
		f.create = ""
		_, err := Dial(context.Background(), f.cfg(voice.Config{}))
		if err == nil || !strings.Contains(err.Error(), "waiting for session.created") {
			t.Fatalf("err = %v", err)
		}
	})
}

// 20 ms of 24 kHz goes up as 20 ms of 16 kHz, one append per frame, nothing batched.
func TestUplinkIs16k(t *testing.T) {
	f := newFake(t)
	s := f.dial(t, voice.Config{})
	frame := make([]byte, 960) // 20 ms PCM16 24 kHz
	total := 0
	for range 5 {
		if err := s.AppendAudio(frame); err != nil {
			t.Fatal(err)
		}
		ev := f.next(t)
		if ev["type"] != "input_audio_buffer.append" {
			t.Fatalf("got %v", ev["type"])
		}
		b, err := base64.StdEncoding.DecodeString(ev["audio"].(string))
		if err != nil {
			t.Fatal(err)
		}
		total += len(b)
	}
	// 5 x 640 bytes at 16 kHz, minus the resampler's 16-input-sample lag (about 11 output samples).
	if total > 5*640 || total < 5*640-24 {
		t.Errorf("sent %d bytes of 16 kHz for 100 ms", total)
	}
}

func TestMuteDedupe(t *testing.T) {
	f := newFake(t)
	s := f.dial(t, voice.Config{})
	s.SetMuted(true)
	s.SetMuted(true)
	if ev := f.next(t); ev["type"] != "input_audio_mute.commit" {
		t.Fatalf("got %v", ev["type"])
	}
	s.AppendAudio(make([]byte, 960)) // silence fill while muted is not sent
	f.quiet(t)
	s.SetMuted(false)
	s.SetMuted(false)
	if ev := f.next(t); ev["type"] != "input_audio_unmute.commit" {
		t.Fatalf("got %v", ev["type"])
	}
	s.AppendAudio(make([]byte, 960))
	if ev := f.next(t); ev["type"] != "input_audio_buffer.append" {
		t.Fatalf("got %v", ev["type"])
	}
	f.quiet(t)
}

func TestToolsParallelOneReply(t *testing.T) {
	set(t, &toolTimeout, 300*time.Millisecond)
	f := newFake(t)
	var started sync.WaitGroup
	started.Add(2)
	turnDone := make(chan struct{}, 2)
	var dones atomic.Int32
	s := f.dial(t, voice.Config{
		Call: func(ctx context.Context, name, args string) string {
			started.Done()
			started.Wait() // both calls run at once, or this deadlocks
			if name == "slow" {
				<-ctx.Done()
			}
			return name + ":" + args
		},
		Handlers: voice.Handlers{
			ResponseDone: func(voice.Usage) { dones.Add(1) },
			TurnDone:     func() { turnDone <- struct{}{} },
		},
	})
	f.push(t, `{"type":"response.function_call_arguments.done","items":[`+
		`{"call_id":"c1","name":"list_panes","arguments":"{}"},{"call_id":"c2","name":"slow","arguments":"{\"x\":1}"}]}`)
	f.push(t, `{"type":"response.done","response":{}}`) // the tool turn's own done: tools still running, not a turn end
	ev := f.next(t)
	if ev["type"] != "conversation.item.create" {
		t.Fatalf("got %v", ev["type"])
	}
	want := `[{"call_id":"c1","content":[{"text":"list_panes:{}","type":"input_text"}],"role":"tool"},` +
		`{"call_id":"c2","content":[{"text":"{\"error\":\"工具超时\"}","type":"input_text"}],"role":"tool"}]`
	if got := js(ev["items"]); got != want {
		t.Errorf("items = %s\nwant    %s", got, want)
	}
	f.quiet(t) // no response.create: the model continues by itself
	select {
	case <-turnDone:
		t.Fatal("TurnDone while tools ran")
	default:
	}
	f.push(t, `{"type":"response.done","response":{}}`)
	wait(t, turnDone, "TurnDone")
	if dones.Load() != 2 {
		t.Errorf("ResponseDone %d times", dones.Load())
	}
	_ = s
}

func TestBargeIn(t *testing.T) {
	f := newFake(t)
	var audio bytes.Buffer
	var mu sync.Mutex
	speech := make(chan struct{}, 4)
	s := f.dial(t, voice.Config{Handlers: voice.Handlers{
		AudioDelta:    func(p []byte) { mu.Lock(); audio.Write(p); mu.Unlock() },
		SpeechStarted: func() { speech <- struct{}{} },
	}})
	delta := func(b string) string {
		return `{"type":"response.output_audio.delta","delta":"` + base64.StdEncoding.EncodeToString([]byte(b)) + `"}`
	}
	f.push(t, `{"type":"response.output_audio.started"}`)
	f.push(t, delta("a1"))
	f.push(t, `{"type":"conversation.item.input_audio_transcription.started"}`)
	if ev := f.next(t); ev["type"] != "response.cancel" {
		t.Fatalf("got %v", ev["type"])
	}
	wait(t, speech, "SpeechStarted")
	f.push(t, delta("xx")) // late audio of the cancelled answer
	// Tolerated: it answers our cancel.
	f.push(t, `{"type":"error","error":{"code":"45000002","message":"no active response"}}`)
	f.push(t, `{"type":"response.canceled"}`)
	f.push(t, `{"type":"response.output_audio.started"}`)
	f.push(t, delta("b2"))
	f.push(t, `{"type":"response.done","response":{}}`) // server ended it itself
	f.push(t, `{"type":"conversation.item.input_audio_transcription.started"}`)
	wait(t, speech, "second SpeechStarted")
	f.quiet(t) // nothing playing: no second cancel
	mu.Lock()
	got := audio.String()
	mu.Unlock()
	if got != "a1b2" {
		t.Errorf("audio = %q", got)
	}
	select {
	case <-s.Done():
		t.Fatal("benign error ended the session")
	default:
	}
}

func TestTranscriptsAndUsage(t *testing.T) {
	f := newFake(t)
	var log bytes.Buffer
	var mu sync.Mutex
	var user, assistant []string
	var usages []voice.Usage
	turn := make(chan struct{}, 1)
	s := f.dial(t, voice.Config{UsageLog: &log, Handlers: voice.Handlers{
		UserText:      func(t string) { mu.Lock(); user = append(user, t); mu.Unlock() },
		AssistantText: func(t string) { mu.Lock(); assistant = append(assistant, t); mu.Unlock() },
		ResponseDone:  func(u voice.Usage) { mu.Lock(); usages = append(usages, u); mu.Unlock() },
		TurnDone:      func() { turn <- struct{}{} },
	}})
	f.push(t, `{"type":"conversation.item.input_audio_transcription.completed","text":"现在几点"}`)
	f.push(t, `{"type":"response.output_text.delta","delta":"三"}`)
	f.push(t, `{"type":"response.output_text.done","text":"三点"}`)
	f.push(t, `{"type":"response.done","response":{"usage":{"total_tokens":300,"input_tokens":200,"output_tokens":100,`+
		`"input_token_details":{"cached_tokens":0,"audio_tokens":150,"text_tokens":50},"output_token_details":{"audio_tokens":90,"text_tokens":10}}}}`)
	wait(t, turn, "TurnDone")
	f.push(t, `{"type":"response.done","response":{"usage":{"total_tokens":3,"input_tokens":2,"output_tokens":1}}}`)
	wait(t, turn, "second TurnDone")
	mu.Lock()
	defer mu.Unlock()
	if js(user) != `["现在几点"]` || js(assistant) != `["三点"]` {
		t.Errorf("user %v assistant %v", user, assistant)
	}
	if len(usages) != 2 || usages[0].Unpriced || usages[0].USD <= 0 || !usages[1].Unpriced || usages[1].Input != 2 {
		t.Errorf("usages = %+v", usages)
	}
	if got := js(s.RecentTurns(5)); got != `[{"Role":"user","Text":"现在几点"},{"Role":"assistant","Text":"三点"}]` {
		t.Errorf("turns = %s", got)
	}
	if lines := strings.Count(log.String(), "\n"); lines != 2 || !strings.Contains(log.String(), `"model":"doubao/1.2.6.1"`) {
		t.Errorf("usage log = %s", log.String())
	}
}

func TestSendTextUnsupported(t *testing.T) {
	f := newFake(t)
	s := f.dial(t, voice.Config{})
	if err := s.SendText("hi"); err != ErrNoTextInput {
		t.Fatalf("err = %v", err)
	}
	f.quiet(t)
}

func TestCloseHandshake(t *testing.T) {
	t.Run("acked", func(t *testing.T) {
		f := newFake(t)
		s := f.dial(t, voice.Config{})
		t0 := time.Now()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if el := time.Since(t0); el > time.Second {
			t.Errorf("Close took %v", el)
		}
		if ev := f.next(t); ev["type"] != "session.close" {
			t.Fatalf("got %v", ev["type"])
		}
		wait(t, s.Done(), "Done")
		if err := s.AppendAudio(make([]byte, 960)); err == nil {
			t.Error("AppendAudio after Close")
		}
	})
	t.Run("no ack", func(t *testing.T) {
		set(t, &closeTimeout, 300*time.Millisecond)
		f := newFake(t)
		f.ackClose = false
		s := f.dial(t, voice.Config{})
		t0 := time.Now()
		s.Close()
		if el := time.Since(t0); el < 250*time.Millisecond {
			t.Errorf("Close did not wait for session.closed: %v", el)
		}
		wait(t, s.Done(), "Done")
	})
}

func TestErrorEndsSession(t *testing.T) {
	f := newFake(t)
	errs := make(chan error, 2)
	s := f.dial(t, voice.Config{Handlers: voice.Handlers{Error: func(err error) { errs <- err }}})
	f.push(t, `{"type":"error","event_id":"e1","error":{"type":"server_error","code":"55000001","message":"boom"}}`)
	wait(t, s.Done(), "Done")
	select {
	case err := <-errs:
		if !strings.Contains(err.Error(), "55000001: boom") {
			t.Errorf("err = %v", err)
		}
	default:
		t.Fatal("no Error handler call")
	}
	if len(errs) != 0 {
		t.Errorf("extra errors: %v", <-errs)
	}
}
