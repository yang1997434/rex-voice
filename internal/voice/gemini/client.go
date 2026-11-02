package gemini

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	defaultURL = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"
	origin     = "https://generativelanguage.googleapis.com"
	maxTurns   = 50
)

// Variables so tests can shorten them.
var (
	readyTimeout = 10 * time.Second
	writeTimeout = 10 * time.Second
	toolTimeout  = 20 * time.Second
)

const timeoutOutput = `{"error":"工具超时"}`

type funcCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type Session struct {
	cfg    voice.Config
	log    *slog.Logger
	start  time.Time
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	wmu sync.Mutex      // guards writes and ws; only the event goroutine replaces ws (goAway)
	ws  *websocket.Conn // the event goroutine reads it without wmu

	amu   sync.Mutex // keeps Downsampler state, mute and append order together
	ds    voice.Downsampler
	muted bool

	mu     sync.Mutex
	closed bool
	turns  []voice.Turn

	tmu      sync.Mutex
	running  map[string]context.CancelFunc // function call id -> cancel, for toolCallCancellation
	inflight atomic.Int32                  // tool batches running; TurnDone waits until none are

	// Owned by the event goroutine.
	handle    string // latest resumable sessionResumptionUpdate.newHandle
	answering bool   // a modelTurn arrived since the last turnComplete
	cut       bool   // the answer was interrupted: drop its late audio until turnComplete
	in, out   strings.Builder
	usage     rawUsage
}

// Dial connects, sends setup and returns once setupComplete arrives.
func Dial(ctx context.Context, cfg voice.Config) (voice.Session, error) {
	s, err := dial(ctx, cfg)
	if err != nil {
		return nil, err // not a typed nil inside the interface
	}
	return s, nil
}

