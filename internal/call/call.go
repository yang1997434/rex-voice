package call

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yang1997434/rex-voice/internal/openai"
	"github.com/yang1997434/rex-voice/internal/pair"
)

const (
	frameBytes = 960 // 20 ms of PCM16 mono 24 kHz, the phone's frame size
	bytesPerMs = 48
	idleText   = "（系统提示：用户 60 秒没说话，用一句话问他还在吗，并说没事就先挂了）"
)

// callState is one phone call; owned by the Run goroutine.
type callState struct {
	sess      *openai.Session // nil while the first Dial runs, and always in echo mode
	sid       int             // id of the session whose events are current
	echo      bool
	rotating  bool
	rotateOK  time.Time // earliest next rotation attempt after a failed one
	state     string    // last status sent: connecting | listening | thinking | speaking
	lastAudio time.Time
	prompted  bool

	item      string // assistant audio item being streamed to the phone
	itemBytes int    // bytes of item already sent
	rem       []byte // tail shorter than one frame, held until more audio or the turn ends
	cut       string // item interrupted by the user; its late deltas are dropped

	echoFrames int
	echoQ      chan echoFrame // replayed in order by echoLoop; closed when the call ends
	lastReport time.Time
}

type echoFrame struct {
	at  time.Time
	pcm []byte
}

func (s *server) startCall() {
	if c := s.call; c != nil {
		s.sendCtl(map[string]any{"t": "status", "state": c.state})
		return
	}
	now := time.Now()
	c := &callState{lastAudio: now, lastReport: now, echo: s.cfg.Echo}
	s.call = c
	s.log.Info("call started", "echo", c.echo)
	if c.echo {
		c.echoQ = make(chan echoFrame, 512)
		go s.echoLoop(c)
		s.setState(c, "listening")
		return
	}
	s.setState(c, "connecting")
	s.dialSession(c, s.cfg.OpenAI.Instructions)
}

// dialSession opens an OpenAI session off the Run goroutine; dialed picks up the result.
func (s *server) dialSession(c *callState, instructions string) {
	s.nextSID++
	sid := s.nextSID
	oc := s.cfg.OpenAI
	oc.Instructions = instructions
	oc.Handlers = s.handlers(sid)
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, dialTimeout)
		defer cancel()
		sess, err := openai.Dial(ctx, oc)
		if !s.post(func() { s.dialed(c, sid, sess, err) }) && sess != nil {
			sess.Close()
		}
	}()
}

func (s *server) dialed(c *callState, sid int, sess *openai.Session, err error) {
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
		s.log.Error("openai dial failed", "err", err)
		s.sendCtl(map[string]any{"t": "error", "msg": "连不上 OpenAI: " + err.Error()})
		s.endCall("error")
		return
	}
	old := c.sess
	c.sess, c.sid, c.rotating = sess, sid, false
	if old != nil {
		old.Close()
		s.log.Info("openai session rotated")
	} else {
		s.setState(c, "listening")
	}
	go func() {
		<-sess.Done()
		s.post(func() {
			if s.call == c && c.sid == sid { // not closed by us: the read loop died (Handlers.Error already sent the error frame)
				s.log.Warn("openai session dropped")
				s.endCall("error")
			}
		})
	}()
}

// handlers forward OpenAI events to the Run goroutine, dropping those from a session that is no longer current.
func (s *server) handlers(sid int) openai.Handlers {
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
	return openai.Handlers{
		AudioDelta:    func(item string, pcm []byte) { on(func(c *callState) { s.onModelAudio(c, item, pcm) }) },
		AssistantText: transcript("assistant"),
		UserText:      transcript("user"),
		SpeechStarted: func() { on(s.onSpeechStarted) },
		TurnDone: func() {
			on(func(c *callState) {
				s.flushAudio(c)
				c.item, c.itemBytes = "", 0
				s.setState(c, "listening")
			})
		},
		Error: func(err error) {
			on(func(*callState) {
				s.log.Warn("openai error", "err", err)
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

// onModelAudio cuts model audio into ≤ 960-byte frames and counts what each item has sent, for truncation.
func (s *server) onModelAudio(c *callState, item string, pcm []byte) {
	if item == c.cut {
		return
	}
	if item != c.item {
		s.flushAudio(c)
		c.item, c.itemBytes = item, 0
	}
	s.setState(c, "speaking")
	c.rem = append(c.rem, pcm...)
	for len(c.rem) >= frameBytes {
		s.sendAudio(c.rem[:frameBytes])
		c.itemBytes += frameBytes
		c.rem = c.rem[frameBytes:]
	}
}

func (s *server) flushAudio(c *callState) {
	if len(c.rem) > 0 {
		s.sendAudio(c.rem)
		c.itemBytes += len(c.rem)
		c.rem = nil
	}
}

// onSpeechStarted stops phone playback and truncates the item to what was sent (protocol section 4).
func (s *server) onSpeechStarted(c *callState) {
	s.sendCtl(map[string]any{"t": "interrupt"})
	if c.item != "" && c.sess != nil {
		if err := c.sess.Truncate(c.item, c.itemBytes/bytesPerMs); err != nil {
			s.log.Warn("truncate failed", "err", err)
		}
		c.cut = c.item
	}
	c.item, c.itemBytes, c.rem = "", 0, nil
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
		if err := c.sess.AppendAudio(pcm); err != nil {
			s.log.Warn("append audio failed", "err", err)
		}
	}
}

func (s *server) onTick(now time.Time) {
	if s.ws != nil && now.Sub(s.lastWrite) >= keepaliveEvery {
		s.writeRaw(pair.KeepaliveFrame())
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
			s.log.Warn("idle prompt failed", "err", err)
		}
	}
	if c.echo && now.Sub(c.lastReport) >= echoReport {
		c.lastReport = now
		s.sendCtl(map[string]any{"t": "transcript", "who": "assistant", "text": fmt.Sprintf("echo: 收到 %d 帧", c.echoFrames)})
	}
	// OpenAI caps a session at 60 minutes; swap only between turns so no answer is cut.
	if c.sess != nil && !c.rotating && c.state == "listening" && c.sess.Age() >= rotateAfter && now.After(c.rotateOK) {
		c.rotating = true
		var b strings.Builder
		b.WriteString(s.cfg.OpenAI.Instructions)
		b.WriteString("\n\n（系统提示：通话超过 55 分钟换了新会话，下面是刚才最后几轮，接着聊，不要提换会话的事）")
		for _, t := range c.sess.RecentTurns(5) {
			fmt.Fprintf(&b, "\n%s：%s", map[string]string{"user": "用户", "assistant": "你"}[t.Role], t.Text)
		}
		s.log.Info("rotating openai session", "age", c.sess.Age().Round(time.Second))
		s.dialSession(c, b.String())
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

// endCall closes OpenAI and tells the phone; the connection stays up for the next call.start.
func (s *server) endCall(reason string) {
	c := s.call
	if c == nil {
		return
	}
	s.call = nil
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
