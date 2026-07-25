# Runtime Specification (delta — change 0010)

## Purpose
Shrink the live message history after a checkpoint is safely written, so a long
session gets actual window relief rather than only a durable record.

## Requirements

### Requirement: Compaction follows a durable write
History SHALL be compacted only after a checkpoint write has succeeded.

#### Scenario: A failed write compacts nothing
- **WHEN** the high-water mark is crossed and the checkpoint write fails
- **THEN** the returned history is unchanged, so no material is lost that was
  never written down

#### Scenario: A successful write compacts
- **WHEN** the high-water mark is crossed and the checkpoint write succeeds
- **THEN** the returned history is shorter than the history the turn produced

#### Scenario: Below the mark nothing changes
- **WHEN** the assembled context is under the high-water mark
- **THEN** the returned history is exactly the turn's history

#### Scenario: No window configured behaves as before
- **WHEN** no context window is configured
- **THEN** no checkpoint is written and no compaction occurs

### Requirement: Compaction is visible
A compacted history SHALL carry a marker stating that compaction occurred and
naming where the dropped material went.

#### Scenario: The marker names the checkpoint
- **WHEN** history is compacted
- **THEN** the first message is a marker mentioning the checkpoint file

#### Scenario: The marker is not mistaken for conversation
- **WHEN** history is compacted
- **THEN** the marker is distinguishable from user and assistant content, so
  neither a reader nor the model treats it as something that was said

#### Scenario: Compaction is reported
- **WHEN** history is compacted
- **THEN** the session reports it, so a front-end can tell the user rather than
  the context shrinking invisibly

### Requirement: Recent turns survive
Compaction SHALL retain the most recent messages up to a bounded keep-count, so
immediate conversational context is never dropped.

#### Scenario: The newest messages are kept
- **WHEN** history is compacted
- **THEN** the most recent messages are present in the result, in order

#### Scenario: A short history is not compacted
- **WHEN** the history is already at or under the keep-count
- **THEN** it is returned unchanged, since there is nothing worth dropping

### Requirement: Mid-turn history is untouched
Compaction SHALL apply only between turns, not within one.

#### Scenario: The judge loop keeps full history
- **WHEN** a goal judge sends the agent round the loop again inside one Run
- **THEN** that inner loop sees the full history, uncompacted
