package qwen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/voice"
)

const (
	defaultURL   = "wss://dashscope.aliyuncs.com/api-ws/v1/realtime"          // 11-01 实测仍可用
	workspaceURL = "wss://%s.cn-beijing.maas.aliyuncs.com/api-ws/v1/realtime" // Extra workspace
	asrModel     = "qwen3-asr-flash-realtime"                                 // the only accepted value
	maxAge       = 120 * time.Minute                                          // Qwen's session cap
	maxTurns     = 50
)

// Variables so tests can shorten them.
var (
	readyTimeout = 10 * time.Second
	writeTimeout = 10 * time.Second
	toolTimeout  = 20 * time.Second
)

const timeoutOutput = `{"error":"工具超时"}`

type funcCall struct{ callID, name, args string }

type Session struct {
	cfg    voice.Config
	log    *slog.Logger
	ws     *websocket.Conn
	start  time.Time
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wmu    sync.Mutex

	amu sync.Mutex // keeps Downsampler state and append order together
	ds  voice.Downsampler

	mu     sync.Mutex
	closed bool
	turns  []voice.Turn

	// Owned by the event goroutine.
	pending  map[string][]funcCall // response_id -> function calls seen in that response
	inflight atomic.Int32          // tool batches running; TurnDone waits until none are
	resp     string                // response being generated
	cut      string                // response interrupted by the user; its late audio is dropped
}

// Dial connects, sends session.update and returns once session.updated arrives.
func Dial(ctx context.Context, cfg voice.Config) (voice.Session, error) {
	s, err := dial(ctx, cfg)
	if err != nil {
		return nil, err // not a typed nil inside the interface
	}
	return s, nil
}

func dial(ctx context.Context, cfg voice.Config) (*Session, error) {
	if cfg.Key == "" || cfg.Model == "" {
		return nil, errors.New("qwen: Key and Model are required")
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.Voice == "" {
		cfg.Voice = "Tina" // the server's own default (Chelsie) is rejected by 3.8, even in text mode
	}
	u, hint := cfg.Extra["url"], ""
	switch ws := cfg.Extra["workspace"]; {
	case u != "":
	case ws != "":
		if strings.ContainsAny(ws, "/.:@?#") {
			return nil, fmt.Errorf("qwen: Extra workspace %q is not a workspace id", ws)
		}
		u = fmt.Sprintf(workspaceURL, ws)
	default:
		u, hint = defaultURL, "（旧端点连不上时，在 Extra workspace 填百炼业务空间 ID 改走新端点）"
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	wc, err := websocket.NewConfig(u+"?model="+url.QueryEscape(cfg.Model), "https://dashscope.aliyuncs.com")
	if err != nil {
		return nil, fmt.Errorf("qwen: %w", err)
	}
	wc.Header.Set("Authorization", "Bearer "+cfg.Key)
	wc.Header.Set("User-Agent", "rex-voiced/"+cfg.Version)
	ws, err := wc.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("qwen: dial: %w%s", err, hint)
	}
	sctx, cancel := context.WithCancel(context.Background())
	s := &Session{cfg: cfg, log: log, ws: ws, start: time.Now(), ctx: sctx, cancel: cancel,
		done: make(chan struct{}), pending: map[string][]funcCall{}}
	if err := s.setup(ctx); err != nil {
		cancel()
		ws.Close()
		return nil, err
	}
	go s.loop()
	return s, nil
}

func (s *Session) setup(ctx context.Context) error {
	if err := s.send(map[string]any{"type": "session.update", "session": s.sessionConfig()}); err != nil {
		return fmt.Errorf("qwen: session.update: %w", err)
	}
	deadline := time.Now().Add(readyTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := s.ws.SetReadDeadline(deadline); err != nil {
		return err
	}
	for {
		ev, raw, err := s.recv()
		if err != nil {
			return fmt.Errorf("qwen: waiting for session.updated: %w", err)
		}
		switch ev.Type {
		case "session.updated":
			return s.ws.SetReadDeadline(time.Time{})
		case "error":
			return fmt.Errorf("qwen: session.update rejected: %s", ev.Error)
		default:
			s.log.Debug("qwen: event before session.updated", "type", ev.Type, "bytes", len(raw))
		}
	}
}

func (s *Session) sessionConfig() map[string]any {
	c := map[string]any{
		"instructions": s.cfg.Instructions,
		"audio": map[string]any{
			"input":  map[string]any{"format": map[string]any{"type": "pcm", "sample_rate": 16000}}, // 3.8 only takes 16k
			"output": map[string]any{"voice": s.cfg.Voice},
		},
	}
	if len(s.cfg.Tools) > 0 {
		c["tools"] = s.cfg.Tools // the flat OpenAI shape is accepted and echoed back as is (11-01)
	}
	if s.cfg.TextOnly {
		c["modalities"] = []string{"text"}
		return c
	}
	c["turn_detection"] = map[string]any{"type": "semantic_vad"}
	if s.cfg.Transcribe {
		c["input_audio_transcription"] = map[string]any{"model": asrModel}
	} else {
		c["input_audio_transcription"] = nil // the server turns a default transcriber on otherwise
	}
	return c
}

// Age is the time since Dial.
func (s *Session) Age() time.Duration { return time.Since(s.start) }

func (s *Session) MaxAge() time.Duration { return maxAge }

// SetMuted is a no-op: server VAD only needs the session layer's silence fill.
func (s *Session) SetMuted(bool) error { return nil }

// Done is closed when the event loop has stopped (read error, server error or Close).
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

// AppendAudio takes PCM16 24 kHz and sends it as 16 kHz.
func (s *Session) AppendAudio(pcm []byte) error {
	s.amu.Lock()
	defer s.amu.Unlock()
	out, err := s.ds.Write(pcm)
	if err != nil || len(out) == 0 {
		return err
	}
	return s.send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(out)})
}

