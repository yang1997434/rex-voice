package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Blocked is a Claude Code pane that just turned blocked (waiting for the user).
type Blocked struct {
	Block   string
	Session string // Rex session label
	Project string // directory name of the Claude's cwd; the session label when unknown
	// Msg is the question: for an AskUserQuestion the one on screen ("answer: <question> (<a · b>)",
	// from the same capture as Options), else the program_status msg.
	Msg string
	// Options are the numbered choices on screen (ParseBox); empty when none could be read.
	Options []string
}

// captureTries bounds the capture / re-read loop when the question keeps changing under it.
const captureTries = 3

// Watch polls Rex every `every` until ctx is done and calls on (from its own goroutine) when a
// claude-code pane turns blocked, and again whenever a blocked pane's question (Msg) changes, e.g.
// the next one of a multi-question AskUserQuestion (program_status keeps naming the first, so the
// screen is read every round). A pane first seen already blocked counts as turning blocked. off is
// called with the block when a reported pane stops being blocked (or is gone). A failed round is
// logged and skipped.
func Watch(ctx context.Context, opts Options, every time.Duration, on func(Blocked), off func(block string)) {
	if opts.Exec == nil {
		opts.Exec = runCmd
	}
	if opts.Home == "" {
		opts.Home, _ = os.UserHomeDir() // only used to find ~/.claude/sessions; lsof is the fallback
	}
	e := &env{Options: opts}
	blocked := map[string]string{} // block -> Msg last reported
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if ps, err := e.panes(ctx); err != nil {
			if ctx.Err() == nil {
				slog.Warn("claude watch: list panes failed", "err", err)
			}
		} else {
			now := map[string]string{}
			for _, p := range ps {
				if p.App != "claude-code" || p.State != "blocked" {
					continue
				}
				prev, seen := blocked[p.Block]
				b, ok := e.readBox(ctx, p)
				if !ok && seen { // screen unreadable this round: keep what was reported
					now[p.Block] = prev
					continue
				}
				now[p.Block] = b.Msg
				if seen && prev == b.Msg {
					continue
				}
				if p.pid > 0 {
					if cwd := e.cwdOf(ctx, p.pid); cwd != "" {
						b.Project = filepath.Base(cwd)
					}
				}
				if b.Project == "" {
					b.Project = p.Session
				}
				on(b)
			}
			for blk := range blocked {
				if _, ok := now[blk]; !ok {
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

// readBox captures p's screen for its options and question. program_status is read again after the
// capture: if its msg moved meanwhile, the screen may show another question than the one read
// before, so it captures again (at most captureTries times). ok is false when no capture worked;
// b then carries the status msg and no options.
func (e *env) readBox(ctx context.Context, p pane) (b Blocked, ok bool) {
	b = Blocked{Block: p.Block, Session: p.Session, Msg: p.Doing}
	msg := p.Doing
	for range captureTries {
		out, err := e.Exec(ctx, "rex", "capture", "-s", p.sid, "-b", p.Block)
		if err != nil {
			slog.Warn("claude watch: capture failed, no options", "block", p.Block, "err", err)
			return b, ok
		}
		q, opts := ParseBox(string(out))
		b.Msg, b.Options, ok = msg, opts, true
		if q != "" {
			b.Msg = fmt.Sprintf("answer: %s (%s)", q, strings.Join(opts, " · "))
		}
		st, err := e.programStatus(ctx, p.sid, p.Block)
		if err != nil || st == nil || st.Msg == msg {
			return b, ok
		}
		msg = st.Msg
	}
	return b, ok
}
