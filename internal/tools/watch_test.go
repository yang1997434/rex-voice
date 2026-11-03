package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// A claude-code pane is reported once per turn into blocked, with the options on its screen: staying
// blocked, a failed round and other apps' blocked panes do not report; leaving reports off once and
// re-entering reports again.
func TestWatchReportsEachTurnIntoBlocked(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude", "sessions"), 0o700)
	os.WriteFile(filepath.Join(home, ".claude", "sessions", "77.json"), []byte(`{"cwd":"/x/myproj"}`), 0o600)

	// state of the claude pane per round; "fail" makes rex ls fail that round
	rounds := []string{"idle", "blocked", "blocked", "fail", "working", "blocked", "blocked", "blocked"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	round := -1
	exec := func(_ context.Context, name string, args ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		cmd := strings.Join(append([]string{name}, args...), " ")
		switch {
		case cmd == "rex ls --json":
			if round++; round >= len(rounds)-1 {
				cancel()
			}
			if rounds[min(round, len(rounds)-1)] == "fail" {
				return nil, errors.New("rex not running")
			}
			return []byte(`{"sessions":[{"session_id":"s1","label":"dev"}]}`), nil
		case cmd == "rex block ls -s s1 --json":
			return []byte(`{"blocks":[{"block_id":"bA"},{"block_id":"bB"}]}`), nil
		case strings.Contains(cmd, "-b bA program_status"):
			st := rounds[min(round, len(rounds)-1)]
			return fmt.Appendf(nil, `{"records":[{"state":%q,"app":"claude-code","msg":"要不要继续？","owner":{"pid":77}}]}`, st), nil
		case cmd == "rex capture -s s1 -b bA":
			return os.ReadFile("testdata/capture_ask.txt")
		case strings.Contains(cmd, "-b bB program_status"):
			return []byte(`{"records":[{"state":"blocked","app":"codex","owner":{"pid":88}}]}`), nil
		}
		return nil, errors.New("fake: unexpected command " + cmd)
	}

	var got []Blocked
	var offs []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, Options{Exec: exec, Home: home}, time.Millisecond, func(b Blocked) { got = append(got, b) },
			func(b string) { offs = append(offs, b) })
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after cancel")
	}
	want := Blocked{Block: "bA", Session: "dev", Project: "myproj", Msg: "answer: 午饭吃什么？ (面条 · 米饭 · 饺子)", Options: []string{"面条", "米饭", "饺子"}}
	if len(got) != 2 || !reflect.DeepEqual(got[0], want) || !reflect.DeepEqual(got[1], want) {
		t.Fatalf("callbacks = %+v, want twice %+v", got, want)
	}
	if !reflect.DeepEqual(offs, []string{"bA"}) {
		t.Fatalf("offs = %v, want [bA] once", offs)
	}
}

