package contextmgr

import (
	"strings"
	"testing"
)

func TestTaskTreeAddAndRender(t *testing.T) {
	var tree TaskTree
	tree.Add("T1", "build the thing", StatusInProgress)
	tree.Add("T1.1", "write the test", StatusDone)
	tree.Add("T1.2", "make it green", StatusOpen)

	out := tree.Render()
	for _, want := range []string{"T1", "T1.1", "T1.2", "build the thing", "write the test"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	// nesting: subtasks are indented under their parent
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), out)
	}
	if strings.HasPrefix(lines[0], " ") {
		t.Errorf("root task should not be indented: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Errorf("subtask should be indented: %q", lines[1])
	}
}

func TestTaskTreeStatusMarkers(t *testing.T) {
	var tree TaskTree
	tree.Add("T1", "done task", StatusDone)
	tree.Add("T2", "active task", StatusInProgress)
	tree.Add("T3", "open task", StatusOpen)
	out := tree.Render()
	if !strings.Contains(out, "[x]") {
		t.Errorf("missing done marker:\n%s", out)
	}
	if !strings.Contains(out, "[~]") {
		t.Errorf("missing in-progress marker:\n%s", out)
	}
	if !strings.Contains(out, "[ ]") {
		t.Errorf("missing open marker:\n%s", out)
	}
}

func TestTaskTreeActiveReturnsInProgress(t *testing.T) {
	var tree TaskTree
	tree.Add("T1", "first", StatusDone)
	tree.Add("T2", "second", StatusInProgress)
	got, ok := tree.Active()
	if !ok {
		t.Fatal("expected an active task")
	}
	if got.ID != "T2" {
		t.Fatalf("active = %q, want T2", got.ID)
	}
}

func TestTaskTreeActiveNoneWhenAllDone(t *testing.T) {
	var tree TaskTree
	tree.Add("T1", "first", StatusDone)
	if _, ok := tree.Active(); ok {
		t.Fatal("expected no active task")
	}
}

func TestTaskTreeSetStatus(t *testing.T) {
	var tree TaskTree
	tree.Add("T1", "task", StatusOpen)
	if !tree.SetStatus("T1", StatusDone) {
		t.Fatal("SetStatus should report success")
	}
	if tree.Nodes[0].Status != StatusDone {
		t.Fatalf("status = %v, want done", tree.Nodes[0].Status)
	}
	if tree.SetStatus("absent", StatusDone) {
		t.Error("SetStatus on missing id should report false")
	}
}

// TestTaskTreeRoundTrip is the T-063 requirement: the tree survives a
// serialize/parse cycle (which is how it crosses a checkpoint boundary).
func TestTaskTreeRoundTrip(t *testing.T) {
	var tree TaskTree
	tree.Add("T1", "parent task", StatusInProgress)
	tree.Add("T1.1", "child task", StatusDone)
	tree.Add("T2", "sibling task", StatusOpen)

	restored, err := ParseTaskTree(tree.Render())
	if err != nil {
		t.Fatalf("ParseTaskTree: %v", err)
	}
	if len(restored.Nodes) != len(tree.Nodes) {
		t.Fatalf("restored %d nodes, want %d", len(restored.Nodes), len(tree.Nodes))
	}
	for i, want := range tree.Nodes {
		got := restored.Nodes[i]
		if got.ID != want.ID || got.Title != want.Title || got.Status != want.Status {
			t.Errorf("node[%d] = %+v, want %+v", i, got, want)
		}
	}
}

func TestParseTaskTreeEmpty(t *testing.T) {
	got, err := ParseTaskTree("")
	if err != nil {
		t.Fatalf("ParseTaskTree(\"\"): %v", err)
	}
	if len(got.Nodes) != 0 {
		t.Fatalf("expected no nodes, got %+v", got.Nodes)
	}
}

func TestParseTaskTreeIgnoresJunkLines(t *testing.T) {
	in := "# heading\n- [x] T1 real task\nnot a task line\n\n  - [ ] T1.1 another\n"
	got, err := ParseTaskTree(in)
	if err != nil {
		t.Fatalf("ParseTaskTree: %v", err)
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("nodes = %+v, want 2", got.Nodes)
	}
	if got.Nodes[0].ID != "T1" || got.Nodes[1].ID != "T1.1" {
		t.Fatalf("ids = %q,%q", got.Nodes[0].ID, got.Nodes[1].ID)
	}
}

func TestTaskTreeDepthFromID(t *testing.T) {
	cases := map[string]int{"T1": 0, "T1.1": 1, "T1.2.3": 2}
	for id, want := range cases {
		if got := depthOf(id); got != want {
			t.Errorf("depthOf(%q) = %d, want %d", id, got, want)
		}
	}
}

func TestTaskTreeAddReplacesExistingID(t *testing.T) {
	var tree TaskTree
	tree.Add("T1", "original", StatusOpen)
	tree.Add("T1", "updated", StatusDone)
	if len(tree.Nodes) != 1 {
		t.Fatalf("nodes = %+v, want 1 (Add must upsert)", tree.Nodes)
	}
	if tree.Nodes[0].Title != "updated" || tree.Nodes[0].Status != StatusDone {
		t.Fatalf("node = %+v, want updated/done", tree.Nodes[0])
	}
}
