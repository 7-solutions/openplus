// Package lsp is the Language Server Protocol adapter behind the
// ports.LanguageService port (change 0026, ADR-0017).
//
// This package is the only place in OpenPlus that knows the LSP wire protocol.
// Everything it hands the rest of the system is a neutral ports type:
// ports.Diagnostic, ports.Location, ports.Symbol. That is the hard rule of this
// change — no go.lsp.dev type may cross the port, exactly as no provider wire
// type escapes internal/provider (ADR-0005).
//
// Two details drove the design, both discovered while considering whether to
// reuse internal/mcp's hand-rolled transport:
//
//  1. LSP frames messages with Content-Length headers; MCP uses newline-
//     delimited JSON. None of the MCP codec carries over.
//  2. Diagnostics arrive *only* as server-initiated notifications. The MCP
//     transport deliberately ignores those, so its read loop is not a template
//     either. jsonrpc2.Conn.Go gives us the notification dispatch we need.
//
// Positions: LSP counts lines and characters from zero; the neutral types count
// from one, which is what a compiler prints and a human reads. Conversion
// happens here, at the boundary, in both directions.
package lsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/7solutions/openplus/internal/ports"
	"go.lsp.dev/jsonrpc2"
)

// ClientConfig describes one language server.
type ClientConfig struct {
	// Root is the project directory the server is initialized against.
	Root string

	// Command and Args spawn the server. Ignored when RWC is set.
	Command string
	Args    []string

	// RWC is a test seam: when non-nil the client talks to it instead of
	// spawning a process. Production callers leave it nil.
	RWC io.ReadWriteCloser
}

// Client is one language-server connection.
type Client struct {
	root string
	conn jsonrpc2.Conn
	cmd  *exec.Cmd
	rwc  io.ReadWriteCloser

	mu          sync.RWMutex
	initialized bool
	closed      bool
	// diags caches the most recent published diagnostics per absolute path.
	// A server republishes the full set for a file on every change, so the
	// latest notification wins outright rather than merging.
	diags map[string][]ports.Diagnostic
	// opened tracks which files we have sent didOpen for, so a re-read sends
	// didChange instead (servers reject a second didOpen for one URI).
	opened map[string]bool
}

// NewClient starts a language server and completes the initialize handshake.
// It returns once the server has answered initialize, so a caller that gets a
// Client back can immediately use it.
func NewClient(ctx context.Context, cfg ClientConfig) (*Client, error) {
	c := &Client{
		root:   cfg.Root,
		diags:  map[string][]ports.Diagnostic{},
		opened: map[string]bool{},
	}

	rwc := cfg.RWC
	if rwc == nil {
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Dir = cfg.Root
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("lsp: %s: stdin: %w", cfg.Command, err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("lsp: %s: stdout: %w", cfg.Command, err)
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("lsp: %s: start: %w", cfg.Command, err)
		}
		c.cmd = cmd
		rwc = &pipeRWC{r: stdout, w: stdin}
	}
	c.rwc = rwc

	c.conn = jsonrpc2.NewConn(jsonrpc2.NewStream(rwc))
	c.conn.Go(ctx, c.handle)

	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// initialize performs the LSP handshake. Capabilities are deliberately minimal:
// we ask for the read-only surfaces the port exposes and nothing else.
func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   pathToURI(c.root),
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
				"hover":              map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
				"definition":         map[string]any{},
				"references":         map[string]any{},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": false},
			},
		},
	}
	var result map[string]any
	if _, err := c.conn.Call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("lsp: initialize: %w", err)
	}
	if err := c.conn.Notify(ctx, "initialized", map[string]any{}); err != nil {
		return fmt.Errorf("lsp: initialized: %w", err)
	}

	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

// Initialized reports whether the handshake completed.
func (c *Client) Initialized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialized
}

// handle dispatches server-initiated traffic. Diagnostics are the reason this
// exists: they are pushed, never requested.
//
// Unknown methods return ErrNotHandled rather than an error that would fail the
// connection — a server is entitled to send us things we did not ask for.
func (c *Client) handle(_ context.Context, req *jsonrpc2.Request) (any, error) {
	switch req.Method() {
	case "textDocument/publishDiagnostics":
		var p publishDiagnosticsParams
		if err := decode(req.Params(), &p); err != nil {
			// A malformed notification must not kill the connection; the file
			// simply keeps its previous diagnostics.
			return nil, nil
		}
		c.storeDiagnostics(p)
		return nil, nil

	case "window/logMessage", "window/showMessage", "$/progress",
		"telemetry/event", "window/workDoneProgress/create":
		return nil, nil
	}
	return nil, jsonrpc2.ErrNotHandled
}

