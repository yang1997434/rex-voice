package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sendKeysAllowed was checked against a real Claude Code AskUserQuestion box on 2026-11-02:
// a digit picks that option directly, down+enter picks the next one, escape cancels; y / n do nothing there.
var sendKeysAllowed = []string{"enter", "escape", "up", "down", "left", "right", "tab", "shift+tab", "ctrl+c",
	"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

// controlTools are open_pane, close_pane, rename and send_keys (docs/protocol.md section 5).
func (e *env) controlTools() []Tool {
	keys, _ := json.Marshal(sendKeysAllowed)
	return []Tool{
		{
			Name: "open_pane",
			Description: "在某个 Rex 会话里开一个新标签页：kind=claude（默认）开一个 Claude Code（跳过权限确认），kind=shell 开一个终端。" +
				"session 填 list_panes 返回的 session（会话名）；project 是 " + e.Workspace + " 下的项目目录名（如 geo、rex-voice），也可以是家目录下的绝对路径；" +
				"name 是新窗格的名字（Claude 的会话名也用它），不填就用项目名。开之前先跟用户确认会话和项目。返回新窗格的 block。",
			Parameters: json.RawMessage(`{"type":"object","properties":{"session":{"type":"string"},"project":{"type":"string"},"kind":{"type":"string","enum":["claude","shell"]},"name":{"type":"string"}},"required":["session","project"],"additionalProperties":false}`),
			Run:        e.openPane,
		},
		{
			Name: "close_pane",
			Description: "关窗格。scope=pane（默认）只关这个窗格，window 关它所在的整个标签页，session 关整个 Rex 会话（里面所有窗格）。" +
				"调用前先跟用户确认要关哪个。范围里有窗格正在干活（working）会被拒、返回 refused；用户明确说还是要关，再带 force=true 调一次。",
			Parameters: json.RawMessage(`{"type":"object","properties":{"block":{"type":"string"},"scope":{"type":"string","enum":["pane","window","session"]},"force":{"type":"boolean"}},"required":["block"],"additionalProperties":false}`),
			Run:        e.closePane,
		},
		{
			Name: "rename",
			Description: "给 Rex 里的东西改名：target=session 改会话名（id 填 list_panes 的 session），window 改标签页名（id 填这个标签页里任一窗格的 block），pane 改窗格名（id 填 block）。" +
				"只改 Rex 里显示的名字；要改 Claude 自己的会话名，往那个 Claude 窗格 send_to_pane 发「/rename 新名」。",
			Parameters: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string","enum":["session","window","pane"]},"id":{"type":"string"},"name":{"type":"string"}},"required":["target","id","name"],"additionalProperties":false}`),
			Run:        e.rename,
		},
		{
			Name: "send_keys",
			Description: "往窗格按键，用来回答 Claude 的提问框（AskUserQuestion）、计划审批、/model /effort 弹出的选择框：先 read_pane 看选项念给用户；" +
				"按数字键直接选中那个编号的选项（不用再按 enter），先 read_pane 看清编号再按，不用 up / down 盲选；escape 取消提问框，也能打断正在干活的 Claude；" +
				"shift+tab 切换 Claude 的权限模式；ctrl+c 中断 shell 里的命令。keys 按顺序按，比如 [\"2\"]。y / n 在 Claude 提问框里不起作用，不支持。",
			Parameters: json.RawMessage(`{"type":"object","properties":{"block":{"type":"string"},"keys":{"type":"array","minItems":1,"items":{"type":"string","enum":` + string(keys) + `}}},"required":["block","keys"],"additionalProperties":false}`),
			Run:        e.sendKeys,
		},
	}
}

func (e *env) openPane(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
		Project string `json:"project"`
		Kind    string `json:"kind"`
		Name    string `json:"name"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	dir, err := e.projectDir(a.Project)
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(a.Name)
	if label == "" {
		label = filepath.Base(dir)
	}
	var run []string
	switch a.Kind {
	case "", "claude":
		run = []string{"--", "claude", "--dangerously-skip-permissions"}
		if a.Name != "" {
			run = append(run, "-n", label)
		}
	case "shell":
		run = []string{"--keep-open", "--", "zsh", "-l"}
	default:
		return nil, errors.New("kind 只能是 claude 或 shell")
	}
	sess, err := e.resolveSession(ctx, a.Session)
	if err != nil {
		return nil, err
	}
	cmd := append([]string{"run", "-s", sess.ID, "-c", dir, "--label", label, "--focus=false", "--json"}, run...)
	// Verified 2026-11-02: -s takes a label or id; output is {"window_id", "block_ids":[…], "revision"}.
	var v struct {
		Blocks []string `json:"block_ids"`
	}
	if err := e.rexJSON(ctx, &v, cmd...); err != nil {
		return nil, err
	}
	if len(v.Blocks) == 0 {
		return nil, errors.New("rex run 没返回新窗格的 block")
	}
	return map[string]string{"block": v.Blocks[0], "session": sess.Label}, nil
}

// projectDir resolves a project name under Workspace, or an absolute path under Home, to an existing directory.
func (e *env) projectDir(project string) (string, error) {
	p := strings.TrimSpace(project)
	if p == "" {
		return "", errors.New("缺少 project")
	}
	base := e.Workspace
	if filepath.IsAbs(p) {
		base = e.Home
	} else {
		p = filepath.Join(base, p)
	}
	p = filepath.Clean(p)
	fi, err := os.Stat(p)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("不是目录: %s", p)
	}
	// Compare real paths: /data is a symlink to ~/data on this Mac, and a symlink inside Home must not lead outside it.
	if r, err := filepath.EvalSymlinks(base); err == nil {
		base = r
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if rel, err := filepath.Rel(base, p); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("项目目录不在 %s 下: %s", base, project)
	}
	return p, nil
}

// locate returns block's session id, that session's blocks, and the block itself.
func (e *env) locate(ctx context.Context, block string) (string, []rexBlock, rexBlock, error) {
	sid, bid, err := e.resolveBlock(ctx, block)
	if err != nil {
		return "", nil, rexBlock{}, err
	}
	bs, err := e.blocks(ctx, sid)
	if err != nil {
		return "", nil, rexBlock{}, err
	}
	for _, b := range bs {
		if b.ID == bid {
			return sid, bs, b, nil
		}
	}
	return "", nil, rexBlock{}, errors.New("没有这个窗格: " + block)
}

func (e *env) closePane(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Block string `json:"block"`
		Scope string `json:"scope"`
		Force bool   `json:"force"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if a.Scope == "" {
		a.Scope = "pane"
	}
	if a.Scope != "pane" && a.Scope != "window" && a.Scope != "session" {
		return nil, errors.New("scope 只能是 pane、window 或 session")
	}
	sid, bs, self, err := e.locate(ctx, a.Block)
	if err != nil {
		return nil, err
	}
	a.Block = self.ID
	targets := []rexBlock{self}
	if a.Scope != "pane" {
		targets = nil
		for _, b := range bs {
			if a.Scope == "session" || b.Window == self.Window {
				targets = append(targets, b)
			}
		}
	}
	if !a.Force {
		for _, b := range targets {
			// A pane with no reporting program (plain shell) has no status: nothing to protect.
			// A lookup that fails (rex timeout) is not "no status": refuse rather than close a working Claude blind.
			st, err := e.programStatus(ctx, sid, b.ID)
			if err != nil {
				return e.audit(ctx, map[string]string{"refused": fmt.Sprintf("%s 查不到状态（%s），没敢关", b.ID, truncRunes(err.Error(), 80))}, "close_pane", a.Block, a.Scope, "refused"), nil
			}
			if st != nil && st.State == "working" {
				doing := st.Msg
				if doing == "" {
					doing = "干活"
				}
				return e.audit(ctx, map[string]string{"refused": fmt.Sprintf("%s 正在 %s，要关请确认 force", b.ID, doing)}, "close_pane", a.Block, a.Scope, "refused"), nil
			}
		}
	}
	// Verified 2026-11-02: block / window close take the id as an argument plus -s; session kill takes the session.
	cmd, closed := []string{"block", "close", "-s", sid, a.Block}, a.Block
	switch a.Scope {
	case "window":
		cmd, closed = []string{"window", "close", "-s", sid, self.Window}, self.Window
	case "session":
		cmd, closed = []string{"session", "kill", sid}, sid
	}
	if _, err := e.Exec(ctx, "rex", cmd...); err != nil {
		return nil, err
	}
	return e.audit(ctx, map[string]string{"closed": closed}, "close_pane", a.Block, a.Scope, "closed"), nil
}

func (e *env) rename(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Target string `json:"target"`
		ID     string `json:"id"`
		Name   string `json:"name"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return nil, errors.New("name 是空的")
	}
	if strings.TrimSpace(a.ID) == "" {
		return nil, errors.New("缺少 id")
	}
	var cmd []string // "--" so a name starting with "-" is not parsed as a rex flag
	switch a.Target {
	case "session":
		s, err := e.resolveSession(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		cmd = []string{"session", "rename", "--", s.ID, name}
	case "window", "pane":
		sid, _, self, err := e.locate(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		if a.Target == "window" {
			cmd = []string{"window", "rename", "-s", sid, "--", self.Window, name}
		} else {
			cmd = []string{"block", "rename", "-s", sid, "--", self.ID, name}
		}
	default:
		return nil, errors.New("target 只能是 session、window 或 pane")
	}
	if _, err := e.Exec(ctx, "rex", cmd...); err != nil {
		return nil, err
	}
	return map[string]string{"renamed": a.ID, "name": name}, nil
}

func (e *env) sendKeys(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Block string   `json:"block"`
		Keys  []string `json:"keys"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if len(a.Keys) == 0 {
		return nil, errors.New("keys 是空的")
	}
	for i, k := range a.Keys {
		k = strings.ToLower(strings.TrimSpace(k))
		ok := false
		for _, w := range sendKeysAllowed {
			ok = ok || k == w
		}
		if !ok {
			return nil, fmt.Errorf("不支持的键 %q，只能用 %s", a.Keys[i], strings.Join(sendKeysAllowed, " "))
		}
		a.Keys[i] = k
	}
	sid, bid, err := e.resolveBlock(ctx, a.Block)
	if err != nil {
		return nil, err
	}
	a.Block = bid
	// In a shell, enter would run whatever is sitting on the command line, bypassing send_to_pane's gate.
	if !e.isClaudePane(ctx, sid, a.Block) {
		for _, k := range a.Keys {
			if k != "ctrl+c" && k != "escape" {
				return nil, fmt.Errorf("不是 Claude 窗格，只能发 ctrl+c / escape，不能发 %q", k)
			}
		}
	}
	if _, err := e.Exec(ctx, "rex", append([]string{"send-key", "-s", sid, "-b", a.Block}, a.Keys...)...); err != nil {
		return nil, err
	}
	keys := strings.Join(a.Keys, " ")
	return e.audit(ctx, map[string]string{"sent": keys}, "send_keys", a.Block, keys, "sent"), nil
}
