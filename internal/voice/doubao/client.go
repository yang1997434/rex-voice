package doubao

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/voice"
)

const defaultURL = "wss://openspeech.bytedance.com/api/v3/duplex/realtime/dialogue"

// Variables so tests can shorten them.
var (
	readyTimeout = 10 * time.Second
	closeTimeout = 3 * time.Second
	writeTimeout = 10 * time.Second
	toolTimeout  = 20 * time.Second
)

const maxTurns = 50

// ErrNoTextInput: 3.0 only listens; a lone user conversation.item is silently dropped by the server
// (no reply), and speech_text_buffer.commit reads the text aloud instead of answering it.
var ErrNoTextInput = errors.New("doubao: 不支持文字输入（Realtime 3.0 只听语音）")

type toolCall struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Session struct {
	cfg       voice.Config
	log       *slog.Logger
	ws        *websocket.Conn
	start     time.Time
	id        string // session.id from session.created
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closedAck chan struct{} // closed on session.closed
	ackOnce   sync.Once
	wmu       sync.Mutex
	inflight  atomic.Int32 // tool batches running

	mu         sync.Mutex
	closing    bool
	muted      bool
	down       voice.Downsampler
	turns      []voice.Turn
	answering  bool // assistant audio is streaming
	cut        bool // the current answer was cancelled: drop its late audio until the next output_audio.started
	cancelSent bool // response.cancel not yet acknowledged; an error meanwhile is the server having nothing to cancel

	text strings.Builder // owned by the event goroutine
}

// Dial connects, sends session.create and returns once session.created arrives.
func Dial(ctx context.Context, cfg voice.Config) (voice.Session, error) {
	s, err := dial(ctx, cfg)
	if err != nil {
		return nil, err // not a typed nil inside the interface
	}
	return s, nil
}

func dial(ctx context.Context, cfg voice.Config) (*Session, error) {
	if cfg.Key == "" || cfg.Model == "" {
		return nil, errors.New("doubao: Key and Model are required")
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.Voice == "" {
		_, cfg.Voice = voice.Defaults(voice.Doubao)
	}
	u := cfg.Extra["url"]
	if u == "" {
		u = defaultURL
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	wc, err := websocket.NewConfig(u, "https://openspeech.bytedance.com")
	if err != nil {
		return nil, fmt.Errorf("doubao: %w", err)
	}
	wc.Header.Set("X-Api-Key", cfg.Key)
	wc.Header.Set("User-Agent", "rex-voiced/"+cfg.Version)
	ws, err := wc.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("doubao: dial: %w", err)
	}
	sctx, cancel := context.WithCancel(context.Background())
	s := &Session{cfg: cfg, log: log, ws: ws, start: time.Now(), ctx: sctx, cancel: cancel,
		done: make(chan struct{}), closedAck: make(chan struct{})}
	if err := s.setup(ctx); err != nil {
		cancel()
		ws.Close()
		return nil, err
	}
	go s.loop()
	return s, nil
}

func (s *Session) setup(ctx context.Context) error {
	if err := s.send(map[string]any{"type": "session.create", "session": s.sessionConfig(),
		// Empty but required: the server refuses session.create without them (third-party live probe, 2026-09).
		"extension": map[string]any{"asr": map[string]any{}, "tts": map[string]any{}, "dialog": map[string]any{}}}); err != nil {
		return fmt.Errorf("doubao: session.create: %w", err)
	}
	deadline := time.Now().Add(readyTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := s.ws.SetReadDeadline(deadline); err != nil {
		return err
	}
	for {
		ev, err := s.recv()
		if err != nil {
			return fmt.Errorf("doubao: waiting for session.created: %w", err)
		}
		switch ev.Type {
		case "session.created":
			s.id = ev.Session.ID
			s.log.Info("doubao: session created", "session_id", s.id)
			return s.ws.SetReadDeadline(time.Time{})
		case "error":
			return fmt.Errorf("doubao: session.create rejected: %s", ev.Error.String())
		default:
			s.log.Debug("doubao: event before session.created", "type", ev.Type)
		}
	}
}

func (s *Session) sessionConfig() map[string]any {
	c := map[string]any{
		"model":        s.cfg.Model,
		"instructions": s.cfg.Instructions,
		"audio": map[string]any{
			"input": map[string]any{"format": map[string]any{"type": "pcm", "rate": 16000}},
			// pcm_s16le, not "pcm": "pcm" is float32 and the default is ogg_opus.
			"output": map[string]any{"format": map[string]any{"type": "pcm_s16le", "rate": 24000}, "voice": s.cfg.Voice},
		},
	}
	if len(s.cfg.Tools) > 0 {
		c["tools"] = s.cfg.Tools // flat {type, name, description, parameters}, same as tools.ToolDef
	}
	return c
}

// ID is the server's session.id.
func (s *Session) ID() string { return s.id }

func (s *Session) Age() time.Duration { return time.Since(s.start) }

// MaxAge is 0: no documented cap; the server drops a session idle for 10 minutes, the session layer hangs up sooner.
func (s *Session) MaxAge() time.Duration { return 0 }

func (s *Session) Done() <-chan struct{} { return s.done }

// RecentTurns returns up to n of the latest user/assistant texts, oldest first.
func (s *Session) RecentTurns(n int) []voice.Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	n = min(n, len(s.turns))
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

// AppendAudio resamples 24 kHz to the 16 kHz the server takes and sends it right away: the session
// layer already paces 20 ms frames in real time, which the server requires. While muted the frames
// are dropped (the session layer's silence fill): mute has told the server no audio is coming.
func (s *Session) AppendAudio(pcm []byte) error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return errors.New("doubao: session closed")
	}
	if s.muted {
		s.mu.Unlock()
		return nil
	}
	out, err := s.down.Write(pcm)
	s.mu.Unlock()
	if err != nil || len(out) == 0 {
		return err
	}
	return s.send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(out)})
}

