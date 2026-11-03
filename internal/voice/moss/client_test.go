package moss

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/voice"
)

// fake is a scripted MOSS-Live server: it records client text events and binary frames, answers
// attach_session and session.update by itself, and lets the test push server frames.
type fake struct {
	srv    *httptest.Server
	got    chan map[string]any
	bin    chan []byte
	conn   chan *websocket.Conn
	header http.Header
	query  string
	ws     *websocket.Conn
	updErr string // session.updated data.error
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{got: make(chan map[string]any, 100), bin: make(chan []byte, 100), conn: make(chan *websocket.Conn, 1)}
	h := websocket.Server{
		Handshake: func(_ *websocket.Config, r *http.Request) error {
			f.header, f.query = r.Header.Clone(), r.URL.RawQuery
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			f.conn <- ws
			for {
				typ, b, err := readFrame(ws)
				if err != nil {
					return
				}
				if typ == websocket.BinaryFrame {
					f.bin <- b
					continue
				}
				var ev map[string]any
				if err := json.Unmarshal(b, &ev); err != nil {
					t.Errorf("client sent bad JSON: %s", b)
					return
				}
				switch ev["action"] {
				case "attach_session":
					websocket.Message.Send(ws, `{"type":"session_attached","data":{"session_id":"s1"}}`)
				case "session.update":
					websocket.Message.Send(ws, `{"type":"session.updated","data":{"error":`+js(f.updErr)+`}}`)
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
	cfg.Provider, cfg.Key = voice.Moss, "test-key"
	cfg.Extra = map[string]string{"url": "ws" + strings.TrimPrefix(f.srv.URL, "http")}
	return cfg
}

// dial connects through voice.Dial and consumes the three setup events.
func (f *fake) dial(t *testing.T, cfg voice.Config) *Session {
	t.Helper()
	vs, err := voice.Dial(context.Background(), f.cfg(cfg))
	if err != nil {
		t.Fatal(err)
	}
	s := vs.(*Session)
	t.Cleanup(func() { s.Close() })
	f.ws = <-f.conn
	for _, want := range []string{"attach_session", "session.update", "mic_state"} {
		if ev := f.next(t); ev["action"] != want {
			t.Fatalf("want %s, got %s", want, js(ev))
		}
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

func (f *fake) pushBin(t *testing.T, b []byte) {
	t.Helper()
	if err := websocket.Message.Send(f.ws, b); err != nil {
		t.Fatal(err)
	}
}

func (f *fake) next(t *testing.T) map[string]any {
	t.Helper()
	return wait(t, f.got, "a client event")
}

// quiet asserts the client sends nothing more for a while.
func (f *fake) quiet(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case ev := <-f.got:
		t.Fatalf("unexpected client event %s", js(ev))
	case b := <-f.bin:
		t.Fatalf("unexpected binary frame of %d bytes", len(b))
	case <-time.After(d):
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

func setVar[T any](t *testing.T, p *T, v T) {
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func TestDialHandshake(t *testing.T) {
	f := newFake(t)
	var called []string
	cfg := f.cfg(voice.Config{Instructions: voice.Build("小王", nil, nil), Version: "1.2",
		Call: func(_ context.Context, name, args string) string {
			called = append(called, name+" "+args)
			return `{"` + name + `":[]}`
		}})
	s, err := voice.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if f.header.Get("Authorization") != "Bearer test-key" || f.header.Get("User-Agent") != "rex-voiced/1.2" {
		t.Errorf("headers = %v", f.header)
	}
	if f.query != "model=moss-live-1.0" {
		t.Errorf("query = %q", f.query)
	}
	if ev := f.next(t); js(ev) != `{"action":"attach_session"}` {
		t.Fatalf("first event %s", js(ev))
	}
	ev := f.next(t)
	sess, _ := ev["session"].(map[string]any)
	persona, _ := sess["persona"].(map[string]any)
	instr, _ := sess["instructions"].(string)
	if ev["action"] != "session.update" || persona["name"] != "Rex" || !strings.Contains(persona["prompt"].(string), "「小王」") {
		t.Fatalf("session.update = %s", js(ev))
	}
	if !strings.HasPrefix(instr, snapshotPrefix) || !strings.Contains(instr, `list_panes：{"list_panes":[]}`) || !strings.Contains(instr, `list_claude：{"list_claude":[]}`) {
		t.Errorf("instructions = %q", instr)
	}
	if js(called) != `["list_panes {}","list_claude {}"]` {
		t.Errorf("tool calls = %v", called)
	}
	if ev := f.next(t); js(ev) != `{"action":"mic_state","enabled":true}` {
		t.Fatalf("third event %s", js(ev))
	}
}

func TestSnapshot(t *testing.T) {
	if got := snapshot(context.Background(), nil); got != snapshotPrefix+"拿不到快照" {
		t.Errorf("nil Call: %q", got)
	}
	failing := func(_ context.Context, name, _ string) string {
		if name == "list_claude" {
			return `{"error":"rex 不在"}`
		}
		return `[]`
	}
	if got := snapshot(context.Background(), failing); got != snapshotPrefix+"拿不到快照" {
		t.Errorf("error from Call: %q", got)
	}
	stamped := func(_ context.Context, name, _ string) string {
		if name == "list_claude" {
			return `[{"started":"2026-11-02T17:19:04-08:00","last_active":"2026-11-03T05:25:14-08:00"}]`
		}
		return `[]`
	}
	now := time.Date(2026, 11, 3, 5, 40, 0, 0, time.FixedZone("PST", -8*3600))
	saved := snapshotNow
	snapshotNow = func() time.Time { return now }
	t.Cleanup(func() { snapshotNow = saved })
	if got := snapshot(context.Background(), stamped); !strings.Contains(got, `"started":"11-02 17:19"`) || !strings.Contains(got, `"last_active":"今天 05:25"`) {
		t.Errorf("timestamps not rewritten: %s", got)
	}
	long := func(context.Context, string, string) string { return strings.Repeat("窗", 5000) }
	if got := snapshot(context.Background(), long); len([]rune(got)) != maxSnapshot {
		t.Errorf("snapshot is %d runes, want %d", len([]rune(got)), maxSnapshot)
	}
}

func TestDialRejected(t *testing.T) {
	f := newFake(t)
	f.updErr = "persona too long"
	var log bytes.Buffer
	cfg := f.cfg(voice.Config{UsageLog: &log})
	_, err := voice.Dial(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "persona too long") {
		t.Fatalf("err = %v", err)
	}
	var l usageLine
	if line := log.String(); strings.Count(line, "\n") != 1 || json.Unmarshal([]byte(line), &l) != nil ||
		l.Provider != "moss" || l.Model != "moss-live-1.0" || !l.Unpriced {
		t.Errorf("a failed dial still writes its usage line; usage log = %q", line)
	}
	cfg.TextOnly = true
	if _, err := voice.Dial(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "不支持文字模式") {
		t.Fatalf("TextOnly err = %v", err)
	}
}

func TestAppendAudioDownsampled(t *testing.T) {
	f := newFake(t)
	s := f.dial(t, voice.Config{})
	pcm := make([]byte, 960) // 20 ms at 24k
	for i := range pcm {
		pcm[i] = byte(i)
	}
	for range 2 {
		if err := s.AppendAudio(pcm); err != nil {
			t.Fatal(err)
		}
	}
	// The streaming downsampler holds back firHalf samples of the first frame; after that 20 ms in is 20 ms out.
	if b := wait(t, f.bin, "first frame"); len(b) == 0 || len(b) > 640 {
		t.Errorf("first frame %d bytes", len(b))
	}
	if b := wait(t, f.bin, "second frame"); len(b) != 640 {
		t.Errorf("second frame %d bytes, want 640 (20 ms at 16k)", len(b))
	}
}

func TestAudioDownlink48to24(t *testing.T) {
	f := newFake(t)
	got := make(chan []byte, 4)
	f.dial(t, voice.Config{Handlers: voice.Handlers{AudioDelta: func(b []byte) { got <- b }}})
	f.pushBin(t, make([]byte, 1920)) // no context before it: dropped
	f.push(t, `{"type":"response.audio.context","data":{"chunk_id":1,"run_id":1,"turn_id":1}}`)
	f.pushBin(t, make([]byte, 1920)) // 20 ms at 48k
	if b := wait(t, got, "AudioDelta"); len(b) != 960 {
		t.Errorf("AudioDelta got %d bytes, want 960 (20 ms at 24k)", len(b))
	}
	select {
	case b := <-got:
		t.Errorf("extra AudioDelta of %d bytes (the frame without context must be dropped)", len(b))
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAudioStop(t *testing.T) {
	setVar(t, &bytesPerMs, 0.96) // 1920 bytes play for 2 s: no receipt before the stop
	f := newFake(t)
	speech := make(chan struct{}, 4)
	audio := make(chan []byte, 4)
	f.dial(t, voice.Config{Handlers: voice.Handlers{SpeechStarted: func() { speech <- struct{}{} }, AudioDelta: func(b []byte) { audio <- b }}})
	f.push(t, `{"type":"response.audio.context","data":{"chunk_id":1,"run_id":5,"turn_id":2}}`)
	f.pushBin(t, make([]byte, 1920))
	wait(t, audio, "AudioDelta")
	f.push(t, `{"type":"response.audio.stop","data":{"run_id":5,"turn_id":2}}`)
	wait(t, speech, "SpeechStarted")
	ev := f.next(t)
	if ev["action"] != "playback.stopped" || ev["run_id"] != float64(5) || ev["turn_id"] != float64(2) {
		t.Fatalf("want playback.stopped run 5, got %s", js(ev))
	}
	if ms, _ := ev["played_audio_ms"].(float64); ms < 0 || ms > 1000 {
		t.Errorf("played_audio_ms = %v", ev["played_audio_ms"])
	}
	// Late audio of the stopped run is dropped, and its pending receipt is never sent.
	f.push(t, `{"type":"response.audio.context","data":{"chunk_id":2,"run_id":5,"turn_id":2}}`)
	f.pushBin(t, make([]byte, 1920))
	select {
	case <-audio:
		t.Error("audio of a stopped run was played")
	case <-speech:
		t.Error("second SpeechStarted")
	case <-time.After(200 * time.Millisecond):
	}
	f.quiet(t, 200*time.Millisecond)
}

func TestInterruptedDropsAudio(t *testing.T) {
	f := newFake(t)
	audio := make(chan []byte, 4)
	f.dial(t, voice.Config{Handlers: voice.Handlers{AudioDelta: func(b []byte) { audio <- b }}})
	f.push(t, `{"type":"response.interrupted","data":{"text":"好的","run_id":"r9","turn_id":"t1"}}`,
		`{"type":"response.audio.context","data":{"chunk_id":1,"run_id":"r9","turn_id":"t1"}}`)
	f.pushBin(t, make([]byte, 1920))
	select {
	case <-audio:
		t.Error("audio of an interrupted run was played")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestResponseEnded(t *testing.T) {
	f := newFake(t)
	turn := make(chan struct{}, 4)
	usage := make(chan voice.Usage, 4)
	f.dial(t, voice.Config{Handlers: voice.Handlers{TurnDone: func() { turn <- struct{}{} }, ResponseDone: func(u voice.Usage) { usage <- u }}})
	f.push(t, `{"type":"response.ended","data":{"status":"aborted","run_id":1,"turn_id":1}}`)
	if u := wait(t, usage, "ResponseDone"); u != (voice.Usage{Unpriced: true}) {
		t.Errorf("usage = %+v", u)
	}
	wait(t, turn, "TurnDone")
}

func TestPlaybackReceipts(t *testing.T) {
	setVar(t, &bytesPerMs, 960) // 1920 bytes play for 2 ms
	f := newFake(t)
	f.dial(t, voice.Config{})
	f.push(t, `{"type":"response.audio.context","data":{"chunk_id":7,"run_id":"r1","turn_id":3}}`)
	f.pushBin(t, make([]byte, 1920))
	if ev := f.next(t); js(ev) != `{"action":"playback.chunk.completed","chunk_id":7,"run_id":"r1","turn_id":3}` {
		t.Fatalf("want chunk receipt, got %s", js(ev))
	}
	f.push(t, `{"type":"response.audio.done","data":{"run_id":"r1","turn_id":3}}`)
	if ev := f.next(t); js(ev) != `{"action":"playback.completed","run_id":"r1","turn_id":3}` {
		t.Fatalf("want playback.completed, got %s", js(ev))
	}
}

func TestHangUpPhrase(t *testing.T) {
	f := newFake(t)
	calls := make(chan string, 4)
	users := make(chan string, 4)
	s := f.dial(t, voice.Config{
		Call: func(_ context.Context, name, args string) string {
			if name == "hang_up" {
				calls <- args
			}
			return `{"ok":true}`
		},
		Handlers: voice.Handlers{UserText: func(u string) { users <- u }}})
	f.push(t, `{"type":"input.transcript.done","data":{"text":"那 挂 断吧"}}`)
	if u := wait(t, users, "UserText"); u != "那 挂 断吧" {
		t.Errorf("UserText = %q", u)
	}
	if a := wait(t, calls, "hang_up"); a != hangUpReason {
		t.Errorf("hang_up args = %s", a)
	}
	f.push(t, `{"type":"output.transcript.done","data":{"text":"好的，拜拜","run_id":1,"turn_id":1}}`,
		`{"type":"input.transcript.done","data":{"text":"挂了"}}`)
	wait(t, users, "second UserText")
	select {
	case a := <-calls:
		t.Errorf("hang_up called twice (%s)", a)
	case <-time.After(200 * time.Millisecond):
	}
	if turns := s.RecentTurns(5); js(turns) != `[{"Role":"user","Text":"那 挂 断吧"},{"Role":"assistant","Text":"好的，拜拜"},{"Role":"user","Text":"挂了"}]` {
		t.Errorf("turns = %s", js(turns))
	}
}

func TestSendTextFails(t *testing.T) {
	f := newFake(t)
	s := f.dial(t, voice.Config{})
	if err := s.SendText("你好"); err == nil {
		t.Fatal("SendText succeeded")
	}
	f.quiet(t, 200*time.Millisecond)
}

func TestMutedSendsSilence(t *testing.T) {
	f := newFake(t)
	s := f.dial(t, voice.Config{})
	if err := s.SetMuted(true); err != nil {
		t.Fatal(err)
	}
	s.AppendAudio(bytes.Repeat([]byte{1}, 960)) // dropped while muted
	deadline := time.After(60 * time.Millisecond)
	n := 0
	for n < 2 {
		select {
		case b := <-f.bin:
			if !bytes.Equal(b, silence) {
				t.Fatalf("muted frame is %d bytes, not 640 zeros", len(b))
			}
			n++
		case <-deadline:
			t.Fatalf("%d silence frames in 60 ms, want >= 2", n)
		}
	}
	s.SetMuted(false)
	time.Sleep(50 * time.Millisecond) // a frame already in flight may still land
	for len(f.bin) > 0 {
		<-f.bin
	}
	f.quiet(t, 100*time.Millisecond)
}

func TestReadErrorCloses(t *testing.T) {
	f := newFake(t)
	errs := make(chan error, 1)
	var log bytes.Buffer
	s := f.dial(t, voice.Config{UsageLog: &log, Handlers: voice.Handlers{Error: func(e error) { errs <- e }}})
	f.ws.Close()
	if e := wait(t, errs, "Error"); !strings.Contains(e.Error(), "moss: read") {
		t.Errorf("err = %v", e)
	}
	wait(t, s.Done(), "Done")
	if strings.Count(log.String(), "\n") != 1 {
		t.Errorf("usage log = %q", log.String())
	}
}

func TestServerError(t *testing.T) {
	f := newFake(t)
	errs := make(chan error, 1)
	f.dial(t, voice.Config{Handlers: voice.Handlers{Error: func(e error) { errs <- e }}})
	f.push(t, `{"type":"error","data":{"error":{"code":"rate_limited","message":"slow down"}}}`)
	if e := wait(t, errs, "Error"); e.Error() != "moss: rate_limited: slow down" {
		t.Errorf("err = %v", e)
	}
}

// MOSS fragments an audio chunk over several WebSocket frames: every fragment must come back as
// a binary frame (x/net only types the continuation after HandleFrame). Raw frames need a raw
// server: the handler hijacks the connection and writes the handshake and frames by hand.
func TestReadFrameFragmented(t *testing.T) {
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, bw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()
		sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		fmt.Fprintf(bw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n",
			base64.StdEncoding.EncodeToString(sum[:]))
		bw.Write([]byte{0x81, 2, '{', '}'}) // text, FIN
		bw.Write([]byte{0x02, 3, 1, 2, 3})  // binary, no FIN
		bw.Write([]byte{0x80, 2, 4, 5})     // continuation, FIN
		bw.Write([]byte{0x81, 2, '[', ']'}) // text, FIN
		bw.Flush()
		<-hold
	}))
	t.Cleanup(func() { close(hold); srv.Close() })
	ws, err := websocket.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), "", "http://localhost/")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for i, want := range []struct {
		typ byte
		b   []byte
	}{{websocket.TextFrame, []byte("{}")}, {websocket.BinaryFrame, []byte{1, 2, 3}}, {websocket.BinaryFrame, []byte{4, 5}}, {websocket.TextFrame, []byte("[]")}} {
		typ, b, err := readFrame(ws)
		if err != nil || typ != want.typ || !bytes.Equal(b, want.b) {
			t.Fatalf("frame %d = type %d %v %v, want type %d %v", i, typ, b, err, want.typ, want.b)
		}
	}
}

// Two binary frames after one context are fragments of one chunk: both play, one receipt, after both.
func TestAudioFragmentsOneReceipt(t *testing.T) {
	setVar(t, &bytesPerMs, 96) // 1920 bytes = 20 ms
	f := newFake(t)
	audio := make(chan int, 4)
	f.dial(t, voice.Config{Handlers: voice.Handlers{AudioDelta: func(pcm []byte) { audio <- len(pcm) }}})
	f.push(t, `{"type":"response.audio.context","data":{"chunk_id":7,"run_id":"r1","turn_id":3}}`)
	f.pushBin(t, make([]byte, 1920))
	f.pushBin(t, make([]byte, 1920))
	if n := wait(t, audio, "fragment 1"); n != 960 {
		t.Errorf("fragment 1 = %d bytes", n)
	}
	if n := wait(t, audio, "fragment 2"); n != 960 {
		t.Errorf("fragment 2 = %d bytes", n)
	}
	t0 := time.Now()
	if ev := f.next(t); js(ev) != `{"action":"playback.chunk.completed","chunk_id":7,"run_id":"r1","turn_id":3}` {
		t.Fatalf("want one chunk receipt, got %s", js(ev))
	}
	if d := time.Since(t0); d < 30*time.Millisecond {
		t.Errorf("receipt after %v, want after both fragments (40 ms)", d)
	}
	select {
	case ev := <-f.got:
		t.Fatalf("second receipt for the same chunk: %s", js(ev))
	case <-time.After(100 * time.Millisecond):
	}
}
