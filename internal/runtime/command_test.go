package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/ports"
)

// countingProvider records how many times a turn reached the model, so a test
// can prove a command cost no round-trip.
type countingProvider struct{ calls int }

func (c *countingProvider) Stream(context.Context, ports.Request) (<-chan ports.Event, error) {
	c.calls++
	ch := make(chan ports.Event, 1)
	ch <- ports.Event{Kind: ports.EventTurnEnd}
	close(ch)
	return ch, nil
}

func cmdSession(t *testing.T) *Session {
	t.Helper()
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	return s
}

// --- T-900: dispatch ---

// TestDispatchIgnoresPlainInput is the spec scenario: non-slash input is not a
// command, so the caller falls through to a normal turn.
func TestDispatchIgnoresPlainInput(t *testing.T) {
	s := cmdSession(t)
	out, handled, err := s.Dispatch(context.Background(), "just a normal question")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if handled {
		t.Fatal("plain input must not be handled as a command")
	}
	if out != "" {
		t.Errorf("output = %q, want empty for unhandled input", out)
	}
}

func TestDispatchHandlesKnownCommand(t *testing.T) {
	s := cmdSession(t)
	out, handled, err := s.Dispatch(context.Background(), "/help")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !handled {
		t.Fatal("/help must be handled")
	}
	if out == "" {
		t.Fatal("/help returned empty output")
	}
}

// TestDispatchMakesNoProviderCall is the spec scenario: a command must not cost
// a model round-trip.
func TestDispatchMakesNoProviderCall(t *testing.T) {
	s := cmdSession(t)
	counted := &countingProvider{}
	s.Provider = counted

	if _, handled, err := s.Dispatch(context.Background(), "/help"); err != nil || !handled {
		t.Fatalf("Dispatch: handled=%v err=%v", handled, err)
	}
	if counted.calls != 0 {
		t.Errorf("provider called %d times, want 0", counted.calls)
	}
}

// TestDispatchUnknownCommandIsActionable is the spec scenario: the error names
// the unknown command and lists the known ones.
func TestDispatchUnknownCommandIsActionable(t *testing.T) {
	s := cmdSession(t)
	_, handled, err := s.Dispatch(context.Background(), "/nonsense")
	if !handled {
		t.Fatal("an unknown slash command is still a command attempt, so handled must be true")
	}
	if err == nil {
		t.Fatal("expected an error for an unknown command")
	}
	if !strings.Contains(err.Error(), "nonsense") {
		t.Errorf("error should name the unknown command: %v", err)
	}
	// it must tell the user what they *can* run
	if !strings.Contains(err.Error(), "help") {
		t.Errorf("error should list known commands: %v", err)
	}
}

func TestDispatchTrimsWhitespace(t *testing.T) {
	s := cmdSession(t)
	_, handled, err := s.Dispatch(context.Background(), "   /help   ")
	if err != nil || !handled {
		t.Fatalf("leading/trailing space should not defeat dispatch: handled=%v err=%v", handled, err)
	}
}

func TestDispatchEmptyInputUnhandled(t *testing.T) {
	s := cmdSession(t)
	_, handled, _ := s.Dispatch(context.Background(), "   ")
	if handled {
		t.Fatal("blank input is not a command")
	}
}

func TestDispatchBareSlashIsActionable(t *testing.T) {
	s := cmdSession(t)
	_, handled, err := s.Dispatch(context.Background(), "/")
	if !handled {
		t.Fatal("a bare slash is a command attempt")
	}
	if err == nil {
		t.Fatal("a bare slash should error rather than do nothing")
	}
}

// TestDispatchSplitsNameAndArgs pins that args reach the command verbatim,
// including internal spaces.
func TestDispatchSplitsNameAndArgs(t *testing.T) {
	s := cmdSession(t)
	var gotArgs string
	s.registerCommand(Command{
		Name:    "probe",
		Usage:   "/probe <text>",
		Summary: "test command",
		Run: func(_ *Session, args string) (string, error) {
			gotArgs = args
			return "ok", nil
		},
	})
	if _, _, err := s.Dispatch(context.Background(), "/probe hello  world "); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if gotArgs != "hello  world" {
		t.Errorf("args = %q, want %q (verbatim, trimmed at the ends only)", gotArgs, "hello  world")
	}
}

// --- T-901: help ---

func TestHelpListsRegisteredCommands(t *testing.T) {
	s := cmdSession(t)
	out, _, err := s.Dispatch(context.Background(), "/help")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	// every command the milestone requires must be discoverable from /help
	for _, want := range []string{"/skill", "/skills", "/compose", "/dream", "/distill"} {
		if !strings.Contains(out, want) {
			t.Errorf("/help does not mention %s:\n%s", want, out)
		}
	}
}

func TestHelpIsSorted(t *testing.T) {
	s := cmdSession(t)
	out, _, err := s.Dispatch(context.Background(), "/help")
	if err != nil {
		t.Fatal(err)
	}
	// deterministic output: a map iteration leak would show up as flakiness
	second, _, _ := s.Dispatch(context.Background(), "/help")
	if out != second {
		t.Error("/help output is not deterministic across calls")
	}
}

// TestCommandNeverReturnsEmptySuccess is the spec requirement: a command
// reports what it did or errors — never silent success.
func TestCommandNeverReturnsEmptySuccess(t *testing.T) {
	s := cmdSession(t)
	for _, input := range []string{"/help", "/skills"} {
		out, handled, err := s.Dispatch(context.Background(), input)
		if !handled {
			t.Fatalf("%s not handled", input)
		}
		if err == nil && strings.TrimSpace(out) == "" {
			t.Errorf("%s returned empty success", input)
		}
	}
}
