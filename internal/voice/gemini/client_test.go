package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
)

const testModel = "gemini-3.8-live"

type conn struct {
	ws    *websocket.Conn
	got   chan map[string]any
	query string
	ua    string
}

// fake is a scripted Gemini Live server. Every connection answers setup with setupComplete
// (unless ready is false) and records the client messages on its own channel.
type fake struct {
	srv   *httptest.Server
	conns chan *conn
	ws    *conn // the latest connection taken by dial / nextConn
}

func newFake(t *testing.T, ready bool) *fake {
	t.Helper()
	f := &fake{conns: make(chan *conn, 4)}
	var mu sync.Mutex
	var query, ua string
	h := websocket.Server{
		Handshake: func(_ *websocket.Config, r *http.Request) error {
			mu.Lock()
			query, ua = r.URL.RawQuery, r.Header.Get("User-Agent")
			mu.Unlock()
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			mu.Lock()
			c := &conn{ws: ws, got: make(chan map[string]any, 100), query: query, ua: ua}
			mu.Unlock()
			f.conns <- c
			for {
				var raw string
				if err := websocket.Message.Receive(ws, &raw); err != nil {
					return
				}
				var m map[string]any
				if err := json.Unmarshal([]byte(raw), &m); err != nil {
					t.Errorf("client sent bad JSON: %s", raw)
					return
				}
				if _, ok := m["setup"]; ok && ready {
					websocket.Message.Send(ws, `{"setupComplete":{}}`)
				}
				c.got <- m
			}
		},
	}
	f.srv = httptest.NewServer(h)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) cfg(cfg voice.Config) voice.Config {
	cfg.Extra = map[string]string{"url": "ws" + strings.TrimPrefix(f.srv.URL, "http")}
	cfg.Key, cfg.Model = "test-key", testModel
	return cfg
}

// nextConn takes the next connection and returns its setup object.
func (f *fake) nextConn(t *testing.T) map[string]any {
	t.Helper()
	select {
	case f.ws = <-f.conns:
	case <-time.After(3 * time.Second):
		t.Fatal("no connection")
	}
	m := f.next(t)
	setup, ok := m["setup"].(map[string]any)
	if !ok {
		t.Fatalf("first message %s", js(m))
	}
	return setup
}

func (f *fake) dial(t *testing.T, cfg voice.Config) (*Session, map[string]any) {
	t.Helper()
	s, err := dial(context.Background(), f.cfg(cfg))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, f.nextConn(t)
}

func (f *fake) push(t *testing.T, msgs ...string) {
	t.Helper()
	for _, m := range msgs {
		if err := websocket.Message.Send(f.ws.ws, m); err != nil {
			t.Fatal(err)
		}
	}
}

// closeWith sends a close frame with code and reason, the way Gemini reports errors.
func (f *fake) closeWith(t *testing.T, code int, reason string) {
	t.Helper()
	w, err := f.ws.ws.NewFrameWriter(websocket.CloseFrame)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(append(binary.BigEndian.AppendUint16(nil, uint16(code)), reason...))
	w.Close()
	f.ws.ws.Close()
}

func (f *fake) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case m := <-f.ws.got:
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a client message")
		return nil
	}
}

