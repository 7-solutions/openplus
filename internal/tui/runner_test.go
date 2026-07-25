package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/7solutions/openplus/internal/provider"
)

// stubRunner records the input it was asked to run and returns a canned history.
type stubRunner struct {
	gotInput   string
	gotHistory []provider.Message
	reply      string
	err        error
}

func (r *stubRunner) Run(_ context.Context, input string, history []provider.Message) ([]provider.Message, error) {
	r.gotInput = input
	r.gotHistory = history
	if r.err != nil {
		return nil, r.err
	}
	// Mirror runtime.Session.Run: build the user message from input and append
	// it (plus the assistant reply) to the prior history. The model therefore
	// never owns user-turn assembly — it only owns the transcript returned by
	// the runner.
	out := append([]provider.Message{}, history...)
	out = append(out, provider.Message{
		Role:   provider.RoleUser,
		Blocks: []provider.Block{{Kind: provider.BlockText, Text: input}},
	})
	return append(out, provider.Message{
		Role:   provider.RoleAssistant,
		Blocks: []provider.Block{{Kind: provider.BlockText, Text: r.reply}},
	}), nil
}

func TestModelRunsTurnThroughRunner(t *testing.T) {
	r := &stubRunner{reply: "the answer"}
	m := New(r, "sys")
	m.input.SetValue("the question")

	// enter submits and returns the turn command
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if !m.busy {
		t.Fatal("model should be busy after submit")
	}
	if cmd == nil {
		t.Fatal("submit should return a turn command")
	}

	// executing the command drives the runner
	msg := cmd()
	done, ok := msg.(turnDoneMsg)
	if !ok {
		t.Fatalf("command produced %T, want turnDoneMsg", msg)
	}
	if done.err != nil {
		t.Fatalf("turn error: %v", done.err)
	}
	if r.gotInput != "the question" {
		t.Errorf("runner input = %q, want the submitted text", r.gotInput)
	}
	// the runner owns history assembly, so the model passes its own prior turns
	if len(r.gotHistory) != 0 {
		t.Errorf("first turn should pass empty history, got %+v", r.gotHistory)
	}
}

func TestModelTurnErrorSurfacesInLog(t *testing.T) {
	boom := errors.New("provider exploded")
	m := New(&stubRunner{err: boom}, "sys")
	mm, _ := m.Update(turnDoneMsg{err: boom})
	m = mm.(Model)
	if m.err == nil {
		t.Fatal("turn error not recorded")
	}
	found := false
	for _, line := range m.log {
		if line == "turn error: provider exploded" {
			found = true
		}
	}
	if !found {
		t.Errorf("turn error missing from the log: %v", m.log)
	}
}

func TestModelKeepsHistoryAcrossTurns(t *testing.T) {
	r := &stubRunner{reply: "first answer"}
	m := New(r, "sys")

	// turn one
	m.input.SetValue("first question")
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	if m.busy {
		t.Fatal("model should not be busy after the turn completes")
	}

	// turn two must carry turn one's history into the runner
	r.reply = "second answer"
	m.input.SetValue("second question")
	mm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	_ = cmd()
	if len(r.gotHistory) == 0 {
		t.Fatal("second turn passed no history")
	}
}

func TestModelIgnoresEnterWhileBusy(t *testing.T) {
	r := &stubRunner{reply: "x"}
	m := New(r, "sys")
	m.busy = true
	m.input.SetValue("queued question")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		// a nil cmd is the input component's own; what matters is no turn started
		if _, ok := cmd().(turnDoneMsg); ok {
			t.Fatal("a second turn started while busy")
		}
	}
}

func TestModelIgnoresEmptySubmit(t *testing.T) {
	m := New(&stubRunner{}, "sys")
	m.input.SetValue("   ")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.busy {
		t.Fatal("whitespace-only input should not start a turn")
	}
}
