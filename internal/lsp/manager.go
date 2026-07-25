package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/7-solutions/openplus/internal/config"
	"github.com/7-solutions/openplus/internal/ports"
)

// Manager routes code-intelligence questions to the language server that
// handles a file's extension, starting servers lazily. It implements
// ports.LanguageService.
//
// Failure policy: a server that cannot start costs the user LSP for that one
// language, never the session. Every failure becomes a named warning, the
// failure is remembered so a missing binary is not re-forked on every call, and
// every surface degrades to an empty result with a nil error. An agent asking
// "what is broken in this file?" and getting "nothing I can see" is correct
// behavior when no server is available; an error there would abort a tool call
// over an optional enhancement.
type Manager struct {
	root string
	cfg  config.LSP

	mu       sync.Mutex
	clients  map[string]*Client // keyed by extension
	failed   map[string]bool    // extensions whose server failed to start
	warnings []string
}

var _ ports.LanguageService = (*Manager)(nil)

// NewManager returns a Manager. It starts nothing: servers are spawned on first
// use of a file they handle.
func NewManager(root string, cfg config.LSP) *Manager {
	return &Manager{
		root:    root,
		cfg:     cfg,
		clients: map[string]*Client{},
		failed:  map[string]bool{},
	}
}

// Warnings reports every server that could not be used, in the order the
// failures happened. The runtime surfaces these the way it surfaces MCP
// warnings.
func (m *Manager) Warnings() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.warnings...)
}

// running reports how many servers are live (tests and diagnostics).
func (m *Manager) running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.clients)
}

// clientFor returns the started client for a path, or nil when no server is
// configured for it, LSP is disabled, or the server previously failed to start.
//
// The file is opened on the server the first time we route to it, so a request
// that arrives before any edit still has a document to work against.
func (m *Manager) clientFor(ctx context.Context, path string) *Client {
	if !m.cfg.Configured() {
		return nil
	}
	srv, ok := m.cfg.ServerFor(path)
	if !ok {
		return nil
	}
	ext := filepath.Ext(path)

	m.mu.Lock()
	if c, ok := m.clients[ext]; ok {
		m.mu.Unlock()
		m.sync(ctx, c, path)
		return c
	}
	if m.failed[ext] {
		m.mu.Unlock()
		return nil
	}
	// Mark the extension as failed before releasing the lock: if the start
	// below fails we leave it set, and if it succeeds we clear it. This is what
	// makes a missing binary cost exactly one fork+exec.
	m.failed[ext] = true
	m.mu.Unlock()

	c, err := NewClient(ctx, ClientConfig{
		Root:    m.root,
		Command: srv.Command,
		Args:    srv.Args,
	})
	if err != nil {
		m.warn(fmt.Sprintf("lsp server %q for %s unavailable: %v", srv.Command, ext, err))
		return nil
	}

	m.mu.Lock()
	m.clients[ext] = c
	delete(m.failed, ext)
	m.mu.Unlock()

	m.sync(ctx, c, path)
	return c
}

// sync sends the file's current contents to the server. A read failure is
// ignored: the server may already know the file, and a transient read error
// must not turn a hover into an error.
func (m *Manager) sync(ctx context.Context, c *Client, path string) {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(m.root, path)
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return
	}
	_ = c.DidOpen(ctx, abs, string(body))
}

func (m *Manager) warn(msg string) {
	m.mu.Lock()
	m.warnings = append(m.warnings, msg)
	m.mu.Unlock()
}

// Diagnostics returns what the server last published for a file.
func (m *Manager) Diagnostics(ctx context.Context, path string) ([]ports.Diagnostic, error) {
	c := m.clientFor(ctx, path)
	if c == nil {
		return nil, nil
	}
	return c.Diagnostics(path), nil
}

// Hover describes the symbol at a position.
func (m *Manager) Hover(ctx context.Context, path string, line, col int) (string, error) {
	c := m.clientFor(ctx, path)
	if c == nil {
		return "", nil
	}
	return c.Hover(ctx, path, line, col)
}

// Definition locates the symbol at a position.
func (m *Manager) Definition(ctx context.Context, path string, line, col int) ([]ports.Location, error) {
	c := m.clientFor(ctx, path)
	if c == nil {
		return nil, nil
	}
	return c.Definition(ctx, path, line, col)
}

// DocumentSymbols lists the declarations in a file.
func (m *Manager) DocumentSymbols(ctx context.Context, path string) ([]ports.Symbol, error) {
	c := m.clientFor(ctx, path)
	if c == nil {
		return nil, nil
	}
	return c.DocumentSymbols(ctx, path)
}

// References finds uses of the symbol at a position.
func (m *Manager) References(ctx context.Context, path string, line, col int) ([]ports.Location, error) {
	c := m.clientFor(ctx, path)
	if c == nil {
		return nil, nil
	}
	return c.References(ctx, path, line, col)
}

// Shutdown stops every started server. Every client's Close is attempted even
// if an earlier one fails — a leaked language server is worse than a lost error
// message. Idempotent.
func (m *Manager) Shutdown(context.Context) error {
	m.mu.Lock()
	clients := m.clients
	m.clients = map[string]*Client{}
	m.mu.Unlock()

	var firstErr error
	for ext, c := range clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("lsp: closing server for %s: %w", ext, err)
		}
	}
	return firstErr
}
