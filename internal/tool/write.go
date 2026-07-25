package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write creates or overwrites a file, creating parent directories as needed.
type Write struct{}

func (Write) Name() string { return "write" }

func (Write) Description() string {
	return "Write content to a file, overwriting it if it exists. Parent " +
		"directories are created automatically."
}

func (Write) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string", "description": "path to the file to write"},
    "content":   {"type": "string", "description": "the full file contents to write"}
  },
  "required": ["file_path", "content"]
}`)
}

type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func (Write) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in writeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("write: bad input: %w", err)
	}
	if in.FilePath == "" {
		return "", fmt.Errorf("write: file_path is required")
	}
	if err := os.MkdirAll(filepath.Dir(in.FilePath), 0o755); err != nil {
		return "", fmt.Errorf("write: mkdir: %w", err)
	}
	if err := os.WriteFile(in.FilePath, []byte(in.Content), 0o644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.FilePath), nil
}
