# AGENTS.md — OpenPlus (canonical, single source of truth)

> Read by **OpenCode** and **Codex** natively. **Claude Code** reads `CLAUDE.md`, which
> points here. This file is the SSOT; keep tool-specific files thin.

## What OpenPlus is
A pure-Go, OpenCode-class coding agent that reaches the **MiMoCode** feature milestone,
built on **Crush** (Go + Charm) and config-compatible with the OpenCode surface
(`AGENTS.md`, `opencode.json`, `.opencode/*`). The model layer speaks **both** the
Anthropic Messages API and the OpenAI-compatible endpoint as first-class citizens.

## Build gate — run in order, never skip (house rule)
1. **Spec first (OpenSpec).** Ship PLAN (`openspec/changes/<id>/proposal.md`) + SPEC
   (delta under `.../specs/`) + TASKS (`.../tasks.md`). **STOP for approval. No code
   before an approved spec.** The spec names the module(s), the ports, and the ADR.
2. **Tests first (TDD).** Write failing tests (unit → integration → e2e). Show red
   before any production code. Production code exists only to green a failing test.
3. **Implement to green.** Minimum to pass. Core depends on **ports**; new I/O is an
   **adapter**. Never call a concrete external system from the core.
4. **Advisor review.** Resolve every finding before commit.
5. **Commit + propagate.** One task = one vertical slice = one PR. Update the knowledge
   graph. Update memory.

## Architecture (ports & adapters — hexagonal)
Ports: Provider · Embedder · MemoryStore · Tool · SkillIndex · Tokenizer · Budgeter ·
Checkpointer · PolicyGate · Workflow. Layout is in
`openspec/changes/0001-foundation/design.md`.

## Decisions (ADRs — read before touching a subsystem)
- ADR-0001 base on Crush, config-compatible with OpenCode
- ADR-0002 feature milestone = MiMoCode subsystems
- ADR-0003 memory = ncruces/go-sqlite3 (cgo-free) + sqlite-vec, hybrid FTS5+vec0 (RRF)
- ADR-0004 Embedder port + local embedding model
- ADR-0005 Provider port: Anthropic + OpenAI-compatible adapters
- ADR-0006 Go-native workflow engine (goja deferred)
- ADR-0007 permission gate as control-plane ES256 seam
- ADR-0008 context budgeter / tokenizer / reconstruction

## Hard rules
- **Pure Go / cgo-free.** No cgo in the default build (that is why ncruces+wazero, not
  mattn+cgo). Single static binary; trivial cross-compile.
- **Provider neutrality.** The loop and all subsystems use only the neutral model.
  No provider-specific type escapes `internal/provider`.
- **Privacy.** Memory + embeddings stay local by default; chunk text never leaves the host.
- **Defer-behind-port, add-on-trigger.** See the deferred list; do not build it early.
- **Latest-stable, lockfile-pinned.** Bump only after green tests.

## Refuse in v1 (recognize and decline — backlog, each needs its ADR trigger)
Voice/ASR · Max Mode (best-of-N + judge) · MCP marketplace · web/share UI · hosted
multi-tenant server mode · goja `.js` workflow compatibility. If a task seems to need
one, **stop and flag it** — the trigger has not fired.

## Self-check before "done"
- [ ] Approved OpenSpec PLAN + SPEC + TASKS existed before code
- [ ] Tests written first, shown red, driven to green
- [ ] Core depends on a port; new I/O is an adapter; no provider type leaked
- [ ] cgo-free build still green
- [ ] Advisor passed; graph updated; memory updated
- [ ] No deferred/backlog item introduced
