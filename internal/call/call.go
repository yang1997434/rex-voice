package call

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/voice"
)

const (
	frameBytes = 960 // 20 ms of PCM16 mono 24 kHz, the phone's frame size
	idleText   = "（系统提示：用户 60 秒没说话，用一句话问他还在吗，并说没事就先挂了）"
)

// callState is one phone call; owned by the Run goroutine.
type callState struct {
	vc        voice.Config  // this call's provider settings, reused on rotation
	sess      voice.Session // nil while the first Dial runs, and always in echo mode
	sid       int           // id of the session whose events are current
	echo      bool
	rotating  bool
	rotateOK  time.Time // earliest next rotation attempt after a failed one
	state     string    // last status sent: connecting | listening | thinking | speaking
	lastAudio time.Time // last real phone frame, or call start; drives the idle prompt and hang-up
	prompted  bool
	lastFrame time.Time    // last real phone frame sent to the current session; zero until one is
	fill      *time.Ticker // silence fill; nil in echo mode
	muted     bool         // SetMuted(true) sent to the current session
	hangup    *time.Timer  // non-nil once hang_up was called: the call ends at TurnDone or when it fires

	rem []byte // tail shorter than one frame, held until more audio or the turn ends

	echoFrames int
	echoQ      chan echoFrame // replayed in order by echoLoop; closed when the call ends
	lastReport time.Time
}

type echoFrame struct {
	at  time.Time
	pcm []byte
}

// startReq is call.start's optional provider choice (protocol section 3). APIKey is never logged.
type startReq struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Voice    string `json:"voice"`
	APIKey   string `json:"api_key"`
}

// voiceConfig merges call.start over config.json; voice.Dial fills what is still empty from voice.Defaults.
func (s *server) voiceConfig(r startReq) (voice.Config, error) {
	vc := s.cfg.Voice
	p := voice.Provider(r.Provider)
	if p == "" {
		p = vc.Provider
	}
	if m, _ := voice.Defaults(p); m == "" {
		return vc, fmt.Errorf("不支持的供应商: %s", p)
	}
	if p != vc.Provider { // config.json's model, voice and key belong to its own provider
		vc.Provider, vc.Model, vc.Voice, vc.Key = p, "", "", ""
	}
	if r.Model != "" {
		vc.Model = r.Model
	}
	if r.Voice != "" {
		vc.Voice = r.Voice
	}
	if r.APIKey != "" {
		vc.Key = r.APIKey
	}
	return vc, nil
}

func (s *server) startCall(r startReq) {
	if c := s.call; c != nil {
		s.sendCtl(map[string]any{"t": "status", "state": c.state})
		return
	}
	if s.preview != nil {
		s.sendCtl(map[string]any{"t": "preview", "state": "error", "msg": "已取消"})
		s.stopPreview()
	}
	var vc voice.Config
	if !s.cfg.Echo {
		var err error
		if vc, err = s.voiceConfig(r); err != nil {
			s.log.Warn("call.start rejected", "err", err)
			s.sendCtl(map[string]any{"t": "error", "msg": err.Error()})
			return
		}
	}
	now := time.Now()
	c := &callState{lastAudio: now, lastReport: now, echo: s.cfg.Echo, vc: vc}
	s.call = c
	s.log.Info("call started", "echo", c.echo, "provider", vc.Provider, "model", vc.Model, "voice", vc.Voice, "phone_key", r.APIKey != "")
	if c.echo {
		c.echoQ = make(chan echoFrame, 512)
		go s.echoLoop(c)
		s.setState(c, "listening")
		return
	}
	s.setState(c, "connecting")
	c.fill = time.NewTicker(fillEvery)
	s.dialSession(c, s.instructions)
}

func (s *server) instructions() string {
	if s.cfg.Instructions != nil {
		return s.cfg.Instructions()
	}
	return s.cfg.Voice.Instructions
}

// dialSession builds the instructions, looks up the fallback key and opens a provider session off
// the Run goroutine; dialed picks up the result.
func (s *server) dialSession(c *callState, instructions func() string) {
	s.nextSID++
	sid := s.nextSID
	vc := c.vc
	vc.Handlers = s.handlers(sid)
	go func() {
		vc.Instructions = instructions()
		if vc.Key == "" && s.cfg.Key != nil {
			vc.Key = s.cfg.Key(vc.Provider)
		}
		ctx, cancel := context.WithTimeout(s.ctx, dialTimeout)
		defer cancel()
		sess, err := voice.Dial(ctx, vc)
		if !s.post(func() { s.dialed(c, sid, sess, err) }) && sess != nil {
			sess.Close()
		}
	}()
}

