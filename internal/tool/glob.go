package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/7-solutions/openplus/internal/glob"
)

// Glob finds files under Root matching a pattern that supports *, ?, and **
// (recursive across path segments). Returns one path per line. Root defaults
// to "." when empty.
type Glob struct {
	Root string
}

func (g Glob) Name() string { return "glob" }

func (g Glob) Description() string {
	return "Find files matching a glob pattern (supports * ? and ** for " +
		"recursive matching). Returns matching paths, one per line."
}

func (g Glob) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "glob pattern, e.g. \"**/*.go\" or \"src/*.md\""}
  },
  "required": ["pattern"]
}`)
}

type globInput struct {
	Pattern string `json:"pattern"`
}

func (g Glob) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in globInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("glob: bad input: %w", err)
	}
	if in.Pattern == "" {
		return "", fmt.Errorf("glob: pattern is required")
	}
	root := g.Root
	if root == "" {
		root = "."
	}

	var matches []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if glob.Match(in.Pattern, rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("glob: walk: %w", walkErr)
	}

	sort.Strings(matches)
	return strings.Join(matches, "\n"), nil
}
