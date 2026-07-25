package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/7-solutions/openplus/internal/ports"
)

// countingLS records how many times Diagnostics was asked for, so a test can
// prove the refresh actually ran (and ran once per edit, not per turn).
type countingLS struct {
	mu    sync.Mutex
	calls int
	diags []ports.Diagnostic
}

func (c *countingLS) Diagnostics(context.Context, string) ([]ports.Diagnostic, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.diags, nil
}
func (c *countingLS) Hover(context.Context, string, int, int) (string, error) { return "", nil }
func (c *countingLS) Definition(context.Context, string, int, int) ([]ports.Location, error) {
	return nil, nil
}
func (c *countingLS) DocumentSymbols(context.Context, string) ([]ports.Symbol, error) {
	return nil, nil
}
func (c *countingLS) References(context.Context, string, int, int) ([]ports.Location, error) {
	return nil, nil
}
func (c *countingLS) Shutdown(context.Context) error { return nil }

func (c *countingLS) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func editCall(path string) ports.ToolCall {
	return ports.ToolCall{Name: "edit", Input: []byte(fmt.Sprintf(`{"path":%q}`, path))}
}

// TestDiagnosticsInjectedAfterEdit is the point of M4: the agent sees the
// breakage it just caused without having to ask.
func TestDiagnosticsInjectedAfterEdit(t *testing.T) {
	s := cmdSession(t)
	s.LanguageService = &countingLS{diags: []ports.Diagnostic{
		{Path: "main.go", Line: 10, Column: 2, Severity: ports.SeverityError,
			Message: "undefined: foo", Source: "compiler"},
	}}

	s.noteEditedFile("main.go")
	s.refreshDiagnostics(context.Background())

	turn, err := s.AssembleContext(context.Background(), "what did I break?", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if !strings.Contains(turn.System, "# Diagnostics") {
		t.Fatalf("no diagnostics section in the system prompt:\n%s", turn.System)
	}
	if !strings.Contains(turn.System, "main.go:10:2") {
		t.Errorf("diagnostics section missing the position:\n%s", turn.System)
	}
	if !strings.Contains(turn.System, "undefined: foo") {
		t.Errorf("diagnostics section missing the message:\n%s", turn.System)
	}
}

// TestDiagnosticsAbsentWithoutLSP: the overwhelmingly common case must add
// nothing to the prompt.
func TestDiagnosticsAbsentWithoutLSP(t *testing.T) {
	s := cmdSession(t) // fake session: LanguageService is nil

	s.noteEditedFile("main.go")
	s.refreshDiagnostics(context.Background())

	turn, err := s.AssembleContext(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if strings.Contains(turn.System, "# Diagnostics") {
		t.Errorf("diagnostics section present with no LanguageService:\n%s", turn.System)
	}
}

// TestDiagnosticsAbsentWhenClean: a file with no problems must not produce an
// empty section — silence is the correct signal for working code.
func TestDiagnosticsAbsentWhenClean(t *testing.T) {
	s := cmdSession(t)
	s.LanguageService = &countingLS{} // no diagnostics

	s.noteEditedFile("main.go")
	s.refreshDiagnostics(context.Background())

	turn, err := s.AssembleContext(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if strings.Contains(turn.System, "# Diagnostics") {
		t.Errorf("diagnostics section present for a clean file:\n%s", turn.System)
	}
}

// TestDiagnosticsAreCapped: a catastrophically broken file must not crowd out
// the rest of the context. The overflow is summarized, never silently dropped.
func TestDiagnosticsAreCapped(t *testing.T) {
	many := make([]ports.Diagnostic, 60)
	for i := range many {
		many[i] = ports.Diagnostic{
			Path: "main.go", Line: i + 1, Column: 1,
			Severity: ports.SeverityError,
			Message:  fmt.Sprintf("problem number %d", i),
		}
	}
	s := cmdSession(t)
	s.LanguageService = &countingLS{diags: many}

	s.noteEditedFile("main.go")
	s.refreshDiagnostics(context.Background())

	turn, err := s.AssembleContext(context.Background(), "status?", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}

	lines := strings.Count(turn.System, "problem number ")
	if lines > maxInjectedDiagnostics {
		t.Errorf("rendered %d diagnostics, want at most %d", lines, maxInjectedDiagnostics)
	}
	if lines == 0 {
		t.Fatal("no diagnostics rendered at all")
	}
	// The remainder must be accounted for, not vanish.
	if !strings.Contains(turn.System, "more") {
		t.Errorf("truncated diagnostics must summarize the remainder:\n%s", turn.System)
	}
}

// TestEditedFilesAreDeduped: touching one file repeatedly in a turn must not
// query the language server once per touch.
func TestEditedFilesAreDeduped(t *testing.T) {
	ls := &countingLS{}
	s := cmdSession(t)
	s.LanguageService = ls

	for range 5 {
		s.noteEditedFile("main.go")
	}
	s.refreshDiagnostics(context.Background())

	if got := ls.callCount(); got != 1 {
		t.Errorf("Diagnostics called %d times for 5 edits of one file, want 1", got)
	}
}

// TestOnToolResultHookPreservesTheUserCallback: the render hook the TUI relies
// on must still fire after the diagnostics wrapper is installed.
func TestOnToolResultHookPreservesTheUserCallback(t *testing.T) {
	s := cmdSession(t)
	s.LanguageService = &countingLS{}

	var got []string
	s.OnToolResult = func(call ports.ToolCall, _ ports.Block) {
		got = append(got, call.Name)
	}

	hook := s.toolResultHook()
	hook(editCall("main.go"), ports.Block{})

	if len(got) != 1 || got[0] != "edit" {
		t.Fatalf("user callback saw %v, want [edit]", got)
	}
	if !s.hasEditedFiles() {
		t.Error("the edit was not recorded for a diagnostics refresh")
	}
}

// TestOnlyMutatingToolsTriggerARefresh: reads do not change the code, so they
// must not schedule work.
func TestOnlyMutatingToolsTriggerARefresh(t *testing.T) {
	s := cmdSession(t)
	s.LanguageService = &countingLS{}
	hook := s.toolResultHook()

	hook(ports.ToolCall{Name: "read", Input: []byte(`{"path":"main.go"}`)}, ports.Block{})
	hook(ports.ToolCall{Name: "grep", Input: []byte(`{"pattern":"x"}`)}, ports.Block{})

	if s.hasEditedFiles() {
		t.Error("a read-only tool scheduled a diagnostics refresh")
	}

	hook(editCall("main.go"), ports.Block{})
	if !s.hasEditedFiles() {
		t.Error("edit did not schedule a diagnostics refresh")
	}
}

// TestToolResultHookIsNilWhenNothingNeedsIt: with no LanguageService and no
// user callback there is nothing to do, and the agent should get a nil hook
// rather than a wrapper that runs on every tool call.
func TestToolResultHookIsNilWhenNothingNeedsIt(t *testing.T) {
	s := cmdSession(t)
	s.OnToolResult = nil
	s.LanguageService = nil

	if s.toolResultHook() != nil {
		t.Error("hook installed with no LanguageService and no user callback")
	}
}
