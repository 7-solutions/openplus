package agent

import (
	"context"
	"testing"

	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/provider"
	"github.com/7solutions/openplus/internal/tool"
)

// This test is the acceptance test for spec: openspec/specs/agent-loop —
// "Requirement: Tool-use iteration" and "Requirement: Provider neutrality".
// It proves the loop's shape (turn -> tool call -> tool result -> turn ->
// done) using zero network access and zero external dependencies.
func TestAgent_ToolUseThenFinish(t *testing.T) {
	fake := &provider.Fake{
		Scripts: [][]provider.Event{
			// Turn 1: model asks to call "echo".
			{
				{Kind: provider.EventToolCallStart, Call: &provider.ToolCall{
					ID: "call_1", Name: "echo", Input: []byte(`{"text":"hello openplus"}`),
				}},
				{Kind: provider.EventTurnEnd},
			},
			// Turn 2: model is satisfied with the tool result and stops.
			{
				{Kind: provider.EventTextDelta, Text: "done"},
				{Kind: provider.EventTurnEnd},
			},
		},
	}

	a := &Agent{
		Provider: fake,
		Tools:    tool.NewRegistry(tool.Echo{}),
		Gate:     policy.AllowAll{},
	}

	history, err := a.Run(context.Background(), "test system prompt", nil, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// Expect: [assistant(tool_call), user(tool_result), assistant(text)]
	if len(history) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(history), history)
	}
	if history[0].Role != provider.RoleAssistant || history[0].Blocks[0].ToolName != "echo" {
		t.Fatalf("turn 1 assistant message malformed: %+v", history[0])
	}
	if history[1].Role != provider.RoleUser || history[1].Blocks[0].Kind != provider.BlockToolResult {
		t.Fatalf("tool result message malformed: %+v", history[1])
	}
	if history[1].Blocks[0].ToolResultText != "hello openplus" {
		t.Fatalf("expected echo tool result %q, got %q", "hello openplus", history[1].Blocks[0].ToolResultText)
	}
	if history[1].Blocks[0].ToolResultError {
		t.Fatalf("expected no error on tool result, got one")
	}
	if history[2].Role != provider.RoleAssistant || history[2].Blocks[0].Text != "done" {
		t.Fatalf("final assistant message malformed: %+v", history[2])
	}
}

// Proves the deny path (Requirement: Permission gate on every tool call,
// Scenario: Denied destructive command) feeds a denial back instead of
// executing, and the loop still proceeds.
func TestAgent_DeniedToolCallDoesNotExecute(t *testing.T) {
	fake := &provider.Fake{
		Scripts: [][]provider.Event{
			{
				{Kind: provider.EventToolCallStart, Call: &provider.ToolCall{
					ID: "call_1", Name: "echo", Input: []byte(`{"text":"should not run"}`),
				}},
				{Kind: provider.EventTurnEnd},
			},
			{{Kind: provider.EventTurnEnd}},
		},
	}

	a := &Agent{
		Provider: fake,
		Tools:    tool.NewRegistry(tool.Echo{}),
		Gate:     policy.DenyList{Denied: map[string]bool{"echo": true}},
	}

	history, err := a.Run(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	result := history[1].Blocks[0]
	if !result.ToolResultError {
		t.Fatalf("expected denied call to produce an error result")
	}
	if result.ToolResultText != "denied by policy" {
		t.Fatalf("expected denial message, got %q", result.ToolResultText)
	}
}

// Proves EventThinkingDelta is captured into a neutral BlockThinking (extended
// thinking, ADR-0005 / T-012) rather than mixed into assistant text.
func TestAgent_CapturesThinking(t *testing.T) {
	fake := &provider.Fake{
		Scripts: [][]provider.Event{
			{
				{Kind: provider.EventThinkingDelta, Text: "reasoning..."},
				{Kind: provider.EventTextDelta, Text: "answer"},
				{Kind: provider.EventTurnEnd},
			},
		},
	}
	a := &Agent{Provider: fake, Tools: tool.NewRegistry(), Gate: policy.AllowAll{}}
	history, err := a.Run(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	blocks := history[0].Blocks
	if len(blocks) != 2 ||
		blocks[0].Kind != provider.BlockThinking || blocks[0].Text != "reasoning..." ||
		blocks[1].Kind != provider.BlockText || blocks[1].Text != "answer" {
		t.Fatalf("thinking not captured separately: %+v", blocks)
	}
}
