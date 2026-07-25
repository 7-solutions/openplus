package orchestrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a throwaway git repo with one commit (worktrees require at
// least one commit to branch from).
func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "seed.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "seed.txt")
	run("commit", "-q", "-m", "seed")
	return root
}

func TestWorktreeIsolatorCreatesAndRemoves(t *testing.T) {
	root := initRepo(t)
	iso := &WorktreeIsolator{RepoRoot: root, BaseDir: t.TempDir()}

	dir, release, err := iso.Isolate(context.Background(), "sub-1")
	if err != nil {
		t.Fatalf("Isolate: %v", err)
	}
	if dir == "" {
		t.Fatal("empty worktree dir")
	}
	// the worktree is a real checkout of the repo
	if _, err := os.Stat(filepath.Join(dir, "seed.txt")); err != nil {
		t.Fatalf("worktree missing repo content: %v", err)
	}
	// git agrees it is a worktree
	out := gitOutput(t, root, "worktree", "list")
	if !strings.Contains(out, dir) {
		t.Errorf("git worktree list missing %q:\n%s", dir, out)
	}

	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("worktree dir still present after release: %v", err)
	}
	if out := gitOutput(t, root, "worktree", "list"); strings.Contains(out, dir) {
		t.Errorf("git still tracks the released worktree:\n%s", out)
	}
}

func TestWorktreeIsolatorIsolatesEdits(t *testing.T) {
	root := initRepo(t)
	iso := &WorktreeIsolator{RepoRoot: root, BaseDir: t.TempDir()}

	dir, release, err := iso.Isolate(context.Background(), "sub-edit")
	if err != nil {
		t.Fatalf("Isolate: %v", err)
	}
	defer release() //nolint:errcheck

	// writing in the worktree must not touch the primary checkout
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	primary, err := os.ReadFile(filepath.Join(root, "seed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(primary) != "seed\n" {
		t.Fatalf("primary checkout mutated: %q", primary)
	}
}

func TestWorktreeIsolatorParallelTasksGetSeparateTrees(t *testing.T) {
	root := initRepo(t)
	iso := &WorktreeIsolator{RepoRoot: root, BaseDir: t.TempDir()}
	r := Runner{Isolator: iso, MaxParallel: 3}

	tasks := []Task{
		{ID: "w1", Run: writeMarker("w1")},
		{ID: "w2", Run: writeMarker("w2")},
		{ID: "w3", Run: writeMarker("w3")},
	}
	got, err := r.RunAll(context.Background(), tasks)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	seen := map[string]bool{}
	for _, res := range got {
		if res.Err != nil {
			t.Fatalf("task %s: %v", res.ID, res.Err)
		}
		if seen[res.Output] {
			t.Fatalf("two tasks shared a worktree: %q", res.Output)
		}
		seen[res.Output] = true
	}
	// all worktrees cleaned up: only the primary remains
	out := gitOutput(t, root, "worktree", "list")
	if lines := strings.Count(strings.TrimSpace(out), "\n"); lines != 0 {
		t.Errorf("expected only the primary worktree, got:\n%s", out)
	}
}

func TestWorktreeIsolatorNonRepoErrors(t *testing.T) {
	iso := &WorktreeIsolator{RepoRoot: t.TempDir(), BaseDir: t.TempDir()}
	if _, _, err := iso.Isolate(context.Background(), "x"); err == nil {
		t.Fatal("expected an error outside a git repo")
	}
}

// writeMarker returns a task that writes a file in its worktree and reports the
// directory it ran in.
func writeMarker(name string) func(context.Context, string) (string, error) {
	return func(ctx context.Context, dir string) (string, error) {
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(name), 0o600); err != nil {
			return "", err
		}
		return dir, nil
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
