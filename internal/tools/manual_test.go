//go:build manual

package tools

import (
	"context"
	"os"
	"testing"
)

// TestManual runs the read-only tools against this Mac (real rex, ps, lsof, ~/.claude).
// go test -tags manual -run Manual -v ./internal/tools/   (never calls send_to_pane)
func TestManual(t *testing.T) {
	r := &Registry{}
	for _, tl := range NewRexTools(Options{}) {
		r.Register(tl)
	}
	ctx := context.Background()
	t.Logf("list_panes: %s", r.Call(ctx, "list_panes", ""))
	t.Logf("list_claude: %s", r.Call(ctx, "list_claude", ""))
	if pid := os.Getenv("REX_VOICE_PID"); pid != "" {
		t.Logf("read_claude(%s): %s", pid, r.Call(ctx, "read_claude", `{"pid":`+pid+`}`))
	}
}
