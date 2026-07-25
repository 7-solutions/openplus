# Orchestration Specification (delta — change 0014)

## Purpose
A goja-backed adapter that compiles a `.js` workflow file into the existing
`orchestrate.Workflow`, firing the ADR-0006 deferral trigger. The engine, the
`Phase`/`Workflow` port, and the report shape are unchanged; this adds an adapter and a
load path, not a second engine.

## Requirements

### Requirement: A JS source compiles to a Workflow
The system SHALL turn a CommonJS-shaped JS source into an `orchestrate.Workflow` whose
phase names and retry budget come from the source.

#### Scenario: A valid source yields its declared phases
- **WHEN** a source exports `{ name, phases: [{name, run}] }`
- **THEN** the returned Workflow has those phases in order, named as declared

#### Scenario: maxRetries is honored
- **WHEN** a source sets `maxRetries: N`
- **THEN** the Workflow's MaxRetries is N, and a failing phase retries N extra times

#### Scenario: A malformed source errors, naming the defect
- **WHEN** a source lacks `phases`, or a phase lacks a function `run`, or the script
  throws at load time
- **THEN** Compile returns an error describing what is missing or wrong, and never
  returns an empty Workflow

### Requirement: A JS phase threads the engine's State
Each JS phase SHALL run against a `state` shim over `orchestrate.State`, so hand-off
works identically to a Go phase.

#### Scenario: Output and hand-off round-trip
- **WHEN** a phase returns a string and calls `state.set`, and a later phase calls
  `state.get` and reads `state.last`
- **THEN** the later phase sees the value set and the prior phase's output

#### Scenario: A thrown error is a phase failure
- **WHEN** a JS phase throws
- **THEN** the phase Run returns that error and the engine retries it per MaxRetries,
  exactly as for a Go phase

### Requirement: Cancellation aborts a running phase
A cancelled context SHALL interrupt JS execution mid-phase, not only between phases.

#### Scenario: An infinite loop is interruptible
- **WHEN** a JS phase runs `while(true){}` and its context is cancelled
- **THEN** the phase Run returns a cancellation error promptly rather than hanging

### Requirement: The JS sandbox has no host I/O
The adapter SHALL bind only `module`, `exports`, `state`, and `console.log`. No
filesystem, network, `require`, `process`, or `fetch`.

#### Scenario: Host globals are undefined
- **WHEN** a JS phase references `require`, `process`, or `fetch`
- **THEN** it raises a ReferenceError rather than reaching the host

### Requirement: A JS workflow loads through the runtime surface
`/workflow load <path>` SHALL compile a file and register it by its declared name, after
which `/workflow run <name>` runs it through the unchanged execution path.

#### Scenario: Load then run
- **WHEN** `/workflow load example.js` succeeds and then `/workflow run <declared-name>`
  is invoked
- **THEN** the workflow executes and its Report is returned, identical in shape to a
  Go-native workflow's Report
