package orchestrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gritOrSkip skips a test when the grit binary is unavailable. Same pattern the
// worktree tests use for git: the adapter is only honestly testable against the
// real tool, and a skip is recorded rather than a pass being faked.
func gritOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("grit"); err != nil {
		t.Skip("grit not installed; adapter integration not exercised")
	}
}

// --- T-1212: the parts that run everywhere ---

// TestGritAvailableFalseWithBogusBin runs with or without grit installed, and is
// the guard that matters most: OpenPlus must degrade rather than fail when the
// binary is absent.
func TestGritAvailableFalseWithBogusBin(t *testing.T) {
	c := &GritCoordinator{RepoRoot: t.TempDir(), Bin: "definitely-not-a-real-binary-xyz"}
	if c.Available() {
		t.Fatal("Available() must be false for a nonexistent binary")
	}
}

func TestGritAvailableReflectsRealBinary(t *testing.T) {
	c := &GritCoordinator{RepoRoot: t.TempDir()}
	// Whatever the answer, it must match what the OS says, not panic or guess.
	_, lookErr := exec.LookPath("grit")
	if got, want := c.Available(), lookErr == nil; got != want {
		t.Errorf("Available() = %v, want %v", got, want)
	}
}

// TestGritClaimFailsClearlyWhenUnavailable pins that using an unavailable
// coordinator gives a usable message rather than an exec error.
func TestGritClaimFailsClearlyWhenUnavailable(t *testing.T) {
	c := &GritCoordinator{RepoRoot: t.TempDir(), Bin: "definitely-not-a-real-binary-xyz"}
	_, err := c.Claim(context.Background(), "a", "i", []string{"f.go::A"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "grit") {
		t.Errorf("error should name the missing tool: %v", err)
	}
}

func TestGritClaimNeedsSymbols(t *testing.T) {
	c := &GritCoordinator{RepoRoot: t.TempDir()}
	if _, err := c.Claim(context.Background(), "a", "i", nil); err == nil {
		t.Fatal("expected an error claiming no symbols")
	}
}

// TestGritWorktreePathConvention pins the path grit creates for an agent, which
// the adapter must derive rather than parse out of stdout.
func TestGritWorktreePathConvention(t *testing.T) {
	root := t.TempDir()
	c := &GritCoordinator{RepoRoot: root}
	want := filepath.Join(root, ".grit", "worktrees", "agent-7")
	if got := c.worktreeDir("agent-7"); got != want {
		t.Errorf("worktreeDir = %q, want %q", got, want)
	}
}

// TestGritBlockedDetection pins T-1211's distinction: "someone holds this" must
// not read as "grit is broken".
func TestGritBlockedDetection(t *testing.T) {
	cases := []struct {
		out     string
		blocked bool
	}{
		{"Blocked (held by agent-1)", true},
		{"error: symbol is blocked by agent-2", true},
		{"claim blocked: src/auth.ts::validateToken held by agent-3", true},
		{"error: not a git repository", false},
		{"error: grit init has not been run", false},
		{"", false},
	}
	for _, c := range cases {
		if got := looksBlocked(c.out); got != c.blocked {
			t.Errorf("looksBlocked(%q) = %v, want %v", c.out, got, c.blocked)
		}
	}
}

// TestGritHolderExtraction pins that the report can name who holds a symbol.
func TestGritHolderExtraction(t *testing.T) {
	cases := map[string]string{
		"Blocked (held by agent-1)":                             "agent-1",
		"claim blocked: src/auth.ts::validate held by agent-42": "agent-42",
		"error: symbol blocked":                                 "",
	}
	for out, want := range cases {
		if got := extractHolder(out); got != want {
			t.Errorf("extractHolder(%q) = %q, want %q", out, got, want)
		}
	}
}

// --- integration: only meaningful with the real binary ---

// TestGritEndToEnd is the claim → work → done cycle against a real grit on a
// scratch repo. Skipped when grit is absent.
func TestGritEndToEnd(t *testing.T) {
	gritOrSkip(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := scratchRepo(t)
	c := &GritCoordinator{RepoRoot: root}

	// grit needs its index built before claims can resolve symbols.
	if out, err := c.run(context.Background(), "init"); err != nil {
		t.Skipf("grit init failed in this environment: %v: %s", err, out)
	}

	claim, err := c.Claim(context.Background(), "agent-1", "test claim", []string{"main.go::Hello"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !claim.Granted {
		t.Fatalf("claim refused unexpectedly: %+v", claim)
	}
	if _, err := os.Stat(claim.Dir); err != nil {
		t.Errorf("granted worktree does not exist: %v", err)
	}

	t.Cleanup(func() { _ = c.Release(context.Background(), "agent-1") })

	if err := c.Done(context.Background(), "agent-1"); err != nil {
		t.Fatalf("Done: %v", err)
	}
}

// scratchRepo builds a small git repo with one Go symbol to claim.
func scratchRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\n\nfunc Hello() string { return \"hi\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-q", "-m", "seed")
	return root
}
