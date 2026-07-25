package memo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingFilesOK(t *testing.T) {
	f := Files{Root: t.TempDir()}
	got, err := f.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestLoadReadsMemoryMD(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "MEMORY.md", "# Mem\n- did a thing\n")
	got, err := Files{Root: root}.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(got, "did a thing") {
		t.Fatalf("missing MEMORY.md content: %q", got)
	}
}

func TestLoadReadsNotesAndProgress(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "MEMORY.md", "mem line")
	writeFile(t, root, "notes.md", "a note")
	writeFile(t, root, "tasks/T-001/progress.md", "progress 1")
	writeFile(t, root, "tasks/T-002/progress.md", "progress 2")
	got, err := Files{Root: root}.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// all sources present
	for _, want := range []string{"mem line", "a note", "progress 1", "progress 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// memory first, then notes, then progress in task-id order
	if strings.Index(got, "mem line") > strings.Index(got, "a note") {
		t.Error("MEMORY.md should precede notes.md")
	}
	if strings.Index(got, "progress 1") > strings.Index(got, "progress 2") {
		t.Error("progress files should be in task-id order")
	}
}

func TestAppendMemory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "MEMORY.md", "existing\n")
	f := Files{Root: root}
	if err := f.AppendMemory("- new fact"); err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "MEMORY.md"))
	s := string(got)
	if !strings.Contains(s, "existing") || !strings.HasSuffix(strings.TrimSpace(s), "- new fact") {
		t.Fatalf("append wrong: %q", s)
	}
}

func TestAppendMemoryCreatesFile(t *testing.T) {
	root := t.TempDir()
	f := Files{Root: root}
	if err := f.AppendMemory("- first"); err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "MEMORY.md"))
	if !strings.Contains(string(got), "- first") {
		t.Fatalf("file not created with entry: %q", got)
	}
}

func TestWriteProgress(t *testing.T) {
	root := t.TempDir()
	f := Files{Root: root}
	if err := f.WriteProgress("T-042", "started T-042"); err != nil {
		t.Fatalf("WriteProgress: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "tasks/T-042/progress.md"))
	if !strings.Contains(string(got), "started T-042") {
		t.Fatalf("progress not written: %q", got)
	}
}
