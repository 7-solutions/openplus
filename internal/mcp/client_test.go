package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTransport answers requests from a handler table, in process. It records
// every method it saw so handshake ordering can be asserted.
type fakeTransport struct {
	handle func(method string, params json.RawMessage) (json.RawMessage, *Error)

	mu       sync.Mutex
	methods  []string
	notified []string
	closed   bool
	// hang blocks Call until it is closed, to exercise cancellation.
	hang chan struct{}
}

func (f *fakeTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	f.mu.Lock()
	f.methods = append(f.methods, method)
	f.mu.Unlock()

	if f.hang != nil {
		select {
		case <-f.hang:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.handle == nil {
		return json.RawMessage(`{}`), nil
	}
	res, rpcErr := f.handle(method, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	return res, nil
}

func (f *fakeTransport) Notify(ctx context.Context, method string, params json.RawMessage) error {
	f.mu.Lock()
	f.notified = append(f.notified, method)
	f.mu.Unlock()
	return nil
}

func (f *fakeTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeTransport) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.methods...)
}

// toolServer is a fakeTransport that answers the handshake, lists two tools, and
// echoes tools/call arguments back as text.
func toolServer() *fakeTransport {
	return &fakeTransport{handle: func(method string, params json.RawMessage) (json.RawMessage, *Error) {
		switch method {
		case "initialize":
			return json.RawMessage(`{"protocolVersion":"2025-06-18",
				"serverInfo":{"name":"fake","version":"1"},"capabilities":{"tools":{}}}`), nil
		case "tools/list":
			return json.RawMessage(`{"tools":[
				{"name":"echo","description":"echo text","inputSchema":{"type":"object","properties":{"text":{"type":"string"}}}},
				{"name":"ping","description":"ping","inputSchema":{"type":"object"}}]}`), nil
		case "tools/call":
			return json.RawMessage(`{"content":[{"type":"text","text":"echoed: hi"}]}`), nil
		default:
			return nil, &Error{Code: -32601, Message: "no such method: " + method}
		}
	}}
}

// T-1512: initialize sends the handshake request and then the initialized
// notification, in that order — a server may reject calls made before it.
func TestClientInitializeHandshake(t *testing.T) {
	tr := toolServer()
	c := NewClient("fake", tr)

	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if seen := tr.seen(); len(seen) != 1 || seen[0] != "initialize" {
		t.Fatalf("methods = %v, want [initialize]", seen)
	}
	tr.mu.Lock()
	notified := append([]string(nil), tr.notified...)
	tr.mu.Unlock()
	if len(notified) != 1 || notified[0] != "notifications/initialized" {
		t.Fatalf("notifications = %v, want [notifications/initialized]", notified)
	}
	if got := c.ServerInfo(); !strings.Contains(got, "fake") {
		t.Errorf("ServerInfo = %q", got)
	}
}

// T-1512: tools/list returns each tool with its description and input schema.
func TestClientListTools(t *testing.T) {
	c := NewClient("fake", toolServer())
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}
	if tools[0].Name != "echo" || tools[0].Description != "echo text" {
		t.Errorf("tool 0 = %+v", tools[0])
	}
	if !strings.Contains(string(tools[0].InputSchema), `"text"`) {
		t.Errorf("tool 0 schema = %s", tools[0].InputSchema)
	}
}

// T-1512: listing before the handshake is refused rather than sent — the protocol
// requires initialize first.
func TestClientListToolsRequiresInitialize(t *testing.T) {
	tr := toolServer()
	c := NewClient("fake", tr)
	if _, err := c.ListTools(context.Background()); err == nil {
		t.Fatal("ListTools before Initialize should error")
	}
	if len(tr.seen()) != 0 {
		t.Errorf("a request was sent before initialize: %v", tr.seen())
	}
}

// T-1512: tools/call returns the server's content as text.
func TestClientCallTool(t *testing.T) {
	c := NewClient("fake", toolServer())
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	out, err := c.CallTool(context.Background(), "echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out != "echoed: hi" {
		t.Fatalf("CallTool = %q", out)
	}
}

// T-1512: a JSON-RPC error becomes a Go error naming the server.
func TestClientServerErrorSurfaces(t *testing.T) {
	tr := &fakeTransport{handle: func(method string, _ json.RawMessage) (json.RawMessage, *Error) {
		if method == "initialize" {
			return json.RawMessage(`{"serverInfo":{"name":"fake"}}`), nil
		}
		return nil, &Error{Code: -32000, Message: "tool exploded"}
	}}
	c := NewClient("boom", tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	_, err := c.CallTool(context.Background(), "echo", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("a server error should surface")
	}
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "tool exploded") {
		t.Errorf("error = %v, want the server name and the server's message", err)
	}
}

// T-1512: an isError result is a failure even though the RPC itself succeeded.
func TestClientToolResultIsError(t *testing.T) {
	tr := &fakeTransport{handle: func(method string, _ json.RawMessage) (json.RawMessage, *Error) {
		if method == "initialize" {
			return json.RawMessage(`{"serverInfo":{"name":"fake"}}`), nil
		}
		return json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"bad input"}]}`), nil
	}}
	c := NewClient("fake", tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := c.CallTool(context.Background(), "echo", json.RawMessage(`{}`)); err == nil ||
		!strings.Contains(err.Error(), "bad input") {
		t.Fatalf("isError result should fail with its text, got %v", err)
	}
}

// T-1512: non-text content is described rather than dropped, so the model is not
// told an image-returning tool produced nothing.
func TestClientNonTextContent(t *testing.T) {
	tr := &fakeTransport{handle: func(method string, _ json.RawMessage) (json.RawMessage, *Error) {
		if method == "initialize" {
			return json.RawMessage(`{"serverInfo":{"name":"fake"}}`), nil
		}
		return json.RawMessage(`{"content":[{"type":"image","mimeType":"image/png"}]}`), nil
	}}
	c := NewClient("fake", tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	out, err := c.CallTool(context.Background(), "shot", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(out, "image") {
		t.Errorf("non-text content should be described, got %q", out)
	}
}

// T-1516: a hung server aborts on context cancellation, promptly.
func TestClientCallHonorsContext(t *testing.T) {
	tr := &fakeTransport{hang: make(chan struct{})}
	tr.handle = func(method string, _ json.RawMessage) (json.RawMessage, *Error) {
		return json.RawMessage(`{"serverInfo":{"name":"slow"}}`), nil
	}
	c := NewClient("slow", tr)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.Initialize(ctx)
	if err == nil {
		t.Fatal("a hung server should fail the handshake")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want a deadline error", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("cancellation took %v", elapsed)
	}
}

// T-1515: closing the client closes its transport.
func TestClientCloseClosesTransport(t *testing.T) {
	tr := toolServer()
	c := NewClient("fake", tr)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if !tr.closed {
		t.Error("transport not closed")
	}
}
