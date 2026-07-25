package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Read returns a file's contents with cat -n style line numbering. offset is a
// 1-based starting line (default 1); limit caps the number of lines returned.
type Read struct{}

func (Read) Name() string { return "read" }

func (Read) Description() string {
	return "Read a file from the local filesystem and return its contents with " +
		"1-based line numbers. Use offset/limit to read a range of lines. " +
		"Returns an error if the file does not exist."
}

func (Read) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string", "description": "absolute or relative path to the file"},
    "offset":    {"type": "integer", "description": "1-based line to start at (default 1)"},
    "limit":     {"type": "integer", "description": "maximum number of lines to return"}
  },
  "required": ["file_path"]
}`)
}

type readInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

func (Read) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in readInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("read: bad input: %w", err)
	}
	if in.FilePath == "" {
		return "", fmt.Errorf("read: file_path is required")
	}

	data, err := os.ReadFile(in.FilePath)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	start := in.Offset
	if start < 1 {
		start = 1
	}
	end := len(lines)
	if in.Limit > 0 && start-1+in.Limit < end {
		end = start - 1 + in.Limit
	}
	if start-1 > len(lines) {
		return "", nil
	}

	var b strings.Builder
	for i := start - 1; i < end; i++ {
		fmt.Fprintf(&b, "%d\t%s\n", i+1, lines[i])
	}
	return b.String(), nil
}
