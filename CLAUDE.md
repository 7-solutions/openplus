# CLAUDE.md

**Canonical instructions live in [`AGENTS.md`](./AGENTS.md).** Read it first — it holds
the stack, the build gate, the ports, the ADR index, the hard rules, and the deferred list.

## Claude Code specifics
- Follow the build gate in `AGENTS.md` exactly. Gate 1 (OpenSpec PLAN+SPEC+TASKS with a
  STOP for approval) is mandatory — do not write code before an approved spec.
- The build procedure is packaged as a skill: `.claude/skills/openplus-build/SKILL.md`.
- ADRs: `docs/adr/`. Specs: `openspec/specs/`. Active change: `openspec/changes/0001-foundation/`.
