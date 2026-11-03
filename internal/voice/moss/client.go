package moss

// Wire format: JSON control events in text frames (client events carry "action", server events
// put their fields under "data"), audio in raw PCM16 binary frames: 16 kHz up, 48 kHz down, each
// downlink frame announced by a response.audio.context event. MOSS wants playback receipts so it
// knows how much of an interrupted answer was heard.

import (
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/voice"
)

const defaultURL = "wss://api.mosi.cn/v1/realtime"

// Variables so tests can shorten them.
var (
	readyTimeout = 10 * time.Second
	writeTimeout = 10 * time.Second
	toolTimeout  = 20 * time.Second
	fillEvery    = 20 * time.Millisecond
	bytesPerMs   = 96.0 // PCM16 mono 48 kHz: playback time of a downlink chunk
)

const (
	maxTurns       = 50
	maxSnapshot    = 6000 // runes of the opening snapshot in instructions
	hangUpReason   = `{"reason":"用户说挂断"}`
	snapshotPrefix = "现在 Mac 上 Rex 各窗格和 Claude 的状态（通话开始时的快照，之后不会更新）："
)

var silence = make([]byte, 640) // 20 ms at 16 kHz

// hangUpWords in the user's final transcript hang up: MOSS has no tools to call hang_up itself.
var hangUpWords = []string{"挂断", "挂了", "拜拜"}

type Session struct {
	cfg    voice.Config
	log    *slog.Logger
	ws     *websocket.Conn
	start  time.Time
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wmu    sync.Mutex

	mu     sync.Mutex
	closed bool
	turns  []voice.Turn

	amu  sync.Mutex // keeps Downsampler state, mute and append order together
	ds   voice.Downsampler
	fill chan struct{} // non-nil while muted: closing it stops the silence filler

	// Owned by the event goroutine.
	cur    *ids            // the response.audio.context waiting for its binary frame
	runs   map[string]*run // run_id -> playback state
	clock  time.Time       // when the last scheduled chunk ends playing
	hungUp bool
}

type ids struct {
	ChunkID json.RawMessage `json:"chunk_id"`
	RunID   json.RawMessage `json:"run_id"`
	TurnID  json.RawMessage `json:"turn_id"`
}

type chunk struct {
	start time.Time
	dur   time.Duration
	t     *time.Timer
}

type run struct {
	chunks []chunk
	end    time.Time   // when its last chunk ends playing
	done   *time.Timer // playback.completed
	cut    bool        // interrupted or stopped: later audio is dropped
}

// Dial snapshots Rex, connects, attaches a new session, sends session.update and returns once it is accepted.
func Dial(ctx context.Context, cfg voice.Config) (voice.Session, error) {
	s, err := dial(ctx, cfg)
	if err != nil {
		return nil, err // not a typed nil inside the interface
	}
	return s, nil
}