func (s *server) dialed(c *callState, sid int, sess voice.Session, err error) {
	if s.call != c { // call ended while dialing
		if sess != nil {
			sess.Close()
		}
		return
	}
	if err != nil {
		if c.rotating {
			c.rotating, c.rotateOK = false, time.Now().Add(30*time.Second)
			s.log.Warn("session rotation failed, keeping the old session", "err", err)
			return
		}
		s.log.Error("voice dial failed", "provider", c.vc.Provider, "err", err)
		s.sendCtl(map[string]any{"t": "error", "msg": fmt.Sprintf("连不上 %s: %v", c.vc.Provider, err)})
		s.endCall("error")
		return
	}
	old := c.sess
	c.sess, c.sid, c.rotating, c.lastFrame, c.muted = sess, sid, false, time.Time{}, false
	if old != nil {
		old.Close()
		s.log.Info("voice session rotated")
	} else {
		s.setState(c, "listening")
	}
	go func() {
		<-sess.Done()
		s.post(func() {
			if s.call == c && c.sid == sid { // not closed by us: the read loop died (Handlers.Error already sent the error frame)
				s.log.Warn("voice session dropped")
				s.endCall("error")
			}
		})
	}()
}

// handlers forward provider events to the Run goroutine, dropping those from a session that is no longer current.
func (s *server) handlers(sid int) voice.Handlers {
	on := func(f func(c *callState)) {
		s.post(func() {
			if c := s.call; c != nil && c.sid == sid {
				f(c)
			}
		})
	}
	transcript := func(who string) func(string) {
		return func(text string) {
			on(func(*callState) { s.sendCtl(map[string]any{"t": "transcript", "who": who, "text": text}) })
		}
	}
	return voice.Handlers{
		AudioDelta:    func(pcm []byte) { on(func(c *callState) { s.onModelAudio(c, pcm) }) },
		AssistantText: transcript("assistant"),
		UserText:      transcript("user"),
		SpeechStarted: func() { on(s.onSpeechStarted) },
		TurnDone: func() {
			on(func(c *callState) {
				s.flushAudio(c)
				if c.hangup != nil { // the goodbye is out: now hang up
					s.endCall("user")
					return
				}
				s.setState(c, "listening")
			})
		},
		Error: func(err error) {
			on(func(*callState) {
				s.log.Warn("voice error", "err", err)
				s.sendCtl(map[string]any{"t": "error", "msg": err.Error()})
			})
		},
	}
}

func (s *server) callTool(ctx context.Context, name, args string) string {
	s.post(func() {
		if c := s.call; c != nil {
			s.setState(c, "thinking")
		}
	})
	return s.cfg.Tools.Call(ctx, name, args)
}

func (s *server) setState(c *callState, state string) {
	if c.state != state {
		c.state = state
		s.sendCtl(map[string]any{"t": "status", "state": state})
	}
}

// onModelAudio cuts model audio into ≤ 960-byte frames.
func (s *server) onModelAudio(c *callState, pcm []byte) {
	s.setState(c, "speaking")
	c.rem = s.sendFrames(append(c.rem, pcm...))
}

// sendFrames sends the whole 960-byte frames of buf and returns the shorter tail.
func (s *server) sendFrames(buf []byte) []byte {
	for len(buf) >= frameBytes {
		s.sendAudio(buf[:frameBytes])
		buf = buf[frameBytes:]
	}
	return buf
}

func (s *server) flushAudio(c *callState) {
	if len(c.rem) > 0 {
		s.sendAudio(c.rem)
		c.rem = nil
	}
}

// onSpeechStarted stops phone playback; the provider cuts its own answer (protocol section 4).
func (s *server) onSpeechStarted(c *callState) {
	s.sendCtl(map[string]any{"t": "interrupt"})
	c.rem = nil
	s.setState(c, "listening")
}

func (s *server) onPhoneAudio(pcm []byte) {
	c := s.call
	if c == nil {
		return
	}
	c.lastAudio, c.prompted = time.Now(), false
	if c.echo {
		c.echoFrames++
		select {
		case c.echoQ <- echoFrame{time.Now().Add(echoDelay), pcm}:
		default:
			s.log.Warn("echo queue full, frame dropped")
		}
		return
	}
	if c.sess != nil {
		c.lastFrame = time.Now()
		if c.muted {
			c.muted = false
			if err := c.sess.SetMuted(false); err != nil {
				s.log.Warn("unmute failed", "err", err)
			}
		}
		if err := c.sess.AppendAudio(pcm); err != nil {
			s.log.Warn("append audio failed", "err", err)
		}
	}
}

