<h1 align="center">OpenPlus</h1>

<p align="center">
  A pure-Go, terminal-native coding agent.<br>
  Anthropic and OpenAI-compatible models · persistent memory · LSP code intelligence · MCP tools.
</p>

<p align="center">
  <a href="#install"><img alt="version" src="https://img.shields.io/badge/version-v0.0.1--alpha-orange"></a>
  <a href="#status"><img alt="status" src="https://img.shields.io/badge/status-alpha-red"></a>
  <img alt="go" src="https://img.shields.io/badge/go-1.26-00ADD8">
  <img alt="platforms" src="https://img.shields.io/badge/platforms-linux%20%7C%20macos%20%7C%20wsl2-lightgrey">
  <a href="LICENSE"><img alt="license" src="https://img.shields.io/badge/license-Apache--2.0-blue"></a>
</p>

---

> **Alpha.** `v0.0.1-alpha` is the first tagged build. The architecture is settled and
> the test suite is green, but interfaces may still move and it has seen little
> real-world mileage. See [Status](#status) for what is proven and what is not.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/7-solutions/openplus/main/scripts/install.sh | sh
```

Works on **Linux, macOS, and WSL2** (amd64 and arm64). The script detects your
platform, verifies the SHA-256 checksum, and installs to `~/.local/bin` (or
`/usr/local/bin` when writable).

<details>
<summary>Other ways to install</summary>

```bash
# Pin a version
OPENPLUS_VERSION=v0.0.1-alpha curl -fsSL https://raw.githubusercontent.com/7-solutions/openplus/main/scripts/install.sh | sh

# Choose the directory
OPENPLUS_INSTALL=$HOME/bin curl -fsSL https://raw.githubusercontent.com/7-solutions/openplus/main/scripts/install.sh | sh

# With Go
go install github.com/7-solutions/openplus/cmd/openplus@latest

# From source
git clone https://github.com/7-solutions/openplus && cd openplus
CGO_ENABLED=0 go build -o openplus ./cmd/openplus
```

**Native Windows is not supported.** Use WSL2 and install from inside your Linux
distribution.

</details>

## Quickstart

```bash
# No API key needed — the scripted fake provider proves the wiring
openplus --fake -p "say hello"

# With a real model
export ANTHROPIC_API_KEY=sk-ant-...
openplus -p "explain the error handling in internal/agent/loop.go"

# Interactive TUI
openplus
```

## Configure

OpenPlus reads `opencode.json` from your project root — the same surface OpenCode
uses — plus `AGENTS.md` for project instructions.

```jsonc
{
  "model": "anthropic/claude-sonnet-5",
  "instructions": ["AGENTS.md"],

  "provider": {
    "anthropic": { "options": { "apiKey": "{env:ANTHROPIC_API_KEY}" } },
    "local": {
      "options": { "baseURL": "http://localhost:11434/v1", "apiKey": "ollama" },
      "models": { "qwen2.5-coder": { "name": "Qwen2.5 Coder" } }
    }
  },

  "permission": { "bash": "ask", "write": "ask" },

  // Code intelligence — opt-in, you supply the server binary
  "lsp": {
    "enabled": true,
    "servers": { ".go": { "command": "gopls" } }
  }
}
```

Full reference: **[docs/configuration.md](docs/configuration.md)**.

## What it does

| | |
|---|---|
| **Models** | Anthropic Messages and any OpenAI-compatible endpoint (incl. Ollama), first-class |
| **Tools** | `read` `write` `edit` `bash` `glob` `grep`, all behind a permission gate |
| **Memory** | Local vector + FTS5 hybrid search with tunable RRF fusion; never leaves your machine |
| **Code intelligence** | LSP: diagnostics, hover, definition, symbols, references — diagnostics fed back automatically after edits |
| **MCP** | Connect any MCP server; its tools join the registry namespaced |
| **Docs** | Context7 wired as a default docs source, so the model reads current library docs |
| **Orchestration** | Parallel subagents in isolated git worktrees, JS workflows, best-of-N with a judge |
| **Self-improvement** | `/dream` mines session traces into memory; `/distill` turns repeated work into skills |

### LSP in one minute

```bash
go install golang.org/x/tools/gopls@latest
```

```jsonc
{ "lsp": { "enabled": true, "servers": { ".go": { "command": "gopls" } } } }
```

Now the agent sees compiler errors in files it edits, without being asked, and can
call `lsp_hover`, `lsp_definition`, `lsp_symbols`, and `lsp_references` directly.
Servers start lazily on first use; a missing binary is a warning, never a crash.

## Commands

`/help` lists everything. Highlights:

| Command | Does |
|---|---|
| `/docs <library>` | Fetch current library documentation (Context7) |
| `/compose <feature>` | Spec → implement → verify → review pipeline |
| `/subagents <prompts>` | Run work in parallel, isolated git worktrees |
| `/max [n] <prompt>` | Best-of-N sampling with a judge |
| `/dream` · `/distill` | Mine this session into durable memory or a reusable skill |
| `/workflow <name>` | Run a Go or JavaScript workflow |
| `/theme` | Switch color theme (color-vision aware) |

Full list: **[docs/commands.md](docs/commands.md)**.

## Status

**Alpha.** Honest accounting of where this stands:

**Proven**
- 27 packages, `go test ./...` green, `-race` clean
- `CGO_ENABLED=0` build gate enforced in CI
- Architecture guarded by regression tests, not convention — six of them, including
  two that fail the build if a wire type crosses a port boundary
- LSP verified end to end against a real `gopls`

**Not yet proven**
- Little real-world mileage. Interfaces may move before `v0.1.0`.
- macOS and arm64 builds are cross-compiled and CI-tested, but have had no manual
  soak testing.

**Not built** (deliberately deferred, each needs its own decision record): voice input,
MCP marketplace, web/share UI, hosted multi-tenant mode, LSP server auto-install.

### Supported platforms

Linux and macOS on amd64 and arm64, and WSL2. Linux builds target **glibc**;
musl systems such as Alpine are out of scope, and the installer says so rather
than installing a binary that cannot run. The reason is recorded in
[docs/install.md](docs/install.md#alpine-and-other-musl-systems--out-of-scope).

## Architecture

Ports and adapters, enforced rather than encouraged. The core depends on eleven port
interfaces in `internal/ports/`; every external system is an adapter behind one.

```
internal/ports/        the eleven seams + neutral types (no wire type may cross)
  providerfake/        scripted provider for tests
internal/provider/     adapter-only: anthropic · openaicompat · select · sse
internal/lsp/          LSP adapter: client · manager · wire  (only place LSP is known)
internal/memory/       Turso vector + modernc FTS5 shadow, RRF fusion
internal/agent/        the turn loop
internal/runtime/      composition root: ports → adapters
internal/tui/          Bubble Tea front-end
```

Two hard rules carry regression tests that fail the build when violated:

- **Core depends on ports, not adapters** — `internal/ports/leak_guard_test.go`
- **No wire type crosses a seam** — `internal/ports/lsp_leak_guard_test.go`

Decisions live in [`docs/adr/`](docs/adr/); specs and change history in
[`openspec/`](openspec/). Start with [`AGENTS.md`](AGENTS.md) — it is the single
source of truth for how this project is built.

## Contributing

The build gate is mandatory and spec-first: an approved OpenSpec change
(`openspec/changes/<id>/`) exists before any code, tests are written red first, and
every architectural rule ships with the test that enforces it. Read
[`AGENTS.md`](AGENTS.md) before starting.

```bash
go build ./... && CGO_ENABLED=0 go build ./...
go test ./... && go test -race ./internal/...
go vet ./...
```

## Documentation

- **[Installation](docs/install.md)** — every method, upgrade, uninstall, troubleshooting
- **[Configuration](docs/configuration.md)** — full `opencode.json` reference
- **[Commands](docs/commands.md)** — every slash command
- **[Feature matrix](docs/feature-matrix.md)** — OpenPlus vs OpenCode vs MiMoCode
- **[ADRs](docs/adr/)** — why the architecture is what it is

## License

[Apache License 2.0](LICENSE) — Copyright 2026 7 Solutions.
