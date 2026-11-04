// Package openai is the Mac-side OpenAI Realtime WebSocket client (docs/protocol.md section 4.1):
// session setup, audio/text in and out, truncation on barge-in, tool-call batching, context pruning
// and usage accounting. It registers itself as voice provider "openai".
package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/voice"
)

func init() { voice.Register(voice.OpenAI, Dial) }

const defaultURL = "wss://api.openai.com/v1/realtime"

// Variables so tests can shorten them.
var (
	readyTimeout = 10 * time.Second
	writeTimeout = 10 * time.Second
	toolTimeout  = 20 * time.Second
)

const maxTurns = 50

const (
	maxAge     = 60 * time.Minute // OpenAI's session cap
	bytesPerMs = 48               // PCM16 mono 24 kHz
)

type funcCall struct{ itemID, callID, name, args string }

type Session struct {
	cfg    voice.Config
	keep   bool // Extra keep_tool_outputs: disable pruning of tool items (for A/B)
	log    *slog.Logger
	ws     *websocket.Conn
	start  time.Time
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	seq    atomic.Uint64
	wmu    sync.Mutex

	mu     sync.Mutex
	closed bool
	turns  []voice.Turn
	usd    float64 // summed response.done estimates, for Cost

	// Owned by the event goroutine.
	pending   map[string][]funcCall // response_id -> function calls seen in that response
	toolItems []string              // function_call + function_call_output item ids awaiting pruning
	text      strings.Builder
	inflight  atomic.Int32 // tool batches running; pruning waits until none are
	item      string       // assistant audio item being streamed
	itemBytes int          // bytes of item handed to AudioDelta, for truncation
	cut       string       // item interrupted by the user; its late deltas are dropped
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
		return nil, errors.New("openai: Key and Model are required")
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.Voice == "" {
		cfg.Voice = "marin"
	}
	u := cfg.Extra["url"]
	if u == "" {
		u = defaultURL
	}
	var keep bool
	if v := cfg.Extra["keep_tool_outputs"]; v != "" {
		var err error
		if keep, err = strconv.ParseBool(v); err != nil {
			return nil, fmt.Errorf("openai: Extra keep_tool_outputs: %w", err)
		}
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	wc, err := websocket.NewConfig(u+"?model="+url.QueryEscape(cfg.Model), "https://api.openai.com")
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	wc.Header.Set("Authorization", "Bearer "+cfg.Key)
	wc.Header.Set("User-Agent", "rex-voiced/"+cfg.Version)
	ws, err := wc.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("openai: dial: %w", err)
	}
	sctx, cancel := context.WithCancel(context.Background())
	s := &Session{cfg: cfg, keep: keep, log: log, ws: ws, start: time.Now(), ctx: sctx, cancel: cancel,
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
		return fmt.Errorf("openai: session.update: %w", err)
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
			return fmt.Errorf("openai: waiting for session.updated: %w", err)
		}
		switch ev.Type {
		case "session.updated":
			return s.ws.SetReadDeadline(time.Time{})
		case "error":
			return fmt.Errorf("openai: session.update rejected: %s", ev.Error.String())
		default:
			s.log.Debug("openai: event before session.updated", "type", ev.Type, "bytes", len(raw))
		}
	}
}

func (s *Session) sessionConfig() map[string]any {
	c := map[string]any{"type": "realtime", "instructions": s.cfg.Instructions}
	if len(s.cfg.Tools) > 0 {
		c["tools"] = s.cfg.Tools
		c["tool_choice"] = "auto"
	}
	if s.cfg.TextOnly {
		c["output_modalities"] = []string{"text"}
		return c
	}
	in := map[string]any{
		"format":          map[string]any{"type": "audio/pcm", "rate": 24000},
		"turn_detection":  map[string]any{"type": "semantic_vad", "interrupt_response": true},
		"noise_reduction": map[string]any{"type": "near_field"},
	}
	if s.cfg.Transcribe {
		in["transcription"] = map[string]any{"model": "gpt-4o-mini-transcribe", "language": "zh"}
	}
	c["audio"] = map[string]any{"input": in, "output": map[string]any{"voice": s.cfg.Voice}}
	return c
}

// Age is the time since Dial.
func (s *Session) Age() time.Duration { return time.Since(s.start) }

// Cost is the summed estimate of this session's responses so far.
func (s *Session) Cost() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usd
}

func (s *Session) addCost(usd float64) {
	s.mu.Lock()
	s.usd += usd
	s.mu.Unlock()
}

func (s *Session) MaxAge() time.Duration { return maxAge }

// SetMuted is a no-op: server VAD only needs the session layer's silence fill.
func (s *Session) SetMuted(bool) error { return nil }

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

func (s *Session) AppendAudio(pcm []byte) error {
	return s.send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pcm)})
}

