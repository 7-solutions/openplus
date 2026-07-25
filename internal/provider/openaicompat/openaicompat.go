// Package openaicompat is the OpenAI-compatible Chat Completions adapter
// (ADR-0005, T-013). It maps the neutral ports.Request/Event model onto the
// Chat Completions wire shape — a system message, assistant tool_calls[] with
// stringified function.arguments, role:"tool" results — and parses the
// chat.completion.chunk SSE stream (argument fragments keyed by tool index,
// terminating on [DONE]) back into neutral Events.
//
// One adapter unlocks OpenAI, Ollama, vLLM, LM Studio, OpenRouter, Groq, and
// any baseURL-configured endpoint. No OpenAI type escapes this package; the
// agent loop sees only ports.Provider.
package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/7solutions/openplus/internal/ports"
	"github.com/7solutions/openplus/internal/provider"
)

// DefaultBaseURL is the public OpenAI API root (includes /v1, as opencode.json
// baseURL values conventionally do). Local/self-hosted endpoints override it.
const DefaultBaseURL = "https://api.openai.com/v1"

// Adapter speaks the OpenAI-compatible Chat Completions API. It implements
// ports.Provider.
type Adapter struct {
	// BaseURL overrides DefaultBaseURL. Must end with the version segment the
	// endpoint expects (typically /v1); the path /chat/completions is appended.
	BaseURL string
	// APIKey is sent as Authorization: Bearer <APIKey>.
	APIKey string
	// HTTP is the client used for the streaming request. nil → http.DefaultClient.
	HTTP *http.Client
}

// Stream posts req to {BaseURL}/chat/completions as a streaming Chat
// Completions request and returns a channel of neutral Events parsed from the
// SSE response.
func (a *Adapter) Stream(ctx context.Context, req ports.Request) (<-chan ports.Event, error) {
	base := a.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}

	body, err := marshalRequest(req)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openaicompat: new request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)

	client := a.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: post: %w", err)
	}

	out := make(chan ports.Event)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		a.pump(ctx, resp, out)
	}()
	return out, nil
}

// pump reads the SSE stream and emits neutral Events.
func (a *Adapter) pump(ctx context.Context, resp *http.Response, out chan<- ports.Event) {
	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		send(ctx, out, ports.Event{Kind: ports.EventError, Err: fmt.Errorf("openaicompat: http %d: %s", resp.StatusCode, preview)})
		return
	}

	frames, errs := provider.ReadSSE(ctx, resp.Body)

	calls := map[int]*toolAcc{}
	var usage *ports.Usage

	emit := func(ev ports.Event) { send(ctx, out, ev) }

	for {
		select {
		case <-ctx.Done():
			emit(ports.Event{Kind: ports.EventError, Err: ctx.Err()})
			return
		case err, ok := <-errs:
			if ok && err != nil {
				emit(ports.Event{Kind: ports.EventError, Err: fmt.Errorf("openaicompat: sse: %w", err)})
				return
			}
		case frame, ok := <-frames:
			if !ok {
				// Stream ended without [DONE]; finalize defensively.
				finalize(calls, usage, emit)
				return
			}
			if frame.Data == "[DONE]" {
				finalize(calls, usage, emit)
				return
			}
			usage = handleChunk(frame.Data, calls, emit, usage)
		}
	}
}

// finalize emits accumulated tool calls (index order), then usage, then
// TurnEnd. Called once on [DONE] or unexpected stream end.
func finalize(calls map[int]*toolAcc, usage *ports.Usage, emit func(ports.Event)) {
	indices := make([]int, 0, len(calls))
	for i := range calls {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		emit(ports.Event{Kind: ports.EventToolCallStart, Call: &ports.ToolCall{
			ID:    calls[i].id,
			Name:  calls[i].name,
			Input: []byte(calls[i].args.String()),
		}})
	}
	if usage != nil {
		emit(ports.Event{Kind: ports.EventUsage, Usage: usage})
	}
	emit(ports.Event{Kind: ports.EventTurnEnd})
}

