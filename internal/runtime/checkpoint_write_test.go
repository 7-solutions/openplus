package runtime

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/contextmgr"
	"github.com/7solutions/openplus/internal/provider"
)

// fakeSession assembles a session on the fake provider with a given window, so
// Run completes offline while the high-water logic is exercised for real.
func fakeSession(t *testing.T, window int) *Session {
	t.Helper()
	s, err := Assemble(project(t, windowConfig(window)), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if window > 0 && s.Checkpointer == nil {
		t.Fatal("expected a Checkpointer")
	}
	return s
}

func checkpointPath(s *Session) string {
	return filepath.Join(s.Root, "checkpoint.md")
}

// --- T-820: write only on crossing ---

// TestRunBelowMarkWritesNothing is the spec scenario: a small turn under a large
// window must not checkpoint.
func TestRunBelowMarkWritesNothing(t *testing.T) {
	s := fakeSession(t, 1_000_000) // enormous window, tiny turn
	if _, err := s.Run(context.Background(), "hi", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(checkpointPath(s)); !os.IsNotExist(err) {
		t.Fatalf("checkpoint written below the high-water mark (err=%v)", err)
	}
}

// TestRunCrossingMarkWritesCheckpoint is the spec scenario: crossing the mark
// writes checkpoint.md.
func TestRunCrossingMarkWritesCheckpoint(t *testing.T) {
	// A window of 1 token means any real turn is over the 0.8 mark.
	s := fakeSession(t, 1)
	if _, err := s.Run(context.Background(), "trigger a checkpoint", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	body, err := os.ReadFile(checkpointPath(s))
	if err != nil {
		t.Fatalf("expected a checkpoint: %v", err)
	}
	if !strings.Contains(string(body), "trigger a checkpoint") {
		t.Errorf("checkpoint missing the turn's content:\n%s", body)
	}
}

// TestRunNoWindowNeverCheckpoints is the spec scenario for the off switch.
func TestRunNoWindowNeverCheckpoints(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(context.Background(), "no window here", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(checkpointPath(s)); !os.IsNotExist(err) {
		t.Fatal("checkpoint written with no window configured")
	}
}

// TestRunCheckpointWriteFailureIsSurfaced is the spec scenario: a write failure
// must reach the operator, because it means the session is no longer durable.
func TestRunCheckpointWriteFailureIsSurfaced(t *testing.T) {
	s := fakeSession(t, 1)
	// Make the checkpoint path unwritable by planting a directory where the file
	// belongs — os.WriteFile then fails regardless of privileges.
	if err := os.Mkdir(checkpointPath(s), 0o755); err != nil {
		t.Fatal(err)
	}

	var reported error
	s.OnCheckpointError = func(err error) { reported = err }

	if _, err := s.Run(context.Background(), "durability matters", nil); err != nil {
		t.Fatalf("a checkpoint failure must not fail the turn: %v", err)
	}
	if reported == nil {
		t.Fatal("checkpoint write failure was swallowed")
	}
}

// TestRunCheckpointFailureWithoutHookDoesNotPanic guards the default path: no
// hook wired means the failure is dropped, not a nil-call crash.
func TestRunCheckpointFailureWithoutHookDoesNotPanic(t *testing.T) {
	s := fakeSession(t, 1)
	if err := os.Mkdir(checkpointPath(s), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(context.Background(), "no hook wired", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// --- T-821: checkpoint carries the task tree ---

func TestRunCheckpointCarriesTaskTree(t *testing.T) {
	s := fakeSession(t, 1)
	s.Tasks.Add("T9", "the live task", contextmgr.StatusInProgress)

	if _, err := s.Run(context.Background(), "write the tree", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	cp, err := s.Checkpointer.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	active, ok := cp.Tasks.Active()
	if !ok || active.ID != "T9" {
		t.Fatalf("task tree not carried: %+v", cp.Tasks.Nodes)
	}
	if active.Title != "the live task" {
		t.Errorf("title = %q", active.Title)
	}
}

// TestRunCheckpointReflectsUpdatedStatus is the spec scenario: a status change
// after one checkpoint shows up in the next.
func TestRunCheckpointReflectsUpdatedStatus(t *testing.T) {
	s := fakeSession(t, 1)
	s.Tasks.Add("T1", "work", contextmgr.StatusInProgress)
	if _, err := s.Run(context.Background(), "first", nil); err != nil {
		t.Fatal(err)
	}

	if !s.Tasks.SetStatus("T1", contextmgr.StatusDone) {
		t.Fatal("SetStatus should find T1")
	}
	if _, err := s.Run(context.Background(), "second", nil); err != nil {
		t.Fatal(err)
	}

	cp, err := s.Checkpointer.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(cp.Tasks.Nodes) != 1 || cp.Tasks.Nodes[0].Status != contextmgr.StatusDone {
		t.Fatalf("later checkpoint did not reflect the new status: %+v", cp.Tasks.Nodes)
	}
}

// --- T-822: writing never mutates live context ---

// TestRunHistoryIdenticalWithAndWithoutCheckpoint is the safety property from
// the proposal: the write is observationally pure with respect to live context.
func TestRunHistoryIdenticalWithAndWithoutCheckpoint(t *testing.T) {
	const msg = "identical either way"

	// window 1: definitely checkpoints
	withCP := fakeSession(t, 1)
	gotWith, err := withCP.Run(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("Run (checkpointing): %v", err)
	}
	if _, err := os.Stat(checkpointPath(withCP)); err != nil {
		t.Fatalf("expected a checkpoint to have been written: %v", err)
	}

	// no window: definitely does not
	without, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	gotWithout, err := without.Run(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("Run (no checkpoint): %v", err)
	}

	if !reflect.DeepEqual(gotWith, gotWithout) {
		t.Fatalf("checkpointing changed the returned history:\nwith:    %+v\nwithout: %+v",
			gotWith, gotWithout)
	}
}

// --- T-823: summary is verbatim, capped, visibly truncated ---

func TestCheckpointSummaryVerbatimWhenShort(t *testing.T) {
	history := []provider.Message{
		userMessage("first thing said"),
		{Role: provider.RoleAssistant, Blocks: []provider.Block{
			{Kind: provider.BlockText, Text: "second thing said"},
		}},
	}
	got := buildSummary(history)
	for _, want := range []string{"first thing said", "second thing said"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary dropped %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "earlier message") {
		t.Errorf("short transcript should not report truncation:\n%s", got)
	}
}

func TestCheckpointSummaryTruncatesVisiblyAtBoundary(t *testing.T) {
	// Each message is large enough that only the last few fit under the cap.
	big := strings.Repeat("x", SummaryCap/3)
	var history []provider.Message
	for i, tag := range []string{"OLDEST", "MIDDLE", "NEWEST"} {
		_ = i
		history = append(history, userMessage(tag+" "+big))
	}
	// one more to guarantee the cap is exceeded
	history = append(history, userMessage("FINAL "+big))

	got := buildSummary(history)
	if len(got) > SummaryCap+200 { // marker adds a little
		t.Errorf("summary length %d exceeds the cap %d by more than the marker", len(got), SummaryCap)
	}
	// most recent material is retained
	if !strings.Contains(got, "FINAL") {
		t.Errorf("summary dropped the most recent message:\n%s", got[:min(len(got), 300)])
	}
	// the loss is visible, not silent
	if !strings.Contains(got, "earlier message") {
		t.Errorf("truncation was silent; expected a visible marker:\n%s", got[:min(len(got), 300)])
	}
	// truncation happens at a message boundary: no half-message tail
	if strings.Contains(got, "OLDEST") {
		t.Errorf("oldest message should have been dropped whole:\n%s", got[:min(len(got), 300)])
	}
}

func TestCheckpointSummaryEmptyHistory(t *testing.T) {
	if got := buildSummary(nil); got != "" {
		t.Fatalf("buildSummary(nil) = %q, want empty", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