func (s *Session) SendText(string) error { return ErrNoTextInput }

// SetMuted sends input_audio_mute.commit / input_audio_unmute.commit on a change only: a session that
// stops getting audio without mute stops answering.
func (s *Session) SetMuted(muted bool) error {
	s.mu.Lock()
	if s.muted == muted {
		s.mu.Unlock()
		return nil
	}
	s.muted = muted
	s.mu.Unlock()
	typ := "input_audio_unmute.commit"
	if muted {
		typ = "input_audio_mute.commit"
	}
	return s.send(map[string]any{"type": typ})
}

func (s *Session) Cancel() error {
	s.mu.Lock()
	s.cut, s.answering, s.cancelSent = true, false, true
	s.mu.Unlock()
	return s.send(map[string]any{"type": "response.cancel"})
}

// Close sends session.close, waits up to closeTimeout for session.closed, then drops the socket:
// a socket dropped without the handshake is reported against the account.
func (s *Session) Close() error {
	if !s.markClosing() {
		return nil
	}
	if err := s.send(map[string]any{"type": "session.close"}); err != nil {
		s.log.Debug("doubao: session.close not sent", "err", err)
	} else {
		select {
		case <-s.closedAck:
		case <-s.done:
		case <-time.After(closeTimeout):
			s.log.Warn("doubao: no session.closed", "after", closeTimeout)
		}
	}
	s.cancel()
	return s.ws.Close()
}

// abort drops the socket without the handshake; for the event goroutine when the server already gave up.
func (s *Session) abort() {
	if s.markClosing() {
		s.cancel()
		s.ws.Close()
	}
}

func (s *Session) markClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.closing = true
	return true
}

func (s *Session) isClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

func (s *Session) send(ev map[string]any) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.ws.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	// Clear it afterwards: the read loop writes pongs from inside Receive and would hit a stale deadline.
	defer s.ws.SetWriteDeadline(time.Time{})
	return websocket.Message.Send(s.ws, string(b))
}

type apiError struct{ Type, Code, Message string }

func (e *apiError) String() string {
	if e == nil {
		return "unknown error"
	}
	return e.Code + ": " + e.Message
}

type event struct {
	Type       string     `json:"type"`
	Delta      string     `json:"delta"`
	Text       string     `json:"text"`
	Transcript string     `json:"transcript"` // not seen in references; read in case transcription uses it instead of text
	Items      []toolCall `json:"items"`
	Session    struct {
		ID string `json:"id"`
	} `json:"session"`
	Response json.RawMessage `json:"response"`
	Error    *apiError       `json:"error"`
}

