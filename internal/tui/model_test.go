package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/7solutions/openplus/internal/ports"
)

func newTestModel() Model {
	return New(&stubRunner{}, "sys")
}

func TestApplyEventTextDeltaAccumulates(t *testing.T) {
	m := newTestModel()
	m.applyEvent(ports.Event{Kind: ports.EventTextDelta, Text: "hel"})
	m.applyEvent(ports.Event{Kind: ports.EventTextDelta, Text: "lo"})
	// accumulated into the current buffer, flushed on turn end
	m.applyEvent(ports.Event{Kind: ports.EventTurnEnd})
	if len(m.log) != 1 || m.log[0] != "hello" {
		t.Fatalf("log = %v, want [hello]", m.log)
	}
	if m.cur.Len() != 0 {
		t.Fatalf("buffer not flushed: %q", m.cur.String())
	}
}

func TestApplyEventToolCallFlushesTextAndRenders(t *testing.T) {
	m := newTestModel()
	m.applyEvent(ports.Event{Kind: ports.EventTextDelta, Text: "thinking..."})
	m.applyEvent(ports.Event{Kind: ports.EventToolCallStart, Call: &ports.ToolCall{
		ID: "c1", Name: "echo", Input: []byte(`{"text":"hi"}`),
	}})
	// text flushed before the tool line
	if len(m.log) != 2 {
		t.Fatalf("log = %v", m.log)
	}
	if m.log[0] != "thinking..." {
		t.Errorf("text not flushed first: %q", m.log[0])
	}
	if m.log[1] != `echo({"text":"hi"})` {
		t.Errorf("tool line = %q", m.log[1])
	}
}

func TestApplyEventThinkingRendersDim(t *testing.T) {
	m := newTestModel()
	m.applyEvent(ports.Event{Kind: ports.EventThinkingDelta, Text: "hm"})
	if len(m.log) != 1 {
		t.Fatalf("log = %v", m.log)
	}
	// thinking goes straight to log (separate from assistant text buffer)
	if m.log[0] != "(thinking) hm" {
		t.Errorf("thinking line = %q", m.log[0])
	}
}

func TestApplyEventErrorRecords(t *testing.T) {
	m := newTestModel()
	m.applyEvent(ports.Event{Kind: ports.EventError, Err: errBoom})
	if m.err == nil || m.err.Error() != "boom" {
		t.Fatalf("err = %v", m.err)
	}
	if len(m.log) == 0 || m.log[len(m.log)-1] != "error: boom" {
		t.Errorf("error not logged: %v", m.log)
	}
}

func TestSubmitAppendsUserAndStartsBusy(t *testing.T) {
	// The model never owns the user turn on its own — runtime.Session.Run
	// builds it from the submitted input string and returns the assembled
	// history in turnDoneMsg. This test drives that end-to-end shape so a
	// regression in either half (submit's capture or turnDoneMsg's assignment)
	// surfaces here.
	r := &stubRunner{reply: "ack"}
	m := New(r, "sys")
	m.input.SetValue("hello there")

	// enter → submit() captures input + marks busy + clears input,
	// and returns the tea.Cmd that drives runTurn.
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if !m.busy {
		t.Fatal("not busy after submit")
	}
	if m.input.Value() != "" {
		t.Fatalf("input not cleared: %q", m.input.Value())
	}
	if len(m.history) != 0 {
		t.Fatalf("history must be empty until the runner returns, got %v", m.history)
	}
	if cmd == nil {
		t.Fatal("submit should return a turn command")
	}

	// Run the turn synchronously the way the Bubble Tea program would,
	// then deliver the resulting turnDoneMsg.
	mm, _ = m.Update(cmd())
	m = mm.(Model)

	if r.gotInput != "hello there" {
		t.Errorf("runner input = %q, want the submitted text", r.gotInput)
	}
	if m.busy {
		t.Fatal("model should not be busy after the turn completes")
	}
	if len(m.history) != 2 {
		t.Fatalf("history = %v, want [user, assistant]", m.history)
	}
	if m.history[0].Role != ports.RoleUser || m.history[0].Blocks[0].Text != "hello there" {
		t.Fatalf("history[0] = %+v, want user/hello there", m.history[0])
	}
	if m.history[1].Role != ports.RoleAssistant || m.history[1].Blocks[0].Text != "ack" {
		t.Fatalf("history[1] = %+v, want assistant/ack", m.history[1])
	}
}

func TestApplyToolResultRendersDiff(t *testing.T) {
	m := newTestModel()
	m.applyToolResult(ports.ToolCall{Name: "edit"}, ports.Block{
		Kind:           ports.BlockToolResult,
		ToolResultText: "- old\n+ new\n",
	})
	if len(m.log) != 1 || m.log[0] != "- old\n+ new" {
		t.Fatalf("edit diff not rendered: %v", m.log)
	}
}

func TestApplyToolResultError(t *testing.T) {
	m := newTestModel()
	m.applyToolResult(ports.ToolCall{Name: "bash"}, ports.Block{
		Kind:            ports.BlockToolResult,
		ToolResultText:  "denied by policy",
		ToolResultError: true,
	})
	if len(m.log) != 1 || m.log[0] != "✗ bash: denied by policy" {
		t.Fatalf("error result not rendered: %v", m.log)
	}
}

func TestPromptMsgSetsPending(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(promptMsg{call: ports.ToolCall{Name: "bash"}})
	m = mm.(Model)
	if m.pending == nil || m.pending.Name != "bash" {
		t.Fatalf("pending not set: %+v", m.pending)
	}
}

func TestAnswerPromptSendsAndClears(t *testing.T) {
	m := newTestModel()
	m.answer = make(chan bool, 1)
	m.pending = &ports.ToolCall{Name: "bash"}
	m = m.answerPrompt(true)
	select {
	case got := <-m.answer:
		if !got {
			t.Fatal("want true on answer channel")
		}
	default:
		t.Fatal("answer not sent")
	}
	if m.pending != nil {
		t.Fatal("pending not cleared")
	}
}

func TestAnswerPromptWithoutChannelNoPanic(t *testing.T) {
	m := newTestModel()
	m.pending = &ports.ToolCall{Name: "x"}
	// answer is nil — must not panic, must clear pending.
	m = m.answerPrompt(false)
	if m.pending != nil {
		t.Fatal("pending not cleared")
	}
}
