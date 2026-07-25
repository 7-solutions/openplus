package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditReplacesUniqueMatch(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.go")
	os.WriteFile(p, []byte("func old() {}\n"), 0o600)
	out, err := (Edit{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"file_path": p, "old_string": "func old()", "new_string": "func new()",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "func new()") {
		t.Fatalf("file not updated: %q", got)
	}
	if strings.Contains(string(got), "old") {
		t.Fatalf("old string still present: %q", got)
	}
	_ = out
}

func TestEditNoMatchErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(p, []byte("nothing here"), 0o600)
	_, err := (Edit{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"file_path": p, "old_string": "absent", "new_string": "x",
	}))
	if err == nil {
		t.Fatal("want error for no match")
	}
}

// TestEditReturnsUnifiedDiff proves the edit result is a +/- diff (T-031).
func TestEditReturnsUnifiedDiff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(p, []byte("alpha\nbeta\ngamma\n"), 0o600)
	out, err := (Edit{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"file_path": p, "old_string": "beta", "new_string": "BETA",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "- beta") || !strings.Contains(out, "+ BETA") {
		t.Fatalf("edit result not a diff:\n%s", out)
	}
}

func TestEditMultipleMatchesErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(p, []byte("dup\ndup\n"), 0o600)
	_, err := (Edit{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"file_path": p, "old_string": "dup", "new_string": "x",
	}))
	if err == nil {
		t.Fatal("want error for multiple matches")
	}
}

// jsonInput is a tiny helper shared by the tool tests.
func jsonInput(t *testing.T, m map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
