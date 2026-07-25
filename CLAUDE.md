# CLAUDE.md

**Canonical instructions live in [`AGENTS.md`](./AGENTS.md).** Read it first — it holds
the stack, the build gate, the ports, the ADR index, the hard rules, and the deferred list.

## Hard rules (mirrored from AGENTS.md §Hard rules)
- **Core depends on ports, not on adapters — at the package level.** `internal/ports/`
  is the canonical home of every port's interface AND its neutral types; concrete
  adapters live in `internal/provider/`. No core package may import `internal/provider/`.
  The regression test `internal/ports/leak_guard_test.go` fails the build if it does.
- **Wire neutrality at every port — no wire type crosses a seam.** A port's interface
  and its result types use neutral types only; the adapter converts at its boundary.
  For LSP: no `go.lsp.dev` type in `internal/ports/`, and only `internal/lsp/` may
  import one. Enforced by `internal/ports/lsp_leak_guard_test.go`.
- **Every hard rule arrives with a regression test.** When adding a rule to AGENTS.md,
  add a failing test in the same slice.

## Claude Code specifics
- Follow the build gate in `AGENTS.md` exactly. Gate 1 (OpenSpec PLAN+SPEC+TASKS with a
  STOP for approval) is mandatory — do not write code before an approved spec.
- The build procedure is packaged as a skill: `.claude/skills/openplus-build/SKILL.md`.
- A SWE-Bench-style evaluation workflow (single problem, hidden test suite,
  scored patch) is packaged as a separate skill: `.claude/skills/swe-loop/SKILL.md`.
  Use it when the task is benchmark-style, not shipped-feature work.
- ADRs: `docs/adr/`. Specs: `openspec/specs/`. Most recent change: `openspec/changes/0026-language-service-port/`.
