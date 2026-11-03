package call

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/net/websocket"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yang1997434/rex-voice/internal/relay"
	"github.com/yang1997434/rex-voice/internal/tools"
)

// The phone gets an ask when a Claude turns blocked (no call needed), ask_done when it stops, and every
// still-open ask again after a reconnect's hello.
func TestAskSentDoneAndReplayed(t *testing.T) {
	var blocked atomic.Bool
	e := startWith(t, false, "", relay.NewHub(), withWatch(t, &blocked))
	ph := e.ready(t)
	blocked.Store(true)
	m := ph.ctlUntil(t, "ask")
	if m["block"] != "b1" || m["session"] != "dev" || m["project"] != "dev" || m["msg"] != "要合并 PR 吗？" {
		t.Fatalf("ask = %v", m)
	}
	if opts, _ := m["options"].([]any); len(opts) != 5 || opts[0] != "合并" {
		t.Fatalf("ask options = %v", m["options"])
	}
	blocked.Store(false)
	if m := ph.ctlUntil(t, "ask_done"); m["block"] != "b1" {
		t.Fatalf("ask_done = %v", m)
	}
	blocked.Store(true)
	ph.ctlUntil(t, "ask")
	ph.ws.Close()

	ph2 := e.ready(t)
	if m := ph2.ctlUntil(t, "ask"); m["block"] != "b1" {
		t.Fatalf("replayed ask = %v", m)
	}
}

// A multi-question AskUserQuestion keeps the pane blocked while its msg moves to the next question:
// the phone gets a second ask with that question's options, and outside a call the relay gets a
// second notify even within notifyEvery.
func TestNextQuestionAsksAndNotifiesAgain(t *testing.T) {
	qs := []struct{ msg, box string }{
		{"answer: 晚饭吃什么？ (火锅 · 烧烤)", "晚饭吃什么？\n❯ 1. 火锅\n  2. 烧烤\n"},
		{"answer: 喝什么？ (茶 · 水 · 可乐)", "喝什么？\n❯ 1. 茶\n  2. 水\n  3. 可乐\n"},
	}
	// q is the question on screen, 1-based; 0 means the pane is working
	watch := func(q *atomic.Int32) func(*Config) {
		set(t, &watchEvery, 20*time.Millisecond)
		exec := func(_ context.Context, name string, args ...string) ([]byte, error) {
			cmd := name + " " + strings.Join(args, " ")
			i := int(q.Load()) - 1
			switch {
			case cmd == "rex ls --json":
				return []byte(`{"sessions":[{"session_id":"s1","label":"dev"}]}`), nil
			case cmd == "rex block ls -s s1 --json":
				return []byte(`{"blocks":[{"block_id":"b1"}]}`), nil
			case strings.HasSuffix(cmd, "program_status") && i < 0:
				return []byte(`{"records":[{"state":"working","app":"claude-code"}]}`), nil
			case strings.HasSuffix(cmd, "program_status"):
				return fmt.Appendf(nil, `{"records":[{"state":"blocked","app":"claude-code","msg":%q}]}`, qs[i].msg), nil
			case cmd == "rex capture -s s1 -b b1" && i >= 0:
				return []byte(qs[i].box), nil
			}
			return nil, fmt.Errorf("fake: unexpected command %s", cmd)
		}
		return func(c *Config) { c.Watch, c.Mac = true, tools.Options{Exec: exec, Home: t.TempDir()} }
	}

	t.Run("ask", func(t *testing.T) {
		var q atomic.Int32
		e := startWith(t, false, "", relay.NewHub(), watch(&q))
		ph := e.ready(t)
		for i, want := range []string{"[火锅 烧烤]", "[茶 水 可乐]"} {
			q.Store(int32(i + 1))
			if m := ph.ctlUntil(t, "ask"); m["block"] != "b1" || m["msg"] != qs[i].msg || fmt.Sprint(m["options"]) != want {
				t.Fatalf("ask %d = %v, want msg %q options %s", i+1, m, qs[i].msg, want)
			}
		}
	})

	t.Run("notify", func(t *testing.T) {
		set(t, &keepaliveEvery, 20*time.Millisecond)
		h, texts, bins := recordingRelay()
		var q atomic.Int32
		startWith(t, false, "", h, watch(&q))
		select { // a keepalive means the Mac holds the connection
		case <-bins:
		case <-time.After(wait):
			t.Fatal("Mac never wrote to the relay")
		}
		for i, want := range []string{"[火锅 烧烤]", "[茶 水 可乐]"} {
			q.Store(int32(i + 1))
			select {
			case got := <-texts:
				var m struct {
					T, Body, Block string
					Options        []string
				}
				json.Unmarshal([]byte(got), &m)
				if m.T != "notify" || m.Block != "b1" || m.Body != qs[i].msg || fmt.Sprint(m.Options) != want {
					t.Fatalf("notify %d = %s, want body %q options %s", i+1, got, qs[i].msg, want)
				}
			case <-time.After(wait):
				t.Fatalf("no notify for question %d", i+1)
			}
		}
	})

	// The phone's prefs push=false stops the notify (the ask card still arrives); push=true resumes it.
	t.Run("prefs push off", func(t *testing.T) {
		hub, rec := relay.NewHub(), &pushRecorder{pushes: make(chan string, 4)}
		hub.Notifier = rec
		var q atomic.Int32
		e := startWith(t, false, "", hub, watch(&q))
		ph := e.ready(t)
		if err := websocket.Message.Send(ph.ws, `{"t":"push_token","token":"`+strings.Repeat("ab", 32)+`"}`); err != nil {
			t.Fatal(err)
		}
		ph.ctl(t, map[string]any{"t": "prefs", "push": false})
		time.Sleep(5 * watchEvery) // the prefs frame is read before the pane turns blocked
		q.Store(1)
		ph.ctlUntil(t, "ask")
		select {
		case title := <-rec.pushes:
			t.Fatalf("pushed with push off: %q", title)
		case <-time.After(5 * watchEvery):
		}
		ph.ctl(t, map[string]any{"t": "prefs", "push": true})
		time.Sleep(5 * watchEvery)
		q.Store(2)
		ph.ctlUntil(t, "ask")
		select {
		case title := <-rec.pushes:
			if !strings.Contains(title, "dev") {
				t.Fatalf("push after push on: %q", title)
			}
		case <-time.After(wait):
			t.Fatal("no push after push on")
		}
	})
}

