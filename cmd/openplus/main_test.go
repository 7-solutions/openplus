package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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

// --- Change 0004 / T-422: --config points at a non-default opencode.json ---

// TestMainConfigFlagSuccess: --config /tmp/x.json with a valid config and
// --fake must exit 0 and produce the fake provider's reply. If --config
// is silently ignored (or fails to load the override), the run would
// either fail to assemble or hit the default <root>/opencode.json which
// doesn't exist.
func TestMainConfigFlagSuccess(t *testing.T) {
	bin := buildOpenplus(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "my-config.json")
	if err := os.WriteFile(cfg, []byte(`{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}}
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "--config", cfg, "--fake", "-p", "say hello", "-C", dir)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--config run failed: %v\n%s", err, stdout)
	}
	if !strings.Contains(string(stdout), "openplus runtime is wired") {
		t.Errorf("stdout = %q, want fake-provider reply", stdout)
	}
}

// TestMainConfigFlagMissing: --config /missing.json must exit non-zero
// with a clear error. (Proves we don't silently fall back to the default
// <root>/opencode.json when the override is named but missing.)
func TestMainConfigFlagMissing(t *testing.T) {
	bin := buildOpenplus(t)
	missing := filepath.Join(t.TempDir(), "nope.json")
	cmd := exec.Command(bin, "--config", missing, "--fake", "-C", t.TempDir())
	stdout, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("--config /missing.json should exit non-zero, got success: %s", stdout)
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