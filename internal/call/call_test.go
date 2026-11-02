package call

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/relay"
	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
	_ "github.com/yang1997434/rex-voice/internal/voice/openai"
)

const wait = 3 * time.Second

func TestMain(m *testing.M) {
	tickEvery, echoDelay, echoReport = 10*time.Millisecond, 50*time.Millisecond, 200*time.Millisecond
	backoffMin, backoffMax = 20*time.Millisecond, 100*time.Millisecond
	os.Exit(m.Run())
}

// set overrides a timing for one test; call it before start so Run never sees the restore.
func set(t *testing.T, v *time.Duration, d time.Duration) {
	old := *v
	*v = d
	t.Cleanup(func() { *v = old })
}

// ---- fake OpenAI Realtime ----

type aiConn struct {
	ws     *websocket.Conn
	auth   string // Authorization header of the upgrade
	got    chan map[string]any
	closed chan struct{}
}

type fakeAI struct {
	holdUpdated atomic.Bool
	srv         *httptest.Server
	conns       chan *aiConn
}

// holdUpdated keeps the Mac dialing: session.updated is then pushed by the test, not auto-replied.
func newFakeAI(t *testing.T) *fakeAI {
	f := &fakeAI{conns: make(chan *aiConn, 4)}
	f.srv = httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		c := &aiConn{ws: ws, auth: ws.Request().Header.Get("Authorization"), got: make(chan map[string]any, 1000), closed: make(chan struct{})}
		defer close(c.closed)
		f.conns <- c
		for {
			var raw string
			if err := websocket.Message.Receive(ws, &raw); err != nil {
				return
			}
			var ev map[string]any
			json.Unmarshal([]byte(raw), &ev)
			if ev["type"] == "session.update" && !f.holdUpdated.Load() {
				websocket.Message.Send(ws, `{"type":"session.updated"}`)
			}
			c.got <- ev
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAI) accept(t *testing.T) *aiConn {
	t.Helper()
	select {
	case c := <-f.conns:
		return c
	case <-time.After(wait):
		t.Fatal("OpenAI was never dialed")
		return nil
	}
}

func (c *aiConn) push(t *testing.T, ev string) {
	t.Helper()
	if err := websocket.Message.Send(c.ws, ev); err != nil {
		t.Fatal(err)
	}
}

// next returns the next client event of the given type, skipping others.
func (c *aiConn) next(t *testing.T, typ string) map[string]any {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case ev := <-c.got:
			if ev["type"] == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s from the Mac", typ)
		}
	}
}

func (c *aiConn) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-c.closed:
	case <-time.After(wait):
		t.Fatal("OpenAI session was not closed")
	}
}

func delta(item string, pcm []byte) string {
	return `{"type":"response.output_audio.delta","item_id":"` + item + `","delta":"` + base64.StdEncoding.EncodeToString(pcm) + `"}`
}

const responseDone = `{"type":"response.done","response":{"id":"r1","status":"completed"}}`

// ---- environment: real relay + Run ----

type env struct {
	relayURL string
	path     string
	p        *pair.Pairing
	ai       *fakeAI
}

func start(t *testing.T, echo bool, deviceID string) *env {
	t.Helper()
	return startWith(t, echo, deviceID, relay.NewHub())
}

func startWith(t *testing.T, echo bool, deviceID string, h http.Handler, mods ...func(*Config)) *env {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := pair.Generate()
	if err != nil {
		t.Fatal(err)
	}
	p.DeviceID = deviceID
	e := &env{relayURL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/_voice/ws", path: filepath.Join(t.TempDir(), "pair.json"), p: &p}
	if err := p.Save(e.path); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Pairing: e.p, PairingPath: e.path, RelayURL: e.relayURL, Echo: echo, Tools: &tools.Registry{}}
	if !echo {
		e.ai = newFakeAI(t)
		cfg.Voice = voice.Config{Provider: voice.OpenAI, Model: "gpt-realtime-2.1", Key: "test-key", Instructions: "base prompt",
			Extra: map[string]string{"url": "ws" + strings.TrimPrefix(e.ai.srv.URL, "http")}}
	}
	for _, m := range mods {
		m(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(wait):
			t.Error("Run did not return after cancel")
		}
	})
	return e
}

// ---- fake phone ----

type phone struct {
	e      *env
	ws     *websocket.Conn
	salt   [16]byte
	rx, tx *pair.Cipher
	frames chan wsFrame
}

