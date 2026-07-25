package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestReadFullFile(t *testing.T) {
	p := writeFile(t, t.TempDir(), "a.txt", "alpha\nbeta\ngamma\n")
	out, err := (Read{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"file_path": p,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "gamma") {
		t.Fatalf("read output = %q", out)
	}
	// cat -n style line numbering present
	if !strings.Contains(out, "1\t") {
		t.Fatalf("expected line numbering, got %q", out)
	}
}

func TestReadOffsetLimit(t *testing.T) {
	p := writeFile(t, t.TempDir(), "a.txt", "l1\nl2\nl3\nl4\nl5\n")
	out, err := (Read{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"file_path": p, "offset": 2, "limit": 2,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// offset 2 (1-based) + limit 2 → lines 2 and 3 only.
	if !strings.Contains(out, "l2") || !strings.Contains(out, "l3") {
		t.Fatalf("want l2+l3, got %q", out)
	}
	if strings.Contains(out, "l1") || strings.Contains(out, "l4") || strings.Contains(out, "l5") {
		t.Fatalf("unexpected lines in %q", out)
	}
}

func TestReadMissingFileErrors(t *testing.T) {
	_, err := (Read{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"file_path": filepath.Join(t.TempDir(), "nope.txt"),
	}))
	if err == nil {
		t.Fatal("want error for missing file")
	}
}
