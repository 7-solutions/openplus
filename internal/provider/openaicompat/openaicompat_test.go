package openaicompat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/7solutions/openplus/internal/provider"
)

// recordedSSE is a canned Chat Completions stream: an assistant text delta
// ("Hello"), one tool_call (echo {"text":"hi"}) assembled from argument
// fragments keyed by index, a final finish_reason, a usage chunk, and [DONE].
const recordedSSE = `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"}}]}

data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":""}}]}}]}

data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"text\":"}}]}}]}

data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hi\"}"}}]}}]}

data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"id":"1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}

data: [DONE]

`

func startFixtureServer(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := make(map[string]any)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		got["__path"] = r.URL.Path
		got["__auth"] = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, recordedSSE)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestStreamParsesTextAndToolCall(t *testing.T) {
	srv, _ := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "sk-test"}

	req := provider.Request{
		Model:  "openai/gpt-4o-mini",
		System: "helpful",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Blocks: []provider.Block{
				{Kind: provider.BlockText, Text: "hi"},
			}},
		},
		Tools: []provider.ToolSchema{{
			Name:        "echo",
			Description: "echo",
			InputSchema: []byte(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		}},
	}
	events, err := a.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var textDeltas []string
	var calls []provider.ToolCall
	var usage *provider.Usage
	sawTurnEnd := false
	for ev := range events {
		switch ev.Kind {
		case provider.EventTextDelta:
			textDeltas = append(textDeltas, ev.Text)
		case provider.EventToolCallStart:
			if ev.Call != nil {
				calls = append(calls, *ev.Call)
			}
		case provider.EventUsage:
			usage = ev.Usage
		case provider.EventTurnEnd:
			sawTurnEnd = true
		case provider.EventError:
			t.Fatalf("error event: %v", ev.Err)
		}
	}
	if !sawTurnEnd {
		t.Fatal("no TurnEnd event")
	}
	if len(textDeltas) != 1 || textDeltas[0] != "Hello" {
		t.Fatalf("textDeltas = %v", textDeltas)
	}
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "echo" {
		t.Fatalf("calls = %+v", calls)
	}
	var in map[string]string
	if err := json.Unmarshal(calls[0].Input, &in); err != nil {
		t.Fatalf("unmarshal input %s: %v", calls[0].Input, err)
	}
	if in["text"] != "hi" {
		t.Fatalf("input = %v", in)
	}
	if usage == nil || usage.InputTokens != 10 || usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestStreamSendsWireShape(t *testing.T) {
	srv, got := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "sk-test"}

	req := provider.Request{
		Model:  "gpt-4o-mini",
		System: "helpful",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Blocks: []provider.Block{
				{Kind: provider.BlockText, Text: "hello"},
			}},
		},
		Tools: []provider.ToolSchema{{
			Name:        "echo",
			Description: "echo",
			InputSchema: []byte(`{"type":"object"}`),
		}},
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if (*got)["__path"] != "/chat/completions" {
		t.Fatalf("path = %v", (*got)["__path"])
	}
	if (*got)["__auth"] != "Bearer sk-test" {
		t.Fatalf("auth = %v", (*got)["__auth"])
	}
	if (*got)["model"] != "gpt-4o-mini" {
		t.Fatalf("model = %v (prefix not stripped)", (*got)["model"])
	}
	if (*got)["stream"] != true {
		t.Fatalf("stream = %v", (*got)["stream"])
	}
	// system → first message with role "system"
	msgs, _ := (*got)["messages"].([]any)
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "helpful" {
		t.Fatalf("system message = %+v", first)
	}
	// tools → function parameters
	tools, _ := (*got)["tools"].([]any)
	t0, _ := tools[0].(map[string]any)
	if t0["type"] != "function" {
		t.Fatalf("tool type = %v", t0["type"])
	}
}

// TestStreamRequestsUsageInStream proves the adapter opts into usage so token
// accounting is emitted (stream_options.include_usage).
func TestStreamRequestsUsageInStream(t *testing.T) {
	srv, got := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "k"}
	if _, err := a.Stream(context.Background(), provider.Request{Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	so, ok := (*got)["stream_options"].(map[string]any)
	if !ok || so["include_usage"] != true {
		t.Fatalf("stream_options.include_usage not set: %#v", (*got)["stream_options"])
	}
}

// TestStreamAssistantToolCallMarshaled proves a neutral assistant ToolCall
// block becomes a message-level tool_calls[] entry with STRINGIFIED arguments
// (OpenAI requires arguments as a string, not an object).
func TestStreamAssistantToolCallMarshaled(t *testing.T) {
	srv, got := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "k"}

	req := provider.Request{
		Model: "gpt-4o-mini",
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, Blocks: []provider.Block{
				{Kind: provider.BlockToolCall, ToolCallID: "call_1", ToolName: "echo", ToolInput: []byte(`{"text":"hi"}`)},
			}},
		},
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	msgs, _ := (*got)["messages"].([]any)
	m, _ := msgs[0].(map[string]any)
	if m["role"] != "assistant" {
		t.Fatalf("role = %v", m["role"])
	}
	tcs, _ := m["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls = %+v", tcs)
	}
	tc, _ := tcs[0].(map[string]any)
	fn, _ := tc["function"].(map[string]any)
	if fn["name"] != "echo" {
		t.Fatalf("name = %v", fn["name"])
	}
	if args, ok := fn["arguments"].(string); !ok || args != `{"text":"hi"}` {
		t.Fatalf("arguments = %#v (want stringified json)", fn["arguments"])
	}
	if tc["id"] != "call_1" || tc["type"] != "function" {
		t.Fatalf("tc = %+v", tc)
	}
}

// TestStreamToolResultMappedToToolRole proves a neutral ToolResult block
// becomes a role:"tool" message with tool_call_id (OpenAI shape, per ADR-0005).
func TestStreamToolResultMappedToToolRole(t *testing.T) {
	srv, got := startFixtureServer(t)
	a := &Adapter{BaseURL: srv.URL, APIKey: "k"}

	req := provider.Request{
		Model: "gpt-4o-mini",
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, Blocks: []provider.Block{
				{Kind: provider.BlockToolCall, ToolCallID: "call_1", ToolName: "echo", ToolInput: []byte(`{}`)},
			}},
			{Role: provider.RoleUser, Blocks: []provider.Block{
				{Kind: provider.BlockToolResult, ToolResultForID: "call_1", ToolResultText: "hi"},
			}},
		},
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	msgs, _ := (*got)["messages"].([]any)
	// expect: assistant(tool_call) then tool(call_1)
	if len(msgs) != 2 {
		t.Fatalf("messages = %+v", msgs)
	}
	tool, _ := msgs[1].(map[string]any)
	if tool["role"] != "tool" {
		t.Fatalf("role = %v", tool["role"])
	}
	if tool["tool_call_id"] != "call_1" {
		t.Fatalf("tool_call_id = %v", tool["tool_call_id"])
	}
	if tool["content"] != "hi" {
		t.Fatalf("content = %v", tool["content"])
	}
}
