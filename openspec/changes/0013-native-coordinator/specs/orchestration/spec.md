# Orchestration Specification (delta — change 0013)

## Purpose
Symbol coordination implemented natively in Go, so parallel subagents can be
coordinated without an external binary. A second adapter behind the `Coordinator`
port introduced in change 0012.

## Requirements

### Requirement: Go symbol indexing
The system SHALL extract claimable symbols from Go source with their locations.

#### Scenario: Functions, methods and types are found
- **WHEN** a Go file declares a function, a method with a receiver, and a type
- **THEN** all three are indexed with their file and line range

#### Scenario: A method is distinguishable from a function
- **WHEN** a type has a method whose name matches a package-level function
- **THEN** the two are distinct symbols

#### Scenario: Unparseable source is reported, not skipped
- **WHEN** a file does not parse
- **THEN** the error names the file rather than silently indexing nothing

#### Scenario: Non-Go files are rejected by name
- **WHEN** a claim names a symbol in a non-Go file
- **THEN** the error says the native coordinator is Go-only and points at grit for
  other languages

### Requirement: Atomic symbol locks
A symbol SHALL be held by at most one agent at a time, even under concurrent
claims.

#### Scenario: Exactly one winner under contention
- **WHEN** several agents claim the same symbol simultaneously
- **THEN** exactly one claim is granted and the rest are refused

#### Scenario: A refusal names the holder
- **WHEN** a claim is refused
- **THEN** the holder and the blocking symbol are reported

#### Scenario: All-or-nothing claims
- **WHEN** an agent claims several symbols and one is already held
- **THEN** none of them are locked, so a partial claim cannot leave the agent
  holding what it was refused

#### Scenario: A nonexistent symbol is refused
- **WHEN** a claim names a symbol that does not exist in the file
- **THEN** the claim fails, because granting a lock on nothing would let two
  agents both believe they had exclusive access

### Requirement: Stale locks are reclaimable
An expired lock SHALL not block work forever.

#### Scenario: An expired lock can be taken over
- **WHEN** a lock is older than the configured expiry and another agent claims it
- **THEN** the claim is granted

#### Scenario: A takeover is reported
- **WHEN** an expired lock is reclaimed
- **THEN** the takeover is visible, so a crashed agent's work is not silently
  overwritten without trace

#### Scenario: A live lock is never stolen
- **WHEN** a lock is within its expiry
- **THEN** a competing claim is refused

### Requirement: Release always happens
Locks SHALL be freed whether an agent succeeds or fails.

#### Scenario: Success releases
- **WHEN** an agent completes and merges
- **THEN** its locks are freed

#### Scenario: Failure releases
- **WHEN** an agent fails
- **THEN** its locks are freed and nothing is merged

#### Scenario: Releasing an unheld agent is harmless
- **WHEN** release is called for an agent holding nothing
- **THEN** it succeeds quietly

### Requirement: Disjoint edits both land
Two agents editing different symbols in one file SHALL both reach the base branch.

#### Scenario: Same file, different functions
- **WHEN** two agents claim and edit different functions in the same file, then
  both complete
- **THEN** both changes are present on the base branch

### Requirement: Coordinator selection
The coordinator SHALL be selectable, defaulting to the native one.

#### Scenario: Native by default
- **WHEN** nothing is configured
- **THEN** coordination is available without any external binary

#### Scenario: grit on request
- **WHEN** grit is configured
- **THEN** the grit adapter is used and reports unavailable if not installed
