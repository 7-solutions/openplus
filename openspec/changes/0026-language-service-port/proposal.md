# Change 0026 — LanguageService port (LSP)

> Status: PROPOSED. Awaiting Gate 1 approval. **No code before approval.**

## Why
`docs/feature-matrix.md` names **LSP** as OpenPlus's largest functional gap against
OpenCode and MiMoCode. Today the agent edits code blind: it cannot see a compiler
diagnostic it just caused, cannot resolve a symbol, cannot find a definition or its
references. Every such question costs a `grep` or a full build.

Change 0025 shipped the docs half (Context7). This change ships the code-intelligence
half: an **eleventh port**, `LanguageService`, with a stdio LSP client behind it.

Reference point: OpenCode ships **diagnostics only**, **disabled by default**, with
servers starting on file-extension detection. OpenPlus deliberately goes further on
surface and on delivery, while keeping OpenCode's opt-in default.

## What Changes

### The port (11th)
`internal/ports/` gains `LanguageService`:

```go
type LanguageService interface {
    Diagnostics(ctx context.Context, path string) ([]Diagnostic, error)
    Hover(ctx context.Context, path string, line, col int) (string, error)
    Definition(ctx context.Context, path string, line, col int) ([]Location, error)
    DocumentSymbols(ctx context.Context, path string) ([]Symbol, error)
    References(ctx context.Context, path string, line, col int) ([]Location, error)
    Shutdown(ctx context.Context) error
}
```

with neutral result types (`Diagnostic`, `Location`, `Symbol`, `Severity`) declared in
`internal/ports/`. `PortNames()` grows to eleven; the count test and the package doc
("the ten seams") move with it.

### The adapter
New package `internal/lsp/` (analog to `internal/embed/`):
- `client.go` — one language-server process: spawn, `initialize`/`initialized`,
  `textDocument/didOpen`/`didChange`, request surfaces (hover, definition,
  documentSymbol, references), and a **notification handler** for
  `textDocument/publishDiagnostics` that caches the latest diagnostics per URI.
- `manager.go` — extension → server routing, lazy start on first use, shutdown-all.
  Implements `ports.LanguageService`; converts `protocol.*` → neutral `ports.*` at
  the boundary.

### The dependencies
`go.lsp.dev/jsonrpc2 v1.0.1` (BSD-3, pure-Go, requires Go 1.26 — the repo is on 1.26)
and `go.lsp.dev/protocol v1.0.1` for LSP types. Both verified present on the module
proxy. See "Why a library" below.

### Model surface
`internal/tool/lsp.go` adds five `tool.Tool` implementations — `lsp_diagnostics`,
`lsp_hover`, `lsp_definition`, `lsp_symbols`, `lsp_references` — registered in the
tools slice at `internal/runtime/assemble.go` **only when LSP is enabled and the
session is not fake**.

### Auto-injected diagnostics
The agent should see its own breakage without being asked. After a `write`/`edit`/
`bash` tool result, an **async** diagnostics refresh runs for the touched files; a
bounded "Diagnostics" section is injected in `AssembleContext` so the Budgeter
accounts for it.

### Config
`lsp` section: `enabled` plus a `servers` map (extension → `{command, args}`),
following the Embedder config recipe.

## Architecture (ports & adapters — hexagonal)
**This is a new port, and the neutrality rule extends to it.** No `go.lsp.dev` type
may appear in a `ports.LanguageService` signature — exactly as no provider-specific
type may escape `internal/provider`. `protocol.Diagnostic` is converted to
`ports.Diagnostic` inside `internal/lsp/`. This is proposed as a **hard rule** with a
regression test (see ADR-0017), hoisted to `AGENTS.md` + `CLAUDE.md`.

`internal/embed` redeclares its own `Embedder` interface rather than importing the
port. **That pattern is not copied here**: `internal/lsp` implements
`ports.LanguageService` directly, as `internal/memory` and `internal/provider/*` do.

## Why a library (not hand-rolled, unlike MCP)
`internal/mcp` is stdlib-only, so hand-rolling was considered. Two findings decided it:

1. **The MCP framing is not reusable.** `internal/mcp/jsonrpc.go` is
   newline-delimited JSON; LSP requires `Content-Length` header framing. None of the
   MCP codec carries over.
2. **MCP ignores server-initiated notifications** (`internal/mcp/stdio.go`), because
   no MCP flow needed them. LSP diagnostics arrive *exclusively* as pushed
   notifications, so the transport must demux notifications alongside responses —
   the one place the proven MCP transport template cannot be copied.

`go.lsp.dev/jsonrpc2` is pure-Go, BSD-3, v1.0.1 (Jun 2026), and its only core
dependency is `github.com/go-json-experiment/json`. Pairing it with
`go.lsp.dev/protocol` avoids hand-writing (and hand-maintaining) the LSP type set.

## Defaults & safety
- **Opt-in.** No language server spawns unless `lsp.enabled` is set (matches OpenCode).
- **Never in fake mode.** `opts.Fake` sessions spawn nothing. This is not a test
  convenience but a correctness rule: change 0025 regressed the runtime suite from
  0.9s to 159s when a default dialed out during tests.
- **Failure is non-fatal.** A missing or broken language server degrades to a named
  warning (the `startMCPServers` pattern), never a dead session.
- **Gated.** LSP tools are read-only but still pass `PolicyGate` on the autonomous
  path, like every other tool.
- **Bounded injection.** The auto-injected diagnostics section is capped in count and
  length; diagnostics must never crowd out retrieved context.

## Scope (explicitly OUT of this change)
- **Completions.** High-frequency, low value to a batch agent.
- **Code actions / apply-edit / rename.** These *mutate*; they need a gating story of
  their own. Read-only surfaces first.
- **Formatting, semantic tokens, inlay hints.** IDE-presentation concerns.
- **Auto-installing language servers.** The user supplies the command; OpenPlus never
  downloads a toolchain.
- **Workspace-wide diagnostics.** Per-file only; a whole-project sweep is a separate
  performance problem.

## Alternatives considered
1. **Diagnostics only (the OpenCode scope)** — rejected by decision: hover/definition/
   symbols/references are what turn "sees errors" into "understands the code".
2. **Tool-only, no auto-injection** — rejected: an agent that must remember to ask for
   diagnostics mostly won't. Auto-injection is why the port (not just a tool) exists.
3. **Hand-rolled JSON-RPC** — rejected: see "Why a library". Neither the framing nor
   the notification handling carries over from MCP.
4. **Expose LSP through the MCP port** (an LSP↔MCP bridge) — rejected: it would make
   a core capability depend on an external server process and the MCP config surface.

## Risks
- **Process lifecycle.** Language servers are long-lived subprocesses. Mitigation:
  lazy start, explicit `Shutdown`, non-fatal failure, and no spawning in fake mode.
- **Async refresh racing the loop.** The only post-edit seam (`OnToolResult`) runs on
  the agent goroutine. Mitigation: the refresh is fired async and never awaited by the
  loop; injection reads whatever is cached at `AssembleContext` time.
- **Two new dependencies.** Mitigation: both pure-Go and pinned; `TestNoBannedDirectDeps`
  and the cgo-free gate both run.
- **Scope.** Five surfaces + a port + injection is the largest change so far.
  Mitigation: five milestones, each independently green.

## Rollback
`git revert` the milestone commits. No schema change, no on-disk state. Disabling the
config block makes the port and adapter inert.