var silenceFrame = make([]byte, frameBytes)

func (s *server) fillTick() <-chan time.Time {
	if s.call == nil || s.call.fill == nil {
		return nil
	}
	return s.call.fill.C
}

// onFillTick tells the provider the phone went quiet (SetMuted) and keeps server VAD fed after the
// phone stops sending (it gates on energy), for fillFor only so a long silence is not billed.
// Fill does not count as audio for the idle timers.
func (s *server) onFillTick(now time.Time) {
	c := s.call
	if c.sess == nil || c.rotating || c.lastFrame.IsZero() {
		return
	}
	gap := now.Sub(c.lastFrame)
	if gap < fillAfter {
		return
	}
	if !c.muted {
		c.muted = true
		if err := c.sess.SetMuted(true); err != nil {
			s.log.Warn("mute failed", "err", err)
		}
	}
	if gap > fillFor {
		return
	}
	if err := c.sess.AppendAudio(silenceFrame); err != nil {
		s.log.Warn("append silence failed", "err", err)
	}
}

func (s *server) onTick(now time.Time) {
	if s.ws != nil && now.Sub(s.lastWrite) >= keepaliveEvery {
		s.writeRaw(pair.KeepaliveFrame())
	}
	if p := s.preview; p != nil && now.Sub(p.t0) >= previewTimeout {
		s.failPreview("超时")
	}
	c := s.call
	if c == nil {
		return
	}
	switch idle := now.Sub(c.lastAudio); {
	case idle >= idleHangup:
		s.log.Info("no audio, hanging up", "idle", idle.Round(time.Second))
		s.endCall("idle")
		return
	case idle >= idlePrompt && !c.prompted && c.sess != nil:
		c.prompted = true
		if err := c.sess.SendText(idleText); err != nil {
			// providers without text input: the phone shows the question instead of the model asking it
			s.log.Warn("idle prompt failed", "err", err)
			s.sendCtl(map[string]any{"t": "transcript", "who": "assistant", "text": "还在吗？再没声音就先挂了。"})
		}
	}
	if c.echo && now.Sub(c.lastReport) >= echoReport {
		c.lastReport = now
		s.sendCtl(map[string]any{"t": "transcript", "who": "assistant", "text": fmt.Sprintf("echo: 收到 %d 帧", c.echoFrames)})
	}
	// Providers cap a session (MaxAge); swap rotateMargin before that, only between turns so no answer is cut.
	if c.sess == nil || c.rotating || c.state != "listening" || !now.After(c.rotateOK) {
		return
	}
	if limit := c.sess.MaxAge() - rotateMargin; limit > 0 && c.sess.Age() >= limit {
		c.rotating = true
		var b strings.Builder
		fmt.Fprintf(&b, "\n\n（系统提示：通话超过 %d 分钟换了新会话，下面是刚才最后几轮，接着聊，不要提换会话的事）", int(limit.Minutes()))
		for _, t := range c.sess.RecentTurns(5) {
			fmt.Fprintf(&b, "\n%s：%s", map[string]string{"user": "用户", "assistant": "你"}[t.Role], t.Text)
		}
		s.log.Info("rotating voice session", "age", c.sess.Age().Round(time.Second))
		recent := b.String()
		s.dialSession(c, func() string { return s.instructions() + recent })
	}
}

func (s *server) echoLoop(c *callState) {
	for f := range c.echoQ {
		time.Sleep(time.Until(f.at))
		s.post(func() {
			if s.call == c {
				s.sendAudio(f.pcm)
			}
		})
	}
}

// endCall closes the provider session and tells the phone; the connection stays up for the next call.start.
func (s *server) endCall(reason string) {
	c := s.call
	if c == nil {
		return
	}
	s.call = nil
	if c.fill != nil {
		c.fill.Stop()
	}
	if c.hangup != nil {
		c.hangup.Stop()
	}
	if c.sess != nil {
		c.sess.Close()
	}
	if c.echoQ != nil {
		close(c.echoQ)
	}
	s.sendCtl(map[string]any{"t": "status", "state": "ended"})
	s.sendCtl(map[string]any{"t": "hang_up", "reason": reason})
	s.log.Info("call ended", "reason", reason)
}
