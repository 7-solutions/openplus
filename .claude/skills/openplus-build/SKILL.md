---
name: openplus-build
description: The mandatory build workflow for the OpenPlus pure-Go coding-agent repo (Crush/Bubble Tea base, ncruces/go-sqlite3 + sqlite-vec memory, Anthropic + OpenAI-compatible providers). Use whenever working on OpenPlus — implementing a feature, fixing a bug, adding a provider adapter, a tool, a memory or compose subsystem, or picking up any T-### task from openspec/changes/*/tasks.md — to enforce spec-first (OpenSpec), strict TDD, cgo-free builds, ports/adapters, and the Advisor + graph + memory commit gates. Trigger BEFORE writing any code.
---

# OpenPlus Build Workflow
The procedure for every coding task in OpenPlus. Rules, stack, ports, and decisions live in
`AGENTS.md`, `docs/adr/`, and `openspec/`. Read them first. **Never skip a gate.**

## Gates (in order)
1. **Spec first (OpenSpec).** PLAN + SPEC delta + TASKS under `openspec/changes/<id>/`.
   STOP for approval. No code before an approved spec. The spec names modules, ports, ADR.
2. **Tests first (TDD).** Failing tests (unit → integration → e2e), shown red first.
3. **Implement to green.** Minimum to pass. Core depends on a **port**; new I/O is an
   **adapter**. Keep the build **cgo-free**. No provider type escapes `internal/provider`.
4. **Advisor review.** Resolve every finding.
5. **Commit + propagate.** One task = one slice = one PR. Update graph + memory.

## Refuse in v1 (backlog — flag the missing trigger)
voice/ASR · Max Mode · MCP marketplace · web/share UI · hosted server · goja JS workflows.

## Self-check
- [ ] Approved PLAN+SPEC+TASKS before code
- [ ] Red-first tests → green
- [ ] Port/adapter respected; cgo-free build green; no leaked provider type
- [ ] Advisor passed; graph + memory updated
- [ ] No backlog item introduced