func dial(ctx context.Context, cfg voice.Config) (*Session, error) {
	if cfg.TextOnly {
		return nil, errors.New("moss 不支持文字模式（没有文字输入）")
	}
	if cfg.Key == "" || cfg.Model == "" {
		return nil, errors.New("moss: Key and Model are required")
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	u := cfg.Extra["url"]
	if u == "" {
		u = defaultURL
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	instructions := snapshot(ctx, cfg.Call) // before connecting: the tools may take a while
	wc, err := websocket.NewConfig(u+"?"+url.Values{"model": {cfg.Model}}.Encode(), "https://api.mosi.cn")
	if err != nil {
		return nil, fmt.Errorf("moss: %w", err)
	}
	wc.Header.Set("Authorization", "Bearer "+cfg.Key)
	wc.Header.Set("User-Agent", "rex-voiced/"+cfg.Version)
	ws, err := wc.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("moss: dial: %w", err)
	}
	sctx, cancel := context.WithCancel(context.Background())
	s := &Session{cfg: cfg, log: log, ws: ws, start: time.Now(), ctx: sctx, cancel: cancel,
		done: make(chan struct{}), runs: map[string]*run{}}
	if err := s.setup(ctx, instructions); err != nil {
		s.Close() // still writes the usage line
		return nil, err
	}
	go s.loop()
	return s, nil
}

// snapshot is the instructions: Rex's panes and Claudes at call start, since MOSS cannot look later.
func snapshot(ctx context.Context, call func(context.Context, string, string) string) string {
	if call == nil {
		return snapshotPrefix + "拿不到快照"
	}
	var parts []string
	for _, tool := range []string{"list_panes", "list_claude"} {
		tctx, cancel := context.WithTimeout(ctx, toolTimeout)
		out := call(tctx, tool, "{}")
		cancel()
		if strings.HasPrefix(out, `{"error"`) {
			return snapshotPrefix + "拿不到快照"
		}
		parts = append(parts, tool+"："+out)
	}
	t := snapshotPrefix + strings.Join(parts, "\n")
	if utf8.RuneCountInString(t) > maxSnapshot {
		t = string([]rune(t)[:maxSnapshot])
	}
	return t
}

func (s *Session) setup(ctx context.Context, instructions string) error {
	deadline := time.Now().Add(readyTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := s.ws.SetReadDeadline(deadline); err != nil {
		return err
	}
	if err := s.send(map[string]any{"action": "attach_session"}); err != nil {
		return fmt.Errorf("moss: attach_session: %w", err)
	}
	if _, err := s.await("session_attached"); err != nil {
		return fmt.Errorf("moss: waiting for session_attached: %w", err)
	}
	persona := map[string]any{"name": "Rex", "aliases": []string{}, "prompt": voice.BuildChat(voice.NameOf(s.cfg.Instructions))}
	if err := s.send(map[string]any{"action": "session.update", "session": map[string]any{"persona": persona, "instructions": instructions}}); err != nil {
		return fmt.Errorf("moss: session.update: %w", err)
	}
	f, err := s.await("session.updated")
	if err != nil {
		return fmt.Errorf("moss: waiting for session.updated: %w", err)
	}
	if e := errText(f.Error); e != "" {
		return fmt.Errorf("moss: session.update rejected: %s", e)
	}
	if err := s.send(map[string]any{"action": "mic_state", "enabled": true}); err != nil {
		return fmt.Errorf("moss: mic_state: %w", err)
	}
	return s.ws.SetReadDeadline(time.Time{})
}

// await reads until the named server event; an error event fails it.
func (s *Session) await(want string) (fields, error) {
	for {
		typ, b, err := readFrame(s.ws)
		if err != nil {
			return fields{}, err
		}
		if typ != websocket.TextFrame {
			continue
		}
		s.log.Debug("moss: raw", "raw", json.RawMessage(b))
		name, f, err := parse(b)
		switch {
		case err != nil:
			s.log.Warn("moss: bad event", "err", err)
		case name == want:
			return f, nil
		case name == "error":
			return f, errors.New(errText(f.Error))
		default:
			s.log.Debug("moss: event before "+want, "type", name, "bytes", len(b))
		}
	}
}

// Age is the time since Dial.
func (s *Session) Age() time.Duration { return time.Since(s.start) }

func (s *Session) MaxAge() time.Duration { return 0 }

// Done is closed when the event loop has stopped (read error or Close).
func (s *Session) Done() <-chan struct{} { return s.done }

// RecentTurns returns up to n of the latest user/assistant texts, oldest first.
func (s *Session) RecentTurns(n int) []voice.Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > len(s.turns) {
		n = len(s.turns)
	}
	return append([]voice.Turn(nil), s.turns[len(s.turns)-n:]...)
}

func (s *Session) addTurn(role, text string) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	s.mu.Lock()
	s.turns = append(s.turns, voice.Turn{Role: role, Text: text})
	if len(s.turns) > maxTurns {
		s.turns = s.turns[len(s.turns)-maxTurns:]
	}
	s.mu.Unlock()
}

// AppendAudio takes PCM16 24 kHz and sends it as a 16 kHz binary frame; dropped while muted
// (the silence filler is sending then).
func (s *Session) AppendAudio(pcm []byte) error {
	s.amu.Lock()
	defer s.amu.Unlock()
	if s.fill != nil {
		return nil
	}
	out, err := s.ds.Write(pcm)
	if err != nil || len(out) == 0 {
		return err
	}
	return s.sendBin(out)
}

// SetMuted(true) keeps 20 ms of silence going every 20 ms: MOSS wants audio in real time even when
// the user is quiet. SetMuted(false) stops it.
func (s *Session) SetMuted(muted bool) error {
	s.amu.Lock()
	defer s.amu.Unlock()
	if muted == (s.fill != nil) {
		return nil
	}
	if !muted {
		close(s.fill)
		s.fill = nil
		s.ds = voice.Downsampler{} // the history before the pause is stale
		return nil
	}
	s.fill = make(chan struct{})
	go s.fillSilence(s.fill)
	return nil
}

func (s *Session) fillSilence(stop <-chan struct{}) {
	t := time.NewTicker(fillEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-s.ctx.Done():
			return
		case <-t.C:
			if err := s.sendBin(silence); err != nil {
				s.log.Debug("moss: silence fill stopped", "err", err)
				return
			}
		}
	}
}

