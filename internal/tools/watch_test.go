package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A claude-code pane is reported once per turn into blocked: staying blocked, a failed round and
// other apps' blocked panes do not report; leaving and re-entering reports again.
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
		case strings.Contains(cmd, "-b bB program_status"):
			return []byte(`{"records":[{"state":"blocked","app":"codex","owner":{"pid":88}}]}`), nil
		}
		return nil, errors.New("fake: unexpected command " + cmd)
	}

	var got []Blocked
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, Options{Exec: exec, Home: home}, time.Millisecond, func(b Blocked) { got = append(got, b) })
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after cancel")
	}
	want := Blocked{Block: "bA", Session: "dev", Project: "myproj", Msg: "要不要继续？"}
	if len(got) != 2 || got[0] != want || got[1] != want {
		t.Fatalf("callbacks = %+v, want twice %+v", got, want)
	}
}
