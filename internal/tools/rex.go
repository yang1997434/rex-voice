package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Options struct {
	// Exec runs an external command (rex, ps, lsof) and returns stdout; on failure the error text is its stderr.
	Exec        func(ctx context.Context, name string, args ...string) ([]byte, error)
	Home        string    // default os.UserHomeDir
	Workspace   string    // where open_pane resolves project names; default <Home>/data/claude
	Audit       io.Writer // one JSON line per send_to_pane / send_keys / close_pane attempt; default discard
	ForbidExtra []string  // more words send_to_pane refuses for non-Claude panes (config.json forbid_extra)
}

func runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, errors.New(msg)
		}
		return out, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

type env struct {
	Options
	auditMu sync.Mutex
}

// NewRexTools returns list_panes, read_pane, list_claude, read_claude, send_to_pane
// and the control group (open_pane, close_pane, rename, send_keys).
func NewRexTools(opts Options) []Tool {
	if opts.Exec == nil {
		opts.Exec = runCmd
	}
	if opts.Home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			panic("tools: no home directory: " + err.Error())
		}
		opts.Home = h
	}
	if opts.Audit == nil {
		opts.Audit = io.Discard
	}
	if opts.Workspace == "" {
		opts.Workspace = filepath.Join(opts.Home, "data", "claude")
	}
	e := &env{Options: opts}
	return append([]Tool{
		{
			Name:        "list_panes",
			Description: "列出 Mac 上所有 Rex 终端窗格，以及每个窗格里程序报的状态（working / blocked / idle / done）、它正在做什么、属于哪个项目。",
			Parameters:  noParams,
			Run:         e.listPanes,
		},
		{
			Name:        "read_pane",
			Description: "读取一个窗格屏幕上最后几十行文字。只有问「屏幕上现在显示什么」才用；问 Claude 说了什么用 read_claude。block 用 list_panes 返回的 block 字段。",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"block":{"type":"string"},"lines":{"type":"integer","description":"要最后多少行，默认 40"}},"required":["block"],"additionalProperties":false}`),
			Run:         e.readPane,
		},
		{
			Name:        "list_claude",
			Description: "列出这台 Mac 上所有正在跑的 Claude Code 进程（不管开在 Rex 还是别的终端里）：项目目录、会话名、忙还是闲、什么时候开的、会话标题、最近一次活动时间。问「有几个 Claude / 哪些项目在开发」先用这个。",
			Parameters:  noParams,
			Run:         e.listClaude,
		},
		{
			Name:        "read_claude",
			Description: "读某个 Claude 的会话记录：最近一条用户指令和 Claude 最近两段原话（完整，不受屏幕滚动影响），以及它现在 busy / idle / blocked。问「Claude 说了什么 / 结论是什么 / 做到哪了 / 卡住没有」优先用这个。传 pid（来自 list_claude）或 block（来自 list_panes）。",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"pid":{"type":"integer"},"block":{"type":"string"}},"additionalProperties":false}`),
			Run:         e.readClaude,
		},
		{
			Name:        "send_to_pane",
			Description: "把一句话打进某个窗格并回车（比如给 Claude 下一条指令、斜杠命令）。调用前必须先把要发的原文逐字复述给用户、得到明确同意。发往 shell 窗格的破坏性命令（部署、上线、生产、删除之类）会被拒，让用户回 Mac 手打；发往 Claude 的部署指令要先逐字复述、用户确认后才发。",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"block":{"type":"string"},"text":{"type":"string"}},"required":["block","text"],"additionalProperties":false}`),
			Run:         e.sendToPane,
		},
	}, e.controlTools()...)
}

var noParams = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

func (e *env) rexJSON(ctx context.Context, v any, args ...string) error {
	out, err := e.Exec(ctx, "rex", args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("rex %s: %v", strings.Join(args, " "), err)
	}
	return nil
}

type rexSession struct {
	ID    string `json:"session_id"`
	Label string `json:"label"`
}

type rexBlock struct {
	ID     string `json:"block_id"`
	Label  string `json:"label"`
	Window string `json:"window_id"`
}

func (e *env) sessions(ctx context.Context) ([]rexSession, error) {
	var v struct{ Sessions []rexSession }
	return v.Sessions, e.rexJSON(ctx, &v, "ls", "--json")
}

func (e *env) blocks(ctx context.Context, sid string) ([]rexBlock, error) {
	var v struct{ Blocks []rexBlock }
	return v.Blocks, e.rexJSON(ctx, &v, "block", "ls", "-s", sid, "--json")
}

// sessionOf finds which Rex session holds block: rex only resolves a block inside its session (-s is required).
func (e *env) sessionOf(ctx context.Context, block string) (string, error) {
	if block == "" {
		return "", errors.New("缺少 block")
	}
	ss, err := e.sessions(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range ss {
		bs, err := e.blocks(ctx, s.ID)
		if err != nil {
			return "", err
		}
		for _, b := range bs {
			if b.ID == block {
				return s.ID, nil
			}
		}
	}
	return "", errors.New("没有这个窗格: " + block)
}

type progStatus struct {
	State string `json:"state"`
	App   string `json:"app"`
	Msg   string `json:"msg"`
	Owner struct {
		Pid int `json:"pid"`
	} `json:"owner"`
}

// programStatus returns records[0] of `rex block call … program_status` (no --json flag; output is JSON already), or nil.
func (e *env) programStatus(ctx context.Context, sid, bid string) (*progStatus, error) {
	var v struct{ Records []progStatus }
	if err := e.rexJSON(ctx, &v, "block", "call", "-s", sid, "-b", bid, "program_status"); err != nil {
		return nil, err
	}
	if len(v.Records) == 0 {
		return nil, nil
	}
	return &v.Records[0], nil
}

type pane struct {
	Session string `json:"session"`
	Block   string `json:"block"`
	Label   string `json:"label"`
	Project string `json:"project"`
	App     string `json:"app"`
	State   string `json:"state"`
	Doing   string `json:"doing"`
	pid     int
}

func (e *env) panes(ctx context.Context) ([]pane, error) {
	ss, err := e.sessions(ctx)
	if err != nil {
		return nil, err
	}
	out := []pane{}
	for _, s := range ss {
		bs, err := e.blocks(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range bs {
			p := pane{Session: s.Label, Block: b.ID, Label: b.Label, App: "shell", State: "idle"}
			// A pane with no reporting program (plain shell) has no status; that is not an error.
			if st, err := e.programStatus(ctx, s.ID, b.ID); err == nil && st != nil {
				p.App, p.State, p.Doing, p.pid = st.App, st.State, st.Msg, st.Owner.Pid
				if p.App == "" {
					p.App = "shell"
				}
				if p.State == "" {
					p.State = "idle"
				}
			}
			out = append(out, p)
		}
	}
	return out, nil
}

func (e *env) listPanes(ctx context.Context, args json.RawMessage) (any, error) {
	if err := decodeArgs(args, &struct{}{}); err != nil {
		return nil, err
	}
	ps, err := e.panes(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ps {
		if ps[i].pid > 0 {
			// The session label is not necessarily the project name; the process cwd is.
			ps[i].Project = filepath.Base(e.cwdOf(ctx, ps[i].pid))
			if ps[i].Project == "." {
				ps[i].Project = ""
			}
		}
	}
	return ps, nil
}

const (
	defaultPaneLines = 40
	maxPaneRunes     = 6000
)

func (e *env) readPane(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Block string `json:"block"`
		Lines int    `json:"lines"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	sid, err := e.sessionOf(ctx, a.Block)
	if err != nil {
		return nil, err
	}
	out, err := e.Exec(ctx, "rex", "capture", "-s", sid, "-b", a.Block)
	if err != nil {
		return nil, err
	}
	if a.Lines <= 0 {
		a.Lines = defaultPaneLines
	}
	return tailScreen(string(out), a.Lines), nil
}

