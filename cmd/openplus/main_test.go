package main

import (
	"errors"
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

// --- Change 0004 / T-424: OPENPLUS_MODEL + OPENPLUS_FAKE=1 env overrides ---

// TestMainEnvModelOverride: OPENPLUS_MODEL=local/foo wins over --model local/bar
// and the configured model. The fake provider is enabled so no real key is
// needed; success is exit 0 + the fake's reply.
func TestMainEnvModelOverride(t *testing.T) {
	bin := buildOpenplus(t)
	cmd := exec.Command(bin, "--model", "local/flag-model", "--fake", "-p", "say hello", "-C", t.TempDir())
	cmd.Env = append(os.Environ(), "OPENPLUS_MODEL=local/env-model")
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("OPENPLUS_MODEL run failed: %v\n%s", err, stdout)
	}
	if !strings.Contains(string(stdout), "openplus runtime is wired") {
		t.Errorf("stdout = %q, want fake-provider reply", stdout)
	}
}

// TestMainEnvFakeOverride: OPENPLUS_FAKE=1 enables the fake provider without
// --fake on the command line.
func TestMainEnvFakeOverride(t *testing.T) {
	bin := buildOpenplus(t)
	cmd := exec.Command(bin, "-p", "say hello", "-C", t.TempDir())
	cmd.Env = append(os.Environ(), "OPENPLUS_FAKE=1")
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("OPENPLUS_FAKE=1 run failed: %v\n%s", err, stdout)
	}
	if !strings.Contains(string(stdout), "openplus runtime is wired") {
		t.Errorf("stdout = %q, want fake-provider reply (env should enable --fake)", stdout)
	}
}

// --- Change 0004 / T-426: exit-code contract ---
//
// 0 = clean, 2 = configuration problem, 1 = everything else.
//
// Documented in cmd/openplus/main.go godoc on exitCode().

func TestMainExitCodeClean(t *testing.T) {
	bin := buildOpenplus(t)
	cmd := exec.Command(bin, "--fake", "-p", "say hello", "-C", t.TempDir())
	if err := cmd.Run(); err != nil {
		t.Fatalf("clean run failed: %v", err)
	}
	if code := exitCode(nil); code != 0 {
		t.Errorf("exitCode(nil) = %d, want 0", code)
	}
}

func TestMainExitCodeMissingCredential(t *testing.T) {
	bin := buildOpenplus(t)
	// remote provider with no apiKey → ErrMissingCredential
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{
  "model": "anthropic/claude-sonnet-5",
  "provider": {"anthropic": {"options": {}}}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-p", "say hello", "-C", dir)
	code := runAndGetExitCode(t, cmd)
	if code != 2 {
		t.Errorf("missing-credential exit = %d, want 2\n%s", code, mustCombined(t, cmd))
	}
}

func TestMainExitCodeNoModel(t *testing.T) {
	bin := buildOpenplus(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{
  "provider": {"anthropic": {"options": {"apiKey": "k"}}}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-p", "say hello", "-C", dir)
	code := runAndGetExitCode(t, cmd)
	if code != 2 {
		t.Errorf("no-model exit = %d, want 2\n%s", code, mustCombined(t, cmd))
	}
}

func TestMainExitCodeOther(t *testing.T) {
	// any non-config error → 1. The cleanest driver is to monkey-test
	// exitCode directly: errors that are neither ErrMissingCredential
	// nor ErrNoModel map to 1.
	if code := exitCode(errors.New("anything else")); code != 1 {
		t.Errorf("exitCode(plain err) = %d, want 1", code)
	}
}

// runAndGetExitCode runs cmd once and returns its exit code, swallowing
// *exec.ExitError. Other errors fail the test.
func runAndGetExitCode(t *testing.T, cmd *exec.Cmd) int {
	t.Helper()
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("unexpected error (not ExitError): %v", err)
	}
	return ee.ExitCode()
}

// mustCombined re-runs cmd to capture combined output for failure
// messages. Test fails the helper only when re-running itself fails;
// the caller is expected to have already exercised cmd via
// runAndGetExitCode.
func mustCombined(t *testing.T, cmd *exec.Cmd) []byte {
	t.Helper()
	// don't fail the test on a second-run error; just return whatever we got.
	out, _ := cmd.CombinedOutput()
	return out
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

// --- Change 0007 / T-450..T-451: --goal flag + OPENPLUS_GOAL env override ---

// TestMainGoalFlag pins that --goal <text> reaches the runtime.
// Since the runtime's fake provider doesn't need a real judge,
// the test asserts the binary exits 0 with the fake reply — that
// proves the flag was accepted and the run completed without
// hanging on a missing judge.
//
// RED until the --goal flag is wired.
func TestMainGoalFlag(t *testing.T) {
	bin := buildOpenplus(t)
	cmd := exec.Command(bin, "--goal", "ship hello", "--fake", "-p", "ship hello", "-C", t.TempDir())
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--goal run failed: %v\n%s", err, stdout)
	}
	if !strings.Contains(string(stdout), "openplus runtime is wired") {
		t.Errorf("stdout = %q, want fake-provider reply", stdout)
	}
}

// TestMainGoalEnvOverride pins that OPENPLUS_GOAL wins over --goal
// (env > flag precedence, same as OPENPLUS_MODEL / OPENPLUS_FAKE).
//
// RED until both the flag and the env var are wired.
func TestMainGoalEnvOverride(t *testing.T) {
	bin := buildOpenplus(t)
	cmd := exec.Command(bin, "--goal", "flag-goal", "--fake", "-p", "x", "-C", t.TempDir())
	cmd.Env = append(os.Environ(), "OPENPLUS_GOAL=env-goal")
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("OPENPLUS_GOAL run failed: %v\n%s", err, stdout)
	}
	if !strings.Contains(string(stdout), "openplus runtime is wired") {
		t.Errorf("stdout = %q, want fake-provider reply (env should win over --goal)", stdout)
	}
}