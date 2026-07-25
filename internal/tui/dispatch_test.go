package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/7-solutions/openplus/internal/ports"
)

// dispatchRunner is a Runner that also implements Dispatcher, so the TUI can
// route slash commands without a turn.
type dispatchRunner struct {
	stubRunner
	gotCommand string
	output     string
	err        error
	handled    bool
}

func (d *dispatchRunner) Dispatch(_ context.Context, input string) (string, bool, error) {
	d.gotCommand = input
	return d.output, d.handled, d.err
}

// TestSubmitDispatchesCommandWithoutTurn is the spec scenario: a command renders
// in the transcript and costs no provider round-trip.
func TestSubmitDispatchesCommandWithoutTurn(t *testing.T) {
	d := &dispatchRunner{handled: true, output: "commands:\n  /help"}
	m := New(d, "sys")
	m.input.SetValue("/help")

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)

	// the command output is in the transcript
	joined := strings.Join(m.log, "\n")
	if !strings.Contains(joined, "/help") {
		t.Errorf("submitted command missing from the log:\n%s", joined)
	}
	if !strings.Contains(joined, "commands:") {
		t.Errorf("command output missing from the log:\n%s", joined)
	}
	// no turn was started
	if m.busy {
		t.Error("a dispatched command must not put the model in a turn")
	}
	if cmd != nil {
		if _, isTurn := cmd().(turnDoneMsg); isTurn {
			t.Error("a dispatched command must not run a turn")
		}
	}
	if d.gotInput != "" {
		t.Errorf("Runner.Run was called with %q; the command should not have reached it", d.gotInput)
	}
}

// TestSubmitCommandErrorShowsInLog pins that a failing command is visible rather
// than silently swallowed.
func TestSubmitCommandErrorShowsInLog(t *testing.T) {
	d := &dispatchRunner{handled: true, err: errors.New("unknown command /nope")}
	m := New(d, "sys")
	m.input.SetValue("/nope")

	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)

	joined := strings.Join(m.log, "\n")
	if !strings.Contains(joined, "unknown command") {
		t.Errorf("command error missing from the log:\n%s", joined)
	}
	if m.busy {
		t.Error("a failed command should not leave the model busy")
	}
}

// TestSubmitUnhandledFallsThroughToTurn pins that plain input still runs a turn
// even when the runner can dispatch.
func TestSubmitUnhandledFallsThroughToTurn(t *testing.T) {
	d := &dispatchRunner{handled: false}
	d.reply = "an answer"
	m := New(d, "sys")
	m.input.SetValue("a normal question")

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if !m.busy {
		t.Fatal("unhandled input should start a turn")
	}
	if cmd == nil {
		t.Fatal("expected a turn command")
	}
	if _, ok := cmd().(turnDoneMsg); !ok {
		t.Error("expected the turn to run")
	}
	if d.gotInput != "a normal question" {
		t.Errorf("runner input = %q", d.gotInput)
	}
}

// TestSubmitPlainRunnerStillWorks pins backward compatibility: a Runner that
// does not implement Dispatcher behaves exactly as before.
func TestSubmitPlainRunnerStillWorks(t *testing.T) {
	r := &stubRunner{reply: "fine"}
	m := New(r, "sys")
	m.input.SetValue("/looks-like-a-command")

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if !m.busy {
		t.Fatal("without a Dispatcher, everything is a turn")
	}
	if cmd == nil {
		t.Fatal("expected a turn command")
	}
	_ = cmd()
	if r.gotInput != "/looks-like-a-command" {
		t.Errorf("runner input = %q", r.gotInput)
	}
}

// compile-time: dispatchRunner satisfies both seams.
var (
	_ Runner     = (*dispatchRunner)(nil)
	_ Dispatcher = (*dispatchRunner)(nil)
	_            = ports.Message{}
)
