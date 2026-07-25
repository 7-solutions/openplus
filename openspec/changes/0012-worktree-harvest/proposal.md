# Change 0012 — Subagent merge via grit (PLAN)

## Why
Change 0011 gave subagents isolated git worktrees. Nothing collects what they
produce: `release` runs `git worktree remove --force`, which discards uncommitted
work by design. A subagent can be told to edit files, do it correctly, and have the
result deleted — the worst kind of failure, because the run *looks* like it
succeeded and the report even quotes the subagent saying so.

## Method: grit, not harvest-and-refuse
The first draft of this change proposed harvesting each worktree as a patch and
refusing any that conflicted. That is the pessimistic-*after* approach: let agents
collide, then throw one away. On the operator's instruction this change instead
uses **[grit](https://github.com/rtk-ai/grit)**, which inverts the order:

- **Claim first.** An agent locks the AST symbols (functions, types, methods) it
  intends to edit, *before* editing. A second agent asking for the same symbol is
  blocked; asking for a different symbol in the same file is granted.
- **Work in a worktree.** Same isolation model change 0011 already uses.
- **Done merges.** grit auto-commits, rebases on the base branch, and merges,
  serializing merges behind a file lock so `index.lock` races cannot happen.

The difference that matters: conflicts are **prevented at claim time** rather than
detected at merge time. Two subagents editing different functions in one file — the
case raw git fails on, and the case my draft would have refused — both succeed.

This is a better fit than what I specced, and the reason is worth recording: my
design's safety came from *declining to merge*, which protects the working tree by
throwing away agent work. grit's safety comes from *not producing conflicts*, which
protects both.

## The constraint grit imposes
grit is a **Rust binary**, not a Go library. Importing it is not an option: the
house rule is a cgo-free single static Go binary (ADR-0001), and vendoring a Rust
tree-sitter stack would break that outright.

So grit is an **external CLI behind a port**:
- `Coordinator` — the port. `Claim`, `Done`, `Release`, `Available`.
- `GritCoordinator` — the adapter, shelling out to `grit`.
- When `grit` is not on `PATH`, the coordinator reports unavailable and fan-out
  falls back to change 0011's behavior: isolated worktrees, text results, no file
  merge. OpenPlus must not require a Rust toolchain to run.

## What changes
Extends capability `orchestration`.

- `internal/orchestrate`: the `Coordinator` port plus the grit adapter.
- Fan-out gains an opt-in coordinated mode: claim symbols, run in grit's worktree,
  `grit done` to merge.
- A subagent whose claim is **blocked** is reported as blocked with the holder,
  and does not run — better than running it and discarding the result.
- Uncoordinated fan-out is unchanged and remains the default.

## What this deliberately does not do
- **No reimplementation of grit.** No AST parsing, no symbol locking, no merge
  logic in this repo. The adapter shells out; grit owns the hard part.
- **No bundling of the grit binary.** Installation is the operator's, exactly like
  `git`.
- **No non-local backends.** grit supports Azure and S3 for multi-machine
  coordination; this change wires the default local SQLite backend only. Remote
  backends are configuration, and belong to whoever needs them.
- **No symbol inference.** Which symbols a subagent intends to edit is stated by
  the caller, not guessed from the prompt. Guessing wrong would claim the wrong
  locks and block the wrong agents.

## Governing decisions
ADR-0006 (git-worktree parallelism) · ADR-0002 #4 ("results merge back
deterministically" — this is the file half; 0011 delivered the text half) ·
ADR-0001 (cgo-free: grit is therefore an external CLI, not a dependency).

## Risk
**grit writes to the repository.** `grit done` commits and merges — so unlike
everything else in this codebase, a coordinated fan-out produces commits on the
user's branch. That is grit's model, not something to paper over, so:
- Coordinated mode is opt-in per invocation, never a default.
- The base branch grit merges into is reported before running.
- A blocked claim is reported rather than silently downgraded to uncoordinated,
  since the whole point is that the agent should not have edited that symbol.

**A missing binary must degrade, not fail.** OpenPlus is a Go binary that must run
with no Rust installed; `Available()` is checked before any coordinated path.

**Version skew.** grit's CLI is young. The adapter pins nothing and parses as
little as possible — exit status and stderr, not structured output — so a flag
change degrades to a reported error rather than a misparse.

## Verification
The port is testable with a fake coordinator: claim granted, claim blocked, done
merges, unavailable falls back. The grit adapter itself is only testable where the
binary exists, so those tests skip when `grit` is absent — the same pattern the
worktree tests already use for `git`.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks
are approved (house Gate 1).