func (e *env) dialPhone(t *testing.T) *phone {
	t.Helper()
	u := e.relayURL + "?role=phone&id=" + base64.RawURLEncoding.EncodeToString(e.p.ID[:])
	ws, err := websocket.Dial(u, "", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	ph := &phone{e: e, ws: ws, frames: make(chan wsFrame, 1000)}
	rand.Read(ph.salt[:])
	go func() {
		defer close(ph.frames)
		for {
			var f wsFrame
			if frameCodec.Receive(ws, &f) != nil {
				return
			}
			ph.frames <- f
		}
	}()
	return ph
}

func (ph *phone) raw(t *testing.T) wsFrame {
	t.Helper()
	select {
	case f, ok := <-ph.frames:
		if !ok {
			t.Fatal("phone connection closed")
		}
		return f
	case <-time.After(wait):
		t.Fatal("phone got nothing")
		return wsFrame{}
	}
}

func (ph *phone) send(t *testing.T, b []byte) {
	t.Helper()
	if err := websocket.Message.Send(ph.ws, b); err != nil {
		t.Fatal(err)
	}
}

// macSalt waits for the salt the Mac sends when it sees the phone present.
func (ph *phone) macSalt(t *testing.T) [16]byte {
	t.Helper()
	for {
		f := ph.raw(t)
		if f.typ == websocket.BinaryFrame && f.data[0] == pair.TypeSalt {
			mac, ok := pair.ParseSaltFrame(f.data)
			if !ok {
				t.Fatalf("bad mac salt frame %x", f.data)
			}
			return mac
		}
	}
}

// derive sends our salt and keys both directions with the Mac's.
func (ph *phone) derive(t *testing.T, mac [16]byte) {
	ph.rx = pair.NewCipher(ph.e.p.Key, pair.MacToPhone, ph.salt, mac)
	ph.tx = pair.NewCipher(ph.e.p.Key, pair.PhoneToMac, ph.salt, mac)
	ph.send(t, pair.SaltFrame(ph.salt))
}

func (ph *phone) handshake(t *testing.T) { ph.derive(t, ph.macSalt(t)) }

func (ph *phone) ctl(t *testing.T, v map[string]any) {
	t.Helper()
	b, _ := json.Marshal(v)
	ph.send(t, ph.tx.Seal(pair.TypeControl, b))
}

func (ph *phone) hello(t *testing.T, deviceID string) {
	ph.ctl(t, map[string]any{"t": "hello", "app": "test", "device": "iPhone", "device_id": deviceID})
}

func (ph *phone) audio(t *testing.T, pcm []byte) { ph.send(t, ph.tx.Seal(pair.TypeAudio, pcm)) }

// next returns the next decrypted frame, skipping keepalives and relay text frames.
func (ph *phone) next(t *testing.T) (byte, []byte) {
	t.Helper()
	for {
		f := ph.raw(t)
		if f.typ != websocket.BinaryFrame || f.data[0] == pair.TypeKeepalive || f.data[0] == pair.TypeSalt {
			continue
		}
		typ, _, plain, err := ph.rx.Open(f.data)
		if err != nil {
			t.Fatalf("phone could not open frame: %v", err)
		}
		return typ, plain
	}
}

// ctlUntil returns the first control message with t == want, skipping audio and other control.
func (ph *phone) ctlUntil(t *testing.T, want string) map[string]any {
	t.Helper()
	for {
		typ, plain := ph.next(t)
		if typ != pair.TypeControl {
			continue
		}
		var m map[string]any
		json.Unmarshal(plain, &m)
		if m["t"] == want {
			return m
		}
	}
}

func (ph *phone) expectStatus(t *testing.T, states ...string) {
	t.Helper()
	for _, want := range states {
		if m := ph.ctlUntil(t, "status"); m["state"] != want {
			t.Fatalf("status %v, want %s", m["state"], want)
		}
	}
}

// ready connects a phone that has finished the salt exchange and hello.
func (e *env) ready(t *testing.T) *phone {
	ph := e.dialPhone(t)
	ph.handshake(t)
	ph.hello(t, "phone-1")
	return ph
}

func (e *env) inCall(t *testing.T) (*phone, *aiConn) {
	ph := e.ready(t)
	ph.ctl(t, map[string]any{"t": "call.start"})
	ai := e.ai.accept(t)
	ai.next(t, "session.update")
	ph.expectStatus(t, "connecting", "listening")
	return ph, ai
}

func pattern(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i)
	}
	return b
}

// ---- tests ----

