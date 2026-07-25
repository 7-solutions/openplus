# Change 0010 — Context compaction (PLAN)

## Why
Change 0008 made checkpointing work: at the high-water mark the session writes
`checkpoint.md` and a later session reconstructs from it. What it deliberately did
not do is shrink what the live loop holds. So within one long session the window
still fills — the checkpoint is bookkeeping, not relief. ADR-0008's promise ("long
sessions survive window limits without losing the current task") is only half
delivered.

Compaction is the other half: after a checkpoint is safely on disk, replace the
older messages the loop is carrying with a reference to that checkpoint, so the
next turn has room.

## The invariant this changes
Change 0008 pinned a deliberate property (T-822): the history `Run` returns is
byte-identical whether or not a checkpoint was written. Compaction **breaks that
on purpose** — shrinking the history is the entire point. This is the one place in
the codebase where the safety property must be relaxed, so the relaxation is
narrow and explicit:

- Compaction happens **only after** a checkpoint write has succeeded. If the write
  fails, nothing is dropped. Durability strictly precedes forgetting.
- The 0008 test is not deleted. It is re-scoped to "no checkpoint, no change",
  which is still the guarantee that matters when the feature is off.
- Compaction is off unless a window is configured, exactly like checkpointing.

## What changes
Extends capability `runtime`.

- After a successful checkpoint write, `Run` returns a compacted history: a
  synthetic message pointing at `checkpoint.md` plus the most recent turns.
- How many recent turns survive is bounded by a keep-count, so compaction is
  predictable rather than dependent on token estimates a second time.
- The synthetic message is unmistakably not user or model output — a reader (and
  the model) must be able to tell that material was compacted away, and where it
  went.

## What this deliberately does not do
- **No summarization model call.** Same reasoning as 0008: the checkpoint already
  holds the transcript verbatim, and a summarizer introduces a cost and a failure
  mode this does not need. The compacted history points at that file rather than
  paraphrasing it.
- **No compaction without a checkpoint.** Dropping messages that were never
  written down is data loss, not compaction.
- **No mid-turn compaction.** The judge loop inside a single `Run` keeps its full
  history; compaction applies to what is carried *between* turns.
- **No subagent or workflow surface.** Still slice 3.

## Governing decisions
ADR-0008 (checkpoint + reconstruction; the high-water mark is already the trigger).

## Risk
The failure mode is silent context loss — the user asks about something from
earlier and the model no longer has it, with no indication why. Mitigations:
- Compaction only ever follows a durable write, so the dropped material is
  recoverable from `checkpoint.md` by hand.
- The synthetic marker states that compaction happened and names the file, so
  both the model and a human reading the transcript can see it.
- A keep-count floor means the immediate conversational context is never
  compacted away, only older material.
- Reported through the same `OnCheckpointError`-style hook so a front-end can
  surface it, rather than happening invisibly.

## Verification
The property that matters is ordering: a failed checkpoint write must leave
history untouched. That is directly testable by making the write fail and
asserting the returned history is unchanged. Beyond that: crossing the mark
shrinks the history, the marker names the checkpoint, the newest turns survive,
and a session with no window configured is byte-identical to today.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks
are approved (house Gate 1).
