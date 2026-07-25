// Contract tests for the provider port (T-015, ADR-0005). These prove the
// defining invariant of the neutral model: ONE neutral Request flows through
// EITHER adapter, each emits its OWN correct native wire shape, and each parses
// its OWN native tool-call stream back into the SAME neutral ToolCall. The
// agent loop therefore never learns which provider is live.
package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/7solutions/openplus/internal/ports"
	"github.com/7solutions/openplus/internal/provider/anthropic"
	"github.com/7solutions/openplus/internal/provider/openaicompat"
)

// sharedRequest is the single neutral Request every adapter must accept. Its
// model prefix is irrelevant (each adapter strips its own); what matters is the
// neutral shape: system, one user text turn, one tool schema.
func sharedRequest() ports.Request {
	return ports.Request{
		Model:  "neutral/model", // adapters strip any "<prefix>/"
		System: "you are a contract-test assistant",
		Messages: []ports.Message{
			{Role: ports.RoleUser, Blocks: []ports.Block{
				{Kind: ports.BlockText, Text: "say hello then call echo"},
			}},
		},
		Tools: []ports.ToolSchema{{
			Name:        "echo",
			Description: "echo the text back",
			InputSchema: []byte(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		}},
	}
}

// expectedCall / expectedText are the neutral outputs BOTH adapters must
// produce from their own native streams. This is the contract: different wire
// in, identical neutral out.
var (
	expectedText  = "Hello"
	expectedInput = map[string]string{"text": "hi"}
)

// anthropicSSE: text "Hello" then a tool_use block (echo) assembled from
// input_json_delta fragments.
const anthropicSSE = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"echo","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"text\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"hi\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}

event: message_stop
data: {"type":"message_stop"}

`

// openaiSSE: text "Hello" then a tool_call (echo) assembled from argument
// fragments keyed by index, then [DONE].
const openaiSSE = `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}

