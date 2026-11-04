// Package tools is the registry of functions the realtime voice model can call,
// plus the Rex / Claude Code tools (docs/protocol.md section 5).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Tool struct {
	Name, Description string
	Parameters        json.RawMessage // JSON Schema object
	Run               func(ctx context.Context, args json.RawMessage) (any, error)
}

// ToolDef is the OpenAI Realtime function-tool shape.
type ToolDef struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Registry is not safe for concurrent Register; Call is safe once registration is done.
type Registry struct {
	tools map[string]Tool
	order []string
}

func (r *Registry) Register(t Tool) {
	if r.tools == nil {
		r.tools = map[string]Tool{}
	}
	if _, dup := r.tools[t.Name]; dup {
		panic("tools: duplicate tool " + t.Name)
	}
	r.tools[t.Name] = t
	r.order = append(r.order, t.Name)
}

func (r *Registry) Defs() []ToolDef {
	defs := make([]ToolDef, 0, len(r.order))
	for _, n := range r.order {
		t := r.tools[n]
		defs = append(defs, ToolDef{"function", t.Name, t.Description, t.Parameters})
	}
	return defs
}

const maxErrRunes = 300

// Call runs a tool and always returns JSON for the model; failures become {"error":"…"} so the model can see them.
func (r *Registry) Call(ctx context.Context, name, argsJSON string) (out string) {
	t, ok := r.tools[name]
	if !ok {
		return errJSON("没有这个工具: " + name)
	}
	args := strings.TrimSpace(argsJSON)
	if args == "" {
		args = "{}"
	}
	if !json.Valid([]byte(args)) || args[0] != '{' {
		return errJSON("参数不是合法的 JSON 对象: " + args)
	}
	defer func() {
		if p := recover(); p != nil {
			out = errJSON(fmt.Sprint("工具内部错误: ", p))
		}
	}()
	res, err := t.Run(ctx, json.RawMessage(args))
	if err != nil {
		return errJSON(err.Error())
	}
	b, err := json.Marshal(res)
	if err != nil {
		return errJSON(err.Error())
	}
	return string(b)
}

func errJSON(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": truncRunes(strings.TrimSpace(msg), maxErrRunes)})
	return string(b)
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// decodeArgs rejects unknown fields; the schemas leave out additionalProperties / required to save tokens, so this is the check.
func decodeArgs(args json.RawMessage, v any) error {
	d := json.NewDecoder(strings.NewReader(string(args)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("参数不合法: %v", err)
	}
	return nil
}

// HangUpDef builds the hang_up tool; run closes the call and is owned by the session layer.
func HangUpDef(run func(reason string)) Tool {
	return Tool{
		Name:        "hang_up",
		Description: "挂断电话，先告别再调。",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string"}}}`),
		Run: func(_ context.Context, args json.RawMessage) (any, error) {
			var a struct {
				Reason string `json:"reason"`
			}
			if err := decodeArgs(args, &a); err != nil {
				return nil, err
			}
			run(a.Reason)
			return map[string]bool{"ok": true}, nil
		},
	}
}
