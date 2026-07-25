# Feature Matrix: OpenCode vs MiMoCode vs OpenPlus

> Snapshot: 2026-07-26. Sources: [OpenCode](https://opencode.ai/),
> [MiMo-Code](https://github.com/XiaomiMiMo/MiMo-Code),
> [MiMo docs](https://mimo.mi.com/docs/en-US/news/latest/mimocode),
> OpenPlus `AGENTS.md` + `openspec/`.

## Identity

| | **OpenCode** | **MiMoCode** | **OpenPlus** |
|---|---|---|---|
| Builder | SST / Anomaly | Xiaomi | fork of Crush, OpenCode-config-compat |
| Language | TypeScript/Bun | TS/JS (node) | **pure Go, cgo-free** |
| Binary | node runtime | node runtime | single static binary |
| License | open source | open source (free) | open source |
| Config | `opencode.json` + `AGENTS.md` | `.mimocode/` JSONC | `opencode.json` + `AGENTS.md` (compat) |

## Providers

| | OpenCode | MiMoCode | OpenPlus |
|---|---|---|---|
| Anthropic | yes | yes | yes first-class |
| OpenAI-compat | yes 75+ via Models.dev | yes any base URL | yes first-class |
| Free bundled models | yes Zen platform | yes MiMo Auto (anon) | no (BYO key) |
| Local (Ollama) | yes | yes | yes config ships Ollama |
| OAuth login (Copilot/ChatGPT) | yes | yes Codex OAuth | no |

## Tools / agent surface

| | OpenCode | MiMoCode | OpenPlus |
|---|---|---|---|
| Bash exec | yes | yes | yes |
| File read/write/edit | yes | yes | yes bash, edit, read, write, glob, grep |
| Subagents (parallel) | yes multi-session | yes lifecycle + bg | yes `/subagents`, worktree harvest |
| Worktree isolation | yes | yes (compose) | yes change 0012 |
| MCP connection | yes | yes | yes change 0015 |
| MCP marketplace/install | yes | ? | no deferred |
| LSP integration | yes auto-load | yes | no not shipped |
| Skills system | ? | yes 20+ builtin | yes `/skills` + spec-driven |

## Memory / context

| | OpenCode | MiMoCode | OpenPlus |
|---|---|---|---|
| Persistent project memory | yes | yes `MEMORY.md` + notes + tasks | yes Turso vector store |
| Full-text search | — | yes SQLite FTS5 | yes modernc FTS5 shadow |
| Hybrid vector+lexical (RRF) | — | yes | yes **tunable RRF weights+K** (0024) |
| Checkpoint + reconstruct | yes | yes auto + budgeted | yes change 0008/0010 |
| Goal/judge stop condition | — | yes `/goal` | yes change 0007 |
| Local-only privacy | yes | yes | yes hard rule (embeddings never leave host) |

## Workflows / orchestration

| | OpenCode | MiMoCode | OpenPlus |
|---|---|---|---|
| JS workflow runtime | ? | yes goja-style, `.js` files | yes goja adapter (ADR-0009) |
| Spec->ship pipeline | — | yes compose-next | yes `/compose` `/orchestrate` `/spec` |
| Best-of-N + judge (max) | — | yes experimental | yes `/max` (ADR-0011) |
| Knowledge extraction | — | yes `/dream` `/distill` | yes `/dream` `/distill` |
| Self-modify skills | — | yes evolve | partial (distill) |

## UX / platform

| | OpenCode | MiMoCode | OpenPlus |
|---|---|---|---|
| TUI | yes Bubble Tea-style | yes tabs + themes | yes Charm/Bubble Tea + color-vision |
| Desktop app | yes mac/win/linux | no | no deferred |
| IDE extension | yes | no | no |
| Share links | yes | no | no deferred |
| Voice input | no | yes `/voice` (MiMo ASR) | no deferred |
| Hosted multi-tenant | no | no | no deferred |

## Architecture discipline (OpenPlus differentiators)

| Trait | OpenPlus |
|---|---|
| Hexagonal ports/adapters | yes 10 ports, enforced by `leak_guard_test.go` |
| Provider-neutrality hard rule | yes regression-guarded |
| Every hard rule -> failing test | yes house rule |
| Spec-first (OpenSpec) gate | yes mandatory, STOP for approval |
| Banned-dep guard | yes `TestNoBannedDirectDeps` (ncruces, sqlite-vec, archived turso) |
| cgo-free build gate | yes `CGO_ENABLED=0` |

## Gaps vs the other two (OpenPlus not-yet)

1. **LSP** — neither auto-load nor diagnostics. Biggest functional gap.
2. **OAuth provider login** (Copilot/ChatGPT/Codex) — API-key only.
3. **Bundled free models** — no Zen/MiMo-Auto equivalent.
4. **Marketplace, web/share UI, desktop, voice** — all explicitly deferred behind ADR triggers.

## Bottom line

- **OpenPlus** = OpenCode config surface + MiMoCode feature milestone (memory, workflows, max mode, dream/distill, goal judge), in **pure Go**, with **strict ports/adapters + spec-first discipline** the others lack. Trails on **LSP, OAuth login, bundled models, desktop/voice**.
- **OpenCode** leads on breadth (75+ providers, LSP, desktop, share, marketplace) and distribution.
- **MiMoCode** leads on agentic depth (200+ step workflows, voice, evolve, bundled MiMo Auto free tier).