func (s *Session) SendText(text string) error {
	s.addTurn("user", text)
	if err := s.send(map[string]any{"type": "conversation.item.create", "item": map[string]any{
		"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}}); err != nil {
		return err
	}
	return s.send(map[string]any{"type": "response.create"})
}

func (s *Session) truncate(itemID string, audioEndMs int) error {
	return s.send(map[string]any{"type": "conversation.item.truncate", "item_id": itemID, "content_index": 0, "audio_end_ms": audioEndMs})
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

func (s *Session) newID(prefix string) string { return fmt.Sprintf("%s_%d", prefix, s.seq.Add(1)) }

type apiError struct {
	Type, Code, Message string
	EventID             string `json:"event_id"`
}

func (e *apiError) String() string {
	if e == nil {
		return "unknown error"
	}
	return e.Code + ": " + e.Message
}

type event struct {
	Type       string          `json:"type"`
	ResponseID string          `json:"response_id"`
	ItemID     string          `json:"item_id"`
	Delta      string          `json:"delta"`
	Transcript string          `json:"transcript"`
	Item       json.RawMessage `json:"item"`
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
		return event{}, raw, fmt.Errorf("openai: bad event: %w", err)
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
					h.Error(fmt.Errorf("openai: read: %w", err))
				}
				s.Close()
			}
			return
		}
		switch ev.Type {
		case "response.output_text.delta":
			s.text.WriteString(ev.Delta)
			if h.TextDelta != nil {
				h.TextDelta(ev.Delta)
			}
		case "response.output_audio.delta":
			pcm, err := base64.StdEncoding.DecodeString(ev.Delta)
			if err != nil {
				s.log.Warn("openai: bad audio delta", "err", err)
				continue
			}
			if ev.ItemID == s.cut {
				continue
			}
			if ev.ItemID != s.item {
				s.item, s.itemBytes = ev.ItemID, 0
			}
			s.itemBytes += len(pcm)
			if h.AudioDelta != nil {
				h.AudioDelta(pcm)
			}
		case "response.output_audio_transcript.done":
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
			s.onSpeechStarted()
			if h.SpeechStarted != nil {
				h.SpeechStarted()
			}
		case "response.output_item.done":
			s.onItemDone(ev)
		case "response.done":
			s.onResponseDone(ev)
		case "conversation.item.deleted":
			s.log.Debug("openai: item deleted", "item_id", ev.ItemID)
		case "error":
			s.onError(ev.Error)
		default:
			s.log.Debug("openai: event", "type", ev.Type, "bytes", len(raw))
		}
	}
}

// onSpeechStarted truncates the answer being played to what was handed out (protocol section 4.1):
// in WebSocket mode the server does not cut it by itself.
func (s *Session) onSpeechStarted() {
	if s.item != "" {
		if err := s.truncate(s.item, s.itemBytes/bytesPerMs); err != nil {
			s.sendFailed("conversation.item.truncate", err)
		}
		s.cut = s.item
	}
	s.item, s.itemBytes = "", 0
}

func (s *Session) onError(e *apiError) {
	switch {
	case e != nil && e.Code == "conversation_already_has_active_response":
		s.log.Debug("openai: benign error", "code", e.Code)
		return
	case e != nil && strings.HasPrefix(e.EventID, "rv_del_"):
		// Deleting an item that is already gone; nothing to do.
		s.log.Debug("openai: delete failed", "code", e.Code, "message", e.Message)
		return
	}
	if h := s.cfg.Handlers.Error; h != nil {
		h(fmt.Errorf("openai: %s", e.String()))
	}
}

func (s *Session) onResponseDone(ev event) {
	var r struct {
		ID     string          `json:"id"`
		Status string          `json:"status"`
		Detail json.RawMessage `json:"status_details"`
		Usage  rawUsage        `json:"usage"`
	}
	if err := json.Unmarshal(ev.Response, &r); err != nil {
		s.log.Warn("openai: bad response.done", "err", err)
	}
	if r.Status != "" && r.Status != "completed" {
		s.log.Info("openai: response not completed", "status", r.Status, "details", string(r.Detail))
	}
	h := s.cfg.Handlers
	if t := s.text.String(); t != "" { // text mode; voice mode reports output_audio_transcript.done instead
		s.addTurn("assistant", t)
		if h.AssistantText != nil {
			h.AssistantText(t)
		}
	}
	s.text.Reset()
	u := r.Usage.toUsage(s.cfg.Model)
	s.addCost(u.USD)
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
		return // another tool batch is still running; its final answer will prune
	}
	s.prune()
	s.item, s.itemBytes = "", 0 // answer fully sent: a later barge-in has nothing to cut
	if h.TurnDone != nil {
		h.TurnDone()
	}
}