// SendText fails: MOSS-Live has no text input. The session layer falls back to a transcript.
func (s *Session) SendText(string) error { return errors.New("moss 没有文字输入") }

// Cancel is a no-op: there is no cancel event; the user talking over the answer interrupts it.
func (s *Session) Cancel() error { return nil }

// Close ends the session and writes its one usage line.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	secs := time.Since(s.start).Seconds()
	s.mu.Unlock()
	s.writeUsage(secs)
	s.cancel()
	return s.ws.Close()
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Session) send(ev map[string]any) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return s.write(string(b))
}

func (s *Session) sendBin(pcm []byte) error { return s.write(pcm) }

// write sends a string as a text frame, []byte as a binary frame.
func (s *Session) write(v any) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.ws.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	// Clear it afterwards: the read loop writes pongs from inside HandleFrame and would hit a stale deadline.
	defer s.ws.SetWriteDeadline(time.Time{})
	return websocket.Message.Send(s.ws, v)
}

// sendLater is send from a timer: nothing once the session is closed.
func (s *Session) sendLater(ev map[string]any) {
	if s.isClosed() {
		return
	}
	if err := s.send(ev); err != nil {
		s.log.Debug("moss: receipt not sent", "action", ev["action"], "err", err)
	}
}

type closeError struct {
	Code   int
	Reason string
}

func (e *closeError) Error() string {
	return fmt.Sprintf("服务端关闭连接 %d: %s", e.Code, e.Reason)
}

// readFrame returns the next data frame and its payload type; a close frame's code and reason
// are kept (x/net/websocket throws them away as io.EOF). Only one goroutine reads at a time.
func readFrame(ws *websocket.Conn) (byte, []byte, error) {
	for {
		fr, err := ws.NewFrameReader()
		if err != nil {
			return 0, nil, err
		}
		typ := fr.PayloadType()
		if typ == websocket.CloseFrame {
			b, _ := io.ReadAll(io.LimitReader(fr, 125))
			if len(b) < 2 {
				return 0, nil, io.EOF
			}
			return 0, nil, &closeError{int(binary.BigEndian.Uint16(b)), string(b[2:])}
		}
		if fr, err = ws.HandleFrame(fr); err != nil {
			return 0, nil, err
		}
		if fr != nil {
			b, err := io.ReadAll(io.LimitReader(fr, websocket.DefaultMaxPayloadBytes))
			return typ, b, err
		}
	}
}

// fields are a server event's payload, read from "data" (and the top level, in case some events are flat).
type fields struct {
	ids
	SessionID string          `json:"session_id"`
	Text      string          `json:"text"`
	Status    string          `json:"status"`
	Error     json.RawMessage `json:"error"` // a string in session.updated, {code, message} in error
}

// parse returns the event name and fields. The docs do not pin the name's key: it is tried as
// type, event and action (not confirmed against the live server).
func parse(b []byte) (string, fields, error) {
	var head struct {
		Type, Event, Action string
		Data                json.RawMessage
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return "", fields{}, err
	}
	var f fields
	_ = json.Unmarshal(b, &f) // flat fields, best effort
	if len(head.Data) > 0 && head.Data[0] == '{' {
		if err := json.Unmarshal(head.Data, &f); err != nil {
			return "", f, err
		}
	}
	return cmp.Or(head.Type, head.Event, head.Action), f, nil
}

