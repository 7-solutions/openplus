package coordinate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/7solutions/openplus/internal/orchestrate"
	"github.com/7solutions/openplus/internal/symbols"
)

// defaultLockExpiry is how long a lock lives before it can be reclaimed. Long
// enough that a real agent is never stolen from, short enough that a crashed one
// does not block work for the rest of a session.
const defaultLockExpiry = 30 * time.Minute

// NativeCoordinator implements orchestrate.Coordinator natively: symbol locks
// backed by the file Store, worktrees via git, merges via git. No external binary
// (change 0013).
//
// The Coordinator port from change 0012 is unchanged; this is a second adapter.
// Native is the default because it ships with OpenPlus; grit remains available for
// non-Go languages and multi-machine coordination.
type NativeCoordinator struct {
	RepoRoot string
	// Expiry bounds how long a lock lives. Zero uses defaultLockExpiry.
	Expiry time.Duration

	store *Store
}

func (n *NativeCoordinator) ensureStore() {
	if n.store == nil {
		exp := n.Expiry
		if exp == 0 {
			exp = defaultLockExpiry
		}
		n.store = NewStore(filepath.Join(n.RepoRoot, ".openplus", "locks"), exp)
	}
}

// Available reports whether coordination is possible: the project must be a git
// repository (worktrees and merges need it). No external binary is required.
func (n *NativeCoordinator) Available() bool {
	if fi, err := os.Stat(filepath.Join(n.RepoRoot, ".git")); err != nil || (!fi.IsDir() && fi.Size() == 0) {
		return false
	}
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	return true
}

// Claim validates every symbol exists in the Go index, acquires locks for all of
// them atomically, and creates a detached worktree for the agent. A nonexistent
// symbol is refused — granting a lock on nothing would let two agents both
// believe they had exclusive access to nothing.
func (n *NativeCoordinator) Claim(ctx context.Context, agent, intent string, syms []string) (orchestrate.Claim, error) {
	if !n.Available() {
		return orchestrate.Claim{}, fmt.Errorf("coordinate: native coordinator is unavailable " +
			"(need a git repository and git on PATH)")
	}
	n.ensureStore()

	// Validate every symbol first. A claim on a symbol that does not exist is a
	// programming error, not a contention event.
	for _, sym := range syms {
		ok, err := symbols.Exists(n.RepoRoot, sym)
		if err != nil {
			return orchestrate.Claim{}, fmt.Errorf("coordinate: %w", err)
		}
		if !ok {
			return orchestrate.Claim{}, fmt.Errorf("coordinate: symbol %q does not exist", sym)
		}
	}

	held, err := n.store.Acquire(agent, intent, syms)
	if err != nil {
		return orchestrate.Claim{}, err
	}
	if !held.Granted {
		return orchestrate.Claim{
			BlockedBy:     held.BlockedBy,
			BlockedSymbol: held.BlockedSymbol,
		}, nil
	}

	// Create the worktree. If it fails, release the locks so the claim is not
	// left half-done.
	dir, err := n.createWorktree(ctx, agent)
	if err != nil {
		_ = n.store.ReleaseAgent(agent)
		return orchestrate.Claim{}, fmt.Errorf("coordinate: worktree: %w", err)
	}

	return orchestrate.Claim{Granted: true, Dir: dir}, nil
}