// A phone that reconnects must redo the salt exchange: frames sealed with the previous
// connection's keys are dropped until its new salt arrives.
func TestRejectBeforeSalt(t *testing.T) {
	e := start(t, true, "")
	ph1 := e.ready(t)
	ph1.ctl(t, map[string]any{"t": "call.start"})
	ph1.expectStatus(t, "listening")
	ph1.ctl(t, map[string]any{"t": "call.end"})
	ph1.ctlUntil(t, "hang_up")
	ph1.ws.Close()

	ph2 := e.dialPhone(t)
	mac := ph2.macSalt(t) // the Mac's fresh salt; ours is not sent yet
	old := ph1.tx         // keys from the earlier connection, seq continuing
	for _, m := range []string{`{"t":"hello","device_id":"phone-1"}`, `{"t":"call.start"}`} {
		ph2.send(t, old.Seal(pair.TypeControl, []byte(m)))
	}
	ph2.send(t, old.Seal(pair.TypeAudio, pattern(960, 1)))
	select {
	case f := <-ph2.frames:
		if f.typ == websocket.BinaryFrame && f.data[0] != pair.TypeKeepalive {
			t.Fatalf("Mac answered frames sent before the salt exchange: %x", f.data[:5])
		}
	case <-time.After(300 * time.Millisecond):
	}

	ph2.derive(t, mac) // after the exchange the same phone is accepted
	ph2.hello(t, "phone-1")
	ph2.ctl(t, map[string]any{"t": "call.start"})
	ph2.expectStatus(t, "listening")
}

func TestDeviceIDRecordedThenOtherPhoneRejected(t *testing.T) {
	e := start(t, true, "")
	ph := e.ready(t)
	ph.ctl(t, map[string]any{"t": "call.start"})
	ph.expectStatus(t, "listening") // hello was processed before call.start
	got, err := pair.Load(e.path)
	if err != nil || got.DeviceID != "phone-1" || got.PairedAt.IsZero() {
		t.Fatalf("pair.json after first hello: %+v err=%v", got, err)
	}

	other := e.dialPhone(t) // replaces the first phone at the relay
	other.handshake(t)
	other.hello(t, "phone-2")
	if m := other.ctlUntil(t, "error"); !strings.Contains(m["msg"].(string), "pair") {
		t.Fatalf("error = %v", m)
	}
	for { // the Mac drops its relay connection, so the relay reports it gone
		f := other.raw(t)
		if f.typ == websocket.TextFrame && strings.Contains(string(f.data), `"present":false`) {
			break
		}
	}
	if got, _ := pair.Load(e.path); got.DeviceID != "phone-1" {
		t.Fatalf("device_id overwritten: %q", got.DeviceID)
	}
}

func TestCallAudioBothWays(t *testing.T) {
	e := start(t, false, "")
	ph, ai := e.inCall(t)

	in := pattern(960, 7)
	ph.audio(t, in)
	if got, _ := base64.StdEncoding.DecodeString(ai.next(t, "input_audio_buffer.append")["audio"].(string)); !bytes.Equal(got, in) {
		t.Fatalf("OpenAI got %d bytes, want the phone frame", len(got))
	}

	out := pattern(3500, 0)
	ai.push(t, delta("it1", out[:2500]))
	ai.push(t, delta("it1", out[2500:]))
	ai.push(t, responseDone)
	ph.expectStatus(t, "speaking")
	var got []byte
	var sizes []int
	for len(got) < len(out) {
		typ, plain := ph.next(t)
		if typ != pair.TypeAudio {
			t.Fatalf("control frame in the middle of audio: %s", plain)
		}
		got, sizes = append(got, plain...), append(sizes, len(plain))
	}
	if !bytes.Equal(got, out) || len(sizes) != 4 || sizes[0] != 960 || sizes[1] != 960 || sizes[2] != 960 || sizes[3] != 620 {
		t.Fatalf("frames %v, equal=%v", sizes, bytes.Equal(got, out))
	}
	ph.expectStatus(t, "listening")
}

func TestSpeechStartedInterruptsAndTruncates(t *testing.T) {
	e := start(t, false, "")
	ph, ai := e.inCall(t)
	ai.push(t, delta("it1", pattern(2500, 0))) // 2500 bytes handed out = 52 ms
	ai.push(t, `{"type":"input_audio_buffer.speech_started"}`)
	ph.ctlUntil(t, "interrupt")
	tr := ai.next(t, "conversation.item.truncate")
	if tr["item_id"] != "it1" || tr["audio_end_ms"] != float64(52) {
		t.Fatalf("truncate = %v", tr)
	}
	ai.push(t, delta("it1", pattern(960, 50))) // late delta of the cut item: dropped
	next := pattern(960, 99)
	ai.push(t, delta("it2", next))
	for {
		typ, plain := ph.next(t)
		if typ == pair.TypeAudio {
			if !bytes.Equal(plain, next) {
				t.Fatal("audio from the interrupted item reached the phone")
			}
			return
		}
	}
}

