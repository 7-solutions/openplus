# Compose Specification

## Purpose
Structured spec→ship workflow enforcing the house gates. Governed by ADR-0002, ADR-0006.

## Requirements

### Requirement: Phase sequence
Compose SHALL run grill → spec → implement → verify → review → finish, writing feature
documents under `docs/compose/spec/<feature>.md`.

#### Scenario: Spec gate blocks implementation
- **WHEN** the spec phase has no approved output
- **THEN** the implement phase does not start

### Requirement: TDD per task
Each implementable task SHALL require a failing test before production code.

#### Scenario: Red before green
- **WHEN** a task enters implement
- **THEN** a failing test is written and shown red before any production code

### Requirement: Review gate
The review phase SHALL run an Advisor pass and require all findings resolved before finish.

#### Scenario: Unresolved finding blocks finish
- **WHEN** the review phase reports an open finding
- **THEN** finish is blocked until it is resolved
