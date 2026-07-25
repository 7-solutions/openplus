package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesFileAndParentDirs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "deep", "f.txt")
	_, err := (Write{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"file_path": p, "content": "hello world",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("content = %q", got)
	}
}

func TestWriteOverwritesExisting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Write{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"file_path": p, "content": "new",
	})); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
}
