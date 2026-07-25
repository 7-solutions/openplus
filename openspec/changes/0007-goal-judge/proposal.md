# Change 0007 — Goal-judge wired into the runtime (PLAN)

## Why
Change 0001/M7 shipped `internal/orchestrate.Judge` as the
"goal / stop condition with independent judge model" subsystem (ADR-0006
+ the orchestration spec). The `Judge` type, its `Evaluate` method, and
its `Verdict{Met, Feedback}` parse logic are all in place — proven by
`internal/orchestrate/goal_test.go` (3 tests, all green since 0001).

But the agent loop never calls the judge. `internal/agent/loop.go:55`
terminates a turn when `len(calls) == 0`:

```go
if len(calls) == 0 {
    return history, nil // model is done for this turn
}
```

That's "the model said it's done", not "the goal is met". Without the
judge, a model that hallucinates "done" ends the session prematurely
(missing work), and a model that keeps issuing tool calls has no
off-ramp (infinite loops). The judge is the independent safety net
the spec calls for.

This change wires `Judge.Evaluate` into `runtime.Session.Run` so every
turn that wants to stop consults the judge. The session gains:
- An optional `Goal` field on `Session` (empty = no judge consulted,
  backwards compatible).
- An optional `Judge` field on `Session` carrying provider + model.
- After the agent loop produces a tool-less assistant reply, an
  optional judge check: `Met → return`, `UNMet → append feedback and
  loop again`.

A guardrail: the judge is consulted **only when the agent wants to
stop** (i.e. `len(calls) == 0`). The agent's own tool calls drive
iteration; the judge breaks the loop. That matches the spec's
"goal / stop condition" intent.

## What changes
Adds capability `runtime.goal-judge` — the seam between the agent
loop and the existing `Judge` subsystem.

- `internal/runtime/session.go`: extend `Session` struct with
  optional `Goal string` and `Judge *orchestrate.Judge` fields. Empty
  `Goal` (or nil `Judge`) preserves today's behavior — no judge is
  consulted, no behavior change for callers that don't set a goal.
- `internal/runtime/session.go`: `Run` consults the judge after the
  agent loop returns. If the judge says `UNMet`, the feedback is
  appended to history as a user message and the loop runs again with
  a bound (`MaxJudgeIterations`, default 3) so a runaway model can't
  loop forever against an unsatisfiable goal.
- `internal/runtime/session.go`: the goal + judge can be set via
  `Options` for parity with `Options.ConfigPath` (the explicit-path
  pattern from 0004 sub-scope C). `Options.Goal string` and
  `Options.Judge *orchestrate.Judge`.
- `cmd/openplus/main.go`: a new `--goal <text>` flag (long form only,
  consistent with `--config`) plus an optional env override
  `OPENPLUS_GOAL`. The CLI doesn't wire a judge by default — that's a
  separate decision (which model? which prompt?) deferred to the user
  in their `opencode.json`. Out of scope: a default judge.
- Tests in `internal/runtime/integration_test.go` (extend existing
  file, no new file):
  - `TestGoalJudgeStopsLoopWhenMet`: goal met → `Run` returns
    cleanly, judge consulted once.
  - `TestGoalJudgeKeepsLoopingWhenUnmet`: first judge replies UNMET,
    second MET → two iterations, both judge calls recorded.
  - `TestGoalJudgeRespectsMaxIterations`: judge never says MET →
    `Run` returns after `MaxJudgeIterations` rounds with a clear
    error / wrap, not an infinite loop.
  - `TestGoalAbsentSkipsJudge`: empty goal → behavior unchanged from
    today (regression guard).
  - `TestGoalEmptyStopsImmediately`: per Judge.Evaluate's existing
    behavior (empty goal short-circuits to Met=true without a
    provider call). Regression guard.

## Why this is a separate change (not just a fix-up commit)
Two reasons:
- It introduces a new optional input on `Session` (Goal + Judge).
  That's a public-surface delta; per the house gate, anything that
  widens a public surface goes through a change.
- It introduces a new CLI flag (`--goal`). The user has expressed
  care about CLI surface hardening in 0004 sub-scope C; adding a new
  flag belongs in its own change with its own Gate 1 review.

## Non-goals (explicitly out of scope)
- **A default judge model.** Wiring a default `Judge{Provider,
  Model}` would commit to a specific OpenAI / Anthropic model that
  the user might not want. The CLI only accepts an optional
  `OPENPLUS_GOAL` text; the judge model is configured by the user in
  `opencode.json` (future change, separate ADR).
- **`/orchestrate` command surface.** The judge is a runtime
  building block; the user-facing `/orchestrate <goal>` command is a
  separate change.
- **Provider count endpoints for the judge.** The judge uses the
  same `provider.Provider` port as everything else; per-provider
  token accounting lives in 0005.
- **Anything on the v1 refuse list** (voice/ASR, Max Mode, MCP
  marketplace, web/share UI, hosted server, goja `.js` workflows).
- **Modifying the existing `Judge` type or its prompts.** ADR-0006
  pins the judge semantics; this change only consumes the existing
  type. A future change that improves the judge (e.g. multi-judge
  consensus, partial-credit scoring) is a separate change with its
  own ADR.

## Governing decisions
- ADR-0006 (Go-native workflow runtime, defer goja JS)
- ADR-0001 (Crush base, config compatibility)
- ADR-0002 (MiMoCode feature milestone — T-072 is on the milestone
  checklist)

No new ADRs.

## Risk
- **Judge provider is expensive.** Each judge call is a separate
  model call; a long session with many UNMET rounds could cost a lot.
  Mitigation: `MaxJudgeIterations` (default 3) caps the loop; the
  proposal exposes this via `Options.MaxJudgeIterations` so callers
  can tune.
- **Judge returns ambiguous verdict (parse failure).** The existing
  `parseVerdict` treats ambiguity as UNMET — that's already the
  safe default (the agent keeps working). The new code path inherits
  this; nothing to add.
- **Judge consult on every "model is done" turn costs latency.**
  Mitigated by the empty-`Goal` short-circuit: callers that don't
  set a goal never invoke the judge. The CLI default is no goal.
- **New CLI flag collides with another short form.** Plan uses long
  form only (`--goal`), per the 0004 sub-scope C lesson about Go's
  flag package panicking on duplicate registration.

## Verification
1. `go build ./...` clean.
2. `go test ./...` **22/22 + the new 5 integration tests** green.
3. `go test ./internal/runtime/...` — the new integration tests green.
4. `go run ./cmd/openplus --goal 'ship a hello world' --fake -C
   $(mktemp -d) -p 'ship a hello world'` — exits 0; with an empty goal
   it's identical to today's behavior (regression guard).
5. `OPENPLUS_GOAL='ship' go run ./cmd/openplus --fake -C $(mktemp -d)
   -p 'ship'` — env override path works without `--goal`.

## Approval
Per house Gate 1, implementation begins only after this proposal +
the delta spec + tasks are approved.