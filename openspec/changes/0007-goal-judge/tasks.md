# Change 0007 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## A — Goal/Judge fields on Session

- [x] T-440 RED `TestGoalAbsentSkipsJudge`: existing test `Run`
      continues to behave exactly as today when `Options.Goal` is
      empty and `Options.Judge` is nil — no judge consult, no extra
      turns. RED until `Options.Goal` / `Options.Judge` exist.
      Done — ee4ece9.
- [x] T-441 RED `TestGoalEmptyStopsImmediately`: when `Options.Goal`
      is empty but `Options.Judge` is non-nil, `Run` still
      short-circuits (existing `Judge.Evaluate` returns
      `Verdict{Met: true}` for empty goals; the runtime must
      inherit this behavior).
      Done — ee4ece9.
- [x] T-442 RED `TestGoalJudgeStopsLoopWhenMet`: a scripted agent
      produces a tool-less reply; the judge (scripted separately)
      returns MET. `Run` returns after one judge consult. History
      shows the assistant reply.
      Done — ee4ece9.
- [x] T-443 RED `TestGoalJudgeKeepsLoopingWhenUnmet`: agent wants to
      stop; judge returns UNMET the first time, MET the second. `Run`
      runs the agent loop twice and the judge twice; the second
      iteration's history includes the first iteration's feedback
      as a user message.
      Done — ee4ece9.
- [x] T-444 RED `TestGoalJudgeRespectsMaxIterations`: judge always
      returns UNMET; `Run` returns after `MaxJudgeIterations`
      rounds (default 3) with an error wrapping the last verdict's
      feedback. No infinite loop.
      Done — ee4ece9.
- [x] T-445 GREEN: extend `runtime.Session` with `Goal string`,
      `Judge *orchestrate.Judge`, and `MaxJudgeIterations int`
      fields. `Run` consults the judge after the agent loop returns
      (only when `Goal != ""`); UNMET appends feedback to history
      and loops; MaxIterations caps the loop and returns an error.
      Empty `Goal` short-circuits before any judge call.
      Done — ee4ece9.

## B — CLI wiring (`--goal` flag + `OPENPLUS_GOAL` env)

- [x] T-450 RED `TestMainGoalFlag`: subprocess test; `--goal 'foo'`
      reaches `Session.Goal`. Same subprocess pattern as
      `--config` (build binary into temp, exec, assert via
      `--fake -p 'foo'` smoke).
      Done — 4846b65.
- [x] T-451 RED `TestMainGoalEnvOverride`: `OPENPLUS_GOAL='foo'`
      wins over `--goal 'bar'` (env > flag precedence).
      Done — 4846b65.
- [x] T-452 GREEN: `cmd/openplus/main.go` adds `flag.StringVar(&goal,
      "goal", "", "stop the agent when this goal is met")`; applies
      `OPENPLUS_GOAL` env override after `flag.Parse`; passes
      through `runtime.Options.Goal`. Long form only — no short
      form (Go flag package panics on duplicate registration per
      the 0004 lesson).
      Done — 4846b65.

## C — Docs close

- [x] T-460 Flip T-440..T-452 to `[x]` with commit hashes in
      `openspec/changes/0007-goal-judge/tasks.md`.
      Done — (this commit).

## Verification (Gate 5 — before declaring 0007 done)
- [x] `go build ./...` clean.
- [x] `go test ./...` **22/22 + 7 new tests** green.
- [x] `go test ./internal/runtime/...` — old + new tests green.
- [x] `go run ./cmd/openplus --goal 'ship hello' --fake -C $(mktemp -d)
      -p 'ship hello'` exits 0; with no `--goal` it's identical to
      pre-0007 behavior (regression guard).
- [x] `OPENPLUS_GOAL='foo' go run ./cmd/openplus --goal 'bar' --fake -C
      $(mktemp -d) -p 'x'` — env wins (regression guard).

## Out of scope (per proposal)
Default judge model · `/orchestrate` command surface · provider
count endpoints · modifications to existing `Judge` type · anything
on the v1 refuse list.