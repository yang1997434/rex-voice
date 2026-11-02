package call

import (
	"context"
	"fmt"
	"time"

	"github.com/yang1997434/rex-voice/internal/voice"
)

const (
	previewInstructions = "你是电话助手，只用中文。"
	previewText         = "用一句话打招呼，说：%s你好，我是你的语音助手，很高兴为你服务。"
)

// previewState is one voice.preview (protocol section 3): a short provider session that says one
// line in the chosen voice, outside any call. Owned by the Run goroutine.
type previewState struct {
	provider voice.Provider
	t0       time.Time
	sess     voice.Session // nil while dialing
	playing  bool          // "playing" sent
	rem      []byte        // tail shorter than one frame
	text     string        // what to say; greets voice.preview's name when it brought one
}

func (s *server) sendPreview(state, msg string) {
	m := map[string]any{"t": "preview", "state": state}
	if msg != "" {
		m["msg"] = msg
	}
	s.sendCtl(m)
}

func (s *server) startPreview(r startReq) {
	if s.call != nil { // the voice is fixed once a session has spoken, so no preview mid-call
		s.sendPreview("error", "通话中不能试听")
		return
	}
	if s.cfg.Echo {
		s.sendPreview("error", "echo 模式不能试听")
		return
	}
	s.stopPreview() // a newer request replaces the old one without its done
	vc, err := s.voiceConfig(r)
	if err != nil {
		s.log.Warn("preview failed", "err", err)
		s.sendPreview("error", err.Error())
		return
	}
	vc.TextOnly, vc.Transcribe, vc.Tools, vc.Call, vc.Instructions = false, false, nil, nil, previewInstructions
	p := &previewState{provider: vc.Provider, t0: time.Now(), text: fmt.Sprintf(previewText, r.Name)}
	s.preview = p
	vc.Handlers = s.previewHandlers(p)
	s.sendPreview("connecting", "")
	s.log.Info("preview started", "provider", vc.Provider, "model", vc.Model, "voice", vc.Voice, "phone_key", r.APIKey != "")
	go func() {
		if vc.Key == "" && s.cfg.Key != nil {
			vc.Key = s.cfg.Key(vc.Provider)
		}
		ctx, cancel := context.WithTimeout(s.ctx, dialTimeout)
		defer cancel()
		sess, err := voice.Dial(ctx, vc)
		if !s.post(func() { s.previewDialed(p, sess, err) }) && sess != nil {
			sess.Close()
		}
	}()
}

func (s *server) previewDialed(p *previewState, sess voice.Session, err error) {
	if s.preview != p { // stopped while dialing
		if sess != nil {
			sess.Close()
		}
		return
	}
	if err != nil {
		s.failPreview(fmt.Sprintf("连不上 %s: %v", p.provider, err))
		return
	}
	p.sess = sess
	go func() {
		<-sess.Done()
		s.post(func() {
			if s.preview == p { // not closed by us; Handlers.Error usually got there first
				s.failPreview("语音会话断开")
			}
		})
	}()
	if err := sess.SendText(p.text); err != nil {
		s.failPreview(err.Error())
	}
}

func (s *server) previewHandlers(p *previewState) voice.Handlers {
	on := func(f func()) {
		s.post(func() {
			if s.preview == p {
				f()
			}
		})
	}
	return voice.Handlers{
		AudioDelta: func(pcm []byte) {
			on(func() {
				if !p.playing {
					p.playing = true
					s.sendPreview("playing", "")
				}
				p.rem = s.sendFrames(append(p.rem, pcm...))
			})
		},
		TurnDone: func() {
			on(func() {
				if !p.playing {
					s.failPreview("模型没有出声")
					return
				}
				if len(p.rem) > 0 {
					s.sendAudio(p.rem)
				}
				s.sendPreview("done", "")
				s.log.Info("preview done", "ms", time.Since(p.t0).Milliseconds())
				s.stopPreview()
			})
		},
		Error: func(err error) { on(func() { s.failPreview(err.Error()) }) },
	}
}

func (s *server) failPreview(msg string) {
	s.log.Warn("preview failed", "err", msg)
	s.sendPreview("error", msg)
	s.stopPreview()
}

// stopPreview closes the preview session, if any, without telling the phone.
func (s *server) stopPreview() {
	p := s.preview
	if p == nil {
		return
	}
	s.preview = nil
	if p.sess != nil {
		p.sess.Close()
	}
}
