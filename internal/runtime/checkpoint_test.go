package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/contextmgr"
	"github.com/7solutions/openplus/internal/provider"
)

// windowConfig is a project whose context window is small enough that a short
// transcript crosses the high-water mark, so tests can force a checkpoint
// without generating a huge history.
func windowConfig(window int) string {
	return `{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}},
  "context": {"budget": 100000, "window": ` + itoa(window) + `}
}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// --- T-800: Checkpointer assembled from config ---

func TestAssembleBuildsCheckpointerFromWindow(t *testing.T) {
	s, err := Assemble(project(t, windowConfig(200000)), Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if s.Checkpointer == nil {
		t.Fatal("a configured window should produce a Checkpointer")
	}
	if s.Checkpointer.Window != 200000 {
		t.Errorf("Window = %d, want 200000", s.Checkpointer.Window)
	}
	if s.Checkpointer.Root != s.Root {
		t.Errorf("Root = %q, want the project root %q", s.Checkpointer.Root, s.Root)
	}
}

// TestAssembleNoWindowDisablesCheckpointing pins the off switch: without a
// configured window the feature is absent end to end, not merely inert.
func TestAssembleNoWindowDisablesCheckpointing(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if s.Checkpointer != nil {
		t.Fatal("no window configured, so there should be no Checkpointer")
	}
}

// --- T-801: task tree restored at assembly ---

func TestAssembleRestoresTaskTreeFromCheckpoint(t *testing.T) {
	root := project(t, windowConfig(200000))

	// hand-write a checkpoint as a prior session would have left it
	var tree contextmgr.TaskTree
	tree.Add("T1", "parent work", contextmgr.StatusInProgress)
	tree.Add("T1.1", "child work", contextmgr.StatusDone)
	pre := contextmgr.Checkpointer{Root: root, Window: 200000}
	if err := pre.Write(contextmgr.Checkpoint{
		Summary: "earlier session did the first half",
		Tasks:   tree,
	}); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(s.Tasks.Nodes) != 2 {
		t.Fatalf("restored %d task nodes, want 2: %+v", len(s.Tasks.Nodes), s.Tasks.Nodes)
	}
	active, ok := s.Tasks.Active()
	if !ok || active.ID != "T1" {
		t.Fatalf("active task after restore = %+v ok=%v, want T1", active, ok)
	}
	if s.Tasks.Nodes[1].Status != contextmgr.StatusDone {
		t.Errorf("child status = %v, want done", s.Tasks.Nodes[1].Status)
	}
}

func TestAssembleNoCheckpointLeavesEmptyTree(t *testing.T) {
	s, err := Assemble(project(t, windowConfig(200000)), Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(s.Tasks.Nodes) != 0 {
		t.Fatalf("expected an empty task tree, got %+v", s.Tasks.Nodes)
	}
}

// TestAssembleCorruptCheckpointDoesNotFail is the spec scenario: a malformed
// checkpoint degrades to "no checkpoint" rather than breaking startup.
func TestAssembleCorruptCheckpointDoesNotFail(t *testing.T) {
	root := project(t, windowConfig(200000))
	// truncated mid-section: a header with no closing content
	if err := os.WriteFile(filepath.Join(root, "checkpoint.md"),
		[]byte("# Checkpoint\n\n## Summary\nhalf a summ"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatalf("a corrupt checkpoint must not fail assembly: %v", err)
	}
	if s.Checkpointer == nil {
		t.Error("Checkpointer should still be built")
	}
}

// --- T-810/T-811: reconstruction into assembled context ---

// TestAssembleContextInjectsCheckpointSummary is the spec scenario: the summary
// reaches the assembled prompt.
func TestAssembleContextInjectsCheckpointSummary(t *testing.T) {
	root := project(t, windowConfig(200000))
	pre := contextmgr.Checkpointer{Root: root, Window: 200000}
	if err := pre.Write(contextmgr.Checkpoint{
		Summary: "SUMMARY-MARKER earlier work happened here",
	}); err != nil {
		t.Fatal(err)
	}

	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.AssembleContext(context.Background(), "continue please", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if !strings.Contains(turn.System, "SUMMARY-MARKER") {
		t.Errorf("checkpoint summary missing from the assembled prompt:\n%s", turn.System)
	}
}

// TestAssembleContextInjectsActiveTask is the spec scenario for the restored
// active task.
func TestAssembleContextInjectsActiveTask(t *testing.T) {
	root := project(t, windowConfig(200000))
	var tree contextmgr.TaskTree
	tree.Add("T7", "ACTIVE-TASK-MARKER", contextmgr.StatusInProgress)
	pre := contextmgr.Checkpointer{Root: root, Window: 200000}
	if err := pre.Write(contextmgr.Checkpoint{Summary: "s", Tasks: tree}); err != nil {
		t.Fatal(err)
	}

	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.AssembleContext(context.Background(), "carry on", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if !strings.Contains(turn.System, "ACTIVE-TASK-MARKER") {
		t.Errorf("active task missing from the assembled prompt:\n%s", turn.System)
	}
}

// TestAssembleContextLiveHistoryOutranksDigest is the spec scenario: live
// messages beat the checkpoint's digest of earlier ones.
func TestAssembleContextLiveHistoryOutranksDigest(t *testing.T) {
	root := project(t, windowConfig(200000))
	pre := contextmgr.Checkpointer{Root: root, Window: 200000}
	if err := pre.Write(contextmgr.Checkpoint{
		Summary: "s",
		Recent: []provider.Message{{
			Role:   provider.RoleUser,
			Blocks: []provider.Block{{Kind: provider.BlockText, Text: "STALE-DIGEST-LINE"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	live := []provider.Message{{
		Role:   provider.RoleUser,
		Blocks: []provider.Block{{Kind: provider.BlockText, Text: "LIVE-LINE"}},
	}}
	turn, err := s.AssembleContext(context.Background(), "next", live)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}

	var joined strings.Builder
	for _, m := range turn.History {
		for _, b := range m.Blocks {
			joined.WriteString(b.Text)
			joined.WriteByte(' ')
		}
	}
	if !strings.Contains(joined.String(), "LIVE-LINE") {
		t.Errorf("live history missing: %q", joined.String())
	}
	if strings.Contains(joined.String(), "STALE-DIGEST-LINE") {
		t.Errorf("checkpoint digest overrode live history: %q", joined.String())
	}
}

func TestAssembleContextWithoutCheckpointUnchanged(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true, BaseSystemPrompt: "BASE"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.AssembleContext(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if !strings.Contains(turn.System, "BASE") || !strings.Contains(turn.System, "cgo-free") {
		t.Errorf("no-checkpoint path changed shape:\n%s", turn.System)
	}
}

// TestAssembleContextReportsUsage pins that the measured usage the checkpoint
// decision depends on is actually surfaced.
func TestAssembleContextReportsUsage(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.AssembleContext(context.Background(), "measure me", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if turn.Used <= 0 {
		t.Fatalf("Used = %d, want > 0", turn.Used)
	}
}
