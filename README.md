# OpenPlus — OpenSpec Handoff Pack

Pure-Go, OpenCode-class coding agent targeting the **MiMoCode** feature milestone.
Built on **Crush** (Go + Charm), config-compatible with the OpenCode surface, model
layer speaking **both** Anthropic Messages and OpenAI-compatible endpoints.


## What the three artifacts are
- **ADR** — `docs/adr/0001..0008` — the settled decisions (decision-as-diff for ADR-0003).
- **SPEC** — `openspec/specs/<capability>/spec.md` — target truth in Requirement/Scenario
  form for: agent-loop, provider, memory, skills, compose, orchestration.
- **BACKLOG** — `openspec/changes/0001-foundation/tasks.md` — T-### vertical slices,
  milestone-ordered M0–M9, plus the deferred list.

## How each agent consumes this
| Agent | Reads | Notes |
|-------|-------|-------|
| **OpenCode** | `AGENTS.md` (canonical) + `opencode.json` + `.opencode/agent/*` + `.opencode/command/*` | `/openspec <id>` scaffolds a change |
| **Codex** | `AGENTS.md` | project rules via AGENTS.md; model/provider via your `~/.codex` config |
| **Claude Code** | `CLAUDE.md` → `AGENTS.md` + `.claude/skills/openplus-build/SKILL.md` | skill triggers the build gate |

## Start here
1. Read `AGENTS.md` (SSOT: gate, ports, ADR index, hard rules, deferred list).
2. Read `openspec/changes/0001-foundation/{proposal,design,tasks}.md`.
3. Approve the change (house Gate 1) if not already done.

## Scaffold status (this pack includes runnable Go source)
`cmd/openplus/main.go` + `internal/{provider,tool,agent,policy}/` implement T-010, T-011,
T-016 (added), T-020, T-022, T-023 — the neutral provider model, a stdlib SSE frame
reader, a deterministic `Fake` provider, the `Tool` port + an `Echo` builtin, a minimal
`PolicyGate`, and the core agent loop, proven by `internal/agent/loop_test.go`.

**Not yet verified to compile** — this pack was assembled in a container with no Go
toolchain. Before trusting it:
```bash
go build ./...
go test ./...
go run ./cmd/openplus     # smoke test: runs the loop end-to-end with the Fake provider
```
Deliberately **not yet scaffolded** (real network/deps, next slices): the Anthropic and
OpenAI-compatible provider adapters (T-012/T-013), the memory store
(ncruces/go-sqlite3 + sqlite-vec, T-040), and the Bubble Tea TUI (T-030). Those pull in
external modules this container can't fetch — pick up from `tasks.md` once you have a
normal Go + network environment.

## Layout
```
AGENTS.md  CLAUDE.md  opencode.json  README.md
docs/adr/0001..0008-*.md
openspec/project.md
openspec/specs/{agent-loop,provider,memory,skills,compose,orchestration}/spec.md
openspec/changes/0001-foundation/{proposal,design,tasks}.md + specs/ (deltas)
.opencode/{agent/{build,plan}.md, command/openspec.md}
.claude/skills/openplus-build/SKILL.md
```