// answerRex is a Rex with a live Claude pane bC and a plain shell pane bS; it records send / send-key.
type answerRex struct {
	mu    sync.Mutex
	sent  []string
	audit strings.Builder
}

func (r *answerRex) exec(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	switch {
	case cmd == "rex ls --json":
		return []byte(`{"sessions":[{"session_id":"s1","label":"dev"}]}`), nil
	case cmd == "rex block ls -s s1 --json":
		return []byte(`{"blocks":[{"block_id":"block:bC"},{"block_id":"block:bS"}]}`), nil
	case strings.Contains(cmd, "-b block:bC program_status"):
		return []byte(`{"records":[{"state":"blocked","app":"claude-code","msg":"选哪个？","owner":{"pid":77}}]}`), nil
	case strings.Contains(cmd, "-b block:bS program_status"):
		return []byte(`{"records":[]}`), nil
	case strings.HasPrefix(cmd, "rex send"):
		r.mu.Lock()
		r.sent = append(r.sent, cmd)
		r.mu.Unlock()
		return nil, nil
	}
	return nil, fmt.Errorf("fake: unexpected command %s", cmd)
}

func (r *answerRex) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.audit.Write(p)
}

func (r *answerRex) snapshot() ([]string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...), r.audit.String()
}

func startAnswer(t *testing.T) (*phone, *answerRex) {
	rex := &answerRex{}
	reg := &tools.Registry{}
	for _, tl := range tools.NewRexTools(tools.Options{Exec: rex.exec, Home: t.TempDir(), Audit: rex, Alive: func(int) bool { return true }}) {
		reg.Register(tl)
	}
	e := startWith(t, false, "", relay.NewHub(), func(c *Config) { c.Tools = reg })
	return e.ready(t), rex
}

func (ph *phone) answer(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	v["t"] = "answer"
	ph.ctl(t, v)
	return ph.ctlUntil(t, "answer_result")
}

// keys go through send_keys' whitelist; a key outside it is refused and nothing is sent.
func TestAnswerKeys(t *testing.T) {
	ph, rex := startAnswer(t)
	if m := ph.answer(t, map[string]any{"block": "block:bC", "keys": []string{"y"}}); m["ok"] != false || !strings.Contains(fmt.Sprint(m["msg"]), "不支持的键") {
		t.Fatalf("non-whitelisted key: %v", m)
	}
	if sent, _ := rex.snapshot(); len(sent) != 0 {
		t.Fatalf("sent after refusal: %v", sent)
	}
	if m := ph.answer(t, map[string]any{"block": "block:bC", "keys": []string{"2"}}); m["ok"] != true || m["block"] != "block:bC" {
		t.Fatalf("answer 2: %v", m)
	}
	sent, audit := rex.snapshot()
	if len(sent) != 1 || sent[0] != "rex send-key -s s1 -b block:bC 2" {
		t.Fatalf("sent = %v", sent)
	}
	if !strings.Contains(audit, `"source":"phone"`) || !strings.Contains(audit, `"tool":"send_keys"`) {
		t.Fatalf("audit = %s", audit)
	}
	if m := ph.answer(t, map[string]any{"block": "block:nope", "keys": []string{"1"}}); m["ok"] != false || !strings.Contains(fmt.Sprint(m["msg"]), "没有这个窗格") {
		t.Fatalf("missing pane: %v", m)
	}
}

// text goes through send_to_pane's gate: a shell pane refuses "rm -rf", a Claude pane takes anything.
func TestAnswerTextGate(t *testing.T) {
	ph, rex := startAnswer(t)
	if m := ph.answer(t, map[string]any{"block": "block:bS", "text": "rm -rf build"}); m["ok"] != false || m["msg"] == nil {
		t.Fatalf("shell rm -rf: %v", m)
	}
	if sent, audit := rex.snapshot(); len(sent) != 0 || !strings.Contains(audit, `"result":"refused"`) || !strings.Contains(audit, `"source":"phone"`) {
		t.Fatalf("after refusal sent=%v audit=%s", sent, audit)
	}
	if m := ph.answer(t, map[string]any{"block": "block:bC", "text": "先跑测试，再 rm -rf build"}); m["ok"] != true {
		t.Fatalf("claude text: %v", m)
	}
	sent, _ := rex.snapshot()
	if len(sent) != 2 || sent[0] != "rex send -s s1 -b block:bC -- 先跑测试，再 rm -rf build" || sent[1] != "rex send-key -s s1 -b block:bC enter" {
		t.Fatalf("sent = %v", sent)
	}
}