func TestIdlePromptThenHangUp(t *testing.T) {
	set(t, &idlePrompt, 200*time.Millisecond)
	set(t, &idleHangup, 500*time.Millisecond)
	e := start(t, false, "")
	ph, ai := e.inCall(t)
	item := ai.next(t, "conversation.item.create")
	if b, _ := json.Marshal(item); !strings.Contains(string(b), "60 秒没说话") {
		t.Fatalf("idle prompt = %s", b)
	}
	ai.next(t, "response.create")
	if m := ph.ctlUntil(t, "hang_up"); m["reason"] != "idle" {
		t.Fatalf("hang_up = %v", m)
	}
	ai.waitClosed(t)
}

// toolCall is an OpenAI function call of response r1.
func toolCall(name, args string) string {
	a, _ := json.Marshal(args)
	return `{"type":"response.output_item.done","response_id":"r1","item":{"type":"function_call","id":"fc_` + name + `","call_id":"c_` + name + `","name":"` + name + `","arguments":` + string(a) + `}}`
}

const responseDone2 = `{"type":"response.done","response":{"id":"r2","status":"completed"}}`

// hang_up only marks the call: the goodbye (including the held tail under one frame) reaches the
// phone first, and the call ends when the model's turn is done.
func TestHangUpTool(t *testing.T) {
	e := start(t, false, "")
	ph, ai := e.inCall(t)
	bye := pattern(1000, 5) // one whole frame now, a 40-byte tail held until the turn ends
	ai.push(t, delta("it1", bye))
	ai.push(t, toolCall("hang_up", `{"reason":"拜拜"}`))
	ai.push(t, responseDone)
	ai.next(t, "response.create") // tool output went back; the model finishes its turn
	ai.push(t, responseDone2)
	var got []byte
	for {
		typ, plain := ph.next(t)
		if typ == pair.TypeAudio {
			got = append(got, plain...)
			continue
		}
		var m map[string]any
		json.Unmarshal(plain, &m)
		if m["t"] == "hang_up" {
			if m["reason"] != "user" {
				t.Fatalf("hang_up = %v", m)
			}
			break
		}
	}
	if !bytes.Equal(got, bye) {
		t.Fatalf("phone got %d bytes of the goodbye before hang_up, want %d", len(got), len(bye))
	}
	ai.waitClosed(t)

	ph.ctl(t, map[string]any{"t": "call.start"}) // back to waiting: a new call dials again
	e.ai.accept(t)
}

// A model that never finishes its turn after hang_up is hung up on after hangupGrace.
func TestHangUpToolGraceTimeout(t *testing.T) {
	set(t, &hangupGrace, 300*time.Millisecond)
	e := start(t, false, "")
	ph, ai := e.inCall(t)
	t0 := time.Now()
	ai.push(t, toolCall("hang_up", `{}`))
	ai.push(t, responseDone)
	if m := ph.ctlUntil(t, "hang_up"); m["reason"] != "user" {
		t.Fatalf("hang_up = %v", m)
	}
	if d := time.Since(t0); d < 250*time.Millisecond {
		t.Fatalf("hung up after %v, before the grace period", d)
	}
	ai.waitClosed(t)
}

// fakeRex answers the Claude watch with one claude-code pane (no pid, so the project is the
// session label "dev"), blocked while *blocked is set.
func fakeRex(blocked *atomic.Bool) func(context.Context, string, ...string) ([]byte, error) {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		switch {
		case cmd == "rex ls --json":
			return []byte(`{"sessions":[{"session_id":"s1","label":"dev"}]}`), nil
		case cmd == "rex block ls -s s1 --json":
			return []byte(`{"blocks":[{"block_id":"b1"}]}`), nil
		case strings.HasSuffix(cmd, "program_status"):
			st := "working"
			if blocked.Load() {
				st = "blocked"
			}
			return fmt.Appendf(nil, `{"records":[{"state":%q,"app":"claude-code","msg":"要合并 PR 吗？"}]}`, st), nil
		}
		return nil, fmt.Errorf("fake: unexpected command %s", cmd)
	}
}

func withWatch(t *testing.T, blocked *atomic.Bool) func(*Config) {
	set(t, &watchEvery, 20*time.Millisecond)
	return func(c *Config) { c.Watch, c.Mac = true, tools.Options{Exec: fakeRex(blocked), Home: t.TempDir()} }
}

