package orchestrate

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

// Coordinator locks the code symbols an agent intends to edit, before it edits
// them, so parallel agents cannot produce conflicting changes (change 0012).
//
// The model is grit's: claim → work → done. Claiming AST symbols rather than
// files means two agents editing different functions in the same file both
// proceed — the case raw git fails on. Conflicts are prevented at claim time
// instead of detected at merge time.
//
// The port exists because the reference implementation (grit) is an external
// Rust binary. OpenPlus is a cgo-free Go binary and must run without it, so the
// core depends on this interface and an absent tool is a reportable state rather
// than a build dependency.
type Coordinator interface {
	// Available reports whether coordination can be used at all.
	Available() bool
	// Claim locks symbols for agent. A refusal (someone else holds a symbol) is
	// reported in the Claim, not as an error; an error means coordination itself
	// failed.
	Claim(ctx context.Context, agent, intent string, symbols []string) (Claim, error)
	// Done merges the agent's work and releases its locks.
	Done(ctx context.Context, agent string) error
	// Release frees the agent's locks without claiming its work was merged. It is
	// the failure path: a crashed agent must not hold locks forever.
	Release(ctx context.Context, agent string) error
}

// Claim is the outcome of a claim attempt.
type Claim struct {
	// Granted reports whether the agent may proceed.
	Granted bool
	// Dir is the worktree the agent should work in (granted claims only).
	Dir string
	// BlockedSymbol and BlockedBy name why a claim was refused, so the report can
	// say who holds what rather than just "blocked".
	BlockedSymbol string
	BlockedBy     string
}

// NoCoordinator is the unconfigured coordinator: always unavailable. It exists so
// the uncoordinated path holds a real object rather than forcing a nil check at
// every call site.
type NoCoordinator struct{}

func (NoCoordinator) Available() bool { return false }

func (NoCoordinator) Claim(context.Context, string, string, []string) (Claim, error) {
	return Claim{}, fmt.Errorf("orchestrate: no coordinator configured")
}

func (NoCoordinator) Done(context.Context, string) error    { return nil }
func (NoCoordinator) Release(context.Context, string) error { return nil }

// FakeCoordinator is an in-memory Coordinator for tests: symbol locking without a
// binary, a repository, or a filesystem.
type FakeCoordinator struct {
	// Unavailable makes Available report false.
	Unavailable bool
	// ClaimErr, when set, makes Claim fail — the hard-error path, distinct from a
	// refused claim.
	ClaimErr error

	mu      sync.Mutex
	holders map[string]string   // symbol -> agent
	claimed map[string][]string // agent -> symbols
	merged  map[string]bool     // agent -> Done was called
}

func NewFakeCoordinator() *FakeCoordinator {
	return &FakeCoordinator{
		holders: map[string]string{},
		claimed: map[string][]string{},
		merged:  map[string]bool{},
	}
}

func (f *FakeCoordinator) Available() bool { return !f.Unavailable }

func (f *FakeCoordinator) Claim(_ context.Context, agent, _ string, symbols []string) (Claim, error) {
	if f.ClaimErr != nil {
		return Claim{}, f.ClaimErr
	}
	if len(symbols) == 0 {
		return Claim{}, fmt.Errorf("orchestrate: claim needs at least one symbol")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Check every symbol before taking any, so a partially-granted claim cannot
	// leave the agent holding locks it was refused.
	for _, sym := range symbols {
		if holder, held := f.holders[sym]; held && holder != agent {
			return Claim{BlockedSymbol: sym, BlockedBy: holder}, nil
		}
	}
	for _, sym := range symbols {
		f.holders[sym] = agent
	}
	f.claimed[agent] = append(f.claimed[agent], symbols...)

	return Claim{Granted: true, Dir: filepath.Join("/fake/grit/worktrees", agent)}, nil
}

func (f *FakeCoordinator) Done(_ context.Context, agent string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merged[agent] = true
	f.releaseLocked(agent)
	return nil
}

func (f *FakeCoordinator) Release(_ context.Context, agent string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseLocked(agent)
	return nil
}

func (f *FakeCoordinator) releaseLocked(agent string) {
	for _, sym := range f.claimed[agent] {
		if f.holders[sym] == agent {
			delete(f.holders, sym)
		}
	}
	delete(f.claimed, agent)
}

// Holder reports which agent holds a symbol, or "" if none. Test inspection.
func (f *FakeCoordinator) Holder(symbol string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.holders[symbol]
}

// Merged reports whether Done was called for an agent. Test inspection.
func (f *FakeCoordinator) Merged(agent string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.merged[agent]
}