// Done commits everything in the agent's worktree, merges it into the base
// branch, removes the worktree, and releases locks. A merge conflict is reported
// and the locks still release — a stuck merge must not hold symbols forever.
func (n *NativeCoordinator) Done(ctx context.Context, agent string) error {
	n.ensureStore()
	wt := n.worktreeDir(agent)

	// Commit the agent's work, whatever it is. An empty commit (the agent changed
	// nothing) is fine: the merge is then a no-op and the locks still release.
	if err := n.gitIn(ctx, wt, "add", "-A"); err != nil {
		return err
	}
	// --allow-empty so a no-op agent completes cleanly.
	if _, err := n.gitWithEnv(ctx, wt, []string{
		"GIT_AUTHOR_NAME=OpenPlus", "GIT_AUTHOR_EMAIL=noreply@openplus",
		"GIT_COMMITTER_NAME=OpenPlus", "GIT_COMMITTER_EMAIL=noreply@openplus",
	}, "commit", "--allow-empty", "-q", "-m", "openplus subagent "+agent); err != nil {
		// A commit with nothing staged and --allow-empty still succeeds, so an
		// error here is real.
		return fmt.Errorf("coordinate: commit: %w", err)
	}

	// Merge the agent's branch into the base branch. This is where disjoint
	// symbol edits land cleanly (verified empirically) and same-symbol edits
	// conflict.
	if err := n.mergeAgent(ctx, wt, agent); err != nil {
		// Release locks even on conflict, then surface the error.
		_ = n.store.ReleaseAgent(agent)
		_ = n.removeWorktree(ctx, agent)
		return fmt.Errorf("coordinate: merge: %w", err)
	}

	_ = n.removeWorktree(ctx, agent)
	return n.store.ReleaseAgent(agent)
}

// Release is the failure path: remove the worktree and free locks, merging
// nothing. It must not error on cleanup.
func (n *NativeCoordinator) Release(ctx context.Context, agent string) error {
	n.ensureStore()
	_ = n.removeWorktree(ctx, agent)
	return n.store.ReleaseAgent(agent)
}

// worktreeDir is where an agent's worktree lives.
func (n *NativeCoordinator) worktreeDir(agent string) string {
	return filepath.Join(n.RepoRoot, ".openplus", "worktrees", agent)
}

func (n *NativeCoordinator) createWorktree(ctx context.Context, agent string) (string, error) {
	dir := n.worktreeDir(agent)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if out, err := n.git(ctx, "worktree", "add", "--detach", "-q", dir, "HEAD"); err != nil {
		return "", fmt.Errorf("%s: %w", out, err)
	}
	return dir, nil
}

func (n *NativeCoordinator) removeWorktree(ctx context.Context, agent string) error {
	dir := n.worktreeDir(agent)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	if out, err := n.git(ctx, "worktree", "remove", "--force", dir); err != nil {
		_ = os.RemoveAll(dir)
		_, _ = n.git(ctx, "worktree", "prune")
		return fmt.Errorf("%s: %w", out, err)
	}
	return nil
}

// mergeAgent merges the agent's worktree HEAD into the repo's base branch.
func (n *NativeCoordinator) mergeAgent(ctx context.Context, wt, agent string) error {
	// Read the worktree's commit to merge by SHA.
	out, err := n.gitInOut(ctx, wt, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	sha := strings.TrimSpace(out)

	// Merge that commit into the base branch from the repo root.
	if out, err := n.git(ctx, "merge", "--no-edit", "-q", sha); err != nil {
		// Abort the in-progress merge so the base branch is left clean, then
		// report. Distinguish a conflict (recoverable, expected) from a git fault.
		_, _ = n.git(ctx, "merge", "--abort")
		return fmt.Errorf("%s: %w", out, err)
	}
	return nil
}

// git runs git in the repo root.
func (n *NativeCoordinator) git(ctx context.Context, args ...string) (string, error) {
	return n.gitDir(ctx, n.RepoRoot, args...)
}

// gitIn runs git in a specific directory.
func (n *NativeCoordinator) gitIn(ctx context.Context, dir string, args ...string) error {
	_, err := n.gitDir(ctx, dir, args...)
	return err
}

func (n *NativeCoordinator) gitInOut(ctx context.Context, dir string, args ...string) (string, error) {
	return n.gitDir(ctx, dir, args...)
}

func (n *NativeCoordinator) gitDir(ctx context.Context, dir string, args ...string) (string, error) {
	return n.gitWithEnv(ctx, dir, nil, args...)
}

// gitWithEnv runs git with extra environment (author identity for commits in a
// detached worktree that inherits none).
func (n *NativeCoordinator) gitWithEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
