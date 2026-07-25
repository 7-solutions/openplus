package tui

import (
	"testing"

	"github.com/7solutions/openplus/internal/provider"
)

func newTestModel() Model {
	return New(nil, "sys", nil)
}

func TestApplyEventTextDeltaAccumulates(t *testing.T) {
	m := newTestModel()
	m.applyEvent(provider.Event{Kind: provider.EventTextDelta, Text: "hel"})
	m.applyEvent(provider.Event{Kind: provider.EventTextDelta, Text: "lo"})
	// accumulated into the current buffer, flushed on turn end
	m.applyEvent(provider.Event{Kind: provider.EventTurnEnd})
	if len(m.log) != 1 || m.log[0] != "hello" {
		t.Fatalf("log = %v, want [hello]", m.log)
	}
	if m.cur.Len() != 0 {
		t.Fatalf("buffer not flushed: %q", m.cur.String())
	}
}

func TestApplyEventToolCallFlushesTextAndRenders(t *testing.T) {
	m := newTestModel()
	m.applyEvent(provider.Event{Kind: provider.EventTextDelta, Text: "thinking..."})
	m.applyEvent(provider.Event{Kind: provider.EventToolCallStart, Call: &provider.ToolCall{
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
	m.applyEvent(provider.Event{Kind: provider.EventThinkingDelta, Text: "hm"})
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
	m.applyEvent(provider.Event{Kind: provider.EventError, Err: errBoom})
	if m.err == nil || m.err.Error() != "boom" {
		t.Fatalf("err = %v", m.err)
	}
	if len(m.log) == 0 || m.log[len(m.log)-1] != "error: boom" {
		t.Errorf("error not logged: %v", m.log)
	}
}

func TestSubmitAppendsUserAndStartsBusy(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("hello there")
	m.submit() // captures input, marks busy, clears input
	if m.busy != true {
		t.Fatalf("not busy after submit")
	}
	if m.input.Value() != "" {
		t.Fatalf("input not cleared: %q", m.input.Value())
	}
	if len(m.history) != 1 {
		t.Fatalf("history = %v", m.history)
	}
	if m.history[0].Role != provider.RoleUser {
		t.Fatalf("role = %v", m.history[0].Role)
	}
	if m.history[0].Blocks[0].Text != "hello there" {
		t.Fatalf("user text = %q", m.history[0].Blocks[0].Text)
	}
}
