package orchestrate

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultGritBin is the grit executable name resolved from PATH.
const DefaultGritBin = "grit"

// GritCoordinator is the Coordinator adapter for grit
// (https://github.com/rtk-ai/grit): AST-symbol locking, isolated worktrees, and
// serialized rebase+merge.
//
// grit is a Rust binary, so it is shelled out to rather than imported — OpenPlus
// stays a cgo-free Go binary (ADR-0001) and runs with grit absent. Callers must
// check Available before using a coordinated path.
//
// The adapter parses as little as possible: decisions come from exit status, and
// stderr is surfaced verbatim. grit's CLI is young, so a flag change should
// degrade to a reported error rather than a confident misparse.
type GritCoordinator struct {
	// RepoRoot is the repository grit coordinates.
	RepoRoot string
	// Bin overrides the executable name (tests, or a pinned install path).
	Bin string
}

func (g *GritCoordinator) bin() string {
	if g.Bin != "" {
		return g.Bin
	}
	return DefaultGritBin
}

// Available reports whether the grit binary resolves.
func (g *GritCoordinator) Available() bool {
	_, err := exec.LookPath(g.bin())
	return err == nil
}

// worktreeDir is where grit places an agent's worktree. Derived rather than
// parsed out of stdout: a stable path convention is a smaller dependency on
// grit's output format than scraping it.
func (g *GritCoordinator) worktreeDir(agent string) string {
	return filepath.Join(g.RepoRoot, ".grit", "worktrees", agent)
}

// Claim locks symbols for agent via `grit claim`.
//
// A refusal (another agent holds a symbol) comes back as a non-granted Claim, not
// an error: being blocked is a normal outcome of coordination, while an error
// means coordination itself is broken. Conflating them would make "wait your
// turn" look like a malfunction.
func (g *GritCoordinator) Claim(ctx context.Context, agent, intent string, symbols []string) (Claim, error) {
	if len(symbols) == 0 {
		return Claim{}, fmt.Errorf("orchestrate: grit claim needs at least one symbol")
	}
	if !g.Available() {
		return Claim{}, fmt.Errorf("orchestrate: grit is not installed (looked for %q); "+
			"install it for coordinated fan-out, or run uncoordinated", g.bin())
	}

	args := append([]string{"claim", "-a", agent, "-i", intent}, symbols...)
	out, err := g.run(ctx, args...)
	if err != nil {
		if looksBlocked(out) {
			return Claim{
				BlockedSymbol: firstSymbol(symbols),
				BlockedBy:     extractHolder(out),
			}, nil
		}
		return Claim{}, fmt.Errorf("orchestrate: grit claim: %w: %s", err, out)
	}
	// grit can also report a block on a zero exit depending on version; treat the
	// text as authoritative either way.
	if looksBlocked(out) {
		return Claim{
			BlockedSymbol: firstSymbol(symbols),
			BlockedBy:     extractHolder(out),
		}, nil
	}

	return Claim{Granted: true, Dir: g.worktreeDir(agent)}, nil
}

// Done runs `grit done`, which auto-commits, rebases, merges, and releases locks.
func (g *GritCoordinator) Done(ctx context.Context, agent string) error {
	if !g.Available() {
		return fmt.Errorf("orchestrate: grit is not installed")
	}
	if out, err := g.run(ctx, "done", "-a", agent); err != nil {
		return fmt.Errorf("orchestrate: grit done: %w: %s", err, out)
	}
	return nil
}

// Release frees an agent's locks without merging. It is the failure path, so it
// is best-effort: a release that itself fails must not mask the original problem,
// but a stuck lock is worth reporting.
func (g *GritCoordinator) Release(ctx context.Context, agent string) error {
	if !g.Available() {
		return nil
	}
	if out, err := g.run(ctx, "release", "-a", agent); err != nil {
		return fmt.Errorf("orchestrate: grit release: %w: %s", err, out)
	}
	return nil
}

// run executes grit in the repository root and returns its combined output.
func (g *GritCoordinator) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.bin(), args...)
	cmd.Dir = g.RepoRoot
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// blockedPattern matches grit's several ways of saying a symbol is held.
var blockedPattern = regexp.MustCompile(`(?i)\bblocked\b`)

// looksBlocked reports whether output describes a lock conflict rather than a
// malfunction.
func looksBlocked(out string) bool {
	return blockedPattern.MatchString(out)
}

// holderPattern captures the agent named in "held by <agent>".
var holderPattern = regexp.MustCompile(`(?i)held by ([A-Za-z0-9._\-]+)`)

// extractHolder pulls the blocking agent's name out of grit's message, returning
// "" when it cannot be identified — an unnamed holder is still a real block, so
// this must not fail the claim.
func extractHolder(out string) string {
	m := holderPattern.FindStringSubmatch(out)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimRight(m[1], ").,;")
}

func firstSymbol(symbols []string) string {
	if len(symbols) == 0 {
		return ""
	}
	return symbols[0]
}
