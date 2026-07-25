package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/7-solutions/openplus/internal/ports"
	portsfake "github.com/7-solutions/openplus/internal/ports/providerfake"
)

// TestRunAccumulatesHistory pins that /dream has a transcript to work from in
// real use: Run must record the turn on the session, not just return it.
func TestRunAccumulatesHistory(t *testing.T) {
	s := cmdSession(t)
	if len(s.History) != 0 {
		t.Fatalf("fresh session should have no history: %+v", s.History)
	}

	if _, err := s.Run(context.Background(), "first question", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(s.History) == 0 {
		t.Fatal("Run did not record history on the session; /dream would see nothing")
	}

	var joined strings.Builder
	for _, m := range s.History {
		for _, b := range m.Blocks {
			joined.WriteString(b.Text)
			joined.WriteByte(' ')
		}
	}
	if !strings.Contains(joined.String(), "first question") {
		t.Errorf("history missing the user turn: %q", joined.String())
	}
}

// TestRunAccumulatesAcrossTurns pins that a second turn extends the record
// rather than replacing it.
func TestRunAccumulatesAcrossTurns(t *testing.T) {
	s := cmdSession(t)
	if _, err := s.Run(context.Background(), "turn one", nil); err != nil {
		t.Fatal(err)
	}
	afterFirst := len(s.History)

	if _, err := s.Run(context.Background(), "turn two", s.History); err != nil {
		t.Fatal(err)
	}
	if len(s.History) <= afterFirst {
		t.Fatalf("history did not grow: %d then %d", afterFirst, len(s.History))
	}
}

// TestRunRecordsToolSequence pins that /distill has material: a turn's tool
// calls become an improve.Run.
func TestRunRecordsToolSequence(t *testing.T) {
	s := cmdSession(t)
	// script a turn that calls two tools, then finishes
	s.Provider = &portsfake.Fake{Scripts: [][]ports.Event{
		{
			{Kind: ports.EventToolCallStart, Call: &ports.ToolCall{
				ID: "c1", Name: "glob", Input: []byte(`{"pattern":"*.go"}`),
			}},
			{Kind: ports.EventToolCallStart, Call: &ports.ToolCall{
				ID: "c2", Name: "grep", Input: []byte(`{"pattern":"func"}`),
			}},
			{Kind: ports.EventTurnEnd},
		},
		{{Kind: ports.EventTextDelta, Text: "done"}, {Kind: ports.EventTurnEnd}},
	}}

	if _, err := s.Run(context.Background(), "find things", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(s.Runs) != 1 {
		t.Fatalf("Runs = %d, want 1 recorded run", len(s.Runs))
	}
	got := strings.Join(s.Runs[0].Tools, ",")
	if got != "glob,grep" {
		t.Errorf("recorded sequence = %q, want glob,grep (in call order)", got)
	}
}

// TestRunRecordsNoRunWithoutTools pins that a tool-free turn does not create an
// empty run, which would pollute pattern mining.
func TestRunRecordsNoRunWithoutTools(t *testing.T) {
	s := cmdSession(t)
	if _, err := s.Run(context.Background(), "just chat", nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.Runs {
		if len(r.Tools) == 0 {
			t.Fatal("an empty tool sequence was recorded")
		}
	}
}

// TestDreamWorksAfterRealTurns is the integration point: run a turn, then
// /dream, with no hand-seeded history.
func TestDreamWorksAfterRealTurns(t *testing.T) {
	s := cmdSession(t)
	if _, err := s.Run(context.Background(), "remember the build is cgo-free", nil); err != nil {
		t.Fatal(err)
	}
	// swap in a provider that extracts one fact
	s.Provider = &portsfake.Fake{Scripts: [][]ports.Event{{
		{Kind: ports.EventTextDelta, Text: "- the build is cgo-free\n"},
		{Kind: ports.EventTurnEnd},
	}}}

	out := run(t, s, "/dream")
	if !strings.Contains(out, "1") {
		t.Errorf("/dream after a real turn should report a count: %s", out)
	}
}
