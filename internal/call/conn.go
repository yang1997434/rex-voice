// Package call is the Mac-side session layer (docs/protocol.md sections 2, 3 and 6): it keeps a
// relay connection up, does the per-connection salt handshake and the one-phone hello check, and
// runs the voice call that bridges phone audio and a realtime voice provider session (internal/voice).
package call

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/relay"
	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
)

type Config struct {
	Pairing     *pair.Pairing
	PairingPath string // where Pairing is saved when the first hello records its device_id
	RelayURL    string // e.g. wss://r.maruiao.top/_voice/ws
	// Voice is config.json's provider, model and voice (empty Provider = openai); call.start's
	// provider / model / voice / api_key override them for one call. Model, Voice and Key only
	// apply while the call uses Voice.Provider.
	Voice voice.Config
	// Key, if set, is the fallback key of a provider when call.start brings none (the Keychain).
	// Called off the Run goroutine; its result is never logged.
	Key   func(p voice.Provider) string
	Tools *tools.Registry // Run registers hang_up into it; must not already contain one. Unused with Echo.
	Echo  bool            // no provider: echo phone audio back after 1 s (iOS bring-up)
	// Mac is Exec / Home for show_file and the Claude watch; zero value = real commands.
	Mac tools.Options
	// Watch polls Rex for Claude panes turning blocked and tells the user (in the call, or by a
	// notify text frame to the relay). Ignored with Echo.
	Watch   bool
	Logger  *slog.Logger
	Version string

	// Instructions, if set, replaces Voice.Instructions and is called off the Run goroutine at every
	// call.start and session rotation, so the prompt sees what is running now.
	Instructions func() string
}

// Timings; tests shorten them.
var (
	tickEvery      = time.Second
	keepaliveEvery = 30 * time.Second
	idlePrompt     = 60 * time.Second
	idleHangup     = 90 * time.Second
	rotateMargin   = 5 * time.Minute // rotate this long before the provider's MaxAge
	echoDelay      = time.Second
	echoReport     = 5 * time.Second
	backoffMin     = time.Second
	backoffMax     = 30 * time.Second
	writeTimeout   = 10 * time.Second
	dialTimeout    = 15 * time.Second
	previewTimeout = 20 * time.Second       // a whole voice.preview, dial included
	fillEvery      = 20 * time.Millisecond  // silence fill: check interval
	fillAfter      = 200 * time.Millisecond // silence fill: start only after this gap, so relay jitter does not splice zeros into speech
	fillFor        = 5 * time.Second        // silence fill: stop this long after the last real frame
	hangupGrace    = 8 * time.Second        // hang_up waits at most this long for the model's goodbye (TurnDone)
	watchEvery     = 5 * time.Second        // Claude watch poll interval
	notifyEvery    = 10 * time.Minute       // at most one notify per pane in this window
)

type server struct {
	cfg    Config
	log    *slog.Logger
	ctx    context.Context
	events chan func()
	wsURL  string

	// Everything below is owned by the Run goroutine.
	ws         *websocket.Conn
	lastWrite  time.Time
	macSalt    [16]byte
	seen       map[[16]byte]bool // phone salts already used with macSalt; nil until the peer is present
	rx, tx     *pair.Cipher      // nil until both salts are known
	helloOK    bool
	call       *callState
	preview    *previewState // nil unless a voice.preview is running
	nextSID    int
	nextAttach uint32
	notified   map[string]time.Time // block -> last notify sent to the relay
}

// Run keeps the Mac connected to the relay and serves calls until ctx is done; it returns ctx.Err().
func Run(ctx context.Context, cfg Config) error {
	if cfg.Pairing == nil || cfg.RelayURL == "" || (!cfg.Echo && cfg.Tools == nil) {
		return errors.New("call: Pairing, RelayURL and (without Echo) Tools are required")
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &server{cfg: cfg, log: log, ctx: ctx, events: make(chan func(), 512),
		wsURL: cfg.RelayURL + "?role=mac&id=" + base64.RawURLEncoding.EncodeToString(cfg.Pairing.ID[:])}
	if !cfg.Echo {
		cfg.Tools.Register(tools.HangUpDef(func(reason string) {
			s.post(func() { s.onHangUpTool(reason) })
		}))
		cfg.Tools.Register(tools.ShowFileDef(cfg.Mac, s.showFile))
		cfg.Tools.Register(tools.ShowLinkDef(s.showLink))
		s.cfg.Voice.Tools = cfg.Tools.Defs()
		s.cfg.Voice.Call = s.callTool
		s.cfg.Voice.Version = cfg.Version
		if s.cfg.Voice.Provider == "" {
			s.cfg.Voice.Provider = voice.OpenAI
		}
	}

	if cfg.Watch && !cfg.Echo {
		s.notified = map[string]time.Time{}
		go tools.Watch(ctx, cfg.Mac, watchEvery, func(b tools.Blocked) { s.post(func() { s.onBlocked(b) }) })
	}

	dialDone := make(chan struct{})
	go func() { defer close(dialDone); s.dialLoop() }()
	tick := time.NewTicker(tickEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.endCall("error")
			s.stopPreview()
			if s.ws != nil {
				s.ws.Close()
			}
			<-dialDone
			return ctx.Err()
		case f := <-s.events:
			f()
		case now := <-tick.C:
			s.onTick(now)
		case now := <-s.fillTick():
			s.onFillTick(now)
		}
	}
}

