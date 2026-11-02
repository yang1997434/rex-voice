package call

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

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
