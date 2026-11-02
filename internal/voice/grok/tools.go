package grok

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

const timeoutOutput = `{"error":"工具超时"}`

func (s *Session) onItemDone(ev event) {
	var it struct {
		ID, Type, Name, Arguments string
		CallID                    string `json:"call_id"`
	}
	if err := json.Unmarshal(ev.Item, &it); err != nil || it.Type != "function_call" {
		return
	}
	s.pending[ev.ResponseID] = append(s.pending[ev.ResponseID], funcCall{it.ID, it.CallID, it.Name, it.Arguments})
}

// runTools executes one response's calls in parallel, sends every output, then exactly one response.create.
// Creating a response per output collides with the still-active one (conversation_already_has_active_response).
// audio, when not nil, is closed once that response's audio has fully arrived: xAI says a response.create
// sent while the previous answer's audio is still coming makes the two answers overlap.
func (s *Session) runTools(calls []funcCall, audio <-chan struct{}) {
	outIDs := make([]string, len(calls))
	for i, c := range calls {
		// Our own item id for the output, so pruning does not depend on the server echoing it back.
		outIDs[i] = s.newID("rv_out")
		s.toolItems = append(s.toolItems, c.itemID, outIDs[i])
	}
	s.inflight.Add(1)
	go func() {
		outs := make([]string, len(calls))
		var wg sync.WaitGroup
		for i, c := range calls {
			wg.Go(func() { outs[i] = s.callTool(c) })
		}
		wg.Wait()
		for i, c := range calls {
			if err := s.send(map[string]any{"type": "conversation.item.create", "item": map[string]any{
				"id": outIDs[i], "type": "function_call_output", "call_id": c.callID, "output": outs[i]}}); err != nil {
				s.sendFailed("function_call_output", err)
			}
		}
		if audio != nil {
			select {
			case <-audio:
			case <-time.After(audioWaitTimeout):
				s.log.Warn("grok: no output_audio.done; sending response.create anyway")
			case <-s.ctx.Done():
			}
		}
		s.inflight.Add(-1) // before response.create, so the response.done it causes sees zero
		if err := s.send(map[string]any{"type": "response.create"}); err != nil {
			s.sendFailed("response.create", err)
		}
	}()
}

func (s *Session) sendFailed(what string, err error) {
	if s.isClosed() {
		s.log.Debug("grok: send after close", "event", what)
		return
	}
	s.log.Warn("grok: send failed", "event", what, "err", err)
}

func (s *Session) callTool(c funcCall) string {
	if s.cfg.Call == nil {
		return `{"error":"没有配置工具"}`
	}
	ctx, cancel := context.WithTimeout(s.ctx, toolTimeout)
	defer cancel()
	res := make(chan string, 1)
	go func() { res <- s.cfg.Call(ctx, c.name, c.args) }()
	select {
	case out := <-res:
		return out
	case <-ctx.Done():
		s.log.Warn("grok: tool timeout", "tool", c.name)
		return timeoutOutput
	}
}

// prune deletes the tool items of the finished answer: the model has already turned them into its reply.
func (s *Session) prune() {
	ids := s.toolItems
	s.toolItems = nil
	for _, id := range ids {
		if id == "" {
			continue
		}
		if err := s.send(map[string]any{"type": "conversation.item.delete", "event_id": s.newID("rv_del"), "item_id": id}); err != nil {
			s.sendFailed("conversation.item.delete", err)
		}
	}
}