// post queues f for the Run goroutine; false once Run is stopping.
func (s *server) post(f func()) bool {
	select {
	case s.events <- f:
		return true
	case <-s.ctx.Done():
		return false
	}
}

// dialLoop connects, reads until the connection drops, and reconnects with 1 s → 30 s backoff.
// A connection that stayed up longer than the max backoff resets the backoff.
func (s *server) dialLoop() {
	backoff := backoffMin
	for s.ctx.Err() == nil {
		t0 := time.Now()
		ws, err := s.dial()
		if err != nil {
			s.log.Warn("relay dial failed", "err", err, "retry_in", backoff)
		} else {
			s.log.Info("connected relay", "url", s.cfg.RelayURL)
			stop := context.AfterFunc(s.ctx, func() { ws.Close() })
			if s.post(func() { s.connUp(ws) }) {
				s.read(ws)
				s.post(func() { s.connDown(ws) })
			}
			stop()
			ws.Close()
			if time.Since(t0) > backoffMax {
				backoff = backoffMin
			}
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, backoffMax)
	}
}

func (s *server) dial() (*websocket.Conn, error) {
	// x/net/websocket wants an Origin; the relay does not check it.
	origin := "http" + strings.TrimPrefix(s.cfg.RelayURL, "ws")
	wc, err := websocket.NewConfig(s.wsURL, origin)
	if err != nil {
		return nil, err
	}
	wc.Header.Set("User-Agent", "rex-voiced/"+s.cfg.Version)
	ctx, cancel := context.WithTimeout(s.ctx, dialTimeout)
	defer cancel()
	ws, err := wc.DialContext(ctx) // a 4xx/5xx upgrade response is an error here, so it is retried like any other
	if err != nil {
		return nil, err
	}
	ws.MaxPayloadBytes = relay.MaxFrameBytes
	return ws, nil
}

type wsFrame struct {
	data []byte
	typ  byte
}

var frameCodec = websocket.Codec{Unmarshal: func(data []byte, typ byte, v any) error {
	f := v.(*wsFrame)
	f.data, f.typ = data, typ
	return nil
}}

func (s *server) read(ws *websocket.Conn) {
	for {
		var f wsFrame
		if err := frameCodec.Receive(ws, &f); err != nil {
			s.log.Info("relay connection closed", "err", err)
			return
		}
		if !s.post(func() { s.onFrame(ws, f) }) {
			return
		}
	}
}

func (s *server) connUp(ws *websocket.Conn) {
	if s.ws != nil {
		s.ws.Close()
	}
	s.ws, s.lastWrite = ws, time.Now()
	s.resetPeer()
}

func (s *server) connDown(ws *websocket.Conn) {
	if s.ws == ws {
		s.ws = nil
		s.resetPeer()
	}
}

// resetPeer forgets the phone's salt and keys; the call (if any) survives so a phone that
// reconnects quickly can carry on, and the idle timer ends it otherwise. A voice preview does not.
func (s *server) resetPeer() {
	s.stopPreview()
	s.seen, s.rx, s.tx, s.helloOK = nil, nil, nil, false
}

func (s *server) onFrame(ws *websocket.Conn, f wsFrame) {
	if ws != s.ws {
		return
	}
	if f.typ == websocket.TextFrame {
		var m struct {
			T       string `json:"t"`
			Present bool   `json:"present"`
		}
		if err := json.Unmarshal(f.data, &m); err != nil || m.T != "peer" {
			s.log.Warn("unknown relay text frame", "bytes", len(f.data))
			return
		}
		s.onPeer(m.Present)
		return
	}
	if len(f.data) == 0 {
		return
	}
	switch f.data[0] {
	case pair.TypeKeepalive:
	case pair.TypeSalt:
		salt, ok := pair.ParseSaltFrame(f.data)
		if !ok {
			s.log.Warn("bad salt frame dropped", "bytes", len(f.data))
			return
		}
		s.onPeerSalt(salt)
	case pair.TypeAudio, pair.TypeControl:
		if s.rx == nil {
			s.log.Debug("frame before salt exchange dropped", "type", f.data[0])
			return
		}
		typ, _, plain, err := s.rx.Open(f.data)
		if errors.Is(err, pair.ErrReplay) {
			s.log.Warn("replayed frame, closing relay connection")
			s.ws.Close()
			return
		}
		if err != nil {
			s.log.Warn("frame failed authentication, dropped", "type", f.data[0])
			return
		}
		if typ == pair.TypeControl {
			s.onControl(plain)
		} else if s.helloOK {
			s.onPhoneAudio(plain)
		}
	default:
		s.log.Warn("unknown frame type dropped", "type", f.data[0])
	}
}

