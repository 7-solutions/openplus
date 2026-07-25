// Package agent implements the core turn loop (spec: openspec/specs/agent-loop,
// ADR-0001, ADR-0005, ADR-0007). The loop depends only on the provider.Provider
// port, the tool.Registry port, and the policy.Gate port — it never knows
// which model backend or which concrete tool implementation is behind them.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/provider"
	"github.com/7solutions/openplus/internal/tool"
)

// Agent wires the three ports together and runs turns until the model stops
// requesting tools.
type Agent struct {
	Provider provider.Provider
	Tools    *tool.Registry
	Gate     policy.Gate

	// OnEvent, if set, receives every streamed Event as it arrives — the
	// seam a Bubble Tea front-end (T-030) hooks into for live rendering.
	OnEvent func(provider.Event)

	// OnToolResult, if set, is fired after each tool call executes (or is
	// denied/fails), carrying the call and its neutral result block. The TUI
	// (T-031) uses it to render edit diffs and tool output.
	OnToolResult func(call provider.ToolCall, result provider.Block)
}

// Run drives one session to completion: repeated turns until a turn
// produces zero tool calls. It returns the final message history.
func (a *Agent) Run(ctx context.Context, system string, tools []provider.ToolSchema, history []provider.Message) ([]provider.Message, error) {
	for {
		req := provider.Request{
			System:   system,
			Messages: history,
			Tools:    tools,
		}

		events, err := a.Provider.Stream(ctx, req)
		if err != nil {
			return history, fmt.Errorf("agent: stream: %w", err)
		}

		assistantMsg, calls, err := a.drain(ctx, events)
		if err != nil {
			return history, err
		}
		history = append(history, assistantMsg)

		if len(calls) == 0 {
			return history, nil // model is done for this turn
		}

		resultMsg := provider.Message{Role: provider.RoleUser}
		for _, call := range calls {
			block := a.executeOne(ctx, call)
			if a.OnToolResult != nil {
				a.OnToolResult(call, block)
			}
			resultMsg.Blocks = append(resultMsg.Blocks, block)
		}
		history = append(history, resultMsg)
	}
}

// drain consumes one turn's event stream, forwarding each Event to OnEvent,
// accumulating text into an assistant Message, and collecting completed
// ToolCalls (this scaffold's Fake provider emits complete calls directly;
// a real streaming adapter accumulates ToolArgsDelta first — see T-012/T-013).
func (a *Agent) drain(ctx context.Context, events <-chan provider.Event) (provider.Message, []provider.ToolCall, error) {
	msg := provider.Message{Role: provider.RoleAssistant}
	var calls []provider.ToolCall

	for ev := range events {
		if a.OnEvent != nil {
			a.OnEvent(ev)
		}
		switch ev.Kind {
		case provider.EventTextDelta:
			msg.Blocks = append(msg.Blocks, provider.Block{Kind: provider.BlockText, Text: ev.Text})
		case provider.EventThinkingDelta:
			msg.Blocks = append(msg.Blocks, provider.Block{Kind: provider.BlockThinking, Text: ev.Text})
		case provider.EventToolCallStart:
			if ev.Call != nil {
				calls = append(calls, *ev.Call)
				msg.Blocks = append(msg.Blocks, provider.Block{
					Kind:       provider.BlockToolCall,
					ToolCallID: ev.Call.ID,
					ToolName:   ev.Call.Name,
					ToolInput:  ev.Call.Input,
				})
			}
		case provider.EventError:
			return msg, calls, ev.Err
		case provider.EventTurnEnd, provider.EventUsage, provider.EventToolArgsDelta:
			// no-op at this layer
		}
	}
	return msg, calls, nil
}

// executeOne runs the permission gate then the tool, always returning a
// ToolResult block (never erroring the loop itself — denials and failures
// are fed back to the model as text, matching MiMoCode's behavior of
// letting the model adapt rather than crashing the session).
func (a *Agent) executeOne(ctx context.Context, call provider.ToolCall) provider.Block {
	decision, err := a.Gate.Permit(ctx, call)
	if err != nil {
		return errResult(call, fmt.Sprintf("permission check failed: %v", err))
	}
	switch decision {
	case policy.Deny:
		return errResult(call, "denied by policy")
	case policy.Ask:
		// Scaffold: no interactive prompt wired yet (T-022/T-031). Treat as
		// deny so the loop never silently blocks.
		return errResult(call, "requires approval (ask) — not yet wired in scaffold")
	}

	t, ok := a.Tools.Get(call.Name)
	if !ok {
		return errResult(call, fmt.Sprintf("unknown tool: %s", call.Name))
	}

	out, err := t.Execute(ctx, json.RawMessage(call.Input))
	if err != nil {
		return errResult(call, err.Error())
	}
	return provider.Block{
		Kind:            provider.BlockToolResult,
		ToolResultForID: call.ID,
		ToolResultText:  out,
	}
}

func errResult(call provider.ToolCall, msg string) provider.Block {
	return provider.Block{
		Kind:            provider.BlockToolResult,
		ToolResultForID: call.ID,
		ToolResultText:  msg,
		ToolResultError: true,
	}
}
