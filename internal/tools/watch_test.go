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
	want := Blocked{Block: "bA", Session: "dev", Project: "myproj", Msg: "要不要继续？", Options: []string{"面条", "米饭", "饺子"}}
	if len(got) != 2 || !reflect.DeepEqual(got[0], want) || !reflect.DeepEqual(got[1], want) {
		t.Fatalf("callbacks = %+v, want twice %+v", got, want)
	}
	if !reflect.DeepEqual(offs, []string{"bA"}) {
		t.Fatalf("offs = %v, want [bA] once", offs)
	}
}

// The two box formats seen on a real Claude Code 2.1 screen (2026-11-02): AskUserQuestion, whose
// trailing "Type something." / "Chat about this" rows are dropped, and the indented plan approval.
func TestParseOptions(t *testing.T) {
	for file, want := range map[string][]string{
		"capture_ask.txt":  {"面条", "米饭", "饺子"},
		"capture_plan.txt": {"Yes, and switch to BYPASS PERMISSIONS (no further prompts) for this session", "Yes, manually approve edits", "Tell Claude what to change"},
	} {
		b, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		if got := ParseOptions(string(b)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: options = %q, want %q", file, got, want)
		}
	}
}

// A screen with no numbered box (a plain Claude turn) has no options, even with a numbered list
// that does not start at 1.
func TestParseOptionsNone(t *testing.T) {
	b, err := os.ReadFile("testdata/capture_01a1926c.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := ParseOptions(string(b) + "\n  2. 第二步\n  3. 第三步\n"); len(got) != 0 {
		t.Fatalf("options = %q, want none", got)
	}
}