func TestBlockedClaudeToldInCall(t *testing.T) {
	var blocked atomic.Bool
	e := startWith(t, false, "", relay.NewHub(), withWatch(t, &blocked))
	_, ai := e.inCall(t)
	blocked.Store(true)
	b, _ := json.Marshal(ai.next(t, "conversation.item.create"))
	if !strings.Contains(string(b), "dev 的 Claude 卡住了") || !strings.Contains(string(b), "要合并 PR 吗") {
		t.Fatalf("blocked prompt = %s", b)
	}
	ai.next(t, "response.create")
}

// Outside a call the Mac sends the relay a notify text frame, at most one per pane per notifyEvery.
// recordingRelay is a fake relay that only records what the Mac writes: text frames in full, binary as ticks.
func recordingRelay() (http.Handler, chan string, chan struct{}) {
	texts, bins := make(chan string, 10), make(chan struct{}, 100)
	h := websocket.Handler(func(ws *websocket.Conn) {
		for {
			var f wsFrame
			if frameCodec.Receive(ws, &f) != nil {
				return
			}
			if f.typ == websocket.TextFrame {
				texts <- string(f.data)
			} else {
				select {
				case bins <- struct{}{}:
				default:
				}
			}
		}
	})
	return h, texts, bins
}

func TestBlockedClaudeNotifiesRelayOutsideCall(t *testing.T) {
	set(t, &keepaliveEvery, 20*time.Millisecond)
	h, texts, bins := recordingRelay()
	var blocked atomic.Bool
	startWith(t, false, "", h, withWatch(t, &blocked))
	select { // a keepalive means the Mac holds the connection
	case <-bins:
	case <-time.After(wait):
		t.Fatal("Mac never wrote to the relay")
	}
	blocked.Store(true)
	select {
	case got := <-texts:
		var m map[string]string
		json.Unmarshal([]byte(got), &m)
		if m["t"] != "notify" || m["title"] != "dev 的 Claude 在等你" || m["body"] != "要合并 PR 吗？" {
			t.Fatalf("notify = %s", got)
		}
	case <-time.After(wait):
		t.Fatal("no notify text frame")
	}
	blocked.Store(false) // leave and re-enter blocked within notifyEvery: no second push
	time.Sleep(150 * time.Millisecond)
	blocked.Store(true)
	select {
	case got := <-texts:
		t.Fatalf("second notify within notifyEvery: %s", got)
	case <-time.After(300 * time.Millisecond):
	}
}

// show_file shrinks the image with sips, then the phone gets the attach control and every chunk in order.
func TestShowFileSendsChunks(t *testing.T) {
	src := filepath.Join(t.TempDir(), "shot.png")
	os.WriteFile(src, []byte("png"), 0o600)
	jpeg := pattern(130000, 9) // 60000 + 60000 + 10000
	var sipsArgs atomic.Value
	e := startWith(t, false, "", relay.NewHub(), func(c *Config) {
		c.Mac.Exec = func(_ context.Context, name string, args ...string) ([]byte, error) {
			sipsArgs.Store(name + " " + strings.Join(args, " "))
			if name != "sips" || len(args) < 2 || args[len(args)-2] != "--out" {
				return nil, fmt.Errorf("unexpected %s %v", name, args)
			}
			return nil, os.WriteFile(args[len(args)-1], jpeg, 0o600)
		}
	})
	ph, ai := e.inCall(t)
	ai.push(t, toolCall("show_file", `{"path":"`+src+`"}`))
	ai.push(t, responseDone)
	m := ph.ctlUntil(t, "attach")
	if m["name"] != "shot.png" || m["mime"] != "image/jpeg" || m["size"] != float64(len(jpeg)) || m["chunks"] != float64(3) {
		t.Fatalf("attach = %v", m)
	}
	id := uint32(m["id"].(float64))
	var got []byte
	for i := range 3 {
		typ, plain := ph.next(t)
		if typ != pair.TypeAttach || binary.BigEndian.Uint32(plain) != id || binary.BigEndian.Uint32(plain[4:]) != uint32(i) {
			t.Fatalf("frame %d: type %d header %x", i, typ, plain[:min(8, len(plain))])
		}
		got = append(got, plain[8:]...)
	}
	if !bytes.Equal(got, jpeg) {
		t.Fatalf("reassembled %d bytes, want the sips output", len(got))
	}
	if b, _ := json.Marshal(ai.next(t, "conversation.item.create")); !strings.Contains(string(b), `\"chunks\":3`) {
		t.Fatalf("tool output = %s", b)
	}
	if a := sipsArgs.Load().(string); !strings.HasPrefix(a, "sips -Z 1280 -s format jpeg "+src+" --out ") {
		t.Fatalf("sips args = %s", a)
	}
}