func (s *Session) recv() (event, error) {
	var raw string
	if err := websocket.Message.Receive(s.ws, &raw); err != nil {
		return event{}, err
	}
	var ev event
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		return event{}, fmt.Errorf("doubao: bad event: %w", err)
	}
	return ev, nil
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Session) loop() {
	defer close(s.done)
	h := s.cfg.Handlers
	for {
		ev, err := s.recv()
		if err != nil {
			if !s.isClosing() {
				if h.Error != nil {
					h.Error(fmt.Errorf("doubao: read: %w", err))
				}
				s.abort()
			}
			return
		}
		if ev.Type != "response.output_audio.delta" {
			s.log.Debug("doubao: event", "type", ev.Type)
		}
		switch ev.Type {
		case "response.output_audio.started":
			s.mu.Lock()
			s.cut, s.answering = false, true
			s.mu.Unlock()
		case "response.output_audio.delta":
			s.mu.Lock()
			drop := s.cut
			s.answering = s.answering || !drop
			s.mu.Unlock()
			if drop {
				continue
			}
			pcm, err := base64.StdEncoding.DecodeString(ev.Delta)
			if err != nil {
				s.log.Warn("doubao: bad audio delta", "err", err)
				continue
			}
			if h.AudioDelta != nil {
				h.AudioDelta(pcm)
			}
		case "response.output_audio.done":
			s.setAnswering(false)
		case "response.output_text.delta":
			s.text.WriteString(ev.Delta)
			if h.TextDelta != nil {
				h.TextDelta(ev.Delta)
			}
		case "response.output_text.done":
			t := or(ev.Text, s.text.String())
			s.text.Reset()
			s.addTurn("assistant", t)
			if h.AssistantText != nil {
				h.AssistantText(t)
			}
		case "conversation.item.input_audio_transcription.started":
			s.onSpeechStarted()
			if h.SpeechStarted != nil {
				h.SpeechStarted()
			}
		case "conversation.item.input_audio_transcription.completed":
			t := or(ev.Text, ev.Transcript)
			s.addTurn("user", t)
			if h.UserText != nil {
				h.UserText(t)
			}
		case "response.function_call_arguments.done":
			s.runTools(ev.Items)
		case "response.canceled":
			s.mu.Lock()
			s.answering, s.cancelSent = false, false
			s.mu.Unlock()
		case "response.done":
			s.onResponseDone(ev.Response)
		case "session.closed":
			s.ackOnce.Do(func() { close(s.closedAck) })
		case "error":
			s.onError(ev.Error)
		}
	}
}

func (s *Session) setAnswering(v bool) {
	s.mu.Lock()
	s.answering = v
	s.mu.Unlock()
}

// onSpeechStarted cancels the answer still being spoken. On a barge-in the server usually ends the
// answer itself (a bare response.done); then answering is already false and no cancel is sent.
func (s *Session) onSpeechStarted() {
	s.mu.Lock()
	speaking := s.answering
	s.mu.Unlock()
	if !speaking {
		return
	}
	if err := s.Cancel(); err != nil {
		s.sendFailed("response.cancel", err)
	}
}

func (s *Session) onError(e *apiError) {
	s.mu.Lock()
	benign := s.cancelSent || s.closing // nothing left to cancel / errors that come with a clean close
	s.cancelSent = false
	s.mu.Unlock()
	if benign {
		s.log.Info("doubao: error ignored", "error", e.String())
		return
	}
	if h := s.cfg.Handlers.Error; h != nil {
		h(fmt.Errorf("doubao: %s", e.String()))
	}
	s.abort() // the server drops the socket after an error anyway
}

func (s *Session) onResponseDone(raw json.RawMessage) {
	s.setAnswering(false)
	var r struct {
		Usage *rawUsage `json:"usage"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &r); err != nil {
			s.log.Warn("doubao: bad response.done", "err", err)
		}
	}
	s.log.Debug("doubao: response.done", "response", string(raw)) // usage fields are unverified; keep the raw shape visible
	u := r.Usage.toUsage()
	s.writeUsage(u)
	h := s.cfg.Handlers
	if h.ResponseDone != nil {
		h.ResponseDone(u)
	}
	if s.inflight.Load() > 0 {
		return // a tool batch is running; the answer after its results ends the turn
	}
	if h.TurnDone != nil {
		h.TurnDone()
	}
}

// runTools executes one function_call_arguments.done's calls in parallel and answers them in one
// conversation.item.create: the model continues by itself once every call has a result (no response.create).
func (s *Session) runTools(calls []toolCall) {
	if len(calls) == 0 {
		return
	}
	s.inflight.Add(1)
	go func() {
		items := make([]any, len(calls))
		var wg sync.WaitGroup
		for i, c := range calls {
			wg.Go(func() {
				items[i] = map[string]any{"call_id": c.CallID, "role": "tool",
					"content": []any{map[string]any{"type": "input_text", "text": s.callTool(c)}}}
			})
		}
		wg.Wait()
		s.inflight.Add(-1) // before sending, so the response.done it causes sees zero
		if err := s.send(map[string]any{"type": "conversation.item.create", "items": items}); err != nil {
			s.sendFailed("conversation.item.create", err)
		}
	}()
}

func (s *Session) callTool(c toolCall) string {
	if s.cfg.Call == nil {
		return `{"error":"没有配置工具"}`
	}
	ctx, cancel := context.WithTimeout(s.ctx, toolTimeout)
	defer cancel()
	res := make(chan string, 1)
	go func() { res <- s.cfg.Call(ctx, c.Name, c.Arguments) }()
	select {
	case out := <-res:
		return out
	case <-ctx.Done():
		s.log.Warn("doubao: tool timeout", "tool", c.Name)
		return `{"error":"工具超时"}`
	}
}

func (s *Session) sendFailed(what string, err error) {
	if s.isClosing() {
		s.log.Debug("doubao: send after close", "event", what)
		return
	}
	s.log.Warn("doubao: send failed", "event", what, "err", err)
}