func dial(ctx context.Context, cfg voice.Config) (*Session, error) {
	if cfg.Key == "" || cfg.Model == "" {
		return nil, errors.New("gemini: Key and Model are required")
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.Voice == "" {
		cfg.Voice = "Kore"
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	sctx, cancel := context.WithCancel(context.Background())
	s := &Session{cfg: cfg, log: log, start: time.Now(), ctx: sctx, cancel: cancel,
		done: make(chan struct{}), running: map[string]context.CancelFunc{}}
	ws, err := s.connect(ctx, "")
	if err != nil {
		cancel()
		return nil, err
	}
	s.ws = ws
	go s.loop()
	return s, nil
}

// connect opens a connection, sends setup (resuming handle when set) and waits for setupComplete.
func (s *Session) connect(ctx context.Context, handle string) (*websocket.Conn, error) {
	u := s.cfg.Extra["url"]
	if u == "" {
		u = defaultURL
	}
	// The key goes in the query string; only u (without it) may reach logs and errors.
	wc, err := websocket.NewConfig(u+"?key="+url.QueryEscape(s.cfg.Key), origin)
	if err != nil {
		return nil, fmt.Errorf("gemini: %s: %w", u, s.scrub(err))
	}
	wc.Header.Set("User-Agent", "rex-voiced/"+s.cfg.Version)
	ws, err := wc.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("gemini: dial %s: %w", u, s.scrub(err))
	}
	if err := s.setup(ctx, ws, handle); err != nil {
		ws.Close()
		return nil, err
	}
	return ws, nil
}

// scrub drops the key from err's text: x/net/websocket puts the full URL in dial errors.
func (s *Session) scrub(err error) error {
	return errors.New(strings.NewReplacer(s.cfg.Key, "<key>", url.QueryEscape(s.cfg.Key), "<key>").Replace(err.Error()))
}

func (s *Session) setup(ctx context.Context, ws *websocket.Conn, handle string) error {
	if err := write(ws, s.setupMsg(handle)); err != nil {
		return fmt.Errorf("gemini: setup: %w", err)
	}
	deadline := time.Now().Add(readyTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := ws.SetReadDeadline(deadline); err != nil {
		return err
	}
	for {
		m, n, err := recv(ws)
		if err != nil {
			return fmt.Errorf("gemini: waiting for setupComplete: %w", err)
		}
		if m.SetupComplete != nil {
			return ws.SetReadDeadline(time.Time{})
		}
		s.log.Debug("gemini: event before setupComplete", "kind", m.kind(), "bytes", n)
	}
}

func (s *Session) setupMsg(handle string) map[string]any {
	// 11-01 实测：responseModalities 放 setup 顶层会被拒；gemini-3.8-live 拒绝 TEXT，所以文字模式
	// （rex-voiced chat）也要 AUDIO，文字从 outputTranscription 来、音频因 AudioDelta 为 nil 直接丢，按音频价计费。
	gen := map[string]any{
		"responseModalities": []string{"AUDIO"},
		"speechConfig":       map[string]any{"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": s.cfg.Voice}}},
	}
	resume := map[string]any{}
	if handle != "" {
		resume["handle"] = handle
	}
	c := map[string]any{
		"model":                    "models/" + strings.TrimPrefix(s.cfg.Model, "models/"),
		"generationConfig":         gen,
		"realtimeInputConfig":      map[string]any{"automaticActivityDetection": map[string]any{"silenceDurationMs": 700}},
		"outputAudioTranscription": map[string]any{}, // AssistantText comes from it
		"sessionResumption":        resume,
		"contextWindowCompression": map[string]any{"slidingWindow": map[string]any{}},
	}
	if s.cfg.Instructions != "" {
		c["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": s.cfg.Instructions}}}
	}
	if s.cfg.Transcribe {
		c["inputAudioTranscription"] = map[string]any{}
	}
	if len(s.cfg.Tools) > 0 {
		decls := make([]any, len(s.cfg.Tools))
		for i, t := range s.cfg.Tools {
			// parametersJsonSchema, not parameters: the latter is an OpenAPI subset that rejects
			// additionalProperties, which every rex tool schema has (11-01 实测 close 1007).
			decls[i] = map[string]any{"name": t.Name, "description": t.Description, "behavior": "BLOCKING", "parametersJsonSchema": t.Parameters}
		}
		c["tools"] = []any{map[string]any{"functionDeclarations": decls}}
	}
	return map[string]any{"setup": c}
}

// Age is the time since Dial.
func (s *Session) Age() time.Duration { return time.Since(s.start) }

// MaxAge is 0: goAway is handled inside by session resumption.
func (s *Session) MaxAge() time.Duration { return 0 }

// Done is closed when the event loop has stopped (read error, server close or Close).
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

// AppendAudio takes PCM16 24 kHz and sends it as 16 kHz; dropped while muted.
func (s *Session) AppendAudio(pcm []byte) error {
	s.amu.Lock()
	defer s.amu.Unlock()
	if s.muted {
		return nil
	}
	out, err := s.ds.Write(pcm)
	if err != nil || len(out) == 0 {
		return err
	}
	return s.send(map[string]any{"realtimeInput": map[string]any{"audio": map[string]any{
		"data": base64.StdEncoding.EncodeToString(out), "mimeType": "audio/pcm;rate=16000"}}})
}

// SetMuted(true) tells the server the stream paused (audioStreamEnd) so its VAD does not wait for more.
func (s *Session) SetMuted(muted bool) error {
	s.amu.Lock()
	defer s.amu.Unlock()
	if muted == s.muted {
		return nil
	}
	s.muted = muted
	if !muted {
		s.ds = voice.Downsampler{} // the history before the pause is stale
		return nil
	}
	return s.send(map[string]any{"realtimeInput": map[string]any{"audioStreamEnd": true}})
}

