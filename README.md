# OpenPlus — OpenSpec Handoff Pack

Pure-Go, OpenCode-class coding agent targeting the **MiMoCode** feature milestone.
Built on **Crush** (Go + Charm), config-compatible with the OpenCode surface, model
layer speaking **both** Anthropic Messages and OpenAI-compatible endpoints.

## What the three artifacts are
- **ADR** — `docs/adr/0001..0008` (`0018-audit.md` is the post-foundation audit snapshot) —
  the settled decisions.
- **SPEC** — `openspec/specs/<capability>/spec.md` — target truth in Requirement/Scenario
  form for: agent-loop, provider, memory, skills, compose, orchestration, ports.
- **BACKLOG** — `openspec/changes/<NNNN>-<slug>/tasks.md` — T-### vertical slices,
  milestone-ordered, plus the deferred list. Active change: `0018-provider-port-extraction`.

## How each agent consumes this
| Agent | Reads | Notes |
|-------|-------|-------|
| **OpenCode** | `AGENTS.md` (canonical) + `opencode.json` + `.opencode/agent/*` + `.opencode/command/*` | `/openspec <id>` scaffolds a change |
| **Codex** | `AGENTS.md` | project rules via AGENTS.md; model/provider via your `~/.codex` config |
| **Claude Code** | `CLAUDE.md` → `AGENTS.md` + `.claude/skills/openplus-build/SKILL.md` | skill triggers the build gate |

## Start here
1. Read `AGENTS.md` (SSOT: gate, ports, ADR index, hard rules, deferred list).
2. Read `openspec/changes/0001-foundation/{proposal,design,tasks}.md` for the foundation.
3. Read `openspec/changes/0018-provider-port-extraction/` for the current architecture
   (the package-level split between `internal/ports/` and `internal/provider/`).
4. Approve the change (house Gate 1) if not already done.

## Architecture (ports & adapters — hexagonal)

The core depends on **ports**, never on concrete adapters. After change 0018, the
canonical home of every port's interface AND its neutral types is `internal/ports/`:

- **`internal/ports/`** — `Provider`, `Embedder`, `MemoryStore`, `Tool`, `SkillIndex`,
  `Tokenizer`, `Budgeter`, `Checkpointer`, `PolicyGate`, `Workflow`. Plus the
  provider-neutral model types (`Request`, `Event`, `Message`, `Block`, `ToolCall`,
  `ToolSchema`, `Usage`, `Role`, `BlockKind`, `EventKind`) and the scripted test
  fake (`internal/ports/providerfake`).
- **`internal/provider/`** — adapter-only. Anthropic Messages, OpenAI-compatible
  Chat Completions, prefix-select, and the SSE helper. Adapter packages implement
  `ports.Provider`; the core never imports them.
- **Leak guard** — `internal/ports/leak_guard_test.go` fails the build if any core
  package imports `internal/provider/`. Encodes the AGENTS.md hard rule
  "Core depends on ports, not on adapters — at the package level."

## Scaffold status (this pack includes runnable Go source)

**Currently shipped** (26 packages, `go test ./...` green, `CGO_ENABLED=0` build clean):

- `internal/ports/` — all 10 port interfaces + neutral model types + `providerfake.Fake`.
- `internal/ports/providerfake/` — deterministic scripted provider for the loop.
- `internal/agent/` — the core turn loop (`internal/agent/loop.go` + `loop_test.go`).
- `internal/provider/` — Anthropic Messages + OpenAI-compatible Chat Completions
  adapters + prefix-select, all behind `ports.Provider`. Stdlib SSE frame reader.
- `internal/tool/` — `Tool` port + builtins (`read`, `write`, `edit`, `bash`, `glob`, `grep`).
- `internal/policy/` — `PolicyGate` (allow/ask/deny + Prompting rules).
- `internal/memory/` + `internal/embed/` — ncruces/go-sqlite3 cgo-free + sqlite-vec,
  hybrid FTS5+vec0 with RRF fusion; local OpenAI-compatible embedder.
- `internal/contextmgr/` — tokenizer, budgeter, checkpointer, task tree.
- `internal/skills/` — `SkillIndex` discovery + BM25 ranking.
- `internal/orchestrate/` — subagents, workflows, goal-judge, parallel sampler.
- `internal/compose/` — spec→ship phase machine.
- `internal/runtime/` — wires ports → adapters; powers `cmd/openplus`.
- `internal/tui/` — Bubble Tea front-end (Crush base).
- `internal/config/` — `opencode.json` + `AGENTS.md` loader.
- `internal/jsworkflow/` — goja `.js` workflow adapter (behind the Workflow port).
- `internal/mcp/` — MCP server connection + tool proxy (behind the Tool port).
- `internal/coordinate/` — inter-agent symbol/exclusive lock coordination.
- `internal/memo/` + `internal/improve/` — `/dream` (trace mining) + `/distill`
  (repeated-workflow synthesis).
- `internal/diff/` + `internal/glob/` + `internal/symbols/` — utility packages.

Verify before trusting:

```bash
go build ./...
CGO_ENABLED=0 go build ./...
go test ./...
go run ./cmd/openplus --fake -C $(mktemp -d) -p "say hello"   # end-to-end smoke
```

## Layout

```
AGENTS.md  CLAUDE.md  opencode.json  README.md
docs/adr/0001..0008-*.md  0018-audit.md
openspec/project.md
openspec/specs/{agent-loop,provider,memory,skills,compose,orchestration,ports}/spec.md
openspec/changes/
  0001-foundation/                    # M0–M9 baseline (T-001..T-091)
  0002..0017-*/                       # capability changes
  0018-provider-port-extraction/      # post-foundation architecture (T-1800..T-1811)
.opencode/{agent/{build,plan}.md, command/openspec.md}
.claude/skills/openplus-build/SKILL.md
internal/
  ports/                              # canonical home of every port + neutral types
    providerfake/                     #   the scripted test Fake
  provider/                           # adapter-only; core does not import this
    anthropic/                        # Anthropic Messages adapter
    openaicompat/                     # OpenAI-compatible Chat Completions adapter
    select/                           # prefix-based adapter selection
    sse.go                            #   SSE frame reader (used by adapters)
  agent/  tool/  policy/  memory/  embed/  contextmgr/  skills/  orchestrate/
  compose/  runtime/  tui/  config/  jsworkflow/  mcp/  coordinate/  memo/  improve/
  diff/  glob/  symbols/
```
