package contextmgr

import (
	"fmt"
	"strings"
)

// Status is a task node's state.
type Status int

const (
	StatusOpen Status = iota
	StatusInProgress
	StatusDone
)

// marker returns the checkbox marker used in the rendered tree (and matched
// when parsing it back).
func (s Status) marker() string {
	switch s {
	case StatusDone:
		return "[x]"
	case StatusInProgress:
		return "[~]"
	default:
		return "[ ]"
	}
}

func statusFromMarker(m string) Status {
	switch m {
	case "[x]":
		return StatusDone
	case "[~]":
		return StatusInProgress
	default:
		return StatusOpen
	}
}

// TaskNode is one task in the tree. Nesting is encoded in the dotted ID
// ("T1" is a root, "T1.1" is its child) so the tree is flat in memory but
// renders and reparses as a hierarchy (T-063).
type TaskNode struct {
	ID     string
	Title  string
	Status Status
}

// TaskTree is the ordered task list that survives checkpoint boundaries.
type TaskTree struct {
	Nodes []TaskNode
}

// Add appends a task, or updates it in place when the ID already exists.
func (t *TaskTree) Add(id, title string, status Status) {
	for i := range t.Nodes {
		if t.Nodes[i].ID == id {
			t.Nodes[i].Title = title
			t.Nodes[i].Status = status
			return
		}
	}
	t.Nodes = append(t.Nodes, TaskNode{ID: id, Title: title, Status: status})
}

// SetStatus updates one task's status, reporting whether the ID was found.
func (t *TaskTree) SetStatus(id string, status Status) bool {
	for i := range t.Nodes {
		if t.Nodes[i].ID == id {
			t.Nodes[i].Status = status
			return true
		}
	}
	return false
}

// Active returns the first in-progress task — the one the Budgeter treats as
// highest-priority working state (ADR-0008).
func (t TaskTree) Active() (TaskNode, bool) {
	for _, n := range t.Nodes {
		if n.Status == StatusInProgress {
			return n, true
		}
	}
	return TaskNode{}, false
}

// Render writes the tree as indented markdown checkboxes — human-readable in
// checkpoint.md and machine-parseable by ParseTaskTree.
func (t TaskTree) Render() string {
	var b strings.Builder
	for _, n := range t.Nodes {
		indent := strings.Repeat("  ", depthOf(n.ID))
		fmt.Fprintf(&b, "%s- %s %s %s\n", indent, n.Status.marker(), n.ID, n.Title)
	}
	return b.String()
}

// ParseTaskTree restores a tree from Render's output. Lines that do not match
// the task shape are ignored, so the tree can be embedded in a larger document.
func ParseTaskTree(s string) (TaskTree, error) {
	var t TaskTree
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		if len(rest) < 3 {
			continue
		}
		marker := rest[:3]
		if marker != "[x]" && marker != "[~]" && marker != "[ ]" {
			continue
		}
		rest = strings.TrimSpace(rest[3:])
		id, title, _ := strings.Cut(rest, " ")
		if id == "" {
			continue
		}
		t.Nodes = append(t.Nodes, TaskNode{
			ID:     id,
			Title:  strings.TrimSpace(title),
			Status: statusFromMarker(marker),
		})
	}
	return t, nil
}

// depthOf derives nesting depth from a dotted task ID: "T1" is depth 0,
// "T1.1" depth 1, "T1.2.3" depth 2.
func depthOf(id string) int {
	return strings.Count(id, ".")
}
