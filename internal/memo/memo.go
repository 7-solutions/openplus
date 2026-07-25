// Package memo is the file-based memory surface (T-044): MEMORY.md, notes.md,
// and tasks/<id>/progress.md. Load assembles them into one block for system-
// prompt injection (resume inject); AppendMemory and WriteProgress persist
// updates. These are ordinary project files — nothing here touches the network.
package memo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Files addresses the memory files relative to a project Root.
type Files struct {
	Root string
}

// Load reads MEMORY.md, notes.md, and tasks/*/progress.md (in task-id order),
// concatenating them with section headers for resume injection. Missing files
// are skipped silently. Returns "" (no error) when nothing exists.
func (f Files) Load() (string, error) {
	var b strings.Builder

	if body, ok := readFile(filepath.Join(f.Root, "MEMORY.md")); ok {
		b.WriteString("# Project memory (MEMORY.md)\n")
		b.WriteString(body)
		ensureNewline(&b)
	}
	if body, ok := readFile(filepath.Join(f.Root, "notes.md")); ok {
		b.WriteString("# Notes (notes.md)\n")
		b.WriteString(body)
		ensureNewline(&b)
	}

	progress, err := filepath.Glob(filepath.Join(f.Root, "tasks", "*", "progress.md"))
	if err != nil {
		return "", fmt.Errorf("memo: glob progress: %w", err)
	}
	sort.Strings(progress)
	for _, p := range progress {
		body, ok := readFile(p)
		if !ok {
			continue
		}
		rel, _ := filepath.Rel(f.Root, p)
		fmt.Fprintf(&b, "# %s\n", rel)
		b.WriteString(body)
		ensureNewline(&b)
	}

	return b.String(), nil
}

// AppendMemory appends a line to MEMORY.md, creating it (and tasks/ parents are
// not needed here) if absent.
func (f Files) AppendMemory(line string) error {
	p := filepath.Join(f.Root, "MEMORY.md")
	content := line
	if existing, ok := readFile(p); ok {
		content = existing
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += line + "\n"
	} else {
		content = line + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("memo: mkdir: %w", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memo: write MEMORY.md: %w", err)
	}
	return nil
}

// WriteProgress writes (overwrites) tasks/<task>/progress.md.
func (f Files) WriteProgress(task, content string) error {
	p := filepath.Join(f.Root, "tasks", task, "progress.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("memo: mkdir: %w", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memo: write progress: %w", err)
	}
	return nil
}

func readFile(p string) (string, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func ensureNewline(b *strings.Builder) {
	s := b.String()
	if !strings.HasSuffix(s, "\n") {
		b.WriteByte('\n')
	}
}
