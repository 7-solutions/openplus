package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/7solutions/openplus/internal/ports"
)

// recordedSSE is a canned Anthropic streaming body: a text delta ("Hello"),
// one tool_use block (echo {"text":"hi"}) assembled from input_json_delta
// fragments, usage, and a final message_stop. This is the shape ADR-0005 says
// the adapter must parse.
const recordedSSE = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"text\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"hi\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":42}}

event: message_stop
data: {"type":"message_stop"}

`

// startFixtureServer serves recordedSSE and captures the request body.
func startFixtureServer(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := make(map[string]any)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		// record header seams an adapter must send.
		got["__path"] = r.URL.Path
		got["__xapikey"] = r.Header.Get("x-api-key")
		got["__version"] = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, recordedSSE)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestStreamParsesTextAndToolCall(t *testing.T) {
	srv, _ := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "sk-test"}

	req := ports.Request{
		Model:  "anthropic/claude-sonnet-5",
		System: "you are helpful",
		Messages: []ports.Message{
			{Role: ports.RoleUser, Blocks: []ports.Block{
				{Kind: ports.BlockText, Text: "hi"},
			}},
		},
		Tools: []ports.ToolSchema{{
			Name:        "echo",
			Description: "echo back",
			InputSchema: []byte(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		}},
	}

	events, err := a.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var textDeltas []string
	var calls []ports.ToolCall
	var usage *ports.Usage
	sawTurnEnd := false
	for ev := range events {
		switch ev.Kind {
		case ports.EventTextDelta:
			textDeltas = append(textDeltas, ev.Text)
		case ports.EventToolCallStart:
			if ev.Call != nil {
				calls = append(calls, *ev.Call)
			}
		case ports.EventUsage:
			usage = ev.Usage
		case ports.EventTurnEnd:
			sawTurnEnd = true
		case ports.EventError:
			t.Fatalf("error event: %v", ev.Err)
		}
	}

	if !sawTurnEnd {
		t.Fatal("no TurnEnd event")
	}
	if len(textDeltas) != 1 || textDeltas[0] != "Hello" {
		t.Fatalf("textDeltas = %v", textDeltas)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	c := calls[0]
	if c.ID != "toolu_1" || c.Name != "echo" {
		t.Fatalf("call = %+v", c)
	}
	var in map[string]string
	if err := json.Unmarshal(c.Input, &in); err != nil {
		t.Fatalf("unmarshal call input: %v: %s", err, c.Input)
	}
	if in["text"] != "hi" {
		t.Fatalf("call input = %v", in)
	}
	if usage == nil || usage.InputTokens != 10 || usage.OutputTokens != 42 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestStreamSendsAnthropicWireShape(t *testing.T) {
	srv, got := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "sk-test"}

	req := ports.Request{
		Model:  "claude-sonnet-5",
		System: "you are helpful",
		Messages: []ports.Message{
			{Role: ports.RoleUser, Blocks: []ports.Block{
				{Kind: ports.BlockText, Text: "hello"},
			}},
		},
		Tools: []ports.ToolSchema{{
			Name:        "echo",
			Description: "echo back",
			InputSchema: []byte(`{"type":"object"}`),
		}},
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if (*got)["__path"] != "/v1/messages" {
		t.Fatalf("path = %v", (*got)["__path"])
	}
	if (*got)["__xapikey"] != "sk-test" {
		t.Fatalf("x-api-key = %v", (*got)["__xapikey"])
	}
	if (*got)["__version"] == "" {
		t.Fatal("anthropic-version header missing")
	}
	if (*got)["system"] != "you are helpful" {
		t.Fatalf("system = %v", (*got)["system"])
	}
	if (*got)["model"] != "claude-sonnet-5" {
		t.Fatalf("model = %v (prefix not stripped)", (*got)["model"])
	}
	if (*got)["stream"] != true {
		t.Fatalf("stream = %v", (*got)["stream"])
	}
	tools, _ := (*got)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", tools)
	}
}

// TestStreamEnablesThinking proves req.Thinking sets the Anthropic thinking
// config (type:enabled + budget_tokens) and grows max_tokens past the budget.
func TestStreamEnablesThinking(t *testing.T) {
	srv, got := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "k"}

	req := ports.Request{
		Model:    "claude-sonnet-5",
		Thinking: true,
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	th, ok := (*got)["thinking"].(map[string]any)
	if !ok || th["type"] != "enabled" {
		t.Fatalf("thinking config = %#v, want {type:enabled}", (*got)["thinking"])
	}
	budget, _ := th["budget_tokens"].(float64)
	maxTokens, _ := (*got)["max_tokens"].(float64)
	if budget <= 0 {
		t.Fatalf("budget_tokens = %v", budget)
	}
	if maxTokens <= budget {
		t.Fatalf("max_tokens (%v) must exceed budget_tokens (%v)", maxTokens, budget)
	}
}

// TestStreamParsesThinkingDelta proves a thinking content block streams back as
// neutral EventThinkingDelta events.
func TestStreamParsesThinkingDelta(t *testing.T) {
	const sse = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hm"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer srv.Close()

	a := &Adapter{BaseURL: srv.URL, APIKey: "k"}
	events, err := a.Stream(context.Background(), ports.Request{Model: "claude-sonnet-5", Thinking: true})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var thought string
	for ev := range events {
		if ev.Kind == ports.EventThinkingDelta {
			thought += ev.Text
		}
	}
	if thought != "hm" {
		t.Fatalf("thought = %q, want hm", thought)
	}
}

// TestStreamNoArgToolCallInputDefaultsToObject proves a tool_use with empty
// input marshals as {} (not null, not a marshal error) — regression guard for
// no-arg tool calls round-tripping.
func TestStreamNoArgToolCallInputDefaultsToObject(t *testing.T) {
	srv, got := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "k"}

	req := ports.Request{
		Model: "claude-sonnet-5",
		Messages: []ports.Message{
			{Role: ports.RoleAssistant, Blocks: []ports.Block{
				{Kind: ports.BlockToolCall, ToolCallID: "c1", ToolName: "ping"},
				// ToolInput left nil — no-arg tool
			}},
		},
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream (no-arg tool): %v", err)
	}
	msgs, _ := (*got)["messages"].([]any)
	asst, _ := msgs[0].(map[string]any)
	content, _ := asst["content"].([]any)
	block, _ := content[0].(map[string]any)
	in, ok := block["input"].(map[string]any)
	if !ok || len(in) != 0 {
		t.Fatalf("no-arg input = %#v, want empty object", block["input"])
	}
}

// TestStreamMapsToolResultBack verifies a tool_result block round-trips into the
// Anthropic wire shape (the spec's "round-trip tool call" requirement).
func TestStreamMapsToolResultBack(t *testing.T) {
	srv, got := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "sk-test"}

	req := ports.Request{
		Model: "claude-sonnet-5",
		Messages: []ports.Message{
			{Role: ports.RoleAssistant, Blocks: []ports.Block{
				{Kind: ports.BlockToolCall, ToolCallID: "toolu_1", ToolName: "echo", ToolInput: []byte(`{"text":"hi"}`)},
			}},
			{Role: ports.RoleUser, Blocks: []ports.Block{
				{Kind: ports.BlockToolResult, ToolResultForID: "toolu_1", ToolResultText: "hi", ToolResultError: true},
			}},
		},
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	msgs, _ := (*got)["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %+v", msgs)
	}
	// second message: user role with a tool_result content block
	second, _ := msgs[1].(map[string]any)
	if second["role"] != "user" {
		t.Fatalf("role = %v", second["role"])
	}
	content, _ := second["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %+v", content)
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "tool_result" {
		t.Fatalf("block type = %v", block["type"])
	}
	if block["tool_use_id"] != "toolu_1" {
		t.Fatalf("tool_use_id = %v", block["tool_use_id"])
	}
	if block["is_error"] != true {
		t.Fatalf("is_error = %v", block["is_error"])
	}
}