func TestShowLink(t *testing.T) {
	e := start(t, false, "")
	ph, ai := e.inCall(t)
	ai.push(t, toolCall("show_link", `{"url":"https://r.maruiao.top/_r/x","title":"汇报页"}`))
	ai.push(t, responseDone)
	if m := ph.ctlUntil(t, "link"); m["url"] != "https://r.maruiao.top/_r/x" || m["title"] != "汇报页" {
		t.Fatalf("link = %v", m)
	}
	if b, _ := json.Marshal(ai.next(t, "conversation.item.create")); !strings.Contains(string(b), `\"sent\":\"https://r.maruiao.top/_r/x\"`) {
		t.Fatalf("tool output = %s", b)
	}
}

func TestInstructionsRebuiltPerCall(t *testing.T) {
	var n atomic.Int32
	e := startWith(t, false, "", relay.NewHub(), func(c *Config) {
		c.Instructions = func(string) string { return fmt.Sprintf("table %d", n.Add(1)) }
	})
	instrOf := func(ai *aiConn) string {
		return ai.next(t, "session.update")["session"].(map[string]any)["instructions"].(string)
	}
	ph := e.ready(t)
	for want := 1; want <= 2; want++ {
		ph.ctl(t, map[string]any{"t": "call.start"})
		ai := e.ai.accept(t)
		if got := instrOf(ai); got != fmt.Sprintf("table %d", want) {
			t.Fatalf("call %d instructions = %q", want, got)
		}
		ph.expectStatus(t, "connecting", "listening")
		ph.ctl(t, map[string]any{"t": "call.end"})
		ph.expectStatus(t, "ended")
		ai.waitClosed(t)
	}
}

// call.start's name addresses the user for that call only; the next call without one gets "" (config.json's).
func TestCallStartNameInInstructions(t *testing.T) {
	e := startWith(t, false, "", relay.NewHub(), func(c *Config) {
		c.Instructions = func(name string) string { return voice.Build(cmp.Or(name, "小王"), nil, nil) }
	})
	ph := e.ready(t)
	for _, tc := range []struct{ name, want, not string }{{"LeBron", "你是LeBron的", "小王"}, {"", "你是小王的", "LeBron"}} {
		start := map[string]any{"t": "call.start"}
		if tc.name != "" {
			start["name"] = tc.name
		}
		ph.ctl(t, start)
		ai := e.ai.accept(t)
		instr := ai.next(t, "session.update")["session"].(map[string]any)["instructions"].(string)
		if !strings.Contains(instr, tc.want) || strings.Contains(instr, tc.not) {
			t.Fatalf("name %q: instructions = %q", tc.name, instr)
		}
		ph.expectStatus(t, "connecting", "listening")
		ph.ctl(t, map[string]any{"t": "call.end"})
		ph.expectStatus(t, "ended")
		ai.waitClosed(t)
	}
}

