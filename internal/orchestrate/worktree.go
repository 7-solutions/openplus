package orchestrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// WorktreeIsolator isolates each subagent in its own git worktree (ADR-0006,
// orchestration spec: "each runs in its own git worktree and results merge back
// deterministically"). Edits in one worktree cannot disturb the primary checkout
// or a sibling subagent.
type WorktreeIsolator struct {
	// RepoRoot is the primary checkout the worktrees branch from.
	RepoRoot string
	// BaseDir is where worktree directories are created. Empty uses the OS temp
	// directory.
	BaseDir string
	// Ref is the commit-ish each worktree checks out. Empty uses HEAD.
	Ref string

	// seq disambiguates directory names when the same task id isolates twice.
	seq atomic.Uint64
}

// Isolate creates a detached worktree for id and returns its directory plus a
// release func that removes it.
func (w *WorktreeIsolator) Isolate(ctx context.Context, id string) (string, func() error, error) {
	base := w.BaseDir
	if base == "" {
		base = os.TempDir()
	}
	ref := w.Ref
	if ref == "" {
		ref = "HEAD"
	}

	dir := filepath.Join(base, fmt.Sprintf("openplus-wt-%s-%d", sanitize(id), w.seq.Add(1)))

	// --detach: the subagent works on a detached HEAD, so parallel worktrees
	// never contend over a branch name.
	if out, err := w.git(ctx, "worktree", "add", "--detach", "--quiet", dir, ref); err != nil {
		return "", nil, fmt.Errorf("orchestrate: worktree add: %w: %s", err, out)
	}

	release := func() error {
		if out, err := w.git(context.Background(), "worktree", "remove", "--force", dir); err != nil {
			// Fall back to a manual delete + prune so a failed remove cannot
			// leak the directory or its git metadata.
			_ = os.RemoveAll(dir)
			if _, pruneErr := w.git(context.Background(), "worktree", "prune"); pruneErr != nil {
				return fmt.Errorf("orchestrate: worktree remove: %w: %s", err, out)
			}
			return nil
		}
		return nil
	}
	return dir, release, nil
}

// git runs a git command in the repo root and returns its combined output.
func (w *WorktreeIsolator) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = w.RepoRoot
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// sanitize reduces a task id to a filesystem-safe fragment.
func sanitize(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "task"
	}
	return b.String()
}
