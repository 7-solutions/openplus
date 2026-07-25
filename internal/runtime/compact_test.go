package runtime

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/7-solutions/openplus/internal/orchestrate"
	"github.com/7-solutions/openplus/internal/ports"
	portsfake "github.com/7-solutions/openplus/internal/ports/providerfake"
)

// msgs builds n user messages with distinguishable text.
func msgs(n int) []ports.Message {
	out := make([]ports.Message, n)
	for i := range out {
		out[i] = userMessage(msgTag(i))
	}
	return out
}

func msgTag(i int) string {
	return "MSG-" + itoa(i)
}

// --- T-1010: compact ---

func TestCompactKeepsMarkerPlusRecent(t *testing.T) {
	s := fakeSession(t, 1)
	s.KeepRecent = 3

	got := s.compact(msgs(10))
	// marker + the last 3
	if len(got) != 4 {
		t.Fatalf("compacted to %d messages, want 4 (marker + KeepRecent=3)", len(got))
	}
	for i, want := range []string{msgTag(7), msgTag(8), msgTag(9)} {
		if txt := got[i+1].Blocks[0].Text; txt != want {
			t.Errorf("got[%d] = %q, want %q (newest must survive, in order)", i+1, txt, want)
		}
	}
}

// TestCompactShortHistoryUnchanged is the spec scenario: nothing worth dropping.
func TestCompactShortHistoryUnchanged(t *testing.T) {
	s := fakeSession(t, 1)
	s.KeepRecent = 5

	in := msgs(5)
	got := s.compact(in)
	if len(got) != len(in) {
		t.Fatalf("history at the keep-count was compacted: %d -> %d", len(in), len(got))
	}
	// and one under
	if got := s.compact(msgs(2)); len(got) != 2 {
		t.Fatalf("short history compacted: %d", len(got))
	}
}

func TestCompactUsesDefaultKeepRecent(t *testing.T) {
	s := fakeSession(t, 1)
	s.KeepRecent = 0 // unset

	got := s.compact(msgs(DefaultKeepRecent + 10))
	if len(got) != DefaultKeepRecent+1 {
		t.Fatalf("compacted to %d, want %d (default keep-count + marker)",
			len(got), DefaultKeepRecent+1)
	}
}

// --- T-1011: the marker ---

// TestCompactMarkerNamesCheckpoint is the spec scenario: the marker says where
// the dropped material went.
func TestCompactMarkerNamesCheckpoint(t *testing.T) {
	s := fakeSession(t, 1)
	s.KeepRecent = 2

	got := s.compact(msgs(10))
	marker := got[0].Blocks[0].Text
	if !strings.Contains(marker, "checkpoint.md") {
		t.Errorf("marker does not name the checkpoint file: %q", marker)
	}
	if !strings.Contains(strings.ToLower(marker), "compact") {
		t.Errorf("marker does not say compaction happened: %q", marker)
	}
	// it should say how much went away, so the loss is quantified
	if !strings.Contains(marker, "8") {
		t.Errorf("marker does not report how many messages were dropped: %q", marker)
	}
}

// TestCompactMarkerIsDistinguishable is the spec scenario: neither a reader nor
// the model should mistake the marker for something that was said.
func TestCompactMarkerIsDistinguishable(t *testing.T) {
	s := fakeSession(t, 1)
	s.KeepRecent = 2
	got := s.compact(msgs(10))

	marker := got[0]
	if len(marker.Blocks) != 1 {
		t.Fatalf("marker should be a single block: %+v", marker.Blocks)
	}
	text := marker.Blocks[0].Text
	// a bracketed system-ish prefix is the signal; assert it is not bare prose
	if !strings.HasPrefix(strings.TrimSpace(text), "[") {
		t.Errorf("marker should be visibly set apart, got %q", text)
	}
	// and it must not be confusable with the retained conversation
	for _, m := range got[1:] {
		if m.Blocks[0].Text == text {
			t.Fatal("marker text collides with retained content")
		}
	}
}

// --- T-1012: Run compacts only after a durable write ---

// TestRunCompactsAfterSuccessfulWrite is the spec scenario.
func TestRunCompactsAfterSuccessfulWrite(t *testing.T) {
	s := fakeSession(t, 1) // window 1: always crosses the mark
	s.KeepRecent = 2

	prior := msgs(8)
	got, err := s.Run(context.Background(), "trigger compaction", prior)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(checkpointPath(s)); err != nil {
		t.Fatalf("expected a checkpoint: %v", err)
	}
	// the turn produced more than prior+1 messages; compaction must shrink it
	if len(got) > s.KeepRecent+1 {
		t.Fatalf("history not compacted: %d messages", len(got))
	}
	if !strings.Contains(got[0].Blocks[0].Text, "checkpoint.md") {
		t.Errorf("compacted history missing its marker: %+v", got[0])
	}
}

