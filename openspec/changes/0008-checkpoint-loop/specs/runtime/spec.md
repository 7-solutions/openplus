# Runtime Specification (delta — change 0008)

## Purpose
Wire the Checkpointer into the live session so long-horizon sessions survive the
context window, and carry the task tree across the boundary. Closes milestone
subsystems #2 (context reconstruction) and #3 (tree task tracking).

## Requirements

### Requirement: Checkpoint on high-water mark
The session SHALL write a checkpoint when the assembled context's measured token
usage reaches the configured high-water mark, and SHALL NOT write one otherwise.

#### Scenario: Usage below the mark writes nothing
- **WHEN** a turn's assembled context is well under the window
- **THEN** no `checkpoint.md` is written

#### Scenario: Usage crossing the mark writes a checkpoint
- **WHEN** a turn's assembled context reaches the high-water fraction of the
  window
- **THEN** `checkpoint.md` is written under the project root after the turn
  completes

#### Scenario: No window configured disables checkpointing
- **WHEN** no context window is configured
- **THEN** no checkpoint is ever written and turns run unchanged

#### Scenario: A write failure is surfaced
- **WHEN** the checkpoint cannot be written
- **THEN** the operator is informed that the session is no longer durable, rather
  than the failure being silently discarded

### Requirement: Reconstruction from a checkpoint
When a checkpoint exists, the session SHALL assemble context from it rather than
from the system prompt alone, preserving ADR-0008 priority order.

#### Scenario: Checkpoint summary reaches the prompt
- **WHEN** a session starts in a root holding a checkpoint with a summary
- **THEN** that summary appears in the assembled system prompt

#### Scenario: Active task is restored
- **WHEN** the checkpoint's task tree has an in-progress task
- **THEN** that task appears in the assembled context as the active task

#### Scenario: A corrupt checkpoint does not break the turn
- **WHEN** `checkpoint.md` is unreadable or malformed
- **THEN** the turn proceeds as if no checkpoint existed

#### Scenario: Live history outranks the checkpoint digest
- **WHEN** the caller supplies recent messages and a checkpoint also holds a
  digest of earlier ones
- **THEN** the live messages are used

### Requirement: Task tree survives the boundary
The session's task tree SHALL be written into the checkpoint and restored from it.

#### Scenario: Progress survives resume
- **WHEN** a session with a populated task tree checkpoints, and a later session
  starts from the same root
- **THEN** the task tree is restored with each task's status intact

#### Scenario: Task status is mutable across a checkpoint
- **WHEN** a task is marked done after a checkpoint, and a new checkpoint is
  written
- **THEN** the later checkpoint reflects the new status

### Requirement: Summary is verbatim and capped
The checkpoint summary SHALL be the retained transcript verbatim, bounded by a
character cap, with truncation happening at a message boundary and being visible
in the written summary.

#### Scenario: Short transcript is recorded whole
- **WHEN** the retained transcript is well under the cap
- **THEN** the summary contains every retained message unmodified

#### Scenario: Oversized transcript truncates visibly at a boundary
- **WHEN** the retained transcript exceeds the cap
- **THEN** the summary keeps the most recent whole messages that fit, and states
  that earlier material was dropped

### Requirement: Checkpointing never destroys live context
Writing a checkpoint SHALL NOT mutate or truncate the message history the session
holds.

#### Scenario: History intact after a checkpoint
- **WHEN** a turn triggers a checkpoint write
- **THEN** the history returned to the caller is the same as it would have been
  without the write
