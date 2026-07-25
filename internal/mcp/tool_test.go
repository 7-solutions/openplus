package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/tool"
)

// T-1517: a server's tool becomes a tool.Tool named "<server>.<tool>", and
// Execute forwards to tools/call.
func TestToolsAdaptsToToolPort(t *testing.T) {
	c := NewClient("ci", toolServer())
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}

	var echo tool.Tool
	for _, tl := range tools {
		if tl.Name() == "ci.echo" {
			echo = tl
		}
	}
	if echo == nil {
		names := make([]string, 0, len(tools))
		for _, tl := range tools {
			names = append(names, tl.Name())
		}
		t.Fatalf("no ci.echo among %v", names)
	}
	if !strings.Contains(echo.Description(), "echo text") {
		t.Errorf("Description = %q", echo.Description())
	}
	if !strings.Contains(string(echo.Schema()), `"text"`) {
		t.Errorf("Schema = %s", echo.Schema())
	}

	out, err := echo.Execute(context.Background(), json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "echoed: hi" {
		t.Fatalf("Execute = %q", out)
	}
}

// T-1517: a server error from Execute is a Go error the loop can record.
func TestToolExecuteSurfacesServerError(t *testing.T) {
	tr := &fakeTransport{handle: func(method string, _ json.RawMessage) (json.RawMessage, *Error) {
		switch method {
		case "initialize":
			return json.RawMessage(`{"serverInfo":{"name":"fake"}}`), nil
		case "tools/list":
			return json.RawMessage(`{"tools":[{"name":"boom","inputSchema":{"type":"object"}}]}`), nil
		default:
			return nil, &Error{Code: -32000, Message: "server said no"}
		}
	}}
	c := NewClient("ci", tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if _, err := tools[0].Execute(context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "server said no") {
		t.Fatalf("Execute error = %v", err)
	}
}

// T-1518: a representable object schema passes through; an unrepresentable one is
// rejected at registration, naming server and tool, before any call.
func TestTranslateSchema(t *testing.T) {
	ok := []string{
		`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`,
		`{"type":"object"}`,
		`{"type":"object","properties":{"n":{"type":"integer","description":"count"}}}`,
		`{"type":"object","properties":{"xs":{"type":"array","items":{"type":"string"}}}}`,
		`{}`, // absent schema: treated as "no arguments"
	}
	for _, in := range ok {
		got, err := translateSchema("ci", "t", json.RawMessage(in))
		if err != nil {
			t.Errorf("translateSchema(%s) = %v, want ok", in, err)
			continue
		}
		var probe map[string]any
		if err := json.Unmarshal(got, &probe); err != nil {
			t.Errorf("translateSchema(%s) produced invalid JSON: %v", in, err)
		}
		if probe["type"] != "object" {
			t.Errorf("translateSchema(%s) type = %v, want object", in, probe["type"])
		}
	}

	bad := []string{
		`{"type":"array","items":{"type":"string"}}`,                    // not an object at the top
		`{"type":"object","properties":{"a":{"$ref":"#/defs/x"}}}`,      // reference
		`{"oneOf":[{"type":"object"},{"type":"string"}]}`,               // union
		`{"type":"object","properties":{"a":{"anyOf":[{"type":"x"}]}}}`, // nested union
		`{"type":"object","properties":{"a":{"allOf":[]}}}`,
		`not json`,
	}
	for _, in := range bad {
		_, err := translateSchema("ci", "weird", json.RawMessage(in))
		if err == nil {
			t.Errorf("translateSchema(%s) should be rejected", in)
			continue
		}
		if !strings.Contains(err.Error(), "ci") || !strings.Contains(err.Error(), "weird") {
			t.Errorf("rejection should name server and tool: %v", err)
		}
	}
}

// T-1518: a tool whose schema cannot be translated fails registration for the
// whole server rather than being silently dropped — a missing tool the user
// configured is a surprise worth reporting.
func TestToolsRejectsUnrepresentableSchema(t *testing.T) {
	tr := &fakeTransport{handle: func(method string, _ json.RawMessage) (json.RawMessage, *Error) {
		switch method {
		case "initialize":
			return json.RawMessage(`{"serverInfo":{"name":"fake"}}`), nil
		case "tools/list":
			return json.RawMessage(`{"tools":[{"name":"weird","inputSchema":{"oneOf":[{"type":"object"}]}}]}`), nil
		}
		return json.RawMessage(`{}`), nil
	}}
	c := NewClient("ci", tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := c.Tools(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "weird") {
		t.Fatalf("Tools error = %v, want a rejection naming the tool", err)
	}
}

// T-1517: an unnamed tool is rejected — it could not be called or gated.
func TestToolsRejectsUnnamedTool(t *testing.T) {
	tr := &fakeTransport{handle: func(method string, _ json.RawMessage) (json.RawMessage, *Error) {
		switch method {
		case "initialize":
			return json.RawMessage(`{"serverInfo":{"name":"fake"}}`), nil
		case "tools/list":
			return json.RawMessage(`{"tools":[{"name":"","inputSchema":{"type":"object"}}]}`), nil
		}
		return json.RawMessage(`{}`), nil
	}}
	c := NewClient("ci", tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := c.Tools(context.Background()); err == nil {
		t.Fatal("an unnamed tool should be rejected")
	}
}
