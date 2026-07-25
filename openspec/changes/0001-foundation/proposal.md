# Change 0001 — Foundation (PLAN)

## Why
Establish a pure-Go, OpenCode-class coding agent (codename **OpenPlus**) that reaches the
MiMoCode feature milestone, built on Crush and config-compatible with the OpenCode
surface. This change adds the six foundational capabilities from zero.

## What changes
Adds capabilities: `agent-loop`, `provider`, `memory`, `skills`, `compose`,
`orchestration`. No existing behavior is modified or removed (greenfield).

## Governing decisions
ADR-0001 (base on Crush) · ADR-0002 (feature milestone) · ADR-0003 (memory store) ·
ADR-0004 (embedder) · ADR-0005 (provider) · ADR-0006 (workflow runtime) ·
ADR-0007 (permission seam) · ADR-0008 (context budgeter).

## Impact
- New Go module `github.com/7solutions/openplus` (rename freely).
- New ports: Provider, Embedder, MemoryStore, Tool, SkillIndex, Tokenizer,
  Budgeter, Checkpointer, PolicyGate, Workflow.
- Deferred behind ports (non-goals v1): voice/ASR, Max Mode, MCP marketplace,
  web/share UI, hosted multi-tenant server, goja JS workflows.

## Approval
STOP — implementation begins only after this proposal + the delta specs + tasks are
approved (house Gate 1).
