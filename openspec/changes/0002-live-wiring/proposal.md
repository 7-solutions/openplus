# Change 0002 — Live wiring (PLAN)

## Why
Change 0001 built and tested every subsystem, but nothing connects them in the
running binary. `cmd/openplus` still constructs `provider.Fake` and an
`AllowAll`-adjacent gate by hand, so the agent cannot talk to a real model, has
no memory, loads no skills, and never budgets its context. The parts are all
green in isolation and dead in production.

This change is an **integration slice**, not new subsystems. It adds one
composition root that assembles what already exists, and the smallest surface
needed to see it work end to end.

## What changes
Adds capability `runtime` — the composition root and the live session loop.

- `internal/runtime`: builds an assembled session from a project root —
  config + AGENTS.md instructions (T-003) → provider adapter by prefix (T-014)
  → tool registry → policy gate from `opencode.json` permissions (T-022) →
  memory store + embedder when configured (M4) → skill index (M5) →
  tokenizer/budgeter (M6).
- `internal/runtime`: per-turn context assembly — retrieve memory, auto-load
  skills, budget the result, hand the assembled system prompt to the loop.
- `cmd/openplus`: replace the hand-built Fake wiring with the runtime. Keep an
  explicit `--fake` escape hatch so the smoke path still runs with no API key.

Existing behavior removed: the hard-coded `demoProvider()` scaffold becomes
opt-in rather than the only option.

## Non-goals (explicitly out of scope)
- No new subsystem, port, or adapter. If this change wants one, that is a
  signal to stop and write its own spec.
- No compose/orchestrate/improve wiring. Those are user-facing commands
  (`/compose`, `/dream`, `/distill`) and belong to a later change once the base
  session is live.
- Nothing from the v1 refuse list.

## Governing decisions
ADR-0001 (Crush base, config compatibility) · ADR-0003 (memory) · ADR-0004
(embedder) · ADR-0005 (provider selection by prefix) · ADR-0007 (permission
gate) · ADR-0008 (budgeted injection order).

## Risk
The one real risk is the composition root becoming a god object. Mitigation:
`runtime.Session` only *assembles* — every behavior stays in the subsystem that
owns it, and the runtime holds ports, never concrete adapters.

## Verification
A live turn is only observable with a real endpoint, so the acceptance tests
drive the runtime through the `provider.Fake` and a local temp project:
assembly picks the right adapter, memory retrieval reaches the prompt, skills
auto-load, the budget is respected, and a missing API key fails with a clear
message rather than a nil-pointer panic.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks
are approved (house Gate 1).
