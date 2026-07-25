package tool

import (
	"context"
	"strings"
	"testing"
)

func TestGrepFindsMatches(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "foo here\nbar\nFOO upper\n")
	writeFile(t, root, "b.txt", "nothing\nfoo again\n")

	out, err := (Grep{Root: root}).Execute(context.Background(), jsonInput(t, map[string]any{
		"pattern": "foo",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// case-sensitive: matches the two lowercase "foo" lines, not "FOO".
	if c := strings.Count(out, "foo"); c != 2 {
		t.Errorf("want 2 foo matches, got %d in:\n%s", c, out)
	}
	if strings.Contains(out, "FOO upper") {
		t.Errorf("case-sensitive grep matched uppercase:\n%s", out)
	}
	// results are file:line:content
	if !strings.Contains(out, ":1:") || !strings.Contains(out, ":2:") {
		t.Errorf("expected file:line: format:\n%s", out)
	}
}

func TestGrepCaseInsensitiveFlag(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "Foo\nfOO\nbar\n")
	out, err := (Grep{Root: root}).Execute(context.Background(), jsonInput(t, map[string]any{
		"pattern": "foo", "ignore_case": true,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c := strings.Count(out, ":"); c < 4 { // at least 2 matches × file:line:colon
		t.Errorf("want 2 case-insensitive matches, got:\n%s", out)
	}
}

func TestGrepGlobFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "target\n")
	writeFile(t, root, "b.txt", "target\n")
	out, err := (Grep{Root: root}).Execute(context.Background(), jsonInput(t, map[string]any{
		"pattern": "target", "glob": "*.go",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("missing a.go:\n%s", out)
	}
	if strings.Contains(out, "b.txt") {
		t.Errorf("glob filter should exclude b.txt:\n%s", out)
	}
}
