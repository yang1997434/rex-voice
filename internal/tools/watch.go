package tools

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Blocked is a Claude Code pane that just turned blocked (waiting for the user).
type Blocked struct {
	Block   string
	Session string // Rex session label
	Project string // directory name of the Claude's cwd; the session label when unknown
	Msg     string // program_status msg, usually the question Claude is asking
	// Options are the numbered choices on screen (ParseOptions); empty when none could be read.
	Options []string
}

// Watch polls Rex every `every` until ctx is done and calls on (from its own goroutine) when a
// claude-code pane turns blocked; a pane that stays blocked is reported once. A pane first seen
// already blocked counts as turning blocked. off is called with the block when a reported pane
// stops being blocked (or is gone). A failed round is logged and skipped.
func Watch(ctx context.Context, opts Options, every time.Duration, on func(Blocked), off func(block string)) {
	if opts.Exec == nil {
		opts.Exec = runCmd
	}
	if opts.Home == "" {
		opts.Home, _ = os.UserHomeDir() // only used to find ~/.claude/sessions; lsof is the fallback
	}
	e := &env{Options: opts}
	blocked := map[string]bool{}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if ps, err := e.panes(ctx); err != nil {
			if ctx.Err() == nil {
				slog.Warn("claude watch: list panes failed", "err", err)
			}
		} else {
			now := map[string]bool{}
			for _, p := range ps {
				if p.App != "claude-code" || p.State != "blocked" {
					continue
				}
				now[p.Block] = true
				if blocked[p.Block] {
					continue
				}
				b := Blocked{Block: p.Block, Session: p.Session, Msg: p.Doing}
				if p.pid > 0 {
					if cwd := e.cwdOf(ctx, p.pid); cwd != "" {
						b.Project = filepath.Base(cwd)
					}
				}
				if b.Project == "" {
					b.Project = p.Session
				}
				if out, err := e.Exec(ctx, "rex", "capture", "-s", p.sid, "-b", p.Block); err != nil {
					slog.Warn("claude watch: capture failed, no options", "block", p.Block, "err", err)
				} else {
					b.Options = ParseOptions(string(out))
				}
				on(b)
			}
			for blk := range blocked {
				if !now[blk] {
					off(blk)
				}
			}
			blocked = now
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
