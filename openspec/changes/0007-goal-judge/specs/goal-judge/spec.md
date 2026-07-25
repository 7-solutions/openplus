# Goal-judge wired into runtime (delta — change 0007)

> Change 0007 wires the existing `internal/orchestrate.Judge` (from
> 0001/M7, ADR-0006) into `runtime.Session.Run` as the loop's
> termination condition. The Judge package itself is unchanged.

## Purpose
Provide an optional, independent "is the goal met?" verdict that
the agent loop consults before terminating. Without the judge, the
loop ends when the model says "I'm done" (which can be wrong in
either direction: premature or runaway). With the judge, the
project matches ADR-0006's goal / stop-condition spec.

## Requirements

### Requirement: Session exposes an optional Goal + Judge
`runtime.Session` SHALL accept an optional `Goal string` and `Judge
*orchestrate.Judge` set via `runtime.Options`. Empty `Goal` (or nil
`Judge`) preserves today's behavior — no judge is consulted.

#### Scenario: Empty goal short-circuits
- **WHEN** `Session.Goal` is empty (or `Judge` is nil)
- **THEN** `Run` returns when the agent's tool-call count hits zero,
  identical to the pre-0007 behavior
- **AND** no provider call is made to the judge

#### Scenario: Empty `Judge` provider is configured but Goal is empty
- **WHEN** `Options.Judge` is non-nil but `Options.Goal` is empty
- **THEN** the existing `Judge.Evaluate` short-circuits to
  `Verdict{Met: true}` and `Run` returns without a provider call
  (inherited from `goal.go:60`)

### Requirement: Judge consulted on every "agent wants to stop" turn
After the agent loop produces a tool-less assistant reply (i.e. the
agent thinks it's done), `Run` SHALL consult the judge before
returning.

#### Scenario: Judge says MET, agent stops
- **WHEN** the judge returns `Verdict{Met: true, Feedback: "..."}`
- **THEN** `Run` returns the history with the judge's feedback
  appended as a trailing assistant message (or not — see below)

#### Scenario: Judge says UNMET, agent continues
- **WHEN** the judge returns `Verdict{Met: false, Feedback: "..."}`
- **THEN** the feedback is appended to history as a user message
  and the agent loop runs again with the new feedback in context
- **AND** the next turn's stream picks up the feedback as the latest
  user input

#### Scenario: Max iterations bound
- **WHEN** the judge never returns MET and `Options.MaxJudgeIterations`
  is reached (default 3)
- **THEN** `Run` returns an error wrapping the last verdict's
  feedback, not an infinite loop
- **AND** the error is non-fatal to the persisted history — the
  caller can recover from it

### Requirement: --goal CLI flag + OPENPLUS_GOAL env override
`cmd/openplus` SHALL accept `--goal <text>` (long form only, no short
form). The env var `OPENPLUS_GOAL` overrides any flag value, matching
the established env > flag > precedence.

#### Scenario: --goal with text
- **WHEN** `openplus --goal 'ship hello world'` is invoked
- **THEN** `Session.Goal` is set to `'ship hello world'`

#### Scenario: OPENPLUS_GOAL env override
- **WHEN** `OPENPLUS_GOAL='foo' openplus --goal 'bar'` is invoked
- **THEN** `Session.Goal` is `'foo'` (env wins)

#### Scenario: No goal, no env
- **WHEN** neither flag nor env is set
- **THEN** `Session.Goal` is empty; behavior identical to pre-0007

### Requirement: Callers see no change for sessions without a Goal
Every existing caller of `runtime.Session.Run` that does not set
`Goal` keeps working without modification.

#### Scenario: Existing integration tests stay green
- **WHEN** `go test ./internal/runtime/...` is run
- **THEN** all pre-0007 integration tests (T-430 memory round-trip,
  T-431 permission deny, T-432 credential missing, T-433 --fake smoke)
  stay green
- **AND** the new goal-judge tests (T-440..T-444) are also green

## Out of scope (per proposal)
Default judge model · `/orchestrate` user-facing command · provider
count endpoints for the judge · modifications to the existing
`Judge` type · anything on the v1 refuse list.