// quiet asserts the client sends nothing more for a while.
func (f *fake) quiet(t *testing.T) {
	t.Helper()
	select {
	case m := <-f.ws.got:
		t.Fatalf("unexpected client message %s", js(m))
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

func pcm(b byte) string { return base64.StdEncoding.EncodeToString([]byte{b, b}) }

func audioTurn(b byte) string {
	return `{"serverContent":{"modelTurn":{"parts":[{"inlineData":{"mimeType":"audio/pcm;rate=24000","data":"` + pcm(b) + `"}}]}}}`
}

const turnComplete = `{"serverContent":{"turnComplete":true}}`

var testTools = []tools.ToolDef{{Type: "function", Name: "list_panes", Description: "d",
	Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}

func TestSetup(t *testing.T) {
	f := newFake(t, true)
	_, setup := f.dial(t, voice.Config{Instructions: "hi", Tools: testTools, Transcribe: true})
	if f.ws.query != "key=test-key" || f.ws.ua != "rex-voiced/dev" {
		t.Errorf("query %q, User-Agent %q", f.ws.query, f.ws.ua)
	}
	want := map[string]string{
		"model":                    `"models/gemini-3.8-live"`,
		"generationConfig":         `{"responseModalities":["AUDIO"],"speechConfig":{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Kore"}}}}`,
		"systemInstruction":        `{"parts":[{"text":"hi"}]}`,
		"tools":                    `[{"functionDeclarations":[{"behavior":"BLOCKING","description":"d","name":"list_panes","parametersJsonSchema":{"additionalProperties":false,"properties":{},"type":"object"}}]}]`,
		"realtimeInputConfig":      `{"automaticActivityDetection":{"silenceDurationMs":700}}`,
		"inputAudioTranscription":  `{}`,
		"outputAudioTranscription": `{}`,
		"sessionResumption":        `{}`,
		"contextWindowCompression": `{"slidingWindow":{}}`,
	}
	for k, v := range want {
		if got := js(setup[k]); got != v {
			t.Errorf("setup.%s = %s, want %s", k, got, v)
		}
	}
	if len(setup) != len(want) {
		t.Errorf("setup has extra keys: %s", js(setup))
	}
	_, setup = f.dial(t, voice.Config{TextOnly: true, Voice: "Puck"})
	// gemini-3.8-live rejects TEXT (11-01): text mode still asks for AUDIO and reads the output transcription
	if got := js(setup["generationConfig"]); got != `{"responseModalities":["AUDIO"],"speechConfig":{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Puck"}}}}` {
		t.Errorf("text-only generationConfig = %s", got)
	}
	for _, k := range []string{"tools", "inputAudioTranscription", "systemInstruction"} {
		if _, ok := setup[k]; ok {
			t.Errorf("setup.%s sent without a reason: %s", k, js(setup))
		}
	}
}

func TestDialFails(t *testing.T) {
	old := readyTimeout
	readyTimeout = 200 * time.Millisecond
	defer func() { readyTimeout = old }()
	if _, err := Dial(context.Background(), newFake(t, false).cfg(voice.Config{})); err == nil || !strings.Contains(err.Error(), "setupComplete") {
		t.Errorf("no setupComplete: err = %v", err)
	}
	// A rejected setup is a close frame with the reason; the reason must reach the error.
	f := newFake(t, false)
	errc := make(chan error, 1)
	go func() { _, err := Dial(context.Background(), f.cfg(voice.Config{})); errc <- err }()
	f.nextConn(t)
	f.closeWith(t, 1007, `Unknown name "x" at 'setup'`)
	if err := wait(t, errc, "Dial"); err == nil || !strings.Contains(err.Error(), `1007: Unknown name "x"`) {
		t.Errorf("rejected: err = %v", err)
	}
	// A failed handshake: x/net/websocket puts the full URL, key included, into the error.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := voice.Config{Key: "secret-key/+", Model: testModel, Extra: map[string]string{"url": "ws" + strings.TrimPrefix(srv.URL, "http")}}
	_, err := Dial(context.Background(), cfg)
	if err == nil || strings.Contains(err.Error(), "secret-key") || !strings.Contains(err.Error(), "<key>") {
		t.Errorf("handshake failure: err = %v", err)
	}
}

// Every 24 kHz frame goes out as 16 kHz, 2/3 of its bytes; muting sends one audioStreamEnd and drops audio.
func TestAppendAudioAndMute(t *testing.T) {
	f := newFake(t, true)
	s, _ := f.dial(t, voice.Config{})
	const frames, frame = 10, 960 // 20 ms at 24 kHz
	for range frames {
		if err := s.AppendAudio(make([]byte, frame)); err != nil {
			t.Fatal(err)
		}
	}
	var sent int
	for range frames {
		m := f.next(t)
		a, _ := m["realtimeInput"].(map[string]any)["audio"].(map[string]any)
		if a["mimeType"] != "audio/pcm;rate=16000" {
			t.Fatalf("got %s", js(m))
		}
		b, err := base64.StdEncoding.DecodeString(a["data"].(string))
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
	s.SetMuted(true)
	s.SetMuted(true)
	if m := f.next(t); js(m) != `{"realtimeInput":{"audioStreamEnd":true}}` {
		t.Fatalf("want audioStreamEnd, got %s", js(m))
	}
	s.AppendAudio(make([]byte, frame))
	f.quiet(t)
	s.SetMuted(false)
	s.AppendAudio(make([]byte, frame))
	if m := f.next(t); m["realtimeInput"].(map[string]any)["audio"] == nil {
		t.Fatalf("audio not resumed after unmute: %s", js(m))
	}
}

func TestToolBatchOneResponse(t *testing.T) {
	f := newFake(t, true)
	var both sync.WaitGroup
	both.Add(2)
	call := func(ctx context.Context, name, args string) string {
		both.Done()
		both.Wait() // only returns if both calls run at the same time
		return name + args
	}
	turn := make(chan struct{}, 2)
	f.dial(t, voice.Config{Call: call, Handlers: voice.Handlers{TurnDone: func() { turn <- struct{}{} }}})
	f.push(t, `{"toolCall":{"functionCalls":[{"id":"c1","name":"a","args":{}},{"id":"c2","name":"b","args":{"x":1}}]}}`)
	m := f.next(t)
	if got := js(m); got != `{"toolResponse":{"functionResponses":[{"id":"c1","name":"a","response":{"result":"a{}"}},{"id":"c2","name":"b","response":{"result":"b{\"x\":1}"}}]}}` {
		t.Fatalf("toolResponse = %s", got)
	}
	f.quiet(t)
	f.push(t, audioTurn(1), turnComplete)
	wait(t, turn, "TurnDone")
}

func TestToolCallCancellation(t *testing.T) {
	f := newFake(t, true)
	stopped := make(chan string, 1)
	call := func(ctx context.Context, name, _ string) string {
		if name == "slow" {
			<-ctx.Done()
			stopped <- name
			return "late"
		}
		return "ok"
	}
	f.dial(t, voice.Config{Call: call})
	f.push(t, `{"toolCall":{"functionCalls":[{"id":"c1","name":"slow"},{"id":"c2","name":"fast"}]}}`)
	f.quiet(t)
	f.push(t, `{"toolCallCancellation":{"ids":["c1"]}}`)
	if got := wait(t, stopped, "cancel of the slow call"); got != "slow" {
		t.Errorf("stopped %q", got)
	}
	if got := js(f.next(t)); got != `{"toolResponse":{"functionResponses":[{"id":"c2","name":"fast","response":{"result":"ok"}}]}}` {
		t.Errorf("toolResponse = %s", got)
	}
	f.push(t, `{"toolCall":{"functionCalls":[{"id":"c3","name":"slow"}]}}`, `{"toolCallCancellation":{"ids":["c3"]}}`)
	wait(t, stopped, "cancel of c3")
	f.quiet(t) // a batch that was cancelled whole sends nothing
}

func TestInterruptedDropsLateAudio(t *testing.T) {
	f := newFake(t, true)
	audio := make(chan []byte, 10)
	speech := make(chan struct{}, 4)
	f.dial(t, voice.Config{Handlers: voice.Handlers{AudioDelta: func(p []byte) { audio <- p }, SpeechStarted: func() { speech <- struct{}{} }}})
	f.push(t, audioTurn(1), `{"serverContent":{"interrupted":true}}`, audioTurn(2), turnComplete, audioTurn(3))
	if got := wait(t, audio, "AudioDelta"); got[0] != 1 {
		t.Errorf("audio = %v", got)
	}
	wait(t, speech, "SpeechStarted")
	if got := wait(t, audio, "AudioDelta"); got[0] != 3 {
		t.Errorf("late audio of the interrupted answer was played: %v", got)
	}
	// The VAD signal cuts too, and interrupted after it does not report the speech twice.
	f.push(t, `{"voiceActivity":{"type":"ACTIVITY_START"}}`, audioTurn(4), `{"serverContent":{"interrupted":true}}`, turnComplete, audioTurn(5))
	wait(t, speech, "SpeechStarted")
	if got := wait(t, audio, "AudioDelta"); got[0] != 5 {
		t.Errorf("late audio after ACTIVITY_START was played: %v", got)
	}
	select {
	case <-speech:
		t.Error("SpeechStarted twice for one interruption")
	default:
	}
}

func TestGoAwayResumes(t *testing.T) {
	f := newFake(t, true)
	s, _ := f.dial(t, voice.Config{})
	first, oldWS := f.ws, s.ws
	f.push(t, `{"sessionResumptionUpdate":{"newHandle":"h1","resumable":true}}`,
		`{"sessionResumptionUpdate":{"resumable":false}}`, `{"goAway":{"timeLeft":"5s"}}`)
	setup := f.nextConn(t)
	if got := js(setup["sessionResumption"]); got != `{"handle":"h1"}` {
		t.Errorf("resumed setup sessionResumption = %s", got)
	}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		s.wmu.Lock()
		swapped := s.ws != oldWS
		s.wmu.Unlock()
		if swapped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connection not swapped")
		}
	}
	if err := s.SendText("还在吗"); err != nil {
		t.Fatal(err)
	}
	if m := f.next(t); m["clientContent"] == nil {
		t.Errorf("text went elsewhere: %s", js(m))
	}
	select {
	case <-s.Done():
		t.Fatal("session ended on goAway")
	case <-first.got:
		t.Fatal("old connection still used")
	case <-time.After(200 * time.Millisecond):
	}
	var b [1]byte // the client closed the old connection
	first.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := first.ws.Read(b[:]); err == nil {
		t.Error("old connection still open")
	}
}

func TestUsageLine(t *testing.T) {
	f := newFake(t, true)
	var log bytes.Buffer
	got := make(chan voice.Usage, 1)
	turn := make(chan struct{}, 1)
	s, _ := f.dial(t, voice.Config{UsageLog: &log, Handlers: voice.Handlers{ResponseDone: func(u voice.Usage) { got <- u }, TurnDone: func() { turn <- struct{}{} }}})
	// Real turnComplete shape, 11-01.
	f.push(t, `{"serverContent":{"turnComplete":true},"usageMetadata":{"promptTokenCount":542,"responseTokenCount":150,"totalTokenCount":692,`+
		`"promptTokensDetails":[{"modality":"TEXT","tokenCount":329},{"modality":"AUDIO","tokenCount":168}],`+
		`"responseTokensDetails":[{"modality":"AUDIO","tokenCount":150}],"thoughtsTokenCount":74}}`)
	u := wait(t, got, "ResponseDone")
	wait(t, turn, "TurnDone")
	want := voice.Usage{Input: 542, TextIn: 329, AudioIn: 168, Output: 224, AudioOut: 150, TextOut: 74,
		USD: (329*0.75 + 168*3 + 150*12 + 74*4.5) / 1e6}
	if u.USD-want.USD > 1e-12 || want.USD-u.USD > 1e-12 {
		t.Errorf("USD = %v, want %v", u.USD, want.USD)
	}
	if c := s.Cost(); c != u.USD {
		t.Errorf("Cost = %v, want %v", c, u.USD)
	}
	if u.USD = want.USD; u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}
	var l usageLine
	if err := json.Unmarshal(log.Bytes(), &l); err != nil || strings.Count(log.String(), "\n") != 1 {
		t.Fatalf("usage log %q: %v", log.String(), err)
	}
	if l.Model != "gemini/"+testModel || l.Input != 542 || l.AudioOut != 150 || l.TextOut != 74 || l.Unpriced {
		t.Errorf("usage line = %+v", l)
	}
}

func TestTextTurn(t *testing.T) {
	f := newFake(t, true)
	var deltas []string
	final := make(chan string, 1)
	user := make(chan string, 1)
	turn := make(chan struct{}, 1)
	s, _ := f.dial(t, voice.Config{Handlers: voice.Handlers{UserText: func(x string) { user <- x },
		TextDelta: func(d string) { deltas = append(deltas, d) }, AssistantText: func(x string) { final <- x }, TurnDone: func() { turn <- struct{}{} }}})
	if err := s.SendText("你好"); err != nil {
		t.Fatal(err)
	}
	if got := js(f.next(t)); got != `{"clientContent":{"turnComplete":true,"turns":[{"parts":[{"text":"你好"}],"role":"user"}]}}` {
		t.Errorf("SendText = %s", got)
	}
	f.push(t, `{"serverContent":{"modelTurn":{"parts":[{"inlineData":{"data":"AAA="}}]},"outputTranscription":{"text":"我是"}}}`,
		`{"serverContent":{"outputTranscription":{"text":"Gemini"}}}`, turnComplete)
	if got := wait(t, final, "AssistantText"); got != "我是Gemini" {
		t.Errorf("AssistantText = %q", got)
	}
	wait(t, turn, "TurnDone")
	if strings.Join(deltas, "|") != "我是|Gemini" {
		t.Errorf("deltas = %v", deltas)
	}
	// Spoken turn: the transcript pieces are one UserText once the answer starts.
	f.push(t, `{"serverContent":{"inputTranscription":{"text":"几点"}}}`, `{"serverContent":{"inputTranscription":{"text":"了"}}}`, audioTurn(1), turnComplete)
	if got := wait(t, user, "UserText"); got != "几点了" {
		t.Errorf("UserText = %q", got)
	}
	wait(t, turn, "TurnDone")
	if got := s.RecentTurns(5); js(got) != `[{"Role":"user","Text":"你好"},{"Role":"assistant","Text":"我是Gemini"},{"Role":"user","Text":"几点了"}]` {
		t.Errorf("turns = %s", js(got))
	}
}

func TestServerCloseEndsSession(t *testing.T) {
	for _, tc := range []struct{ name, want string }{{"close frame", "1008: quota"}, {"plain close", "1000: "}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t, true)
			errs := make(chan error, 2)
			s, _ := f.dial(t, voice.Config{Handlers: voice.Handlers{Error: func(e error) { errs <- e }}})
			if tc.want == "1000: " {
				f.ws.ws.Close()
			} else {
				f.closeWith(t, 1008, "quota")
			}
			if e := wait(t, errs, "Error"); !strings.Contains(e.Error(), tc.want) {
				t.Errorf("error = %v", e)
			}
			select {
			case <-s.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("session not done after the server closed")
			}
			select {
			case e := <-errs:
				t.Errorf("second error reported after the session ended: %v", e)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}