// onPeer starts a fresh salt per phone presence, so frames from an earlier connection can never decrypt.
// The relay may report present:true twice when both ends connect at once; until a phone salt has
// arrived the same Mac salt is resent instead, or the phone would key on a salt we already dropped.
func (s *server) onPeer(present bool) {
	if present && s.seen != nil && len(s.seen) == 0 {
		s.writeRaw(pair.SaltFrame(s.macSalt))
		return
	}
	s.resetPeer()
	if !present {
		s.log.Info("phone not connected")
		return
	}
	if _, err := rand.Read(s.macSalt[:]); err != nil {
		panic(err) // crypto/rand does not fail on macOS
	}
	s.seen = map[[16]byte]bool{}
	s.writeRaw(pair.SaltFrame(s.macSalt))
}

func (s *server) onPeerSalt(salt [16]byte) {
	switch {
	case s.seen == nil:
		s.log.Warn("salt before peer present, dropped")
		return
	case s.seen[salt]:
		// Re-deriving would restart seq at 1 under the same key and let old frames replay.
		s.log.Debug("repeated phone salt ignored")
		return
	}
	s.seen[salt] = true
	k := s.cfg.Pairing.Key
	s.rx = pair.NewCipher(k, pair.PhoneToMac, salt, s.macSalt)
	s.tx = pair.NewCipher(k, pair.MacToPhone, salt, s.macSalt)
	s.helloOK = false
	s.log.Info("salt exchanged")
}

func (s *server) onControl(plain []byte) {
	var m struct {
		T        string `json:"t"`
		App      string `json:"app"`
		Device   string `json:"device"`
		DeviceID string `json:"device_id"`
		startReq
	}
	if err := json.Unmarshal(plain, &m); err != nil {
		s.log.Warn("bad control frame", "err", err)
		return
	}
	if m.T == "hello" {
		s.onHello(m.DeviceID, m.App, m.Device)
		return
	}
	if !s.helloOK {
		s.log.Warn("control before hello dropped", "t", m.T)
		return
	}
	switch m.T {
	case "call.start":
		s.startCall(m.startReq)
	case "voice.preview":
		s.startPreview(m.startReq)
	case "call.end":
		s.endCall("user")
	default:
		s.log.Debug("unknown control", "t", m.T)
	}
}

func (s *server) onHello(deviceID, app, device string) {
	p := s.cfg.Pairing
	switch {
	case deviceID == "":
		s.reject("hello 缺少 device_id")
		return
	case p.DeviceID == "":
		p.DeviceID = deviceID
		p.MarkPaired()
		if err := p.Save(s.cfg.PairingPath); err != nil {
			s.log.Error("pairing not saved; this phone is bound only until restart", "err", err)
		}
		s.log.Info("phone paired", "device_id", deviceID, "device", device)
	case p.DeviceID != deviceID:
		s.log.Warn("hello from a different phone rejected", "device_id", deviceID, "device", device)
		s.reject("这台手机没有和这台 Mac 配对；要换手机，在 Mac 上重新 rex-voiced pair")
		return
	}
	s.helloOK = true
	s.log.Info("phone hello", "app", app, "device", device)
	if c := s.call; c != nil { // phone reconnected mid-call
		s.sendCtl(map[string]any{"t": "status", "state": c.state})
	}
}

func (s *server) reject(msg string) {
	s.sendCtl(map[string]any{"t": "error", "msg": msg})
	s.ws.Close()
}

func (s *server) writeRaw(b []byte) { s.write(b) }

// writeText sends a plaintext JSON text frame to the relay itself (protocol section 2: notify).
func (s *server) writeText(v any) {
	if s.ws == nil {
		s.log.Warn("relay not connected, text frame dropped")
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only maps of strings are sent
	}
	s.write(string(b))
}

// write sends msg as a binary ([]byte) or text (string) frame.
func (s *server) write(msg any) {
	if s.ws == nil {
		return
	}
	s.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
	err := websocket.Message.Send(s.ws, msg)
	s.ws.SetWriteDeadline(time.Time{}) // pongs are written from inside Receive; a stale deadline would fail them
	if err != nil {
		s.log.Warn("relay write failed, closing", "err", err)
		s.ws.Close()
		return
	}
	s.lastWrite = time.Now()
}

func (s *server) sendCtl(v any) {
	if s.tx == nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only maps of strings are sent
	}
	s.writeRaw(s.tx.Seal(pair.TypeControl, b))
}

func (s *server) sendAudio(pcm []byte) {
	if s.tx != nil {
		s.writeRaw(s.tx.Seal(pair.TypeAudio, pcm))
	}
}
