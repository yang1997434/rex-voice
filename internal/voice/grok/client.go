package grok

// Differences from internal/voice/openai: the session shape, a 24 kHz output format, cumulative input
// transcripts, response.create held until the answer's audio is complete, and per-minute billing.

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

const defaultURL = "wss://api.x.ai/v1/realtime"

// Variables so tests can shorten them.
var (
	readyTimeout     = 10 * time.Second
	writeTimeout     = 10 * time.Second
	toolTimeout      = 20 * time.Second
	audioWaitTimeout = 10 * time.Second // a tool batch waits at most this long for the answer's output_audio.done
)

const maxTurns = 50

const (
	maxAge     = 120 * time.Minute // xAI's session cap (error.type max_duration)
	bytesPerMs = 48                // PCM16 mono 24 kHz
)

type funcCall struct{ itemID, callID, name, args string }

type Session struct {
	cfg    voice.Config
	log    *slog.Logger
	ws     *websocket.Conn
	start  time.Time
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	seq    atomic.Uint64
	wmu    sync.Mutex

	mu            sync.Mutex
	closed        bool
	turns         []voice.Turn
	tokIn, tokOut int // summed from response.done for the usage line; not billed
	texts         int // SendText calls, billed per item

	// Owned by the event goroutine.
	pending   map[string][]funcCall    // response_id -> function calls seen in that response
	audio     map[string]chan struct{} // response_id -> closed at its output_audio.done
	toolItems []string                 // function_call + function_call_output item ids awaiting pruning
	text      strings.Builder
	textType  string       // which of the two text delta event names this response uses
	inflight  atomic.Int32 // tool batches running; pruning waits until none are
	item      string       // assistant audio item being streamed
	itemBytes int          // bytes of item handed to AudioDelta, for truncation
	cut       string       // item interrupted by the user; its late deltas are dropped
	userItem  string       // input item whose cumulative transcript is in userText
	userText  string       // latest cumulative transcript not yet reported
	userDone  string       // input item already reported as UserText
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
		return nil, errors.New("grok: Key and Model are required")
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.Voice == "" {
		cfg.Voice = "ara"
	}
	u := cfg.Extra["url"]
	if u == "" {
		u = defaultURL
	}
	effort := cfg.Extra["reasoning"]
	if effort == "" {
		effort = "none" // the server default "high" delays the first audio
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	q := url.Values{"model": {cfg.Model}, "reasoning.effort": {effort}}
	wc, err := websocket.NewConfig(u+"?"+q.Encode(), "https://api.x.ai")
	if err != nil {
		return nil, fmt.Errorf("grok: %w", err)
	}
	wc.Header.Set("Authorization", "Bearer "+cfg.Key)
	wc.Header.Set("User-Agent", "rex-voiced/"+cfg.Version)
	ws, err := wc.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("grok: dial: %w", err)
	}
	sctx, cancel := context.WithCancel(context.Background())
	s := &Session{cfg: cfg, log: log, ws: ws, start: time.Now(), ctx: sctx, cancel: cancel,
		done: make(chan struct{}), pending: map[string][]funcCall{}, audio: map[string]chan struct{}{}}
	if err := s.setup(ctx); err != nil {
		s.Close() // the connection time is billed too, so this still writes a usage line
		return nil, err
	}
	go s.loop()
	return s, nil
}

func (s *Session) setup(ctx context.Context) error {
	if err := s.send(map[string]any{"type": "session.update", "session": s.sessionConfig()}); err != nil {
		return fmt.Errorf("grok: session.update: %w", err)
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
			return fmt.Errorf("grok: waiting for session.updated: %w", err)
		}
		switch ev.Type {
		case "session.updated":
			return s.ws.SetReadDeadline(time.Time{})
		case "error":
			return fmt.Errorf("grok: session.update rejected: %s", ev.Error.String())
		default:
			s.log.Debug("grok: event before session.updated", "type", ev.Type, "bytes", len(raw))
		}
	}
}

// sessionConfig differs from OpenAI's: no type, voice and turn_detection at the top level, no tool_choice,
// no semantic_vad / interrupt_response / noise_reduction (not in xAI's schema), 24k output format set.
func (s *Session) sessionConfig() map[string]any {
	c := map[string]any{"instructions": s.cfg.Instructions, "voice": s.cfg.Voice}
	if len(s.cfg.Tools) > 0 {
		c["tools"] = s.cfg.Tools
	}
	if s.cfg.TextOnly {
		c["output_modalities"] = []string{"text"}
		return c
	}
	pcm := map[string]any{"type": "audio/pcm", "rate": 24000}
	in := map[string]any{"format": pcm}
	if s.cfg.Transcribe {
		in["transcription"] = map[string]any{"model": "grok-transcribe", "language_hint": "zh"}
	}
	c["turn_detection"] = map[string]any{"type": "server_vad", "idle_timeout_ms": 60000}
	c["audio"] = map[string]any{"input": in, "output": map[string]any{"format": pcm}}
	return c
}

// Age is the time since Dial.
func (s *Session) Age() time.Duration { return time.Since(s.start) }

