package lsp

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/7solutions/openplus/internal/ports"
	"go.lsp.dev/jsonrpc2"
)

// fakeServer is a language server that never runs a process: it speaks the LSP
// base protocol over an in-memory pipe. Unit tests must not depend on gopls
// being installed, and must not pay a process spawn.
type fakeServer struct {
	conn jsonrpc2.Conn

	// hover is returned for textDocument/hover.
	hover string
	// locs is returned for definition and references.
	locs []protocolLocation
	// syms is returned for documentSymbol.
	syms []protocolSymbol
	// onDidOpen, when set, is called after the server sees didOpen — the hook a
	// test uses to push diagnostics the way a real server does.
	onDidOpen func(uri string)
}

// protocolLocation / protocolSymbol are the minimal wire shapes the fake emits.
// They mirror the LSP JSON, not the neutral ports types.
type protocolLocation struct {
	URI   string `json:"uri"`
	Range struct {
		Start struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
	} `json:"range"`
}

type protocolSymbol struct {
	Name  string `json:"name"`
	Kind  int    `json:"kind"`
	Range struct {
		Start struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
	} `json:"range"`
}

func loc(uri string, line, char int) protocolLocation {
	var l protocolLocation
	l.URI = uri
	l.Range.Start.Line = line
	l.Range.Start.Character = char
	return l
}

func sym(name string, kind, line int) protocolSymbol {
	var s protocolSymbol
	s.Name = name
	s.Kind = kind
	s.Range.Start.Line = line
	return s
}

func (f *fakeServer) handle(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	switch req.Method() {
	case "initialize":
		return map[string]any{"capabilities": map[string]any{}}, nil
	case "initialized", "textDocument/didChange", "exit":
		return nil, nil
	case "textDocument/didOpen":
		if f.onDidOpen != nil {
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal([]byte(req.Params()), &p)
			go f.onDidOpen(p.TextDocument.URI)
		}
		return nil, nil
	case "textDocument/hover":
		return map[string]any{"contents": map[string]any{"kind": "plaintext", "value": f.hover}}, nil
	case "textDocument/definition", "textDocument/references":
		return f.locs, nil
	case "textDocument/documentSymbol":
		return f.syms, nil
	case "shutdown":
		return nil, nil
	}
	return nil, jsonrpc2.ErrNotHandled
}

// startFake wires a Client to an in-memory fake server and returns both.
func startFake(t *testing.T, f *fakeServer) *Client {
	t.Helper()

	clientSide, serverSide := net.Pipe()
	f.conn = jsonrpc2.NewConn(jsonrpc2.NewStream(serverSide))
	f.conn.Go(context.Background(), f.handle)

	c, err := NewClient(context.Background(), ClientConfig{
		Root: "/proj",
		RWC:  clientSide, // test seam: talk to this pipe instead of spawning
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestClientInitializeHandshake(t *testing.T) {
	c := startFake(t, &fakeServer{})
	if !c.Initialized() {
		t.Fatal("client did not complete the initialize handshake")
	}
}

// TestClientCachesPushedDiagnostics is the load-bearing test of this change:
// diagnostics arrive as a server-initiated notification, which is exactly what
// the MCP transport ignores. The client must demux and cache them.
func TestClientCachesPushedDiagnostics(t *testing.T) {
	f := &fakeServer{}
	f.onDidOpen = func(uri string) {
		_ = f.conn.Notify(context.Background(), "textDocument/publishDiagnostics", map[string]any{
			"uri": uri,
			"diagnostics": []map[string]any{{
				"range": map[string]any{
					"start": map[string]any{"line": 9, "character": 1},
					"end":   map[string]any{"line": 9, "character": 4},
				},
				"severity": 1,
				"message":  "undefined: foo",
				"source":   "compiler",
			}},
		})
	}
	c := startFake(t, f)

	if err := c.DidOpen(context.Background(), "/proj/main.go", "package main\n"); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	diags := waitForDiagnostics(t, c, "/proj/main.go")
	if len(diags) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1", len(diags))
	}
	d := diags[0]
	if d.Message != "undefined: foo" {
		t.Errorf("message = %q", d.Message)
	}
	if d.Severity != ports.SeverityError {
		t.Errorf("severity = %v, want error", d.Severity)
	}
	// LSP is 0-based; the neutral type is 1-based.
	if d.Line != 10 || d.Column != 2 {
		t.Errorf("position = %d:%d, want 10:2 (1-based)", d.Line, d.Column)
	}
	if d.Path != "main.go" {
		t.Errorf("path = %q, want main.go (relative to root)", d.Path)
	}
}

func waitForDiagnostics(t *testing.T, c *Client, path string) []ports.Diagnostic {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d := c.Diagnostics(path); len(d) > 0 {
			return d
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("diagnostics for %s never arrived", path)
	return nil
}

func TestClientHover(t *testing.T) {
	c := startFake(t, &fakeServer{hover: "func Foo()"})
	got, err := c.Hover(context.Background(), "/proj/main.go", 7, 1)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if got != "func Foo()" {
		t.Errorf("Hover = %q, want %q", got, "func Foo()")
	}
}

func TestClientDefinitionConvertsToNeutral(t *testing.T) {
	c := startFake(t, &fakeServer{locs: []protocolLocation{loc("file:///proj/other.go", 2, 4)}})
	got, err := c.Definition(context.Background(), "/proj/main.go", 7, 1)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(locations) = %d, want 1", len(got))
	}
	want := ports.Location{Path: "other.go", Line: 3, Column: 5} // 0-based -> 1-based
	if got[0] != want {
		t.Errorf("location = %+v, want %+v", got[0], want)
	}
}

func TestClientDocumentSymbols(t *testing.T) {
	c := startFake(t, &fakeServer{syms: []protocolSymbol{sym("Foo", 12, 6)}})
	got, err := c.DocumentSymbols(context.Background(), "/proj/main.go")
	if err != nil {
		t.Fatalf("DocumentSymbols: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Foo" {
		t.Fatalf("symbols = %+v, want one named Foo", got)
	}
	if got[0].Kind != "func" {
		t.Errorf("kind = %q, want func (LSP kind 12)", got[0].Kind)
	}
	if got[0].Line != 7 {
		t.Errorf("line = %d, want 7 (1-based)", got[0].Line)
	}
}

func TestClientCloseIsIdempotent(t *testing.T) {
	c := startFake(t, &fakeServer{})
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close must be a no-op, got: %v", err)
	}
}
