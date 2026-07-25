// Package anthropic is the Anthropic Messages API adapter (ADR-0005, T-012).
// It maps the neutral ports.Request/Event model onto the Anthropic wire
// shape — top-level system, tool_use/tool_result content blocks, input_schema
// tool definitions — and parses the message_* / content_block_* SSE stream back
// into neutral Events. No Anthropic type escapes this package; the agent loop
// sees only ports.Provider.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/7-solutions/openplus/internal/ports"
	"github.com/7-solutions/openplus/internal/provider"
)

const (
	// DefaultBaseURL is the public Anthropic API root.
	DefaultBaseURL = "https://api.anthropic.com"
	// APIVersion is the anthropic-version header pinned per ADR-0005
	// ("pin adapters against current docs").
	APIVersion = "2023-06-01"
	// DefaultMaxTokens is the max_tokens sent when the caller does not set one.
	// Anthropic requires the field; 4096 is a safe local default.
	DefaultMaxTokens = 4096
	// ThinkingBudgetTokens is the extended-thinking budget sent when
	// Request.Thinking is set and the caller didn't override MaxTokens past it.
	ThinkingBudgetTokens = 4096
)

// Adapter speaks the Anthropic Messages API. It implements ports.Provider.
type Adapter struct {
	// BaseURL overrides DefaultBaseURL (set this to a proxy/mock in tests).
	BaseURL string
	// APIKey is sent as x-api-key.
	APIKey string
	// HTTP is the client used for the streaming request. nil → http.DefaultClient.
	HTTP *http.Client
	// MaxTokens overrides DefaultMaxTokens when non-zero.
	MaxTokens int
}

// Stream posts req to {BaseURL}/v1/messages as a streaming Messages request
// and returns a channel of neutral Events parsed from the SSE response.
func (a *Adapter) Stream(ctx context.Context, req ports.Request) (<-chan ports.Event, error) {
	base := a.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	maxTokens := a.MaxTokens
	if maxTokens == 0 {
		maxTokens = DefaultMaxTokens
	}

	body, err := marshalRequest(req, maxTokens)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: new request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", APIVersion)

	client := a.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: post: %w", err)
	}

	out := make(chan ports.Event)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		a.pump(ctx, resp, out)
	}()
	return out, nil
}

// pump reads the SSE stream and emits neutral Events. It owns the response
// body's lifetime (closed by the goroutine in Stream).
func (a *Adapter) pump(ctx context.Context, resp *http.Response, out chan<- ports.Event) {
	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		send(ctx, out, ports.Event{Kind: ports.EventError, Err: fmt.Errorf("anthropic: http %d: %s", resp.StatusCode, preview)})
		return
	}

	frames, errs := provider.ReadSSE(ctx, resp.Body)

	blocks := map[int]*blockState{}

	var inputTokens, outputTokens int

	emit := func(ev ports.Event) { send(ctx, out, ev) }

	for {
		select {
		case <-ctx.Done():
			emit(ports.Event{Kind: ports.EventError, Err: ctx.Err()})
			return
		case err, ok := <-errs:
			if ok && err != nil {
				emit(ports.Event{Kind: ports.EventError, Err: fmt.Errorf("anthropic: sse: %w", err)})
				return
			}
		case frame, ok := <-frames:
			if !ok {
				return
			}
			a.handleFrame(frame, blocks, &inputTokens, &outputTokens, emit)
		}
	}
}

