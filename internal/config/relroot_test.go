package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRelativeRootAcceptsInstructions is a regression test for the default
// invocation. `openplus -p "..."` uses root "." and every project that
// configures instructions hit:
//
//	error: config: instruction path "AGENTS.md" escapes the project root
//
// The cause: filepath.Join(".", "AGENTS.md") is "AGENTS.md", which does not
// have the "./" prefix the containment check looked for. Absolute roots were
// unaffected, which is why every existing test — all using t.TempDir() —
// passed.
func TestRelativeRootAcceptsInstructions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"),
		[]byte(`{"model":"local/m","instructions":["AGENTS.md"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Run with the relative root the CLI defaults to.
	restore := chdir(t, dir)
	defer restore()

	pc, err := LoadProjectContext(".")
	if err != nil {
		t.Fatalf(`LoadProjectContext("."): %v`, err)
	}
	if pc.Instructions == "" {
		t.Error("instructions were not loaded from a relative root")
	}
}

// TestRelativeRootStillRejectsEscape: the fix must not weaken containment.
// Traversal out of the project is still refused.
func TestRelativeRootStillRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"),
		[]byte(`{"model":"local/m","instructions":["../outside.md"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The file exists outside the root, so only the containment check can
	// reject it — a missing-file error would prove nothing.
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.md"),
		[]byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := chdir(t, dir)
	defer restore()

	if _, err := LoadProjectContext("."); err == nil {
		t.Fatal("a path escaping the project root must be rejected, even from a relative root")
	}
}

// TestAbsoluteRootUnaffected: the previously working path keeps working.
func TestAbsoluteRootUnaffected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"),
		[]byte(`{"model":"local/m","instructions":["AGENTS.md"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	pc, err := LoadProjectContext(dir)
	if err != nil {
		t.Fatalf("LoadProjectContext(abs): %v", err)
	}
	if pc.Instructions == "" {
		t.Error("instructions were not loaded from an absolute root")
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() { _ = os.Chdir(prev) }
}