func (c *Client) storeDiagnostics(p publishDiagnosticsParams) {
	abs := uriToPath(p.URI)
	rel := c.relPath(abs)

	out := make([]ports.Diagnostic, 0, len(p.Diagnostics))
	for _, d := range p.Diagnostics {
		out = append(out, ports.Diagnostic{
			Path:     rel,
			Line:     d.Range.Start.Line + 1,
			Column:   d.Range.Start.Character + 1,
			Severity: toSeverity(d.Severity),
			Message:  d.Message,
			Source:   d.Source,
		})
	}

	c.mu.Lock()
	c.diags[abs] = out
	c.mu.Unlock()
}

// Diagnostics returns the latest published diagnostics for a file.
func (c *Client) Diagnostics(path string) []ports.Diagnostic {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.diags[c.absPath(path)]
}

// DidOpen tells the server about a file's contents. Calling it again for the
// same file sends didChange, which is what servers expect.
func (c *Client) DidOpen(ctx context.Context, path, text string) error {
	abs := c.absPath(path)

	c.mu.Lock()
	already := c.opened[abs]
	c.opened[abs] = true
	c.mu.Unlock()

	if already {
		return c.conn.Notify(ctx, "textDocument/didChange", map[string]any{
			"textDocument": map[string]any{"uri": pathToURI(abs), "version": 2},
			"contentChanges": []map[string]any{
				{"text": text},
			},
		})
	}
	return c.conn.Notify(ctx, "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        pathToURI(abs),
			"languageId": languageID(abs),
			"version":    1,
			"text":       text,
		},
	})
}

// Hover returns the server's description of the symbol at a position.
func (c *Client) Hover(ctx context.Context, path string, line, col int) (string, error) {
	var res hoverResult
	if err := c.call(ctx, "textDocument/hover", c.position(path, line, col), &res); err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Contents.Value), nil
}

// Definition locates the definition of the symbol at a position.
func (c *Client) Definition(ctx context.Context, path string, line, col int) ([]ports.Location, error) {
	return c.locations(ctx, "textDocument/definition", c.position(path, line, col))
}

// References finds uses of the symbol at a position.
func (c *Client) References(ctx context.Context, path string, line, col int) ([]ports.Location, error) {
	params := c.position(path, line, col)
	params["context"] = map[string]any{"includeDeclaration": false}
	return c.locations(ctx, "textDocument/references", params)
}

func (c *Client) locations(ctx context.Context, method string, params map[string]any) ([]ports.Location, error) {
	var raw []wireLocation
	if err := c.call(ctx, method, params, &raw); err != nil {
		return nil, err
	}
	out := make([]ports.Location, 0, len(raw))
	for _, l := range raw {
		out = append(out, ports.Location{
			Path:   c.relPath(uriToPath(l.URI)),
			Line:   l.Range.Start.Line + 1,
			Column: l.Range.Start.Character + 1,
		})
	}
	return out, nil
}

// DocumentSymbols lists the declarations in a file.
func (c *Client) DocumentSymbols(ctx context.Context, path string) ([]ports.Symbol, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(c.absPath(path))},
	}
	var raw []wireSymbol
	if err := c.call(ctx, "textDocument/documentSymbol", params, &raw); err != nil {
		return nil, err
	}
	rel := c.relPath(c.absPath(path))
	out := make([]ports.Symbol, 0, len(raw))
	for _, s := range raw {
		line := s.Range.Start.Line
		if s.Location.URI != "" {
			// SymbolInformation (flat) shape rather than DocumentSymbol.
			line = s.Location.Range.Start.Line
		}
		out = append(out, ports.Symbol{
			Name: s.Name,
			Kind: symbolKind(s.Kind),
			Path: rel,
			Line: line + 1,
		})
	}
	return out, nil
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return fmt.Errorf("lsp: %s: client is closed", method)
	}
	if _, err := c.conn.Call(ctx, method, params, result); err != nil {
		return fmt.Errorf("lsp: %s: %w", method, err)
	}
	return nil
}

func (c *Client) position(path string, line, col int) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(c.absPath(path))},
		// Neutral positions are 1-based; LSP is 0-based.
		"position": map[string]any{"line": max(line-1, 0), "character": max(col-1, 0)},
	}
}

// Close shuts the server down. It is idempotent so a deferred Close after an
// error path is safe, and it never blocks on a server that has stopped
// answering — a hung language server must not hang the agent.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	var firstErr error
	if c.rwc != nil {
		if err := c.rwc.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			firstErr = err
		}
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return firstErr
}

// absPath resolves a possibly-relative path against the project root.
func (c *Client) absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.root, path)
}

// relPath reports a path relative to the project root, so neutral values never
// leak the absolute layout of the developer's machine into the model's context.
func (c *Client) relPath(abs string) string {
	if rel, err := filepath.Rel(c.root, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return abs
}

// pipeRWC joins a subprocess's stdout and stdin into one ReadWriteCloser.
type pipeRWC struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p *pipeRWC) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeRWC) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeRWC) Close() error {
	err := p.w.Close()
	if rerr := p.r.Close(); err == nil {
		err = rerr
	}
	return err
}
