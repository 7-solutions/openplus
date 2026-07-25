# Change 0011 — Orchestration surface (PLAN)

## Why
The last two milestone subsystems are unreachable: `orchestrate.Runner` +
`WorktreeIsolator` (ADR-0002 #4, parallel subagents in git worktrees) and
`orchestrate.Workflow` (#7, deterministic phases with bounded retries). Both are
built and tested; nothing outside their own tests imports them. This is slice 3
of 3, and it completes the MiMoCode milestone.

## The one thing that is not wiring
Slices 1 and 2 were pure surface work. This one is not, and the spec says so
plainly: **`Phase` is an interface with no production implementation.** Only test
fakes satisfy it. So "expose workflows" cannot mean routing to an existing engine
behavior — something has to actually *be* a phase.

The narrowest honest answer: one concrete phase type that runs a **prompt** as an
agent turn. That makes a workflow an ordered list of prompts with bounded retries
and structured hand-off, which is exactly what ADR-0006 describes and nothing
more. ADR-0006 also names four built-in workflows (`compose`, `deep-research`,
`fact-check`, `research-experiment`); this change ships the phase type and a
single built-in to prove it, and leaves the other three to a later change rather
than inventing three workflows nobody has asked for yet.

## What changes
Extends capability `runtime`.

- `/subagents <task>…` — fan out prompts as parallel subagents, each in its own
  git worktree, results merged in input order.
- `promptPhase` — the first concrete `orchestrate.Phase`: runs a prompt as an
  agent turn and hands its output to the next phase via `State`.
- `/workflow <name>` — run a registered workflow; `/workflows` lists them.
- One built-in workflow, assembled from `promptPhase`, so the engine is exercised
  end to end rather than only unit-tested.

## What this deliberately does not do
- **No three more built-in workflows.** ADR-0006 names four; inventing the other
  three here would be speculative content, not integration. They land when someone
  wants them.
- **No nested orchestration.** A subagent cannot itself fan out. That is a
  recursion-and-resource question with its own failure modes.
- **No cross-worktree merge strategy.** Worktrees isolate; results merge as
  *text*, in input order. Merging file edits from parallel worktrees back into the
  primary checkout is a separate, much harder change.
- **No compose persistence.** Still deferred (0009).

## Governing decisions
ADR-0006 (Go-native phases, bounded retries, git-worktree parallelism) ·
ADR-0002 (#4 subagents, #7 workflows).

## Risk
Two real hazards, both about resources rather than correctness.

**Worktrees are real directories and real disk.** A fan-out that fails midway must
not leave them behind. The `Runner` already releases each worktree even when its
task errors; this change must not introduce a path that bypasses that, and the
`--fake` path must not create worktrees at all.

**Parallel subagents multiply cost.** Each is a full agent turn against the
provider. The fan-out is therefore bounded by an explicit concurrency cap and a
task-count limit, and the command reports how many it is about to run so a user
who typo'd a long line finds out before paying for it.

A third, quieter hazard: a subagent inherits the session's permission gate. A
`Prompting` gate would block waiting for approval nobody is watching. Subagents
therefore run with a gate that never asks — it allows or denies from the rules
alone — which is stated in the spec rather than left to be discovered.

## Verification
Fan-out is testable offline with the fake provider: results merge in input order,
a failing subagent does not lose its siblings, every worktree is released, and the
concurrency cap holds. The workflow path is testable through the built-in: phases
run in order, a phase's output reaches the next through `State`, and a phase that
keeps failing exhausts its retry budget and reports.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks
are approved (house Gate 1).
