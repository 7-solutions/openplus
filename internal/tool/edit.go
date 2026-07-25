package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/7solutions/openplus/internal/diff"
)

// Edit performs an exact string replacement in a file. old_string must occur
// exactly once — this guards against ambiguous edits. Returns the region around
// the replacement so the model can verify.
type Edit struct{}

func (Edit) Name() string { return "edit" }

func (Edit) Description() string {
	return "Replace a unique exact string in a file. old_string must appear " +
		"exactly once (fails on zero or multiple matches). Prefer this over " +
		"write for targeted changes."
}

func (Edit) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_path":  {"type": "string"},
    "old_string": {"type": "string", "description": "the exact text to replace; must be unique in the file"},
    "new_string": {"type": "string", "description": "the replacement text"}
  },
  "required": ["file_path", "old_string", "new_string"]
}`)
}

type editInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func (Edit) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in editInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("edit: bad input: %w", err)
	}
	if in.FilePath == "" {
		return "", fmt.Errorf("edit: file_path is required")
	}
	if in.OldString == "" {
		return "", fmt.Errorf("edit: old_string is required")
	}
	if in.OldString == in.NewString {
		return "", fmt.Errorf("edit: old_string and new_string are identical")
	}

	data, err := os.ReadFile(in.FilePath)
	if err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	body := string(data)

	count := strings.Count(body, in.OldString)
	switch count {
	case 0:
		return "", fmt.Errorf("edit: old_string not found in %s", in.FilePath)
	case 1:
		// ok
	default:
		return "", fmt.Errorf("edit: old_string matches %d times in %s; make it unique", count, in.FilePath)
	}

	updated := strings.Replace(body, in.OldString, in.NewString, 1)
	if err := os.WriteFile(in.FilePath, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("edit: write: %w", err)
	}

	// Return a unified diff of the change (powers the TUI diff view, T-031).
	return diff.Unified(body, updated), nil
}