// tailScreen strips trailing blanks per line, folds runs of empty lines into one, drops leading and
// trailing empty lines, then keeps the last n lines and at most maxPaneRunes characters.
func tailScreen(s string, n int) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimRight(l, " \t\r")
		if l == "" && (len(keep) == 0 || keep[len(keep)-1] == "") {
			continue
		}
		keep = append(keep, l)
	}
	for len(keep) > 0 && keep[len(keep)-1] == "" {
		keep = keep[:len(keep)-1]
	}
	if len(keep) > n {
		keep = keep[len(keep)-n:]
	}
	return lastRunes(strings.Join(keep, "\n"), maxPaneRunes)
}

// forbidden mirrors voice-probe/tools.mjs: a temporary gate because prod-guard checks text, not origin.
var forbidden = regexp.MustCompile(`(?i)生产|部署|上线|发布|回滚|迁移|deploy|rm -rf|git push|sudo`)

const refusedMsg = "这类指令不能用语音发，请用户回 Mac 手打"

func (e *env) sendToPane(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Block string `json:"block"`
		Text  string `json:"text"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Text) == "" {
		return nil, errors.New("text 是空的")
	}
	sid, err := e.sessionOf(ctx, a.Block)
	if err != nil {
		return nil, err
	}
	// Claude panes pass: deploy instructions to Claude are gated by the prompt's read-back-and-confirm.
	// A failed status lookup counts as "not Claude", so the gate stays on.
	if st, err := e.programStatus(ctx, sid, a.Block); (err != nil || st == nil || st.App != "claude-code") && e.forbids(a.Text) {
		return e.audit(map[string]string{"refused": refusedMsg}, "send_to_pane", a.Block, a.Text, "refused"), nil
	}
	// "--" so text starting with "-" is not parsed as a rex flag.
	if _, err := e.Exec(ctx, "rex", "send", "-s", sid, "-b", a.Block, "--", a.Text); err != nil {
		return nil, err
	}
	if _, err := e.Exec(ctx, "rex", "send-key", "-s", sid, "-b", a.Block, "enter"); err != nil {
		return nil, err
	}
	return e.audit(map[string]string{"sent": a.Text}, "send_to_pane", a.Block, a.Text, "sent"), nil
}

// forbids checks the built-in list plus Options.ForbidExtra (case-insensitive substrings).
func (e *env) forbids(text string) bool {
	if forbidden.MatchString(text) {
		return true
	}
	low := strings.ToLower(text)
	for _, w := range e.ForbidExtra {
		if w != "" && strings.Contains(low, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

// audit writes one JSON line and returns res, with audit_error added if the write failed
// (the action already happened, so it is reported rather than turned into a tool error).
func (e *env) audit(res map[string]string, tool, block, text, result string) map[string]string {
	line, _ := json.Marshal(map[string]string{
		"time": time.Now().Format(time.RFC3339), "tool": tool, "block": block, "text": text, "result": result,
	})
	e.auditMu.Lock()
	defer e.auditMu.Unlock()
	if _, err := e.Audit.Write(append(line, '\n')); err != nil {
		res["audit_error"] = err.Error()
	}
	return res
}