data: {"choices":[{"index":0,"delta":{"content":"Hello"}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":""}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"text\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hi\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`

type adapterCase struct {
	name      string
	newAd     func(baseURL string) ports.Provider
	nativeSSE string
	// wireCheck asserts protocol-specific correctness of the recorded body.
	wireCheck func(t *testing.T, body map[string]any)
}

func adapterCases() []adapterCase {
	return []adapterCase{
		{
			name:      "anthropic",
			newAd:     func(u string) ports.Provider { return &anthropic.Adapter{BaseURL: u, APIKey: "k"} },
			nativeSSE: anthropicSSE,
			wireCheck: func(t *testing.T, body map[string]any) {
				if body["system"] != "you are a contract-test assistant" {
					t.Fatalf("anthropic system not top-level: %v", body["system"])
				}
				tools, _ := body["tools"].([]any)
				if len(tools) != 1 {
					t.Fatalf("anthropic tools = %v", tools)
				}
				tc, _ := tools[0].(map[string]any)
				if tc["name"] != "echo" || tc["input_schema"] == nil {
					t.Fatalf("anthropic tool shape wrong: %+v", tc)
				}
			},
		},
		{
			name:      "openaicompat",
			newAd:     func(u string) ports.Provider { return &openaicompat.Adapter{BaseURL: u, APIKey: "k"} },
			nativeSSE: openaiSSE,
			wireCheck: func(t *testing.T, body map[string]any) {
				// system becomes the first message with role "system"
				msgs, _ := body["messages"].([]any)
				first, _ := msgs[0].(map[string]any)
				if first["role"] != "system" || first["content"] != "you are a contract-test assistant" {
					t.Fatalf("openai system message wrong: %+v", first)
				}
				tools, _ := body["tools"].([]any)
				tc, _ := tools[0].(map[string]any)
				fn, _ := tc["function"].(map[string]any)
				if tc["type"] != "function" || fn["name"] != "echo" || fn["parameters"] == nil {
					t.Fatalf("openai tool shape wrong: %+v", tc)
				}
			},
		},
	}
}

// drain collects a stream into neutral text and completed ToolCalls.
func drain(t *testing.T, events <-chan ports.Event) (text string, calls []ports.ToolCall) {
	t.Helper()
	for ev := range events {
		switch ev.Kind {
		case ports.EventTextDelta:
			text += ev.Text
		case ports.EventToolCallStart:
			if ev.Call != nil {
				calls = append(calls, *ev.Call)
			}
		case ports.EventError:
			t.Fatalf("error event: %v", ev.Err)
		}
	}
	return text, calls
}

// TestContractRoundTrip is the ADR-0005 "round-trip tool call across adapters"
// scenario: same neutral request in, same neutral tool call out, each via its
// own correct native wire shape.
func TestContractRoundTrip(t *testing.T) {
	req := sharedRequest()

	for _, c := range adapterCases() {
		t.Run(c.name, func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &body)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, c.nativeSSE)
			}))
			defer srv.Close()

			events, err := c.newAd(srv.URL).Stream(context.Background(), req)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			text, calls := drain(t, events)

			c.wireCheck(t, body)

			// Contract: identical neutral output regardless of ports.
			if text != expectedText {
				t.Errorf("text = %q, want %q", text, expectedText)
			}
			if len(calls) != 1 {
				t.Fatalf("calls = %+v, want exactly 1", calls)
			}
			if calls[0].Name != "echo" {
				t.Errorf("call name = %q, want echo", calls[0].Name)
			}
			var in map[string]string
			if err := json.Unmarshal(calls[0].Input, &in); err != nil {
				t.Fatalf("unmarshal input %s: %v", calls[0].Input, err)
			}
			if !reflect.DeepEqual(in, expectedInput) {
				t.Errorf("call input = %v, want %v", in, expectedInput)
			}
		})
	}
}

// TestContractNeutralOutputIsEqualAcrossAdapters asserts the two adapters,
// given the same neutral request, return byte-for-byte equal neutral tool-call
// input and equal text. This is the literal provider-neutrality guarantee.
func TestContractNeutralOutputIsEqualAcrossAdapters(t *testing.T) {
	req := sharedRequest()
	type out struct {
		text  string
		input []byte
		name  string
	}
	var got []out

	for _, c := range adapterCases() {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, c.nativeSSE)
			}))
			defer srv.Close()

			events, err := c.newAd(srv.URL).Stream(context.Background(), req)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			text, calls := drain(t, events)
			if len(calls) != 1 {
				t.Fatalf("calls = %+v", calls)
			}
			got = append(got, out{text: text, input: calls[0].Input, name: calls[0].Name})
		})
	}

	// Normalize/compare: both entries identical.
	if len(got) != 2 {
		t.Fatalf("got %d outputs", len(got))
	}
	if got[0].text != got[1].text {
		t.Errorf("text differs across adapters: %q vs %q", got[0].text, got[1].text)
	}
	if got[0].name != got[1].name {
		t.Errorf("call name differs: %q vs %q", got[0].name, got[1].name)
	}
	// Compare input as canonical JSON. Go's encoding/json sorts map keys, so
	// re-marshalling gives a stable comparison even if stream order differs.
	canon := func(b []byte) string {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return string(b)
		}
		out, _ := json.Marshal(m)
		return string(out)
	}
	if canon(got[0].input) != canon(got[1].input) {
		t.Errorf("input differs across adapters: %s vs %s", got[0].input, got[1].input)
	}
}

// TestContractRoundTripToolResult proves a prior tool-call + result turn
// round-trips into EACH adapter's correct native shape (anthropic tool_use/
// tool_result blocks; openai tool_calls + role:tool message). The neutral
// history is identical; only the wire representation differs.
func TestContractRoundTripToolResult(t *testing.T) {
	req := ports.Request{
		Model: "neutral/model",
		Messages: []ports.Message{
			{Role: ports.RoleAssistant, Blocks: []ports.Block{
				{Kind: ports.BlockToolCall, ToolCallID: "call_1", ToolName: "echo", ToolInput: []byte(`{"text":"hi"}`)},
			}},
			{Role: ports.RoleUser, Blocks: []ports.Block{
				{Kind: ports.BlockToolResult, ToolResultForID: "call_1", ToolResultText: "hi"},
			}},
		},
	}
	// empty SSE that ends the turn immediately (anthropic) / [DONE] (openai).
	const anthropicEnd = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":1}}}

event: message_stop
data: {"type":"message_stop"}

`
	const openaiEnd = `data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	for _, c := range []struct {
		name  string
		newAd func(string) ports.Provider
		sse   string
		check func(*testing.T, map[string]any)
	}{
		{
			name:  "anthropic",
			newAd: func(u string) ports.Provider { return &anthropic.Adapter{BaseURL: u, APIKey: "k"} },
			sse:   anthropicEnd,
			check: func(t *testing.T, body map[string]any) {
				msgs, _ := body["messages"].([]any)
				if len(msgs) != 2 {
					t.Fatalf("anthropic msgs = %+v", msgs)
				}
				// assistant tool_use block + user tool_result block
				asst, _ := msgs[0].(map[string]any)
				ac, _ := asst["content"].([]any)
				if len(ac) != 1 {
					t.Fatalf("anthropic assistant content = %+v", ac)
				}
				if ac[0].(map[string]any)["type"] != "tool_use" {
					t.Errorf("anthropic: want tool_use block")
				}
				usr, _ := msgs[1].(map[string]any)
				uc, _ := usr["content"].([]any)
				if uc[0].(map[string]any)["type"] != "tool_result" {
					t.Errorf("anthropic: want tool_result block")
				}
			},
		},
		{
			name:  "openaicompat",
			newAd: func(u string) ports.Provider { return &openaicompat.Adapter{BaseURL: u, APIKey: "k"} },
			sse:   openaiEnd,
			check: func(t *testing.T, body map[string]any) {
				msgs, _ := body["messages"].([]any)
				if len(msgs) != 2 {
					t.Fatalf("openai msgs = %+v", msgs)
				}
				asst, _ := msgs[0].(map[string]any)
				if asst["role"] != "assistant" || asst["tool_calls"] == nil {
					t.Errorf("openai: want assistant with tool_calls, got %+v", asst)
				}
				tool, _ := msgs[1].(map[string]any)
				if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" {
					t.Errorf("openai: want role:tool message, got %+v", tool)
				}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &body)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, c.sse)
			}))
			defer srv.Close()

			events, err := c.newAd(srv.URL).Stream(context.Background(), req)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			for range events { // drain to completion
			}
			c.check(t, body)
		})
	}
}
