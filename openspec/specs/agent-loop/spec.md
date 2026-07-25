# Agent Loop Specification

## Purpose
The core turn loop: send messages, receive text + tool calls, gate and execute tools,
feed results back, repeat until the model stops requesting tools. Governed by ADR-0001,
ADR-0005, ADR-0007.

## Requirements

### Requirement: Tool-use iteration
The system SHALL iterate model turns until the assistant returns no tool calls,
appending each assistant message and each tool result to history in order.

#### Scenario: Model requests a tool
- **WHEN** the provider stream yields one or more tool calls in a turn
- **THEN** each call is passed to the permission gate, then executed if permitted
- **AND** a tool result (or denial) is appended to history keyed by the call id
- **AND** the loop starts another turn

#### Scenario: Model finishes
- **WHEN** a turn yields text and zero tool calls
- **THEN** the loop returns and the turn is considered complete

### Requirement: Provider neutrality
The loop SHALL operate only on the neutral domain model (Blocks/Events) and MUST NOT
reference any provider-specific type.

#### Scenario: Switching providers mid-session
- **WHEN** the active `provider/model` changes between turns
- **THEN** the loop continues unchanged using the newly selected adapter

### Requirement: Permission gate on every tool call
Every tool call SHALL pass through the `PolicyGate` before `Execute`.

#### Scenario: Denied destructive command
- **WHEN** a `bash` call matches a `deny` rule
- **THEN** it is not executed and a denial result is fed back for the model to adapt

#### Scenario: Forced-ask timeout
- **WHEN** a forced-ask operation receives no human decision within the timeout
- **THEN** it auto-rejects with feedback the model can act on, never hanging the loop

### Requirement: Streaming surfaced to the UI
Text and tool events SHALL stream to the UI incrementally while the loop runs in a
separate goroutine.

#### Scenario: Live token rendering
- **WHEN** the provider emits text deltas
- **THEN** they render in the TUI before the turn completes