// A pane that stays blocked is reported again when its msg changes (the next question of a
// multi-question AskUserQuestion), with that round's options; the same msg again does not report.
func TestWatchReportsNewMsgWhileBlocked(t *testing.T) {
	msgs := []string{"answer: 晚饭吃什么？ (面条 · 米饭 · 饺子)", "answer: 晚饭吃什么？ (面条 · 米饭 · 饺子)",
		"answer: 喝什么？ (茶 · 水)", "answer: 喝什么？ (茶 · 水)", "answer: 喝什么？ (茶 · 水)"}
	screens := map[string]string{msgs[0]: "❯ 1. 面条\n  2. 米饭\n  3. 饺子\n", msgs[2]: "❯ 1. 茶\n  2. 水\n"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	round := -1
	exec := func(_ context.Context, name string, args ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		cmd := strings.Join(append([]string{name}, args...), " ")
		switch {
		case cmd == "rex ls --json":
			if round++; round >= len(msgs)-1 {
				cancel()
			}
			return []byte(`{"sessions":[{"session_id":"s1","label":"dev"}]}`), nil
		case cmd == "rex block ls -s s1 --json":
			return []byte(`{"blocks":[{"block_id":"bA"}]}`), nil
		case strings.Contains(cmd, "-b bA program_status"):
			return fmt.Appendf(nil, `{"records":[{"state":"blocked","app":"claude-code","msg":%q}]}`, msgs[round]), nil
		case cmd == "rex capture -s s1 -b bA":
			return []byte(screens[msgs[round]]), nil
		}
		return nil, errors.New("fake: unexpected command " + cmd)
	}
	var got []Blocked
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, Options{Exec: exec, Home: t.TempDir()}, time.Millisecond, func(b Blocked) { got = append(got, b) },
			func(b string) { t.Errorf("off(%s) while still blocked", b) })
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after cancel")
	}
	want := []Blocked{{Block: "bA", Session: "dev", Project: "dev", Msg: msgs[0], Options: []string{"面条", "米饭", "饺子"}},
		{Block: "bA", Session: "dev", Project: "dev", Msg: msgs[2], Options: []string{"茶", "水"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callbacks = %+v, want %+v", got, want)
	}
}

// twoQuestions are the screens of one AskUserQuestion call with two questions, before and after
// answering the first (Claude Code 2.1.316, 2026-11-02). program_status says
// "answer: 晚饭吃什么？ (火锅 · 烧烤)" on both: it does not follow the tabs.
var twoQuestions = [2]string{`────────────────────────────────────────
←  ☐ 晚饭  ☐ 饮品  ✔ Submit  →
晚饭吃什么？
❯ 1. 火锅
     吃火锅
  2. 烧烤
     吃烧烤
  3. Type something.
────────────────────────────────────────
  4. Chat about this
Enter to select · Tab/Arrow keys to navigate · Esc to cancel`, `────────────────────────────────────────
←  ☒ 晚饭  ☐ 饮品  ✔ Submit  →
喝什么？
❯ 1. 茶
     喝茶
  2. 水
     喝水
  3. 可乐
     喝可乐
  4. Type something.
────────────────────────────────────────
  5. Chat about this
Enter to select · Tab/Arrow keys to navigate · Esc to cancel`}

// The question comes from the capture, not program_status: when the screen moves to the second
// question while the status still names the first, the pane is reported again with the question
// and options both from that screen; the same screen again does not report.
func TestWatchQuestionFromCapture(t *testing.T) {
	const status = "answer: 晚饭吃什么？ (火锅 · 烧烤)"
	screens := []string{twoQuestions[0], twoQuestions[0], twoQuestions[1], twoQuestions[1], twoQuestions[1]}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	round := -1
	exec := func(_ context.Context, name string, args ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		cmd := strings.Join(append([]string{name}, args...), " ")
		switch {
		case cmd == "rex ls --json":
			if round++; round >= len(screens)-1 {
				cancel()
			}
			return []byte(`{"sessions":[{"session_id":"s1","label":"dev"}]}`), nil
		case cmd == "rex block ls -s s1 --json":
			return []byte(`{"blocks":[{"block_id":"bA"}]}`), nil
		case strings.Contains(cmd, "-b bA program_status"):
			return fmt.Appendf(nil, `{"records":[{"state":"blocked","app":"claude-code","msg":%q}]}`, status), nil
		case cmd == "rex capture -s s1 -b bA":
			return []byte(screens[round]), nil
		}
		return nil, errors.New("fake: unexpected command " + cmd)
	}
	var got []Blocked
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, Options{Exec: exec, Home: t.TempDir()}, time.Millisecond, func(b Blocked) { got = append(got, b) },
			func(b string) { t.Errorf("off(%s) while still blocked", b) })
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after cancel")
	}
	want := []Blocked{
		{Block: "bA", Session: "dev", Project: "dev", Msg: "answer: 晚饭吃什么？ (火锅 · 烧烤)", Options: []string{"火锅", "烧烤"}},
		{Block: "bA", Session: "dev", Project: "dev", Msg: "answer: 喝什么？ (茶 · 水 · 可乐)", Options: []string{"茶", "水", "可乐"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callbacks = %+v, want %+v", got, want)
	}
}

// When program_status moves between the capture and the re-read after it, the screen is captured
// again, so question and options come from one screen of the current question.
func TestReadBoxRecapturesWhenStatusMoves(t *testing.T) {
	captures := 0
	exec := func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch cmd := strings.Join(append([]string{name}, args...), " "); {
		case cmd == "rex capture -s s1 -b bA":
			captures++
			return []byte(twoQuestions[min(captures-1, 1)]), nil
		case strings.Contains(cmd, "-b bA program_status"):
			return fmt.Appendf(nil, `{"records":[{"state":"blocked","app":"claude-code","msg":"status 2"}]}`), nil
		}
		return nil, errors.New("fake: unexpected command")
	}
	e := &env{Options: Options{Exec: exec}}
	b, ok := e.readBox(context.Background(), pane{Session: "dev", Block: "bA", Doing: "status 1", sid: "s1"})
	if !ok || captures != 2 || b.Msg != "answer: 喝什么？ (茶 · 水 · 可乐)" || len(b.Options) != 3 {
		t.Fatalf("readBox = %+v ok=%v after %d captures, want the second question from the second capture", b, ok, captures)
	}
}

