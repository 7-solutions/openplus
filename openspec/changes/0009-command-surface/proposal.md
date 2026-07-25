# Change 0009 — Command surface (PLAN)

## Why
Four milestone subsystems are built, tested, and unreachable: `internal/compose`
and `internal/improve` are imported by nobody at all, `internal/memo` (the
`MEMORY.md` / notes / task-progress file memory named in ADR-0002 #1) is imported
by nobody, and `skills` has auto-load but not the `/skill` invocation ADR-0002 #8
names explicitly. A user cannot reach any of them.

This is slice 2 of 3 (slice 1 was change 0008). It adds the surface; the engines
already exist and are not touched.

## What changes
Extends capability `runtime` with a command dispatcher, plus the commands that
close the four gaps.

- `internal/runtime`: a `Command` dispatcher. Input beginning with `/` is a
  command; anything else is a normal turn, exactly as today.
- `/skill <name>` — explicit invocation (ADR-0002 #8), via `skills.Index.Find`.
- `/skills` — list what is discoverable, so a user can find out what to invoke.
- `/compose <feature>` and the phase verbs that drive `compose.Session` through
  grill → spec → implement → verify → review → finish (ADR-0002 #6).
- `/dream` — `improve.Dreamer.Extract` over the session transcript, with the
  extracted facts appended to `MEMORY.md` via `memo.Files.AppendMemory`. This
  closes ADR-0002 #9 and the file-memory half of #1 in one move, because the
  natural sink for extracted facts *is* the file memory.
- `/distill` — `improve.MinePatterns` over recorded runs, scaffolding via
  `ScaffoldSkill` / `ScaffoldCommand` / `ScaffoldSubagent`.

## Design constraint
The dispatcher shape is inherited by every command added later, so it is fixed
here deliberately: a command is `name + args + a func on *Session returning
(string, error)`. Registration is a map, so adding a command is one entry and
cannot alter dispatch behavior for the others. Commands return text rather than
printing, so the same command works in the TUI and the one-shot path without
either knowing about the other.

## What this deliberately does not do
- **No subagent or workflow surface.** That is slice 3 (`orchestrate.Runner`,
  `WorktreeIsolator`, `Workflow`). Bundling it here is what stalled the milestone
  after change 0002.
- **No compose persistence across processes.** `compose.Session` is in-memory, so
  a phase machine lives for one process. Persisting it is a follow-on; specifying
  it here would mean inventing a state file this change does not need.
- **No new engine behavior.** If a command wants a capability the engine lacks,
  that is a signal to stop and spec the engine change separately.

## Governing decisions
ADR-0002 (#1 file memory, #6 compose, #8 `/skill`, #9 `/dream` + `/distill`) ·
ADR-0001 (OpenCode config compatibility: `.opencode/command/*.md` is the existing
convention, so generated commands land there).

## Risk
Two failure modes matter.

**A command that silently does nothing** is worse than an absent one, because the
user believes it worked. Every command returns either a description of what it
changed or an error naming what was missing — never empty success.

**`/dream` writes to `MEMORY.md`,** a file the user owns and may have hand-edited.
It only ever appends, never rewrites, and it reports how many facts it added, so a
surprising result is visible and recoverable with `git diff`.

## Verification
Commands are pure functions of `*Session` plus args, so they test directly. The
end-to-end checks that matter: `/skill` on a discovered skill returns its body;
`/dream` against a fake provider appends to a real `MEMORY.md` on disk;
`/distill` writes a scaffold the `skills.Index` then discovers; an unknown
command returns a usable error listing the known ones.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks
are approved (house Gate 1).
