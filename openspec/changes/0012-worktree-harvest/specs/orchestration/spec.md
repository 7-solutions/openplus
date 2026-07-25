# Orchestration Specification (delta — change 0012)

## Purpose
Merge subagent file changes back to the codebase using grit's claim→work→done
model, so parallel subagents editing different symbols in the same file both
succeed. Completes the file half of ADR-0002 #4.

## Requirements

### Requirement: Coordinator port
Symbol coordination SHALL sit behind a port, with grit as one adapter, so the
core never shells out directly and an absent binary is a reportable state rather
than a crash.

#### Scenario: Coordination is available
- **WHEN** the coordinator reports itself available
- **THEN** coordinated fan-out may claim symbols

#### Scenario: A missing binary degrades rather than fails
- **WHEN** the grit binary is not installed
- **THEN** the coordinator reports unavailable and fan-out runs uncoordinated,
  exactly as it did before this change

#### Scenario: OpenPlus needs no Rust toolchain
- **WHEN** OpenPlus is built and run
- **THEN** it builds cgo-free and runs without grit present

### Requirement: Claim before editing
A coordinated subagent SHALL claim the symbols it intends to edit before it runs.

#### Scenario: A granted claim yields a worktree
- **WHEN** a subagent claims symbols nobody holds
- **THEN** the claim is granted and the subagent runs in its coordinated worktree

#### Scenario: Different symbols in one file do not conflict
- **WHEN** two subagents claim different symbols in the same file
- **THEN** both claims are granted

#### Scenario: A blocked claim does not run the subagent
- **WHEN** a subagent claims a symbol another agent holds
- **THEN** the claim is refused, the subagent does not run, and the report names
  the blocked symbol

#### Scenario: Symbols come from the caller
- **WHEN** a coordinated fan-out is requested
- **THEN** the symbols to claim are those the caller stated, never inferred from
  the prompt text

### Requirement: Done merges and releases
Completing a coordinated subagent SHALL merge its work and release its locks.

#### Scenario: Work reaches the codebase
- **WHEN** a coordinated subagent finishes successfully
- **THEN** its changes are merged and its locks released

#### Scenario: Locks are released even on failure
- **WHEN** a coordinated subagent fails
- **THEN** its locks are released, so a failure cannot block later agents forever

#### Scenario: Merging is reported
- **WHEN** a coordinated fan-out completes
- **THEN** the report says which subagents merged, which were blocked, and which
  failed

### Requirement: Coordination is opt-in
Coordinated mode SHALL be off unless requested.

#### Scenario: Default fan-out is unchanged
- **WHEN** a fan-out does not request coordination
- **THEN** no claim is made, nothing is merged, and behavior matches change 0011

#### Scenario: The destination is stated before writing
- **WHEN** a coordinated fan-out starts
- **THEN** the report states that it will commit and merge, since grit writes to
  the repository
