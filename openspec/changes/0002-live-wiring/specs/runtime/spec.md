# Runtime Specification (delta — change 0002)

## Purpose
One composition root that assembles the subsystems built in change 0001 into a
live session. The runtime wires ports to adapters and assembles per-turn
context; it owns no behavior of its own.

## Requirements

### Requirement: Composition root
The system SHALL assemble a session from a project root, resolving config,
instructions, provider adapter, tools, policy gate, memory, skills, and the
context budgeter from what is configured — and no more.

#### Scenario: Adapter selected from config
- **WHEN** `opencode.json` sets `model` to `<provider>/<model>` and configures
  that provider
- **THEN** the session's Provider is the adapter for that prefix, carrying the
  provider's resolved `baseURL` and `apiKey`

#### Scenario: Missing credential fails clearly
- **WHEN** the configured provider has no resolvable API key
- **THEN** assembly fails with an error naming the provider, rather than
  panicking or silently sending an unauthenticated request

#### Scenario: Optional subsystems stay absent
- **WHEN** no embedder is configured
- **THEN** the session assembles without a memory store and turns still run

### Requirement: Project instructions in the system prompt
The assembled session SHALL prepend the base system prompt to the project
instructions loaded from the configured instruction files.

#### Scenario: AGENTS.md reaches the model
- **WHEN** the project has an `AGENTS.md`
- **THEN** its content appears in the session's system prompt, after the base
  prompt

### Requirement: Per-turn context assembly
Before each turn the session SHALL retrieve relevant memory, auto-load relevant
skills, and budget the assembled context in ADR-0008 priority order.

#### Scenario: Retrieved memory is injected
- **WHEN** memory holds a chunk relevant to the user's message
- **THEN** that chunk appears in the assembled context for the turn

#### Scenario: Relevant skill auto-loads
- **WHEN** a skill's description clears the auto-load threshold for the user's
  message
- **THEN** the skill's instructions appear in the assembled context

#### Scenario: Budget is respected
- **WHEN** the assembled context would exceed the configured budget
- **THEN** lower-priority sections are dropped and the system prompt is retained

### Requirement: Permission rules from config
The session's policy gate SHALL be built from `opencode.json` permission rules,
with `--dangerously-skip-permissions` replacing the base decision.

#### Scenario: Configured ask rule prompts
- **WHEN** `permission.bash` is `ask` and the model calls `bash`
- **THEN** the gate returns Ask so the front-end can prompt

#### Scenario: Skip flag keeps explicit denials
- **WHEN** `--dangerously-skip-permissions` is set and a rule denies a tool
- **THEN** that tool is still denied

### Requirement: Offline escape hatch
The binary SHALL run without any credential when explicitly asked, for smoke
testing.

#### Scenario: Fake provider on request
- **WHEN** the operator passes `--fake`
- **THEN** the session uses the scripted fake provider and needs no API key
