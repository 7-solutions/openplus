# OpenSpec Project — OpenPlus

## What this is
A pure-Go, OpenCode-class coding agent that reaches the **MiMoCode** feature milestone,
built on **Crush** (Go + Charm) and config-compatible with the OpenCode surface
(`AGENTS.md`, `opencode.json`, `.opencode/*`). Model layer speaks **both** the Anthropic
Messages API and the OpenAI-compatible endpoint as first-class citizens.

## Conventions (house rules — enforced, see AGENTS.md)
- **Spec-first (OpenSpec):** every change ships PLAN (proposal) + SPEC (delta) + TASKS
  before code. No production code before an approved spec.
- **Strict TDD:** failing tests first (unit → integration → e2e), red before green.
- **Ports & adapters (hexagonal):** the core depends on ports; all I/O is an adapter.
- **Defer-behind-port, add-on-trigger:** non-goals stay behind a port until a documented
  ADR trigger fires.
- **Decisions-as-ADRs:** see `docs/adr/`. Every spec names its governing ADR.
- **Commit gate:** Advisor review → update knowledge graph → update memory.

## Stack (latest-stable, lockfile-pinned)
Go 1.26 · Bubble Tea/Lipgloss/Bubbles/Glamour · ncruces/go-sqlite3 (cgo-free) +
sqlite-vec (ncruces) · wazero · net/http + SSE (no vendor SDK required).

## Capabilities (source of truth in specs/)
agent-loop · provider · memory · skills · compose · orchestration