// handleFrame decodes one Anthropic SSE frame into neutral Events.
func (a *Adapter) handleFrame(frame provider.SSEFrame, blocks map[int]*blockState, inputTokens, outputTokens *int, emit func(ports.Event)) {
	var hdr struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(frame.Data), &hdr); err != nil {
		// Non-JSON data frames are ignored (defensive; Anthropic always sends JSON).
		return
	}

	switch hdr.Type {
	case "error":
		var e struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(frame.Data), &e)
		emit(ports.Event{Kind: ports.EventError, Err: fmt.Errorf("anthropic: %s: %s", e.Error.Type, e.Error.Message)})

	case "message_start":
		var m struct {
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		_ = json.Unmarshal([]byte(frame.Data), &m)
		*inputTokens = m.Message.Usage.InputTokens

	case "content_block_start":
		var b struct {
			Index int `json:"index"`
			Block struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
		}
		_ = json.Unmarshal([]byte(frame.Data), &b)
		blocks[b.Index] = &blockState{kind: b.Block.Type, callID: b.Block.ID, callName: b.Block.Name}

	case "content_block_delta":
		var d struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		_ = json.Unmarshal([]byte(frame.Data), &d)
		st := blocks[d.Index]
		if st == nil {
			return
		}
		switch d.Delta.Type {
		case "text_delta":
			emit(ports.Event{Kind: ports.EventTextDelta, Text: d.Delta.Text})
		case "thinking_delta":
			emit(ports.Event{Kind: ports.EventThinkingDelta, Text: d.Delta.Thinking})
		case "input_json_delta":
			st.input = append(st.input, d.Delta.PartialJSON...)
		}

	case "content_block_stop":
		var b struct {
			Index int `json:"index"`
		}
		_ = json.Unmarshal([]byte(frame.Data), &b)
		st := blocks[b.Index]
		if st == nil {
			return
		}
		if st.kind == "tool_use" {
			emit(ports.Event{Kind: ports.EventToolCallStart, Call: &ports.ToolCall{
				ID:    st.callID,
				Name:  st.callName,
				Input: st.input,
			}})
		}
		delete(blocks, b.Index)

	case "message_delta":
		var m struct {
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal([]byte(frame.Data), &m)
		*outputTokens = m.Usage.OutputTokens
		emit(ports.Event{Kind: ports.EventUsage, Usage: &ports.Usage{
			InputTokens:  *inputTokens,
			OutputTokens: *outputTokens,
		}})

	case "message_stop":
		emit(ports.Event{Kind: ports.EventTurnEnd})
	}
}

// blockState is per-content-block parse state.
type blockState struct {
	kind     string
	callID   string
	callName string
	input    []byte
}

// marshalRequest builds the Anthropic wire request body from the neutral Request.
func marshalRequest(req ports.Request, maxTokens int) ([]byte, error) {
	out := map[string]any{
		"model":      stripProviderPrefix(req.Model),
		"max_tokens": maxTokens,
		"stream":     true,
		"messages":   marshalMessages(req.Messages),
	}
	if req.System != "" {
		out["system"] = req.System
	}
	if len(req.Tools) > 0 {
		out["tools"] = marshalTools(req.Tools)
	}
	if req.Thinking {
		// Extended thinking: max_tokens must exceed budget_tokens. Grow it if
		// the caller left it at (or below) the budget.
		if maxTokens, _ := out["max_tokens"].(int); maxTokens <= ThinkingBudgetTokens {
			out["max_tokens"] = ThinkingBudgetTokens + 4096
		}
		out["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": ThinkingBudgetTokens,
		}
	}
	return json.Marshal(out)
}

func marshalMessages(msgs []ports.Message) []any {
	out := make([]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, map[string]any{
			"role":    string(m.Role),
			"content": marshalBlocks(m.Blocks),
		})
	}
	return out
}

func marshalBlocks(blocks []ports.Block) []any {
	out := make([]any, 0, len(blocks))
	for _, b := range blocks {
		switch b.Kind {
		case ports.BlockText:
			out = append(out, map[string]any{"type": "text", "text": b.Text})
		case ports.BlockToolCall:
			// Coerce empty input to {} — a nil/empty RawMessage marshals to
			// null (or errors for len-0 non-nil), but Anthropic requires an
			// object. This also makes no-arg tool calls round-trip cleanly.
			input := b.ToolInput
			if len(input) == 0 {
				input = []byte("{}")
			}
			out = append(out, map[string]any{
				"type":  "tool_use",
				"id":    b.ToolCallID,
				"name":  b.ToolName,
				"input": json.RawMessage(input),
			})
		case ports.BlockToolResult:
			out = append(out, map[string]any{
				"type":        "tool_result",
				"tool_use_id": b.ToolResultForID,
				"content":     b.ToolResultText,
				"is_error":    b.ToolResultError,
			})
		case ports.BlockThinking:
			out = append(out, map[string]any{"type": "thinking", "thinking": b.Text})
		}
	}
	return out
}

func marshalTools(tools []ports.ToolSchema) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		schema := json.RawMessage(t.InputSchema)
		if len(t.InputSchema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		out = append(out, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": schema,
		})
	}
	return out
}

// stripProviderPrefix removes a leading "<provider>/" from a model id so the
// neutral "anthropic/claude-…" string maps to Anthropic's bare model name.
func stripProviderPrefix(model string) string {
	if i := strings.IndexByte(model, '/'); i >= 0 {
		return model[i+1:]
	}
	return model
}

// send emits ev on out unless ctx is done (non-blocking relative to ctx).
func send(ctx context.Context, out chan<- ports.Event, ev ports.Event) {
	select {
	case <-ctx.Done():
	case out <- ev:
	}
}