// The box formats seen on a real Claude Code 2.1 screen (2026-11-02): AskUserQuestion, whose trailing
// "Type something." / "Chat about this" rows are dropped and whose question is read, and the
// indented plan approval, which has no question (program_status names it). A question wrapped over
// two lines is joined, with a space only between ASCII ends.
func TestParseBox(t *testing.T) {
	read := func(file string) string {
		b, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, c := range []struct {
		name, screen, q string
		opts            []string
	}{
		{"capture_ask.txt", read("capture_ask.txt"), "午饭吃什么？", []string{"面条", "米饭", "饺子"}},
		{"capture_ask_typed.txt", read("capture_ask_typed.txt"), "午饭吃什么？", []string{"面条", "米饭", "饺子"}}, // the user typing into "Type something." is not a new option
		{"capture_plan.txt", read("capture_plan.txt"), "", []string{"Yes, and switch to BYPASS PERMISSIONS (no further prompts) for this session", "Yes, manually approve edits", "Tell Claude what to change"}},
		{"second question", twoQuestions[1], "喝什么？", []string{"茶", "水", "可乐"}},
		{"wrapped", " ☐ 缓存 \nWhich store should we\nuse for 会话缓存，还是\n先不加？\n❯ 1. Redis\n  2. Type something.\n", "Which store should we use for 会话缓存，还是先不加？", []string{"Redis"}},
	} {
		if q, opts := ParseBox(c.screen); q != c.q || !reflect.DeepEqual(opts, c.opts) {
			t.Errorf("%s: question %q options %q, want %q %q", c.name, q, opts, c.q, c.opts)
		}
	}
}

// A screen with no numbered box (a plain Claude turn) has no options, even with a numbered list
// that does not start at 1.
func TestParseBoxNone(t *testing.T) {
	b, err := os.ReadFile("testdata/capture_01a1926c.txt")
	if err != nil {
		t.Fatal(err)
	}
	if q, opts := ParseBox(string(b) + "\n  2. 第二步\n  3. 第三步\n"); q != "" || len(opts) != 0 {
		t.Fatalf("question %q options %q, want none", q, opts)
	}
}

func TestParseBoxSubmitPage(t *testing.T) {
	q, opts := ParseBox("some earlier text\n\n❯ 1. Submit answers\n  2. Cancel\n")
	if q != "提交答案？" || len(opts) != 2 || opts[0] != "Submit answers" {
		t.Fatalf("got %q %v", q, opts)
	}
}
