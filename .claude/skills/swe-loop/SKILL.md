---
name: swe-loop
description: SWE-Bench-style evaluation workflow for OpenPlus. Use when an OpenPlus problem statement is provided (a failing-issue narrative, a bug report, a feature request with acceptance criteria) and the goal is to produce a minimal patch that makes a hidden test suite pass. Drives the openplus-build gate read-first, then iterates a single problem without writing long-lived OpenSpec changes. Refuses anything that requires new infrastructure, a new ADR, or a port beyond the existing ten.
---

# SWE-Loop — single-problem benchmark workflow
A focused, evaluation-driven slice of the OpenPlus build workflow. The agent is
**scored against a hidden test suite**, not shipping a feature. The diff must be
minimal, the reasoning must stay inside the seams AGENTS.md already names, and
the workflow terminates when the hidden tests pass (or after a documented
blocker).

## Inputs (the harness reads these)
- **Problem statement**: `.swe-loop/problem.md` — markdown narrative describing
  the failing behavior, expected behavior, and any pointers to the relevant code
  area. Single problem per run.
- **Hidden test suite**: `.swe-loop/hidden_tests/` — a directory of `*_test.go`
  files the agent must NOT read during the run. The harness copies these into
  the relevant package at scoring time. Read access is a hard leak.
- **Time / attempt budget**: the runner enforces a wall clock and a max attempt
  count; both are passed in via `.swe-loop/budget.json`.

## Phases (sequentially, gate at every transition)
1. **Read AGENTS.md.** Stop. Confirm the problem is in scope (not in the refuse
   list; not a deferred item). If it is, write the refuse reason and exit.
2. **Read the contract.** Identify the port(s) the change touches. The fix must
   live behind an existing port — no new ports, no new adapters, no new
   packages outside `internal/<existing>/`, no `internal/provider/` imports in
   core (the leak guard at `internal/ports/leak_guard_test.go` will catch it).
3. **Read the relevant code.** Grep the seam. Read the port interface. Read the
   adapter(s). Read the existing tests. Do NOT read `.swe-loop/hidden_tests/`.
4. **Reproduce (red).** Add a failing test inside the existing test file (NOT
   in `hidden_tests/`) that mirrors the failing behavior. Run `go test ./...`
   and confirm the new test fails for the expected reason.
5. **Patch (green).** Smallest change that greens the new test. Run the full
   suite. Stop when green.
6. **Hidden scoring.** The runner compiles the project with
   `.swe-loop/hidden_tests/` overlaid (using a build tag) and runs the suite.
   The agent does NOT do this step — the runner does.
7. **Self-report.** Write `.swe-loop/result.json` with: problem SHA, list of
   files changed, list of tests added, final `go test ./...` exit code, and a
   one-paragraph rationale.

## Hard rules (override the openplus-build defaults)
- **No OpenSpec change.** `openspec/changes/<NNNN>-*` is forbidden — this is
  evaluated work, not a shipped change. Future change authors can promote it
  if warranted.
- **No new deps.** `go.mod` must be untouched.
- **No new ports.** Only the ten existing ports
  (Provider / Embedder / MemoryStore / Tool / SkillIndex / Tokenizer /
  Budgeter / Checkpointer / PolicyGate / Workflow) may be touched.
- **No new CLI commands.** The TUI / cmd surface must be unchanged.
- **Hidden tests are off-limits.** Reading `.swe-loop/hidden_tests/` at any
  phase is a forbidden leak — the score is thrown out.
- **No agent-internal state.** No persistent memory entries, no knowledge
  graph updates, no IG commits. The graph/memory update gates from
  `openplus-build` are SKIPPED for this workflow.

## Refuse in v1 (the harness writes a refusal and exits)
- Anything that requires a new provider, a new embedder, a new database
  backend, or a new build tag.
- Anything that requires a new ADR.
- Anything touching the refuse list in `AGENTS.md`
  (voice/ASR, MCP marketplace, web/share UI, hosted server mode).
- Tasks that exceed the attempt budget or time budget — refused with the
  partial-progress assessment in `result.json`.

## Self-check before `result.json`
- [ ] Problem is in scope (not in refuse list, not a new port/dep/ADR/CLI).
- [ ] New failing test added before patch (red-first).
- [ ] Patch is the smallest change that greens the suite.
- [ ] `go test ./...` green at the patched HEAD.
- [ ] `CGO_ENABLED=0 go build ./...` clean.
- [ ] Leak guard (`internal/ports/leak_guard_test.go`) passing.
- [ ] `hidden_tests/` not read.
- [ ] `go.mod`, `openspec/`, `cmd/`, `internal/provider/` (outside adapter
      subpackages), and `internal/ports/` package layout untouched.
- [ ] `result.json` written with all required fields.