// SendText is a user text turn; it interrupts a running answer on the server side.
func (s *Session) SendText(text string) error {
	s.addTurn("user", text)
	return s.send(map[string]any{"clientContent": map[string]any{
		"turns": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": text}}}}, "turnComplete": true}})
}

// Cancel is a no-op: the server interrupts its own answer when the user speaks.
func (s *Session) Cancel() error { return nil }

func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.ws.Close()
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Session) send(v any) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return write(s.ws, v)
}

func write(ws *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := ws.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	// Clear it afterwards: the read loop writes pongs from inside HandleFrame and would hit a stale deadline.
	defer ws.SetWriteDeadline(time.Time{})
	return websocket.Message.Send(ws, string(b))
}

func (s *Session) sendFailed(what string, err error) {
	if s.isClosed() {
		s.log.Debug("gemini: send after close", "event", what)
		return
	}
	s.log.Warn("gemini: send failed", "event", what, "err", err)
}

// closeError is the server's close frame: Gemini reports every error this way (code 1007 / 1008 + reason).
type closeError struct {
	Code   int
	Reason string
}

func (e *closeError) Error() string {
	return fmt.Sprintf("服务端关闭连接 %d: %s", e.Code, e.Reason)
}

// readFrame is websocket.Message.Receive except that a close frame's code and reason are kept
// (x/net/websocket throws them away as io.EOF). Only the event goroutine reads.
func readFrame(ws *websocket.Conn) ([]byte, error) {
	for {
		fr, err := ws.NewFrameReader()
		if err != nil {
			return nil, err
		}
		if fr.PayloadType() == websocket.CloseFrame {
			b, _ := io.ReadAll(io.LimitReader(fr, 125))
			if len(b) < 2 {
				return nil, io.EOF
			}
			return nil, &closeError{int(binary.BigEndian.Uint16(b)), string(b[2:])}
		}
		if fr, err = ws.HandleFrame(fr); err != nil {
			return nil, err
		}
		if fr != nil {
			return io.ReadAll(io.LimitReader(fr, websocket.DefaultMaxPayloadBytes))
		}
	}
}

type part struct {
	Text       string `json:"text"`
	Thought    bool   `json:"thought"`
	InlineData *struct {
		Data string `json:"data"`
	} `json:"inlineData"`
}

type text struct {
	Text string `json:"text"`
}

type content struct {
	ModelTurn *struct {
		Parts []part `json:"parts"`
	} `json:"modelTurn"`
	Interrupted         bool  `json:"interrupted"`
	TurnComplete        bool  `json:"turnComplete"`
	InputTranscription  *text `json:"inputTranscription"`
	OutputTranscription *text `json:"outputTranscription"`
}

type serverMsg struct {
	SetupComplete *struct{} `json:"setupComplete"`
	ServerContent *content  `json:"serverContent"`
	ToolCall      *struct {
		FunctionCalls []funcCall `json:"functionCalls"`
	} `json:"toolCall"`
	ToolCallCancellation *struct {
		IDs []string `json:"ids"`
	} `json:"toolCallCancellation"`
	GoAway *struct {
		TimeLeft string `json:"timeLeft"`
	} `json:"goAway"`
	SessionResumptionUpdate *struct {
		NewHandle string `json:"newHandle"`
		Resumable bool   `json:"resumable"`
	} `json:"sessionResumptionUpdate"`
	VoiceActivity *struct {
		Type string `json:"type"` // ACTIVITY_START / ACTIVITY_END (11-01 实测)
	} `json:"voiceActivity"`
	UsageMetadata *rawUsage `json:"usageMetadata"`
}

// kind names the message for debug logs.
func (m *serverMsg) kind() string {
	switch {
	case m.SetupComplete != nil:
		return "setupComplete"
	case m.ToolCall != nil:
		return "toolCall"
	case m.ToolCallCancellation != nil:
		return "toolCallCancellation"
	case m.GoAway != nil:
		return "goAway"
	case m.SessionResumptionUpdate != nil:
		return "sessionResumptionUpdate"
	case m.VoiceActivity != nil:
		return "voiceActivity." + m.VoiceActivity.Type
	case m.ServerContent != nil && m.ServerContent.TurnComplete:
		return "turnComplete"
	case m.ServerContent != nil && m.ServerContent.ModelTurn != nil:
		return "modelTurn"
	case m.ServerContent != nil:
		return "serverContent"
	}
	return "empty"
}

func recv(ws *websocket.Conn) (*serverMsg, int, error) {
	b, err := readFrame(ws)
	if err != nil {
		return nil, 0, err
	}
	var m serverMsg
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, len(b), fmt.Errorf("gemini: bad message: %w", err)
	}
	return &m, len(b), nil
}