// handleChunk decodes one chat.completion.chunk into neutral Events (text
// deltas, tool-call argument accumulation) and returns any updated usage.
func handleChunk(data string, calls map[int]*toolAcc, emit func(ports.Event), usage *ports.Usage) *ports.Usage {
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return usage // ignore malformed chunks defensively
	}
	for _, ch := range chunk.Choices {
		if ch.Delta.Content != "" {
			emit(ports.Event{Kind: ports.EventTextDelta, Text: ch.Delta.Content})
		}
		for _, tc := range ch.Delta.ToolCalls {
			acc := calls[tc.Index]
			if acc == nil {
				acc = &toolAcc{}
				calls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.args.WriteString(tc.Function.Arguments)
			}
		}
	}
	if chunk.Usage != nil {
		return &ports.Usage{
			InputTokens:  chunk.Usage.PromptTokens,
			OutputTokens: chunk.Usage.CompletionTokens,
		}
	}
	return usage
}

// --- request marshaling ---

func marshalRequest(req ports.Request) ([]byte, error) {
	out := map[string]any{
		"model":    stripProviderPrefix(req.Model),
		"stream":   true,
		"messages": marshalMessages(req.System, req.Messages),
		// Opt into usage so token accounting is emitted on streams that
		// support it (OpenAI; ignored harmlessly by servers that don't).
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(req.Tools) > 0 {
		out["tools"] = marshalTools(req.Tools)
	}
	return json.Marshal(out)
}

type wireToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// marshalMessages maps neutral Messages onto Chat Completions messages:
//   - req.System → a leading {role:"system"} message (OpenAI has no top-level system).
//   - assistant: text blocks join into content; ToolCall blocks become
//     message-level tool_calls[] with stringified arguments.
//   - user: ToolResult blocks become separate {role:"tool", tool_call_id}
//     messages; text blocks join into one {role:"user"} message.
func marshalMessages(system string, msgs []ports.Message) []any {
	out := make([]any, 0, len(msgs)+1)
	if system != "" {
		out = append(out, map[string]any{"role": "system", "content": system})
	}
	for _, m := range msgs {
		switch m.Role {
		case ports.RoleAssistant:
			var texts []string
			var toolCalls []wireToolCall
			for _, b := range m.Blocks {
				switch b.Kind {
				case ports.BlockText:
					texts = append(texts, b.Text)
				case ports.BlockToolCall:
					var tc wireToolCall
					tc.ID = b.ToolCallID
					tc.Type = "function"
					tc.Function.Name = b.ToolName
					tc.Function.Arguments = string(b.ToolInput) // stringified JSON
					toolCalls = append(toolCalls, tc)
				}
			}
			msg := map[string]any{"role": "assistant"}
			if c := strings.Join(texts, ""); c != "" {
				msg["content"] = c
			}
			if len(toolCalls) > 0 {
				msg["tool_calls"] = toolCalls
			}
			out = append(out, msg)
		case ports.RoleUser:
			var texts []string
			for _, b := range m.Blocks {
				switch b.Kind {
				case ports.BlockToolResult:
					// OpenAI has no is_error field; errors are carried as content.
					out = append(out, map[string]any{
						"role":         "tool",
						"tool_call_id": b.ToolResultForID,
						"content":      b.ToolResultText,
					})
				case ports.BlockText:
					texts = append(texts, b.Text)
				}
			}
			if len(texts) > 0 {
				out = append(out, map[string]any{"role": "user", "content": strings.Join(texts, "")})
			}
		}
	}
	return out
}

func marshalTools(tools []ports.ToolSchema) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		params := json.RawMessage(t.InputSchema)
		if len(t.InputSchema) == 0 {
			params = json.RawMessage(`{"type":"object"}`)
		}
		entry := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			},
		}
		out = append(out, entry)
	}
	return out
}

// stripProviderPrefix removes a leading "<provider>/" so "openai/gpt-4o-mini"
// and "local/qwen2.5-coder" map to the provider-native model name.
func stripProviderPrefix(model string) string {
	if i := strings.IndexByte(model, '/'); i >= 0 {
		return model[i+1:]
	}
	return model
}

func send(ctx context.Context, out chan<- ports.Event, ev ports.Event) {
	select {
	case <-ctx.Done():
	case out <- ev:
	}
}

// toolAcc accumulates streamed tool-call argument fragments by index.
type toolAcc struct {
	id   string
	name string
	args strings.Builder
}