// errText renders an error field: "" for none or an empty string.
func errText(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var e struct{ Code, Message string }
	if json.Unmarshal(raw, &e) == nil && (e.Code != "" || e.Message != "") {
		return e.Code + ": " + e.Message
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return string(raw)
}

func (s *Session) loop() {
	defer close(s.done)
	h := s.cfg.Handlers
	for {
		typ, b, err := readFrame(s.ws)
		if err != nil {
			if !s.isClosed() {
				if h.Error != nil {
					h.Error(fmt.Errorf("moss: read: %w", err))
				}
				s.Close()
			}
			return
		}
		if typ == websocket.BinaryFrame {
			s.onAudio(b)
			continue
		}
		s.log.Debug("moss: raw", "raw", json.RawMessage(b)) // text events only; the wire field names are not confirmed yet
		name, f, err := parse(b)
		if err != nil {
			s.log.Warn("moss: bad event", "err", err)
			continue
		}
		switch name {
		case "response.audio.context":
			c := f.ids
			s.cur = &c
		case "input.transcript.done":
			s.addTurn("user", f.Text)
			if h.UserText != nil {
				h.UserText(f.Text)
			}
			s.maybeHangUp(f.Text)
		case "output.transcript.done":
			s.addTurn("assistant", f.Text)
			if h.AssistantText != nil {
				h.AssistantText(f.Text)
			}
		case "response.audio.done":
			s.onAudioDone(f.ids)
		case "response.audio.stop":
			s.onStop(f.ids)
			if h.SpeechStarted != nil {
				h.SpeechStarted()
			}
		case "response.interrupted":
			s.run(f.RunID).cut = true
		case "response.ended":
			if f.Status != "completed" {
				s.log.Info("moss: response not completed", "status", f.Status)
			}
			delete(s.runs, string(f.RunID)) // its receipt timers still fire
			if h.ResponseDone != nil {
				h.ResponseDone(voice.Usage{Unpriced: true})
			}
			if h.TurnDone != nil {
				h.TurnDone()
			}
		case "error":
			if h.Error != nil {
				h.Error(fmt.Errorf("moss: %s", errText(f.Error)))
			}
		default:
			s.log.Debug("moss: event", "type", name, "bytes", len(b))
		}
	}
}

func (s *Session) run(id json.RawMessage) *run {
	r := s.runs[string(id)]
	if r == nil {
		r = &run{}
		s.runs[string(id)] = r
	}
	return r
}

// onAudio plays one downlink frame: 48 kHz from MOSS, 24 kHz to the session layer.
func (s *Session) onAudio(b []byte) {
	c := s.cur
	s.cur = nil
	if c == nil {
		s.log.Warn("moss: audio frame without response.audio.context, dropped", "bytes", len(b))
		return
	}
	r := s.run(c.RunID)
	if r.cut {
		return
	}
	pcm, err := voice.Down48to24(b)
	if err != nil {
		s.log.Warn("moss: bad audio frame", "bytes", len(b), "err", err)
		return
	}
	s.schedule(r, *c, len(b))
	if h := s.cfg.Handlers.AudioDelta; h != nil {
		h(pcm)
	}
}

// schedule puts a chunk on the playback clock and sends its receipt when it should have finished.
// shortcut: 回执按 Mac 侧估算的播放时间发，不是手机真实播放；MOSS 只用它算 played_audio_ms
func (s *Session) schedule(r *run, c ids, n int) {
	now := time.Now()
	start := now
	if s.clock.After(now) {
		start = s.clock
	}
	dur := time.Duration(float64(n) / bytesPerMs * float64(time.Millisecond))
	s.clock, r.end = start.Add(dur), start.Add(dur)
	t := time.AfterFunc(r.end.Sub(now), func() {
		s.sendLater(map[string]any{"action": "playback.chunk.completed", "chunk_id": c.ChunkID, "run_id": c.RunID, "turn_id": c.TurnID})
	})
	r.chunks = append(r.chunks, chunk{start, dur, t})
}

// onAudioDone sends playback.completed when the run's last chunk should have finished.
func (s *Session) onAudioDone(c ids) {
	r := s.runs[string(c.RunID)]
	if r == nil || r.cut {
		return
	}
	r.done = time.AfterFunc(max(time.Until(r.end), 0), func() {
		s.sendLater(map[string]any{"action": "playback.completed", "run_id": c.RunID, "turn_id": c.TurnID})
	})
}

// onStop drops the rest of the run (the user is talking) and tells MOSS how much of it was heard.
func (s *Session) onStop(c ids) {
	now := time.Now()
	r := s.run(c.RunID)
	var played time.Duration
	for _, ch := range r.chunks {
		ch.t.Stop()
		played += min(max(now.Sub(ch.start), 0), ch.dur)
	}
	if r.done != nil {
		r.done.Stop()
	}
	r.chunks, r.done, r.cut = nil, nil, true
	if s.clock.After(now) {
		s.clock = now // nothing queued is going to play
	}
	if err := s.send(map[string]any{"action": "playback.stopped", "run_id": c.RunID, "turn_id": c.TurnID,
		"played_audio_ms": played.Milliseconds()}); err != nil {
		s.log.Warn("moss: playback.stopped not sent", "err", err)
	}
}

// maybeHangUp calls hang_up once per session when the user's final transcript asks for it; the
// session layer waits for TurnDone so the model's goodbye still plays.
func (s *Session) maybeHangUp(text string) {
	if s.hungUp || s.cfg.Call == nil {
		return
	}
	t := strings.Join(strings.Fields(text), "")
	for _, w := range hangUpWords {
		if strings.Contains(t, w) {
			s.hungUp = true
			go func() { // the tool posts to the session layer, which may be waiting on our handlers
				ctx, cancel := context.WithTimeout(s.ctx, toolTimeout)
				defer cancel()
				s.cfg.Call(ctx, "hang_up", hangUpReason)
			}()
			return
		}
	}
}
