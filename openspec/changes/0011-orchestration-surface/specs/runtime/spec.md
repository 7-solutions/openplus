# Runtime Specification (delta — change 0011)

## Purpose
Reach the last two milestone subsystems: parallel subagents in isolated git
worktrees, and deterministic phase workflows. Completes the MiMoCode milestone.

## Requirements

### Requirement: Subagent fan-out
`/subagents` SHALL run each given prompt as a parallel subagent and merge the
results in input order (ADR-0002 #4).

#### Scenario: Results merge deterministically
- **WHEN** several prompts are fanned out and they finish in a different order
  than they were given
- **THEN** the reported results follow the input order, not completion order

#### Scenario: One failure does not lose the others
- **WHEN** one subagent fails
- **THEN** its failure is reported against its own task and the other results are
  still returned

#### Scenario: Every worktree is released
- **WHEN** a fan-out completes, whether or not individual tasks failed
- **THEN** no worktree directory is left behind

#### Scenario: Concurrency is bounded
- **WHEN** more prompts are given than the concurrency cap
- **THEN** at most the cap run at once

#### Scenario: Cost is stated before it is incurred
- **WHEN** a fan-out is requested
- **THEN** the number of subagents about to run is reported

#### Scenario: Too many tasks is refused
- **WHEN** more prompts are given than the task limit allows
- **THEN** the command refuses rather than launching them

#### Scenario: Fan-out needs at least one prompt
- **WHEN** `/subagents` is given no prompts
- **THEN** an error explains the usage

### Requirement: Subagents never wait on a prompt nobody sees
A subagent SHALL run under a permission gate that resolves from rules alone,
without interactive approval.

#### Scenario: An ask rule does not hang a subagent
- **WHEN** the session's rules would return Ask for a tool a subagent calls
- **THEN** the subagent's gate resolves it without waiting for a human

#### Scenario: Explicit denials still deny
- **WHEN** the session's rules deny a tool
- **THEN** a subagent calling it is still denied

### Requirement: Prompt phases
A workflow phase SHALL be able to run a prompt as an agent turn, passing its
output to the next phase (ADR-0006).

#### Scenario: Phases run in order
- **WHEN** a workflow of several prompt phases runs
- **THEN** each phase runs in the declared order

#### Scenario: Output hands off
- **WHEN** a phase completes
- **THEN** the next phase can read its output

#### Scenario: A failing phase exhausts its retry budget and reports
- **WHEN** a phase keeps failing
- **THEN** it is retried up to the budget, then the workflow fails with a report
  naming the phase

### Requirement: Workflow invocation
`/workflow <name>` SHALL run a registered workflow, and `/workflows` SHALL list
what is available.

#### Scenario: A registered workflow runs
- **WHEN** `/workflow` names a registered workflow
- **THEN** it runs and its report is returned

#### Scenario: An unknown workflow lists the known ones
- **WHEN** `/workflow` names something unregistered
- **THEN** the error lists the available workflows

#### Scenario: Listing is honest when empty
- **WHEN** `/workflows` runs and none are registered
- **THEN** it reports that rather than failing
