package main

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// --- Change 0004 / T-420: --version prints "openplus <version>" ---

// versionRe matches the canonical version line: "openplus <non-whitespace>".
// The exact format isn't part of the contract — only that the line starts
// with the binary name and has a non-empty version token after it.
var versionRe = regexp.MustCompile(`^openplus \S+\s*$`)

// TestMainVersionFlag: invoking the built binary with --version prints a
// single line to stdout matching ^openplus \S+$, exits 0, and never
// assembles a session.
//
// Subprocess is intentional: per the proposal T-426, the cmd surface is
// driven via os/exec when the function isn't testable directly. This
// keeps run()'s flag.Parse(os.Args[1:]) coupling intact.
func TestMainVersionFlag(t *testing.T) {
	bin := buildOpenplus(t)
	cmd := exec.Command(bin, "--version")
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("--version exit nonzero: %v\nstdout=%s", err, stdout)
	}
	if !versionRe.Match(stdout) {
		t.Fatalf("stdout = %q, want match %s", stdout, versionRe)
	}
}

// buildOpenplus compiles the binary into a temp dir and returns its path.
// t.Cleanup removes the file when the test ends.
func buildOpenplus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "openplus")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/7solutions/openplus/cmd/openplus")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}