// SendText uses a message item: not in Qwen's documented item types, but accepted (11-01 实测).
func (s *Session) SendText(text string) error {
	s.addTurn("user", text)
	if err := s.send(map[string]any{"type": "conversation.item.create", "item": map[string]any{
		"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}}); err != nil {
		return err
	}
	return s.send(map[string]any{"type": "response.create"})
}

func (s *Session) Cancel() error { return s.send(map[string]any{"type": "response.cancel"}) }

func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
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
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.ws.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	// Clear it afterwards: the read loop writes pongs from inside Receive and would hit a stale deadline.
	defer s.ws.SetWriteDeadline(time.Time{})
	return websocket.Message.Send(s.ws, string(b))
}

func (s *Session) sendFailed(what string, err error) {
	if s.isClosed() {
		s.log.Debug("qwen: send after close", "event", what)
		return
	}
	s.log.Warn("qwen: send failed", "event", what, "err", err)
}

type apiError struct{ Type, Code, Message, Param string }

func (e *apiError) String() string {
	if e == nil {
		return "unknown error"
	}
	code := e.Code // Qwen fills either code (COMMON_ERROR) or type (invalid_request_error)
	if code == "" {
		code = e.Type
	}
	return code + ": " + e.Message
}

type event struct {
	Type       string          `json:"type"`
	ResponseID string          `json:"response_id"`
	CallID     string          `json:"call_id"`
	Name       string          `json:"name"`
	Arguments  string          `json:"arguments"`
	Delta      string          `json:"delta"`
	Text       string          `json:"text"`
	Transcript string          `json:"transcript"`
	Response   json.RawMessage `json:"response"`
	Error      *apiError       `json:"error"`
}

func (s *Session) recv() (event, string, error) {
	var raw string
	if err := websocket.Message.Receive(s.ws, &raw); err != nil {
		return event{}, "", err
	}
	var ev event
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		return event{}, raw, fmt.Errorf("qwen: bad event: %w", err)
	}
	return ev, raw, nil
}