// TestRunBelowMarkDoesNotCompact is the spec scenario.
func TestRunBelowMarkDoesNotCompact(t *testing.T) {
	s := fakeSession(t, 1_000_000) // enormous window
	s.KeepRecent = 2

	prior := msgs(8)
	got, err := s.Run(context.Background(), "no compaction here", prior)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) <= s.KeepRecent+1 {
		t.Fatalf("history looks compacted below the mark: %d messages", len(got))
	}
	for _, m := range got {
		if strings.Contains(m.Blocks[0].Text, "compacted") {
			t.Fatal("a compaction marker appeared below the high-water mark")
		}
	}
}

// TestRunCompactionUpdatesSessionHistory pins that /dream sees what the caller
// sees: the session's own record is compacted too, not left stale and huge.
func TestRunCompactionUpdatesSessionHistory(t *testing.T) {
	s := fakeSession(t, 1)
	s.KeepRecent = 2

	got, err := s.Run(context.Background(), "compact me", msgs(8))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.History) != len(got) {
		t.Fatalf("Session.History (%d) diverged from the returned history (%d)",
			len(s.History), len(got))
	}
}

// --- T-1013: reporting ---

func TestRunReportsCompaction(t *testing.T) {
	s := fakeSession(t, 1)
	s.KeepRecent = 2

	var before, after int
	s.OnCompact = func(b, a int) { before, after = b, a }

	if _, err := s.Run(context.Background(), "report it", msgs(8)); err != nil {
		t.Fatal(err)
	}
	if before == 0 || after == 0 {
		t.Fatalf("OnCompact not called: before=%d after=%d", before, after)
	}
	if after >= before {
		t.Errorf("reported shrink is not a shrink: %d -> %d", before, after)
	}
}

func TestRunCompactionWithoutHookDoesNotPanic(t *testing.T) {
	s := fakeSession(t, 1)
	s.KeepRecent = 2
	// OnCompact deliberately nil
	if _, err := s.Run(context.Background(), "no hook", msgs(8)); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// --- T-1020: mid-turn safety ---

// TestJudgeLoopSeesFullHistory is the spec scenario: compaction happens after the
// judge loop exits, never between its rounds.
func TestJudgeLoopSeesFullHistory(t *testing.T) {
	s := fakeSession(t, 1) // would compact at the end
	s.KeepRecent = 2

	// A provider that records how many messages it was handed each round.
	rec := &historySizeRecorder{}
	s.Provider = rec
	s.Goal = "keep going"
	s.Judge = unmetThenMetJudge(t)
	s.MaxJudgeIterations = 3

	if _, err := s.Run(context.Background(), "loop please", msgs(8)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.sizes) < 2 {
		t.Fatalf("expected at least two rounds, got sizes %v", rec.sizes)
	}
	// the second round must not have been handed a compacted history
	if rec.sizes[1] < rec.sizes[0] {
		t.Errorf("history shrank between judge rounds: %v", rec.sizes)
	}
	if rec.sizes[0] <= s.KeepRecent+1 {
		t.Errorf("first round already looked compacted: %v", rec.sizes)
	}
}

// unmetThenMetJudge returns UNMET once, then MET, so the loop runs twice.
func unmetThenMetJudge(t *testing.T) *orchestrate.Judge {
	t.Helper()
	return &orchestrate.Judge{
		Provider: &portsfake.Fake{Scripts: [][]ports.Event{
			{{Kind: ports.EventTextDelta, Text: "UNMET: keep working"}, {Kind: ports.EventTurnEnd}},
			{{Kind: ports.EventTextDelta, Text: "MET: done now"}, {Kind: ports.EventTurnEnd}},
		}},
		Model: "fake/judge",
	}
}

// historySizeRecorder records the message count of each request it receives.
type historySizeRecorder struct{ sizes []int }

func (h *historySizeRecorder) Stream(_ context.Context, req ports.Request) (<-chan ports.Event, error) {
	h.sizes = append(h.sizes, len(req.Messages))
	ch := make(chan ports.Event, 2)
	ch <- ports.Event{Kind: ports.EventTextDelta, Text: "ok"}
	ch <- ports.Event{Kind: ports.EventTurnEnd}
	close(ch)
	return ch, nil
}
