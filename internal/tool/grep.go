package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Grep searches file contents under Root for a regex pattern. Returns matches
// as "path:line: text", one per line. Optional glob filters file names;
// ignore_case makes the pattern case-insensitive. Root defaults to ".".
type Grep struct {
	Root string
}

func (g Grep) Name() string { return "grep" }

func (g Grep) Description() string {
	return "Search file contents for a regular expression. Returns matching " +
		"lines as path:line: text. Optional glob filters file names; " +
		"ignore_case makes the match case-insensitive."
}

func (g Grep) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern":      {"type": "string", "description": "regular expression to search for"},
    "glob":         {"type": "string", "description": "optional filename glob filter, e.g. \"*.go\""},
    "ignore_case":  {"type": "boolean", "description": "case-insensitive match (default false)"}
  },
  "required": ["pattern"]
}`)
}

type grepInput struct {
	Pattern    string `json:"pattern"`
	Glob       string `json:"glob"`
	IgnoreCase bool   `json:"ignore_case"`
}

const grepMaxMatches = 200

func (g Grep) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in grepInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("grep: bad input: %w", err)
	}
	if in.Pattern == "" {
		return "", fmt.Errorf("grep: pattern is required")
	}
	pat := in.Pattern
	if in.IgnoreCase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return "", fmt.Errorf("grep: bad pattern: %w", err)
	}

	root := g.Root
	if root == "" {
		root = "."
	}

	var results []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if in.Glob != "" {
			if ok, _ := filepath.Match(in.Glob, d.Name()); !ok {
				return nil
			}
		}
		if err := grepFile(path, re, &results); err != nil {
			return nil // skip unreadable files (e.g. binary)
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("grep: walk: %w", walkErr)
	}

	if len(results) == 0 {
		return "no matches", nil
	}
	return strings.Join(results, "\n"), nil
}

func grepFile(path string, re *regexp.Regexp, results *[]string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if len(*results) >= grepMaxMatches {
			break
		}
		if re.MatchString(scanner.Text()) {
			*results = append(*results, fmt.Sprintf("%s:%d: %s", path, lineNo, scanner.Text()))
		}
	}
	return scanner.Err()
}
