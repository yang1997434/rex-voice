package call

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/tools"
)

// What the Mac pushes on its own: the delayed hang_up, Claude-blocked alerts and show_file / show_link.

const (
	attachChunk    = 60000 // type 4 payload per frame; with header and tag it stays under the relay's 64 KiB
	maxPushOptions = 4     // notify carries at most this many options, each cut to 40 runes
	blockedText    = "（系统提示：%s 的 Claude 卡住了，等你回答：%s。用一句话告诉用户，问要不要听全文或替他回答）"
)

// onHangUpTool lets the model finish its goodbye: the call ends at the next TurnDone, or after hangupGrace.
func (s *server) onHangUpTool(reason string) {
	c := s.call
	if c == nil || c.hangup != nil {
		return
	}
	s.log.Info("hang_up tool, ending after this turn", "reason", reason)
	c.hangup = time.AfterFunc(hangupGrace, func() {
		s.post(func() {
			if s.call == c {
				s.log.Info("no turn end after hang_up, hanging up")
				s.endCall("user")
			}
		})
	})
}

// onBlocked tells the user a Claude is waiting: an ask control to the phone (call or not, so the app
// can answer it), plus through the model in a call, else a notify the relay turns into a push (at
// most one per pane and msg per notifyEvery; the pane's next question pushes again). The push
// carries no full question. Watch calls it again when a still-blocked pane's msg changes.
func (s *server) onBlocked(b tools.Blocked) {
	s.asks[b.Block] = b
	s.sendAsk(b)
	msg := strings.TrimSpace(b.Msg)
	if msg == "" {
		msg = "（没写问题）"
	}
	if c := s.call; c != nil {
		if c.sess == nil { // still dialing: the user is on the phone, a push would only duplicate
			s.log.Info("claude blocked while dialing, skipped", "block", b.Block, "project", b.Project)
			return
		}
		s.log.Info("claude blocked, telling the call", "block", b.Block, "project", b.Project)
		if err := c.sess.SendText(fmt.Sprintf(blockedText, b.Project, truncRunes(msg, 200))); err != nil {
			s.log.Warn("blocked prompt failed", "err", err)
			s.sendCtl(map[string]any{"t": "transcript", "who": "assistant", "text": b.Project + " 的 Claude 在等你回答"})
		}
		return
	}
	now := time.Now()
	if n, ok := s.notified[b.Block]; ok && n.msg == b.Msg && now.Sub(n.at) < notifyEvery {
		s.log.Info("claude blocked, notify skipped: one sent recently", "block", b.Block, "project", b.Project)
		return
	}
	s.notified[b.Block] = notice{now, b.Msg}
	s.log.Info("claude blocked, notifying", "block", b.Block, "project", b.Project)
	opts := make([]string, 0, maxPushOptions)
	for _, o := range b.Options[:min(len(b.Options), maxPushOptions)] {
		opts = append(opts, truncRunes(o, 40))
	}
	s.writeText(map[string]any{"t": "notify", "title": b.Project + " 的 Claude 在等你", "body": truncRunes(msg, 100),
		"block": b.Block, "options": opts})
}

// notice is the last notify sent for a pane.
type notice struct {
	at  time.Time
	msg string
}

// onUnblocked: the pane is no longer waiting, so the app drops its question card.
func (s *server) onUnblocked(block string) {
	delete(s.asks, block)
	s.phoneCtl(map[string]any{"t": "ask_done", "block": block})
}

func (s *server) sendAsk(b tools.Blocked) {
	opts := b.Options
	if opts == nil {
		opts = []string{}
	}
	s.phoneCtl(map[string]any{"t": "ask", "block": b.Block, "session": b.Session, "project": b.Project,
		"msg": truncRunes(strings.TrimSpace(b.Msg), 400), "options": opts})
}

// phoneCtl sends a control only to a phone that has said hello; otherwise it is dropped (the
// phone gets every open ask again after its next hello).
func (s *server) phoneCtl(v map[string]any) {
	if s.tx == nil || !s.helloOK {
		s.log.Info("phone offline, control dropped", "t", v["t"], "block", v["block"])
		return
	}
	s.log.Info("to phone", "t", v["t"], "block", v["block"], "options", v["options"])
	s.sendCtl(v)
}

// answer is the app answering a Claude's question without a call: keys through send_keys, else text
// through send_to_pane, so both keep their gates and audit lines (source phone). The pane need not be
// blocked (the user may type ahead); a pane that does not exist fails. Runs the tool off the Run goroutine.
func (s *server) answer(block string, keys []string, text string) {
	if s.cfg.Echo {
		s.sendCtl(map[string]any{"t": "answer_result", "block": block, "ok": false, "msg": "Mac 在 echo 模式，不能回答"})
		return
	}
	name, args := "send_to_pane", map[string]any{"block": block, "text": text}
	if len(keys) > 0 {
		name, args = "send_keys", map[string]any{"block": block, "keys": keys}
	}
	b, _ := json.Marshal(args)
	go func() {
		out := s.cfg.Tools.Call(tools.WithSource(s.ctx, "phone"), name, string(b))
		var r struct{ Error, Refused string }
		json.Unmarshal([]byte(out), &r)
		msg := r.Error + r.Refused
		s.log.Info("answer from phone", "block", block, "tool", name, "ok", msg == "", "msg", msg)
		res := map[string]any{"t": "answer_result", "block": block, "ok": msg == ""}
		if msg != "" {
			res["msg"] = msg
		}
		s.post(func() { s.sendCtl(res) })
	}()
}

// onRun runs f on the Run goroutine and waits for its result; for tools, which run off it.
func (s *server) onRun(f func() error) error {
	res := make(chan error, 1)
	if !s.post(func() { res <- f() }) {
		return errors.New("服务正在退出")
	}
	select {
	case err := <-res:
		return err
	case <-s.ctx.Done():
		return errors.New("服务正在退出")
	}
}

func (s *server) canShow() error {
	if s.call == nil {
		return errors.New("没有通话")
	}
	if s.tx == nil || !s.helloOK {
		return errors.New("手机没连上")
	}
	return nil
}

// showFile is show_file's sender: an attach control, then every chunk as a type 4 frame.
func (s *server) showFile(name string, jpeg []byte) (int, error) {
	chunks := (len(jpeg) + attachChunk - 1) / attachChunk
	return chunks, s.onRun(func() error {
		if err := s.canShow(); err != nil {
			return err
		}
		s.nextAttach++
		id := s.nextAttach
		s.sendCtl(map[string]any{"t": "attach", "id": id, "name": name, "mime": "image/jpeg", "size": len(jpeg), "chunks": chunks})
		for i := range chunks {
			data := jpeg[i*attachChunk : min((i+1)*attachChunk, len(jpeg))]
			plain := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(make([]byte, 0, 8+len(data)), id), uint32(i))
			s.writeRaw(s.tx.Seal(pair.TypeAttach, append(plain, data...)))
		}
		s.log.Info("attachment sent", "name", name, "bytes", len(jpeg), "chunks", chunks)
		return nil
	})
}

func (s *server) showLink(url, title string) error {
	return s.onRun(func() error {
		if err := s.canShow(); err != nil {
			return err
		}
		s.sendCtl(map[string]any{"t": "link", "url": url, "title": title})
		return nil
	})
}

func truncRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
