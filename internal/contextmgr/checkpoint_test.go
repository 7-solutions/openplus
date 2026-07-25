package contextmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/7-solutions/openplus/internal/ports"
)

func TestCheckpointerShouldCheckpointHighWaterMark(t *testing.T) {
	c := Checkpointer{Root: t.TempDir(), Window: 1000, HighWater: 0.8}
	if c.ShouldCheckpoint(700) {
		t.Error("700/1000 is below the 0.8 mark — should not checkpoint")
	}
	if !c.ShouldCheckpoint(800) {
		t.Error("800/1000 reaches the 0.8 mark — should checkpoint")
	}
	if !c.ShouldCheckpoint(950) {
		t.Error("950/1000 exceeds the mark — should checkpoint")
	}
}

func TestCheckpointerDefaultsHighWater(t *testing.T) {
	c := Checkpointer{Root: t.TempDir(), Window: 1000} // HighWater unset
	if !c.ShouldCheckpoint(900) {
		t.Error("expected a sane default high-water mark below 0.9")
	}
	if c.ShouldCheckpoint(100) {
		t.Error("100/1000 should never trip the mark")
	}
}

func TestCheckpointerNoWindowNeverCheckpoints(t *testing.T) {
	c := Checkpointer{Root: t.TempDir()} // Window 0 = unknown
	if c.ShouldCheckpoint(1_000_000) {
		t.Error("with no window configured, never checkpoint")
	}
}

func TestCheckpointerWriteCreatesFile(t *testing.T) {
	root := t.TempDir()
	c := Checkpointer{Root: root, Window: 1000}

	var tree TaskTree
	tree.Add("T1", "the active task", StatusInProgress)

	cp := Checkpoint{
		Summary: "did the first half of the work",
		Tasks:   tree,
		Recent:  []ports.Message{userMsg("last thing said")},
	}
	if err := c.Write(cp); err != nil {
		t.Fatalf("Write: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(root, "checkpoint.md"))
	if err != nil {
		t.Fatalf("read checkpoint.md: %v", err)
	}
	s := string(body)
	for _, want := range []string{"did the first half", "T1", "the active task", "last thing said"} {
		if !strings.Contains(s, want) {
			t.Errorf("checkpoint.md missing %q:\n%s", want, s)
		}
	}
}

// TestCheckpointerRoundTrip is the T-062/T-063 requirement: a written
// checkpoint reconstructs, and the task tree survives the boundary intact.
func TestCheckpointerRoundTrip(t *testing.T) {
	root := t.TempDir()
	c := Checkpointer{Root: root, Window: 1000}

	var tree TaskTree
	tree.Add("T1", "parent work", StatusInProgress)
	tree.Add("T1.1", "child work", StatusDone)

	orig := Checkpoint{
		Summary: "reconstruction should restore this summary",
		Tasks:   tree,
		Recent:  []ports.Message{userMsg("keep me")},
	}
	if err := c.Write(orig); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := c.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(got.Summary, "reconstruction should restore this summary") {
		t.Errorf("summary = %q", got.Summary)
	}
	if len(got.Tasks.Nodes) != 2 {
		t.Fatalf("restored %d task nodes, want 2: %+v", len(got.Tasks.Nodes), got.Tasks.Nodes)
	}
	if got.Tasks.Nodes[0].ID != "T1" || got.Tasks.Nodes[0].Status != StatusInProgress {
		t.Errorf("node[0] = %+v", got.Tasks.Nodes[0])
	}
	if got.Tasks.Nodes[1].ID != "T1.1" || got.Tasks.Nodes[1].Status != StatusDone {
		t.Errorf("node[1] = %+v", got.Tasks.Nodes[1])
	}
	// the active task must still be recoverable after reconstruction
	active, ok := got.Tasks.Active()
	if !ok || active.ID != "T1" {
		t.Errorf("active after restore = %+v ok=%v, want T1", active, ok)
	}
}

func TestCheckpointerReadMissingIsEmpty(t *testing.T) {
	c := Checkpointer{Root: t.TempDir(), Window: 1000}
	got, err := c.Read()
	if err != nil {
		t.Fatalf("Read on missing checkpoint should not error: %v", err)
	}
	if got.Summary != "" || len(got.Tasks.Nodes) != 0 {
		t.Fatalf("expected empty checkpoint, got %+v", got)
	}
}

func TestCheckpointerWriteOverwrites(t *testing.T) {
	root := t.TempDir()
	c := Checkpointer{Root: root, Window: 1000}
	if err := c.Write(Checkpoint{Summary: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(Checkpoint{Summary: "second"}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Read()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Summary, "first") {
		t.Errorf("stale summary survived overwrite: %q", got.Summary)
	}
	if !strings.Contains(got.Summary, "second") {
		t.Errorf("summary = %q, want second", got.Summary)
	}
}

// TestCheckpointerReconstructFeedsBudgeter proves the checkpoint plugs into the
// ADR-0008 injection path: its summary and task tree become Budgeter Input.
func TestCheckpointerReconstructFeedsBudgeter(t *testing.T) {
	root := t.TempDir()
	c := Checkpointer{Root: root, Window: 1000}
	var tree TaskTree
	tree.Add("T1", "active work", StatusInProgress)
	if err := c.Write(Checkpoint{Summary: "prior context", Tasks: tree}); err != nil {
		t.Fatal(err)
	}

	in, err := c.Reconstruct("system prompt", []string{"a memory"}, nil)
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	if in.System != "system prompt" {
		t.Errorf("system = %q", in.System)
	}
	if !strings.Contains(in.Checkpoint, "prior context") {
		t.Errorf("checkpoint section = %q", in.Checkpoint)
	}
	if !strings.Contains(in.Task, "active work") {
		t.Errorf("task section = %q, want the active task", in.Task)
	}
	if len(in.Memory) != 1 {
		t.Errorf("memory = %v", in.Memory)
	}

	// and the assembled Input budgets cleanly
	out := Budgeter{Tokenizer: Heuristic{}, Budget: 100_000}.Fit(in)
	if out.System == "" || out.Checkpoint == "" {
		t.Errorf("budgeted output lost sections: %+v", out)
	}
}
