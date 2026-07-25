package runtime

import (
	"strings"
	"testing"
)

// TestDocsCommandRegistered: /docs is in the dispatch table.
func TestDocsCommandRegistered(t *testing.T) {
	if _, ok := builtinCommands["docs"]; !ok {
		t.Fatal("builtinCommands missing \"docs\"")
	}
}

// TestDocsArityError: /docs with no argument is a usage error, not a network call.
func TestDocsArityError(t *testing.T) {
	s := cmdSession(t)
	_, err := s.cmdDocs("   ")
	if err == nil {
		t.Fatal("/docs with no arg: want error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "library") {
		t.Errorf("error should mention a library name; got %q", err)
	}
}

// TestDocsNotConnected: a fake session (no Context7 injected) returns a helpful
// hint instead of panicking or silently succeeding.
func TestDocsNotConnected(t *testing.T) {
	s := cmdSession(t) // Fake:true → no default Context7 injected
	_, err := s.cmdDocs("react hooks")
	if err == nil {
		t.Fatal("/docs with Context7 absent: want error, got nil")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "context7") {
		t.Errorf("error should name Context7; got %q", err)
	}
	if !strings.Contains(msg, "mcp.context7.com") {
		t.Errorf("error should point at the endpoint; got %q", err)
	}
}
