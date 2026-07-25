# Change 0013 — Native symbol coordinator (PLAN)

## Why
Change 0012 wired grit behind a `Coordinator` port. grit is a Rust binary, so
coordinated fan-out only works where an operator has installed it — and it is
currently unexercised here for exactly that reason. This change implements the
same capability natively in Go, so symbol coordination ships with OpenPlus and the
cgo-free single binary keeps its promise.

grit stays supported: it is one adapter behind the port, and remains the better
choice for multi-machine coordination and for the ten languages this change does
not parse.

## What I verified before designing this
Two premises, both checked empirically rather than assumed:

1. **`go/parser` extracts symbols with line ranges, in stdlib, with no cgo.**
   Functions, methods (with receivers), and types all resolve. So for Go, symbol
   indexing is free.
2. **git already merges disjoint symbol edits in the same file cleanly** — when
   each agent's work is a separate commit, `ort` auto-merges two functions in one
   file without conflict. The same function edited twice does conflict.

The second finding reshapes the design, and is worth stating plainly: **the value
is the lock, not a merge algorithm.** grit's own numbers describe agents whose
work is thrown away by conflicting merges; the fix is preventing two agents from
claiming the same symbol, after which ordinary git merging suffices. This change
therefore implements locking and leaves merging to git.

## What changes
Adds capability `coordination`.

- `internal/symbols`: a Go symbol indexer over `go/parser` — file, symbol name,
  kind, line range. Go only.
- `internal/coordinate`: a file-backed lock store under `.openplus/locks/`, with
  atomic claim via `O_CREATE|O_EXCL` (the same primitive grit uses on Azure via
  `If-None-Match`). Holds agent, intent, symbols, timestamp.
- `NativeCoordinator` implementing the existing `orchestrate.Coordinator`: claim
  validates symbols exist, takes locks atomically, creates a worktree; done
  commits the worktree and merges it; release frees locks.
- Config picks the coordinator: native by default, grit when asked.

## What this deliberately does not do
- **No non-Go languages.** grit parses thirteen via tree-sitter, which is C and
  therefore cgo. Claiming multi-language support with a Go-only parser would be a
  lie in the release notes. A non-Go file is rejected by name with a pointer to
  grit.
- **No custom merge.** Verified above: git handles disjoint symbol edits. Writing
  a three-way symbol merger would be inventing a hard problem I just measured as
  already solved.
- **No cross-machine coordination.** File locks are single-machine. grit's Azure
  and S3 backends exist for teams; this is for one developer's parallel agents.
- **No dependency graph.** grit tracks symbol dependencies so a claim can warn
  about callers. Useful, and a separate change with its own design.

## Governing decisions
ADR-0006 (git-worktree parallelism) · ADR-0002 #4 · ADR-0001 (cgo-free is the
whole reason this exists natively rather than vendoring tree-sitter). The
`Coordinator` port from change 0012 is unchanged: this is a second adapter, which
is the point of having had a port.

## Risk
**A stale lock blocks work forever.** A crashed agent leaves its file behind, and
a coordinator that never forgets is worse than none. Locks therefore carry a
timestamp and a configurable expiry, and expired locks are reclaimable — with the
takeover reported, never silent.

**`done` commits and merges**, exactly as grit does, so the same warning applies:
coordinated mode writes history to the user's repository, is opt-in, and says so
before running.

**A lock that is taken but not released** on an unexpected path is the quiet
failure. Every claim path releases on failure, and this is asserted rather than
assumed.

**Symbol validation is a real check, not decoration.** Claiming
`auth.go::NoSuchFunc` must fail: silently granting a lock on a symbol that does
not exist would let two agents both "successfully" claim nothing and then collide.

## Verification
The indexer is testable against source strings. The lock store is testable for
atomicity by claiming the same symbol from parallel goroutines and asserting
exactly one winner. The coordinator is testable end to end on scratch repos, and
the merge behavior is verifiable against the case I measured: two agents, one
file, different functions, both landing.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks
are approved (house Gate 1).