func TestOpenAIDropEndsCall(t *testing.T) {
	e := start(t, false, "")
	ph, ai := e.inCall(t)
	ai.ws.Close()
	ph.ctlUntil(t, "error")
	if m := ph.ctlUntil(t, "hang_up"); m["reason"] != "error" {
		t.Fatalf("hang_up = %v", m)
	}
	select {
	case <-e.ai.conns:
		t.Fatal("redialed OpenAI after a drop")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestRotateSession(t *testing.T) {
	set(t, &rotateMargin, 60*time.Minute-300*time.Millisecond) // OpenAI's MaxAge is 60 minutes
	e := start(t, false, "")
	ph, ai1 := e.inCall(t)
	ai1.push(t, `{"type":"response.output_audio_transcript.done","transcript":"八个窗格在跑"}`)
	if m := ph.ctlUntil(t, "transcript"); m["text"] != "八个窗格在跑" || m["who"] != "assistant" {
		t.Fatalf("transcript = %v", m)
	}
	ai2 := e.ai.accept(t)
	instr := ai2.next(t, "session.update")["session"].(map[string]any)["instructions"].(string)
	if !strings.HasPrefix(instr, "base prompt") || !strings.Contains(instr, "八个窗格在跑") {
		t.Fatalf("new instructions = %q", instr)
	}
	ai1.waitClosed(t)
	ph.audio(t, pattern(960, 3))
	ai2.next(t, "input_audio_buffer.append")
}

// Protocol section 3: an unknown provider is refused before anything is dialed and the call stays ended.
func TestCallStartUnknownProvider(t *testing.T) {
	e := start(t, false, "")
	ph := e.ready(t)
	ph.ctl(t, map[string]any{"t": "call.start", "provider": "nope"})
	for {
		typ, plain := ph.next(t)
		if typ != pair.TypeControl {
			continue
		}
		var m map[string]any
		json.Unmarshal(plain, &m)
		if m["t"] != "error" || m["msg"] != "不支持的供应商: nope" {
			t.Fatalf("first control after call.start = %s", plain)
		}
		break
	}
	select {
	case <-e.ai.conns:
		t.Fatal("dialed a provider for an unknown one")
	case <-time.After(200 * time.Millisecond):
	}
	ph.ctl(t, map[string]any{"t": "call.start"}) // not stuck in a call: a valid start dials
	e.ai.accept(t)
	ph.expectStatus(t, "connecting")
}

// syncBuf is a log sink the test can read while Run writes to it.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *syncBuf) Write(p []byte) (int, error) { w.mu.Lock(); defer w.mu.Unlock(); return w.b.Write(p) }
func (w *syncBuf) String() string              { w.mu.Lock(); defer w.mu.Unlock(); return w.b.String() }

// call.start's api_key wins over config.json's key, only for that call, and is never logged.
func TestCallStartAPIKeyOverridesConfig(t *testing.T) {
	logs := &syncBuf{}
	e := startWith(t, false, "", relay.NewHub(), func(c *Config) {
		c.Logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	ph := e.ready(t)
	for _, tc := range []struct{ apiKey, want string }{{"phone-key-123", "Bearer phone-key-123"}, {"", "Bearer test-key"}} {
		ph.ctl(t, map[string]any{"t": "call.start", "provider": "openai", "api_key": tc.apiKey})
		ai := e.ai.accept(t)
		if ai.auth != tc.want {
			t.Fatalf("api_key %q: Authorization = %q, want %q", tc.apiKey, ai.auth, tc.want)
		}
		ph.expectStatus(t, "connecting", "listening")
		ph.ctl(t, map[string]any{"t": "call.end"})
		ph.expectStatus(t, "ended")
		ai.waitClosed(t)
	}
	if strings.Contains(logs.String(), "phone-key-123") {
		t.Fatal("api_key written to the log")
	}
}

func TestEcho(t *testing.T) {
	e := start(t, true, "")
	ph := e.ready(t)
	ph.ctl(t, map[string]any{"t": "call.start"})
	ph.expectStatus(t, "listening")
	var sent [][]byte
	for i := range 3 {
		f := pattern(960, byte(i*10))
		sent = append(sent, f)
		ph.audio(t, f)
	}
	for i := 0; i < 3; {
		typ, plain := ph.next(t)
		if typ != pair.TypeAudio {
			continue
		}
		if !bytes.Equal(plain, sent[i]) {
			t.Fatalf("echo frame %d differs", i)
		}
		i++
	}
	for {
		if m := ph.ctlUntil(t, "transcript"); m["text"] == "echo: 收到 3 帧" {
			break
		}
	}
	ph.ctl(t, map[string]any{"t": "call.end"})
	if m := ph.ctlUntil(t, "hang_up"); m["reason"] != "user" {
		t.Fatalf("hang_up = %v", m)
	}
}

// appends collects the bytes of every input_audio_buffer.append the Mac sends during d.
func (c *aiConn) appends(t *testing.T, d time.Duration) [][]byte {
	t.Helper()
	var out [][]byte
	deadline := time.After(d)
	for {
		select {
		case ev := <-c.got:
			if ev["type"] == "input_audio_buffer.append" {
				b, _ := base64.StdEncoding.DecodeString(ev["audio"].(string))
				out = append(out, b)
			}
		case <-deadline:
			return out
		}
	}
}

// Protocol section 6: after the phone stops sending, the Mac feeds fillFor of zero PCM, then stops.
func TestSilenceFillAfterPhoneStops(t *testing.T) {
	set(t, &fillFor, 200*time.Millisecond)
	set(t, &fillAfter, 40*time.Millisecond)
	e := start(t, false, "")
	ph, ai := e.inCall(t)
	if got := ai.appends(t, 100*time.Millisecond); len(got) != 0 {
		t.Fatalf("%d appends before the first real frame", len(got))
	}
	real := map[string]bool{}
	for i := range 10 {
		f := pattern(960, byte(i+1))
		real[string(f)] = true
		ph.audio(t, f)
	}
	got := ai.appends(t, 600*time.Millisecond)
	var nReal, nZero int
	for _, b := range got {
		switch {
		case real[string(b)]:
			nReal++
		case len(b) == frameBytes && bytes.Count(b, []byte{0}) == frameBytes:
			nZero++
		default:
			t.Fatalf("append of %d bytes is neither a phone frame nor zero PCM", len(b))
		}
	}
	// 200 ms / 20 ms = 10 fill frames; a 600 ms window without the cutoff would hold ~30.
	if nReal != 10 || nZero < 3 || nZero > 12 {
		t.Fatalf("real %d, zero %d", nReal, nZero)
	}
	if more := ai.appends(t, 300*time.Millisecond); len(more) != 0 {
		t.Fatalf("%d appends after fillFor", len(more))
	}
}

func TestEchoNoSilenceFill(t *testing.T) {
	set(t, &fillFor, 200*time.Millisecond)
	set(t, &fillAfter, 40*time.Millisecond)
	e := start(t, true, "")
	ph := e.ready(t)
	ph.ctl(t, map[string]any{"t": "call.start"})
	ph.expectStatus(t, "listening")
	for i := range 10 {
		ph.audio(t, pattern(960, byte(i+1)))
	}
	n := 0
	// echo transcripts arrive every echoReport, so next never blocks past the deadline for long
	for deadline := time.Now().Add(echoDelay + 400*time.Millisecond); time.Now().Before(deadline); {
		if typ, plain := ph.next(t); typ == pair.TypeAudio {
			if bytes.Count(plain, []byte{0}) == len(plain) {
				t.Fatal("zero PCM echoed to the phone")
			}
			n++
		}
	}
	if n != 10 {
		t.Fatalf("phone got %d audio frames, want 10", n)
	}
}

func TestKeepalive(t *testing.T) {
	set(t, &keepaliveEvery, 50*time.Millisecond)
	e := start(t, true, "")
	ph := e.dialPhone(t)
	for {
		if f := ph.raw(t); f.typ == websocket.BinaryFrame && bytes.Equal(f.data, pair.KeepaliveFrame()) {
			return
		}
	}
}

// A relay that fails the upgrade (5xx) is retried with backoff until it works.
func TestRetryAfterUpgradeFailure(t *testing.T) {
	hub := relay.NewHub()
	var macTries atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("role") == "mac" && macTries.Add(1) <= 2 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		hub.ServeHTTP(w, r)
	})
	e := startWith(t, true, "", h)
	ph := e.ready(t)
	ph.ctl(t, map[string]any{"t": "call.start"})
	ph.expectStatus(t, "listening")
	if n := macTries.Load(); n < 3 {
		t.Fatalf("mac tries = %d", n)
	}
}

// pushRecorder stands in for APNs behind the relay: it records every push the Mac's notify causes.
type pushRecorder struct{ pushes chan string }

func (r *pushRecorder) Push(_ context.Context, _, title, _ string) error {
	r.pushes <- title
	return nil
}

// While the call is still dialing the provider there is no session to tell, and the user is already on
// the phone: the event is dropped (no push, nothing replayed once the session is up).
func TestBlockedClaudeWhileDialingNotPushed(t *testing.T) {
	var blocked atomic.Bool
	hub, rec := relay.NewHub(), &pushRecorder{pushes: make(chan string, 4)}
	hub.Notifier = rec
	e := startWith(t, false, "", hub, withWatch(t, &blocked))
	e.ai.holdUpdated.Store(true)
	ph := e.ready(t)
	if err := websocket.Message.Send(ph.ws, `{"t":"push_token","token":"`+strings.Repeat("ab", 32)+`"}`); err != nil {
		t.Fatal(err)
	}
	ph.ctl(t, map[string]any{"t": "call.start"})
	ai := e.ai.accept(t)
	ai.next(t, "session.update") // the Mac now waits for session.updated: sess == nil
	blocked.Store(true)
	time.Sleep(5 * watchEvery)
	ai.push(t, `{"type":"session.updated"}`)
	ph.expectStatus(t, "connecting", "listening")
	select {
	case title := <-rec.pushes:
		t.Fatalf("pushed while dialing: %q", title)
	case ev := <-ai.got:
		if ev["type"] == "conversation.item.create" {
			t.Fatalf("blocked event replayed after dial: %v", ev)
		}
	case <-time.After(5 * watchEvery):
	}
	blocked.Store(false) // the watcher itself still works once in the call
	time.Sleep(3 * watchEvery)
	blocked.Store(true)
	ai.next(t, "conversation.item.create")
}
