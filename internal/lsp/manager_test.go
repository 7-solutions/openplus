package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/7-solutions/openplus/internal/config"
	"github.com/7-solutions/openplus/internal/ports"
)

// TestManagerImplementsPort is the compile-time contract: the manager is what
// the runtime stores in the LanguageService port.
func TestManagerImplementsPort(t *testing.T) {
	var _ ports.LanguageService = (*Manager)(nil)
}

// TestManagerUnknownExtensionIsCleanNoOp: a file no server handles must not be
// an error. The agent edits Markdown and JSON constantly.
func TestManagerUnknownExtensionIsCleanNoOp(t *testing.T) {
	m := NewManager("/proj", config.LSP{
		Enabled: true,
		Servers: map[string]config.LSPServer{".go": {Command: "gopls"}},
	})
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	ctx := context.Background()
	if d, err := m.Diagnostics(ctx, "README.md"); err != nil || len(d) != 0 {
		t.Errorf("Diagnostics(README.md) = %v, %v; want empty, nil", d, err)
	}
	if h, err := m.Hover(ctx, "README.md", 1, 1); err != nil || h != "" {
		t.Errorf("Hover(README.md) = %q, %v; want empty, nil", h, err)
	}
	if l, err := m.Definition(ctx, "README.md", 1, 1); err != nil || len(l) != 0 {
		t.Errorf("Definition(README.md) = %v, %v; want empty, nil", l, err)
	}
	if s, err := m.DocumentSymbols(ctx, "README.md"); err != nil || len(s) != 0 {
		t.Errorf("DocumentSymbols(README.md) = %v, %v; want empty, nil", s, err)
	}
}

// TestManagerLazyStart: constructing a Manager must not spawn anything. A
// session that never touches a Go file never pays for gopls.
func TestManagerLazyStart(t *testing.T) {
	m := NewManager("/proj", config.LSP{
		Enabled: true,
		Servers: map[string]config.LSPServer{".go": {Command: "definitely-not-a-real-binary"}},
	})
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	if n := m.running(); n != 0 {
		t.Fatalf("%d servers running before first use, want 0", n)
	}
}

// TestManagerMissingBinaryIsWarningNotPanic: the user configured a server that
// is not installed. That costs them LSP for that language, not the session.
func TestManagerMissingBinaryIsWarningNotPanic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")

	m := NewManager(root, config.LSP{
		Enabled: true,
		Servers: map[string]config.LSPServer{".go": {Command: "definitely-not-a-real-binary"}},
	})
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	// Must not panic, must not hang, must report empty rather than exploding.
	diags, err := m.Diagnostics(context.Background(), "main.go")
	if err != nil {
		t.Fatalf("Diagnostics with a missing binary returned an error: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("diagnostics = %v, want empty", diags)
	}

	warnings := m.Warnings()
	if len(warnings) == 0 {
		t.Fatal("a missing server binary must produce a warning")
	}
	if !strings.Contains(warnings[0], "definitely-not-a-real-binary") {
		t.Errorf("warning %q should name the failing command", warnings[0])
	}
}

// TestManagerDoesNotRetryAFailedServer: one failed start is a warning; retrying
// on every call would mean a fork+exec per tool call for a missing binary.
func TestManagerDoesNotRetryAFailedServer(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")

	m := NewManager(root, config.LSP{
		Enabled: true,
		Servers: map[string]config.LSPServer{".go": {Command: "definitely-not-a-real-binary"}},
	})
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	ctx := context.Background()
	for range 3 {
		_, _ = m.Diagnostics(ctx, "main.go")
	}
	if got := len(m.Warnings()); got != 1 {
		t.Errorf("warnings = %d after 3 calls, want 1 (no retry storm)", got)
	}
}

// TestManagerDisabledStartsNothing: the opt-in guarantee.
func TestManagerDisabledStartsNothing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")

	m := NewManager(root, config.LSP{
		Enabled: false,
		Servers: map[string]config.LSPServer{".go": {Command: "definitely-not-a-real-binary"}},
	})
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	if _, err := m.Diagnostics(context.Background(), "main.go"); err != nil {
		t.Fatalf("disabled Diagnostics: %v", err)
	}
	if n := m.running(); n != 0 {
		t.Errorf("%d servers running while disabled, want 0", n)
	}
	if w := m.Warnings(); len(w) != 0 {
		t.Errorf("disabled manager warned: %v", w)
	}
}

func TestManagerShutdownIsIdempotent(t *testing.T) {
	m := NewManager("/proj", config.LSP{Enabled: true})
	ctx := context.Background()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown must be a no-op, got: %v", err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
