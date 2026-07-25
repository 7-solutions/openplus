# Change 0008 — Checkpoint loop and task tree (PLAN)

## Why
ADR-0002 names context reconstruction "the deepest subsystem; budget time
accordingly." It is the one milestone subsystem whose *promise* is unmet rather
than merely unexposed: `contextmgr.Checkpointer` is fully built and tested, and
`internal/runtime` never calls it. A long session still dies at the window limit.
Nothing writes `checkpoint.md`, nothing reconstructs from one, and `TaskTree` is
referenced nowhere outside its own package.

This is the first of three slices closing the MiMoCode milestone (audit: 1 of 9
subsystems fully reachable, 3 partial, 5 unreachable). It is deliberately first
because it is the only slice where the user-visible behavior is *wrong* today,
not just absent — the agent silently loses its own history.

## What changes
Extends capability `runtime`. No new subsystem, port, or adapter.

- `Session` gains a `Checkpointer` (nil disables the whole feature) and a
  `Tasks` tree.
- `AssembleContext` consumes `Checkpointer.Reconstruct` when a checkpoint exists,
  so the summary and active task enter context in ADR-0008 priority order
  instead of being rebuilt from scratch.
- `Session.Run` measures the assembled context, and when usage crosses the
  high-water mark writes a checkpoint after the turn completes.
- The task tree is persisted into `checkpoint.md` and restored from it, closing
  milestone subsystem #3 alongside #2.
- `runtime.Assemble` builds the Checkpointer from `config.Context.Window`.

## What this deliberately does not do
- **No compose / `/dream` / `/distill` / subagent surface.** Those are slices 2
  and 3. Bundling them would make this change unreviewable and is what stalled
  the milestone the first time.
- **No summarization model call.** The checkpoint summary is the retained
  transcript **verbatim, capped** — no model call, no editorial choice about
  what mattered. A digest that quietly drops the important line is precisely the
  failure mode this change is built to avoid; when the cap is hit the summary is
  truncated at a message boundary and says so, so the loss is visible rather than
  silent. Introducing a summarizer is a separate decision with its own cost and
  failure modes; the Checkpointer's contract does not require one.
- **No compaction of the live message history.** Checkpointing records state; it
  does not yet rewrite what the loop holds. That is a follow-on once the write
  and restore paths are proven.

## Governing decisions
ADR-0008 (checkpointer, budgeted injection, high-water mark) · ADR-0002
(milestone subsystems #2 and #3).

## Risk
The failure mode that matters is a checkpoint that *loses* work — a bad summary
or a dropped task tree is worse than no checkpoint, because the user believes
their context survived. Mitigations, all testable:
- Writing a checkpoint never mutates or truncates the live history in this
  change, so a bad write cannot destroy anything.
- A checkpoint write failure is reported, never swallowed: the operator must know
  the session is no longer durable.
- Restoring a corrupt or partial `checkpoint.md` degrades to "no checkpoint"
  rather than erroring the turn.
- The task tree round-trip is pinned by a test asserting the active task survives
  the boundary, since that is the field the Budgeter treats as highest priority.

## Verification
`ShouldCheckpoint` is driven by measured token usage, so the acceptance tests use
a deliberately tiny window to force a crossing without generating a huge
transcript. An end-to-end test runs two sessions against the same root: the first
crosses the mark and writes, the second reconstructs and proves the active task
and summary reach the assembled prompt.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks
are approved (house Gate 1).
