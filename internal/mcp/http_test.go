package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// jsonRPCHandler answers a JSON-RPC POST as plain JSON.
func jsonRPCHandler(t *testing.T, sessionID string, seen *[]string, mu *sync.Mutex) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if mu != nil {
			mu.Lock()
			*seen = append(*seen, req.Method+" session="+r.Header.Get("Mcp-Session-Id"))
			mu.Unlock()
		}

		if len(req.ID) == 0 {
			// A notification: the spec allows an empty 202.
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var result string
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", sessionID)
			result = `{"protocolVersion":"2025-06-18","serverInfo":{"name":"httpecho","version":"2"}}`
		case "tools/list":
			result = `{"tools":[{"name":"search","description":"search things","inputSchema":{"type":"object"}}]}`
		case "tools/call":
			result = `{"content":[{"type":"text","text":"from http"}]}`
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown method"}}`, req.ID)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, result)
	}
}

// T-1514: handshake, list and call round-trip over HTTP, and the session id the
// server hands out on initialize is echoed on later requests.
func TestHTTPTransportRoundTrip(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)
	srv := httptest.NewServer(jsonRPCHandler(t, "sess-1", &seen, &mu))
	defer srv.Close()

	tr := NewHTTP(HTTPConfig{URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer x"}})
	c := NewClient("web", tr)
	defer c.Close()

	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "web.search" {
		t.Fatalf("tools = %+v", tools)
	}
	out, err := tools[0].Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "from http" {
		t.Fatalf("Execute = %q", out)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 3 {
		t.Fatalf("requests = %v", seen)
	}
	if !strings.HasPrefix(seen[0], "initialize session=") || !strings.HasSuffix(seen[0], "session=") {
		t.Errorf("initialize should carry no session id yet: %q", seen[0])
	}
	for _, req := range seen[1:] {
		if !strings.HasSuffix(req, "session=sess-1") {
			t.Errorf("request %q lost the session id", req)
		}
	}
}

// T-1514: a response streamed as SSE is read as the JSON-RPC response.
func TestHTTPTransportSSEResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flush, _ := w.(http.Flusher)
		// A comment line and an unrelated event first: the reader must skip
		// anything that is not the response for this id.
		fmt.Fprint(w, ": keep-alive\n\n")
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
		if flush != nil {
			flush.Flush()
		}
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"serverInfo\":{\"name\":\"sse\"}}}\n\n", req.ID)
		if flush != nil {
			flush.Flush()
		}
	}))
	defer srv.Close()

	tr := NewHTTP(HTTPConfig{URL: srv.URL})
	c := NewClient("sse", tr)
	defer c.Close()

	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize over SSE: %v", err)
	}
	if info := c.ServerInfo(); !strings.Contains(info, "sse") {
		t.Errorf("ServerInfo = %q", info)
	}
}

// T-1514: a non-200 response is an error naming the status.
func TestHTTPTransportNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	tr := NewHTTP(HTTPConfig{URL: srv.URL})
	_, err := tr.Call(context.Background(), "initialize", nil)
	if err == nil {
		t.Fatal("a 403 should error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want the status", err)
	}
}

// T-1514: a stream that ends without the response is an error, not an empty result.
func TestHTTPTransportBrokenStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, ": nothing useful\n\n")
	}))
	defer srv.Close()

	tr := NewHTTP(HTTPConfig{URL: srv.URL})
	if _, err := tr.Call(context.Background(), "initialize", nil); err == nil {
		t.Fatal("a stream with no response should error")
	}
}

// T-1516: a hung endpoint aborts on the context.
func TestHTTPTransportHonorsContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	tr := NewHTTP(HTTPConfig{URL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := tr.Call(ctx, "initialize", nil); err == nil {
		t.Fatal("a hung endpoint should fail the call")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("call took %v to abort", elapsed)
	}
}

// T-1514: an unreachable URL errors rather than hanging.
func TestHTTPTransportUnreachable(t *testing.T) {
	tr := NewHTTP(HTTPConfig{URL: "http://127.0.0.1:1/mcp"})
	if _, err := tr.Call(context.Background(), "initialize", nil); err == nil {
		t.Fatal("an unreachable endpoint should error")
	}
}
