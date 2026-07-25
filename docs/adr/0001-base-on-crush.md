# ADR-0001 — Base on Crush (Go/Charm), config-compatible with OpenCode

**Status:** Accepted
**Deciders:** CTO
**Context date:** foundation

## Context
The goal is an OpenCode-class coding agent in **pure Go**, reaching the MiMoCode
feature set. Three lineages share the OpenCode ancestor: `sst/opencode` (TS core +
Go TUI), `MiMoCode` (TS fork that adds the harness features we want), and
`charmbracelet/crush` (the Go descendant of the original Go `opencode`). MiMoCode's
value is its *subsystems*, not its language; those subsystems are portable.

## Decision
Base the project on **Crush** (Go + Charm: Bubble Tea / Lipgloss / Bubbles / Glamour)
and reimplement MiMoCode's added subsystems in Go on top. Stay **config-compatible**
with the OpenCode surface we already author across repos:
- `AGENTS.md` as the canonical project instruction file (single source of truth).
- `opencode.json` provider/model/mcp/agent/command blocks.
- `.opencode/command/*.md` and `.opencode/agent/*.md`.

## Consequences
- (+) Inherits a mature Go TUI, provider scaffolding, LSP and MCP clients.
- (+) Drops into every repo already configured for OpenCode with zero migration.
- (+) All new work is additive adapters/packages — no rewrite of the core loop.
- (−) We track Crush upstream; divergence is managed via a thin fork boundary.
- (−) MiMoCode features are ported from TS as an executable spec, not transpiled.

## Alternatives
- Fork MiMoCode (TS) — rejected: not pure Go.
- Build from scratch — rejected: rebuilds TUI/provider/LSP/MCP for no gain.

## Legal
MiMoCode is MIT but ships `USE_RESTRICTIONS.md` + a trademark policy. Port logic,
do **not** reuse the MiMo name/logo or the hosted "MiMo Auto" channel.
