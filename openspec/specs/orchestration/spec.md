# Orchestration Specification

## Purpose
Subagents, deterministic workflows, goal-driven stopping, and the task tree.
Governed by ADR-0002, ADR-0006.

## Requirements

### Requirement: Subagents
The primary agent SHALL create subagents that share session context, run in parallel,
and are individually cancellable.

#### Scenario: Parallel isolated work
- **WHEN** independent tasks fan out
- **THEN** each runs in its own git worktree and results merge back deterministically

### Requirement: Deterministic workflows (Go-native)
Workflows SHALL be Go-native phase definitions with bounded retries and structured
hand-off; JS (`goja`) compatibility is deferred behind a port.

#### Scenario: Bounded retry
- **WHEN** a phase fails under its retry budget
- **THEN** it retries; when the budget is exhausted the workflow fails with a report

### Requirement: Goal / stop condition
`/goal` SHALL set a stop condition evaluated by an independent judge model before the
agent is allowed to stop.

#### Scenario: Premature stop rejected
- **WHEN** the agent tries to stop but the judge finds the goal unmet
- **THEN** the agent continues with the judge's feedback

### Requirement: Task tree tied to checkpoints
Tasks SHALL form a tree (T1, T1.1 …) whose progress is preserved across checkpoints.

#### Scenario: Progress survives resume
- **WHEN** a session resumes
- **THEN** the task tree and per-task progress are restored from the latest checkpoint