// Cost is the connection time so far plus text items, at the per-minute price.
func (s *Session) Cost() float64 {
	s.mu.Lock()
	texts := s.texts
	s.mu.Unlock()
	usd, _ := cost(s.cfg.Model, s.Age().Seconds(), texts)
	return usd
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
	s.mu.Lock()
	s.texts++
	s.mu.Unlock()
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

// Close ends the session and writes its one usage line (xAI bills connection time).
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	secs, in, out, texts := time.Since(s.start).Seconds(), s.tokIn, s.tokOut, s.texts
	s.mu.Unlock()
	s.writeUsage(secs, in, out, texts)
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
	switch {
	case e == nil:
		return "unknown error"
	case e.Code == "": // xAI sometimes only sets type, e.g. max_duration
		return e.Type + ": " + e.Message
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
		return event{}, raw, fmt.Errorf("grok: bad event: %w", err)
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
					h.Error(fmt.Errorf("grok: read: %w", err))
				}
				s.Close()
			}
			return
		}
		switch ev.Type {
		case "response.output_text.delta", "response.text.delta":
			// xAI emits both names; keep to the first one seen in a response so a doubled stream is not doubled text.
			if s.textType == "" {
				s.textType = ev.Type
			} else if s.textType != ev.Type {
				continue
			}
			s.text.WriteString(ev.Delta)
			if h.TextDelta != nil {
				h.TextDelta(ev.Delta)
			}
		case "response.output_audio.delta":
			pcm, err := base64.StdEncoding.DecodeString(ev.Delta)
			if err != nil {
				s.log.Warn("grok: bad audio delta", "err", err)
				continue
			}
			if ev.ItemID == s.cut {
				continue
			}
			if s.audio[ev.ResponseID] == nil {
				s.audio[ev.ResponseID] = make(chan struct{})
			}
			if ev.ItemID != s.item {
				s.item, s.itemBytes = ev.ItemID, 0
			}
			s.itemBytes += len(pcm)
			if h.AudioDelta != nil {
				h.AudioDelta(pcm)
			}
		case "response.output_audio.done":
			s.audioDone(ev.ResponseID)
		case "response.output_audio_transcript.done":
			s.addTurn("assistant", ev.Transcript)
			if h.AssistantText != nil {
				h.AssistantText(ev.Transcript)
			}
		case "conversation.item.input_audio_transcription.updated":
			if ev.ItemID != "" && ev.ItemID == s.userDone {
				continue
			}
			t := ev.Transcript
			if t == "" { // field name not confirmed against the live server
				t = ev.Delta
			}
			s.userItem, s.userText = ev.ItemID, t
		case "conversation.item.input_audio_transcription.completed":
			if ev.ItemID != "" && ev.ItemID == s.userDone {
				continue // already reported from the cumulative transcript at response.created
			}
			s.reportUser(ev.ItemID, ev.Transcript)
		case "response.created":
			if s.userText != "" { // .completed did not come before the answer: the cumulative text is final
				s.reportUser(s.userItem, s.userText)
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
			s.log.Debug("grok: item deleted", "item_id", ev.ItemID)
		case "error":
			s.onError(ev.Error)
		default:
			s.log.Debug("grok: event", "type", ev.Type, "bytes", len(raw))
		}
	}
}

func (s *Session) reportUser(itemID, text string) {
	s.userDone, s.userItem, s.userText = itemID, "", ""
	s.addTurn("user", text)
	if h := s.cfg.Handlers.UserText; h != nil {
		h(text)
	}
}

// audioDone releases a tool batch waiting for this response's audio.
func (s *Session) audioDone(responseID string) {
	if c := s.audio[responseID]; c != nil {
		close(c)
		delete(s.audio, responseID)
	}
}

// onSpeechStarted truncates the answer being played to what was handed out (protocol section 4.4).
// The server VAD cancels the answer itself, so no response.cancel; nothing more of it will play.
func (s *Session) onSpeechStarted() {
	if s.item != "" {
		if err := s.truncate(s.item, s.itemBytes/bytesPerMs); err != nil {
			s.sendFailed("conversation.item.truncate", err)
		}
		s.cut = s.item
	}
	s.item, s.itemBytes = "", 0
	for id := range s.audio {
		s.audioDone(id)
	}
}

func (s *Session) onError(e *apiError) {
	switch {
	case e != nil && e.Code == "conversation_already_has_active_response":
		s.log.Debug("grok: benign error", "code", e.Code)
		return
	case e != nil && strings.HasPrefix(e.EventID, "rv_del_"):
		// Deleting an item that is already gone; nothing to do.
		s.log.Debug("grok: delete failed", "code", e.Code, "message", e.Message)
		return
	}
	if h := s.cfg.Handlers.Error; h != nil {
		h(fmt.Errorf("grok: %s", e.String()))
	}
}

func (s *Session) onResponseDone(ev event) {
	var r struct {
		ID     string          `json:"id"`
		Status string          `json:"status"`
		Detail json.RawMessage `json:"status_details"`
		Usage  struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(ev.Response, &r); err != nil {
		s.log.Warn("grok: bad response.done", "err", err)
	}
	if r.Status != "" && r.Status != "completed" {
		s.log.Info("grok: response not completed", "status", r.Status, "details", string(r.Detail))
	}
	h := s.cfg.Handlers
	if t := s.text.String(); t != "" { // text mode; voice mode reports output_audio_transcript.done instead
		s.addTurn("assistant", t)
		if h.AssistantText != nil {
			h.AssistantText(t)
		}
	}
	s.text.Reset()
	s.textType = ""
	s.mu.Lock()
	s.tokIn += r.Usage.In
	s.tokOut += r.Usage.Out
	s.mu.Unlock()
	if h.ResponseDone != nil {
		// Billed per minute, not per response: the cost goes into the usage line at Close.
		h.ResponseDone(voice.Usage{Input: r.Usage.In, Output: r.Usage.Out})
	}
	calls := s.pending[r.ID]
	delete(s.pending, r.ID)
	if len(calls) > 0 {
		s.runTools(calls, s.audio[r.ID])
		return
	}
	delete(s.audio, r.ID) // nobody waits on it
	if s.inflight.Load() > 0 {
		return // another tool batch is still running; its final answer will prune
	}
	s.prune()
	s.item, s.itemBytes = "", 0 // answer fully sent: a later barge-in has nothing to cut
	if h.TurnDone != nil {
		h.TurnDone()
	}
}
