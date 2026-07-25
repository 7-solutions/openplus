package tool

import (
	"context"
	"strings"
	"testing"
)

func TestGlobRecursiveDoubleStar(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "x")
	writeFile(t, root, "pkg/b.go", "x")
	writeFile(t, root, "pkg/sub/c.go", "x")
	writeFile(t, root, "d.md", "x")

	out, err := (Glob{Root: root}).Execute(context.Background(), jsonInput(t, map[string]any{
		"pattern": "**/*.go",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"a.go", "b.go", "c.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("glob missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "d.md") {
		t.Errorf("glob should not include .md:\n%s", out)
	}
}

func TestGlobTopLevelOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "top.md", "x")
	writeFile(t, root, "pkg/deep.md", "x")

	out, err := (Glob{Root: root}).Execute(context.Background(), jsonInput(t, map[string]any{
		"pattern": "*.md",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "top.md") {
		t.Errorf("missing top.md:\n%s", out)
	}
	if strings.Contains(out, "deep.md") {
		t.Errorf("*.md should not recurse:\n%s", out)
	}
}