func (s *Session) loop() {
	defer close(s.done)
	for {
		m, n, err := recv(s.ws)
		if err == nil {
			s.log.Debug("gemini: event", "kind", m.kind(), "bytes", n)
			s.dispatch(m)
			if m.GoAway != nil {
				err = s.resume(m.GoAway.TimeLeft)
			}
		}
		if err != nil {
			if !s.isClosed() {
				if h := s.cfg.Handlers.Error; h != nil {
					h(fmt.Errorf("gemini: %w", err))
				}
				s.Close()
			}
			return
		}
	}
}

// resume swaps in a new connection that continues the conversation from the latest handle.
// Audio sent while it reconnects is lost; that is a fraction of a second every ~10 minutes.
func (s *Session) resume(timeLeft string) error {
	if s.handle == "" {
		s.log.Warn("gemini: goAway before any resumption handle, the new connection starts without context")
	}
	s.log.Info("gemini: goAway, resuming on a new connection", "time_left", timeLeft)
	ctx, cancel := context.WithTimeout(s.ctx, readyTimeout)
	defer cancel()
	ws, err := s.connect(ctx, s.handle)
	if err != nil {
		return fmt.Errorf("resume after goAway: %w", err)
	}
	s.wmu.Lock()
	old := s.ws
	s.ws = ws
	s.wmu.Unlock()
	old.Close()
	if s.isClosed() { // Close ran during connect and closed the old connection only
		ws.Close()
	}
	s.answering, s.cut = false, false
	return nil
}

func (s *Session) dispatch(m *serverMsg) {
	h := s.cfg.Handlers
	if m.UsageMetadata != nil {
		s.usage = *m.UsageMetadata // arrives on the turnComplete message (11-01)
	}
	if r := m.SessionResumptionUpdate; r != nil && r.Resumable && r.NewHandle != "" {
		s.handle = r.NewHandle
	}
	if va := m.VoiceActivity; va != nil {
		switch va.Type {
		case "ACTIVITY_START":
			s.cut = s.cut || s.answering
			if h.SpeechStarted != nil {
				h.SpeechStarted() // also stops playback of an answer whose generation already finished
			}
		case "ACTIVITY_END":
			s.flushUser()
		}
	}
	if c := m.ServerContent; c != nil {
		s.onContent(c)
	}
	if tc := m.ToolCall; tc != nil && len(tc.FunctionCalls) > 0 {
		s.runTools(tc.FunctionCalls)
	}
	if tc := m.ToolCallCancellation; tc != nil {
		s.tmu.Lock()
		for _, id := range tc.IDs {
			if cancel := s.running[id]; cancel != nil {
				cancel()
			}
		}
		s.tmu.Unlock()
		s.log.Debug("gemini: tool calls cancelled", "ids", tc.IDs)
	}
}

