package contextmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/7solutions/openplus/internal/provider"
)

// DefaultHighWater is the fraction of the context window at which a checkpoint
// is taken when Checkpointer.HighWater is unset. Leaves headroom for the turn
// that writes the checkpoint itself.
const DefaultHighWater = 0.8

// checkpointFile is the on-disk checkpoint name (ADR-0008).
const checkpointFile = "checkpoint.md"

// Section headers in checkpoint.md. They double as parse anchors, so changing
// one breaks reading older checkpoints.
const (
	summaryHeader = "## Summary"
	tasksHeader   = "## Tasks"
	recentHeader  = "## Recent"
)

// Checkpoint is a structured snapshot of a session's working state.
type Checkpoint struct {
	// Summary is the condensed narrative of what happened before the cut.
	Summary string
	// Tasks is the task tree, which must survive the boundary intact (T-063).
	Tasks TaskTree
	// Recent holds the messages retained verbatim across the cut.
	Recent []provider.Message
}

// Checkpointer writes and reads checkpoint.md and decides when the live context
// has grown enough to warrant a cut (ADR-0008).
type Checkpointer struct {
	// Root is the directory holding checkpoint.md.
	Root string
	// Window is the model's context window in tokens. Zero means unknown, in
	// which case checkpointing never triggers automatically.
	Window int
	// HighWater is the fraction of Window that triggers a checkpoint. Zero uses
	// DefaultHighWater.
	HighWater float64
}

// ShouldCheckpoint reports whether used tokens have reached the high-water mark.
func (c Checkpointer) ShouldCheckpoint(used int) bool {
	if c.Window <= 0 {
		return false
	}
	mark := c.HighWater
	if mark <= 0 {
		mark = DefaultHighWater
	}
	return float64(used) >= float64(c.Window)*mark
}

// Write persists cp to <Root>/checkpoint.md, replacing any previous checkpoint.
func (c Checkpointer) Write(cp Checkpoint) error {
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		return fmt.Errorf("contextmgr: mkdir: %w", err)
	}

	var b strings.Builder
	b.WriteString("# Checkpoint\n\n")
	b.WriteString(summaryHeader + "\n")
	b.WriteString(strings.TrimSpace(cp.Summary))
	b.WriteString("\n\n")
	b.WriteString(tasksHeader + "\n")
	b.WriteString(cp.Tasks.Render())
	b.WriteString("\n")
	b.WriteString(recentHeader + "\n")
	for _, m := range cp.Recent {
		fmt.Fprintf(&b, "- %s: %s\n", m.Role, flattenBlocks(m.Blocks))
	}

	path := filepath.Join(c.Root, checkpointFile)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("contextmgr: write checkpoint: %w", err)
	}
	return nil
}

// Read loads the checkpoint. A missing file yields a zero Checkpoint and no
// error — the first session has nothing to reconstruct.
func (c Checkpointer) Read() (Checkpoint, error) {
	raw, err := os.ReadFile(filepath.Join(c.Root, checkpointFile))
	if err != nil {
		if os.IsNotExist(err) {
			return Checkpoint{}, nil
		}
		return Checkpoint{}, fmt.Errorf("contextmgr: read checkpoint: %w", err)
	}

	body := string(raw)
	var cp Checkpoint
	cp.Summary = strings.TrimSpace(section(body, summaryHeader, tasksHeader))

	tasks, err := ParseTaskTree(section(body, tasksHeader, recentHeader))
	if err != nil {
		return Checkpoint{}, err
	}
	cp.Tasks = tasks

	// Recent messages are stored as a human-readable digest; they reconstruct as
	// user-role text so the model still sees what was said, without pretending
	// to restore exact block structure.
	for _, line := range strings.Split(section(body, recentHeader, ""), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if text == "" {
			continue
		}
		cp.Recent = append(cp.Recent, provider.Message{
			Role:   provider.RoleUser,
			Blocks: []provider.Block{{Kind: provider.BlockText, Text: text}},
		})
	}
	return cp, nil
}

// Reconstruct reads the checkpoint and assembles a Budgeter Input in ADR-0008
// injection order: the caller's system prompt, the checkpoint's active task,
// the checkpoint summary, retrieved memory, and retained recent messages.
// recent overrides the checkpoint's digest when non-nil (live messages are
// better than a digest of them).
func (c Checkpointer) Reconstruct(system string, memory []string, recent []provider.Message) (Input, error) {
	cp, err := c.Read()
	if err != nil {
		return Input{}, err
	}

	in := Input{
		System:     system,
		Checkpoint: cp.Summary,
		Memory:     memory,
	}
	if active, ok := cp.Tasks.Active(); ok {
		in.Task = fmt.Sprintf("%s %s", active.ID, active.Title)
		in.Progress = cp.Tasks.Render()
	}
	if recent != nil {
		in.Recent = recent
	} else {
		in.Recent = cp.Recent
	}
	return in, nil
}

// section extracts the text between header and the next header (or end of
// document when next is empty).
func section(body, header, next string) string {
	start := strings.Index(body, header)
	if start < 0 {
		return ""
	}
	start += len(header)
	if next == "" {
		return body[start:]
	}
	end := strings.Index(body[start:], next)
	if end < 0 {
		return body[start:]
	}
	return body[start : start+end]
}

// flattenBlocks renders a message's blocks as a single line for the checkpoint
// digest.
func flattenBlocks(blocks []provider.Block) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch b.Kind {
		case provider.BlockText, provider.BlockThinking:
			parts = append(parts, b.Text)
		case provider.BlockToolCall:
			parts = append(parts, fmt.Sprintf("%s(%s)", b.ToolName, b.ToolInput))
		case provider.BlockToolResult:
			parts = append(parts, b.ToolResultText)
		}
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}
