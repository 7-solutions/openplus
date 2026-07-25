// Package provider defines the provider-neutral domain model (ADR-0005).
// The agent loop, tools, memory, and compose packages depend only on these
// types — never on an Anthropic- or OpenAI-shaped type.
package provider

import "context"

// BlockKind identifies the kind of content carried by a Block.
type BlockKind int

const (
	BlockText BlockKind = iota
	BlockToolCall
	BlockToolResult
	BlockThinking
	BlockImage
)

// Block is one neutral unit of message content.
type Block struct {
	Kind BlockKind

	// BlockText
	Text string

	// BlockToolCall
	ToolCallID string
	ToolName   string
	ToolInput  []byte // raw JSON args, accumulated from streamed deltas

	// BlockToolResult
	ToolResultForID string
	ToolResultText  string
	ToolResultError bool

	// BlockImage
	ImageMIME string
	ImageData []byte
}

// Role is the neutral message role.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one neutral turn in the conversation.
type Message struct {
	Role   Role
	Blocks []Block
}

// ToolSchema describes a tool the model may call, in neutral form.
// Each adapter maps this to its native shape (Anthropic input_schema,
// OpenAI-compatible function.parameters).
type ToolSchema struct {
	Name        string
	Description string
	InputSchema []byte // raw JSON Schema
}

// Request is a provider-neutral request for one turn.
type Request struct {
	Model    string // "<provider>/<model>", e.g. "anthropic/claude-…" or "local/qwen2.5-coder"
	System   string
	Messages []Message
	Tools    []ToolSchema
	Thinking bool
}

// EventKind identifies the kind of a streamed Event.
type EventKind int

const (
	EventTextDelta EventKind = iota
	EventToolCallStart
	EventToolArgsDelta
	EventTurnEnd
	EventUsage
	EventError
	EventThinkingDelta
)

// ToolCall is a completed, neutral tool call parsed from a provider stream.
type ToolCall struct {
	ID    string
	Name  string
	Input []byte // fully accumulated JSON args
}

// Usage is neutral token accounting.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Event is one streamed unit from a Provider. TextDelta carries Text;
// ToolCallStart/ToolArgsDelta carry partial tool-call state the adapter
// accumulates internally, surfaced to the caller only as a completed
// ToolCall on the turn's TurnEnd event (see Turn.ToolCalls in loop.go).
type Event struct {
	Kind  EventKind
	Text  string
	Call  *ToolCall
	Usage *Usage
	Err   error
}

// Provider is the single port the agent loop depends on. Every model
// backend — Anthropic, OpenAI-compatible, or a test fake — implements this.
type Provider interface {
	// Stream sends req and returns a channel of Events. The channel is
	// closed when the turn ends (after an EventTurnEnd) or ctx is done.
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}