func (s *Session) onContent(c *content) {
	h := s.cfg.Handlers
	if t := c.InputTranscription; t != nil {
		s.in.WriteString(t.Text)
	}
	if c.Interrupted && !s.cut {
		s.cut = true
		if h.SpeechStarted != nil {
			h.SpeechStarted()
		}
	}
	if mt := c.ModelTurn; mt != nil {
		s.flushUser()
		s.answering = true
		for _, p := range mt.Parts {
			switch {
			case p.Thought:
			case p.InlineData != nil:
				if s.cut || h.AudioDelta == nil {
					continue
				}
				pcm, err := base64.StdEncoding.DecodeString(p.InlineData.Data)
				if err != nil {
					s.log.Warn("gemini: bad audio part", "err", err)
					continue
				}
				h.AudioDelta(pcm)
			case p.Text != "" && s.cfg.TextOnly: // in audio mode the text comes from outputTranscription
				s.text(p.Text)
			}
		}
	}
	if t := c.OutputTranscription; t != nil && t.Text != "" {
		s.text(t.Text)
	}
	if c.TurnComplete {
		s.onTurnComplete()
	}
}

func (s *Session) text(t string) {
	s.out.WriteString(t)
	if h := s.cfg.Handlers.TextDelta; h != nil {
		h(t)
	}
}

func (s *Session) flushUser() {
	t := s.in.String()
	s.in.Reset()
	if strings.TrimSpace(t) == "" {
		return
	}
	s.addTurn("user", t)
	if h := s.cfg.Handlers.UserText; h != nil {
		h(t)
	}
}

func (s *Session) onTurnComplete() {
	h := s.cfg.Handlers
	s.flushUser()
	if t := s.out.String(); strings.TrimSpace(t) != "" {
		s.addTurn("assistant", t)
		if h.AssistantText != nil {
			h.AssistantText(t)
		}
	}
	s.out.Reset()
	s.answering, s.cut = false, false
	u := s.usage.toUsage(s.cfg.Model)
	s.usage = rawUsage{}
	s.writeUsage(u)
	if h.ResponseDone != nil {
		h.ResponseDone(u.public())
	}
	if s.inflight.Load() > 0 {
		return // a tool batch is still running; the answer to its results ends the turn
	}
	if h.TurnDone != nil {
		h.TurnDone()
	}
}

// runTools executes one toolCall's calls in parallel and sends one toolResponse with every call
// that was not cancelled; nothing when all were.
func (s *Session) runTools(calls []funcCall) {
	s.inflight.Add(1)
	ctxs := make([]context.Context, len(calls))
	s.tmu.Lock()
	for i, c := range calls {
		ctx, cancel := context.WithCancel(s.ctx)
		ctxs[i] = ctx
		s.running[c.ID] = cancel
	}
	s.tmu.Unlock()
	go func() {
		outs := make([]string, len(calls))
		var wg sync.WaitGroup
		for i, c := range calls {
			wg.Go(func() { outs[i] = s.callTool(ctxs[i], c) })
		}
		wg.Wait()
		var resps []any
		s.tmu.Lock()
		for i, c := range calls {
			if ctxs[i].Err() == nil {
				resps = append(resps, map[string]any{"id": c.ID, "name": c.Name, "response": map[string]any{"result": outs[i]}})
			}
			if cancel := s.running[c.ID]; cancel != nil {
				cancel()
				delete(s.running, c.ID)
			}
		}
		s.tmu.Unlock()
		s.inflight.Add(-1) // before sending, so the turnComplete it causes sees zero
		if len(resps) == 0 {
			return
		}
		if err := s.send(map[string]any{"toolResponse": map[string]any{"functionResponses": resps}}); err != nil {
			s.sendFailed("toolResponse", err)
		}
	}()
}

func (s *Session) callTool(ctx context.Context, c funcCall) string {
	if s.cfg.Call == nil {
		return `{"error":"没有配置工具"}`
	}
	args := string(c.Args)
	if args == "" || args == "null" {
		args = "{}"
	}
	tctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	res := make(chan string, 1)
	go func() { res <- s.cfg.Call(tctx, c.Name, args) }()
	select {
	case out := <-res:
		return out
	case <-tctx.Done():
		if ctx.Err() != nil {
			s.log.Debug("gemini: tool cancelled", "tool", c.Name)
			return ""
		}
		s.log.Warn("gemini: tool timeout", "tool", c.Name)
		return timeoutOutput
	}
}