func (s *Session) loop() {
	defer close(s.done)
	h := s.cfg.Handlers
	for {
		ev, raw, err := s.recv()
		if err != nil {
			if !s.isClosed() {
				if h.Error != nil {
					h.Error(fmt.Errorf("qwen: read: %w", err))
				}
				s.Close()
			}
			return
		}
		s.log.Debug("qwen: event", "type", ev.Type, "bytes", len(raw))
		switch ev.Type {
		case "response.created":
			var r struct{ ID string }
			json.Unmarshal(ev.Response, &r)
			s.resp = r.ID
		case "response.text.delta":
			if h.TextDelta != nil {
				h.TextDelta(ev.Delta)
			}
		case "response.text.done":
			s.addTurn("assistant", ev.Text)
			if h.AssistantText != nil {
				h.AssistantText(ev.Text)
			}
		case "response.audio.delta":
			if ev.ResponseID == s.cut {
				continue
			}
			pcm, err := base64.StdEncoding.DecodeString(ev.Delta)
			if err != nil {
				s.log.Warn("qwen: bad audio delta", "err", err)
				continue
			}
			if h.AudioDelta != nil {
				h.AudioDelta(pcm)
			}
		case "response.audio_transcript.done":
			s.addTurn("assistant", ev.Transcript)
			if h.AssistantText != nil {
				h.AssistantText(ev.Transcript)
			}
		case "conversation.item.input_audio_transcription.completed":
			s.addTurn("user", ev.Transcript)
			if h.UserText != nil {
				h.UserText(ev.Transcript)
			}
		case "input_audio_buffer.speech_started":
			// No truncate in Qwen: stop the answer and drop its late audio.
			if s.resp != "" {
				s.cut, s.resp = s.resp, ""
			}
			if err := s.Cancel(); err != nil {
				s.sendFailed("response.cancel", err)
			}
			if h.SpeechStarted != nil {
				h.SpeechStarted()
			}
		case "response.function_call_arguments.done":
			s.pending[ev.ResponseID] = append(s.pending[ev.ResponseID], funcCall{ev.CallID, ev.Name, ev.Arguments})
		case "response.done":
			s.onResponseDone(ev)
		case "error":
			s.onError(ev.Error)
		}
	}
}

// onError reports a server error and ends the session, except a cancel with nothing to cancel
// (11-01 实测：「Cannot cancel response before response.created」，会话照常继续).
func (s *Session) onError(e *apiError) {
	if e != nil && strings.Contains(strings.ToLower(e.Message), "cancel") {
		s.log.Debug("qwen: benign cancel error", "message", e.Message)
		return
	}
	if h := s.cfg.Handlers.Error; h != nil {
		h(fmt.Errorf("qwen: %s", e))
	}
	s.Close()
}

func (s *Session) onResponseDone(ev event) {
	var r struct {
		ID     string          `json:"id"`
		Status string          `json:"status"`
		Detail json.RawMessage `json:"status_details"`
		Usage  rawUsage        `json:"usage"`
	}
	if err := json.Unmarshal(ev.Response, &r); err != nil {
		s.log.Warn("qwen: bad response.done", "err", err)
	}
	if r.Status != "" && r.Status != "completed" {
		s.log.Info("qwen: response not completed", "status", r.Status, "details", string(r.Detail))
	}
	if s.resp == r.ID {
		s.resp = ""
	}
	h := s.cfg.Handlers
	u := r.Usage.toUsage(s.cfg.Model)
	s.writeUsage(u)
	if h.ResponseDone != nil {
		h.ResponseDone(u.public())
	}
	calls := s.pending[r.ID]
	delete(s.pending, r.ID)
	if len(calls) > 0 {
		s.runTools(calls)
		return
	}
	if s.inflight.Load() > 0 {
		return // another tool batch is still running; its final answer ends the turn
	}
	if h.TurnDone != nil {
		h.TurnDone()
	}
}

// runTools executes one response's calls in parallel, sends every output, then exactly one response.create.
func (s *Session) runTools(calls []funcCall) {
	s.inflight.Add(1)
	go func() {
		outs := make([]string, len(calls))
		var wg sync.WaitGroup
		for i, c := range calls {
			wg.Go(func() { outs[i] = s.callTool(c) })
		}
		wg.Wait()
		for i, c := range calls {
			if err := s.send(map[string]any{"type": "conversation.item.create", "item": map[string]any{
				"type": "function_call_output", "call_id": c.callID, "output": outs[i]}}); err != nil {
				s.sendFailed("function_call_output", err)
			}
		}
		s.inflight.Add(-1) // before response.create, so the response.done it causes sees zero
		if err := s.send(map[string]any{"type": "response.create"}); err != nil {
			s.sendFailed("response.create", err)
		}
	}()
}

func (s *Session) callTool(c funcCall) string {
	if s.cfg.Call == nil {
		return `{"error":"没有配置工具"}`
	}
	ctx, cancel := context.WithTimeout(s.ctx, toolTimeout)
	defer cancel()
	res := make(chan string, 1)
	go func() { res <- s.cfg.Call(ctx, c.name, c.args) }()
	select {
	case out := <-res:
		return out
	case <-ctx.Done():
		s.log.Warn("qwen: tool timeout", "tool", c.name)
		return timeoutOutput
	}
}
