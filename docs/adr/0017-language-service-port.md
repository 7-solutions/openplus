# ADR-0017: LanguageService port (LSP) as the eleventh seam

Date: 2026-07-26
Status: Accepted (change 0026)
Relates: ADR-0005 (provider neutrality), ADR-0010 (MCP connection), ADR-0016 (docs source)

## Context
OpenPlus edits code blind. It cannot see a diagnostic it just caused, resolve a
symbol, or find a definition — every such question degrades to a `grep` or a full
build. `docs/feature-matrix.md` records this as the largest functional gap against
OpenCode and MiMoCode, both of which integrate the Language Server Protocol.

OpenCode's integration is deliberately minimal: **diagnostics only, disabled by
default**, with servers starting on file-extension detection. That is a sound floor,
but diagnostics alone tell the agent *that* something is wrong, not *what the code
means*.

## Decision
Add `LanguageService` as the **eleventh port**, with a stdio LSP client behind it.

**Surfaces (all read-only):** `Diagnostics`, `Hover`, `Definition`,
`DocumentSymbols`, `References`, plus `Shutdown`.

**Delivery is two-track:**
1. Five model-callable tools (`lsp_diagnostics`, `lsp_hover`, `lsp_definition`,
   `lsp_symbols`, `lsp_references`) for when the agent asks.
2. **Automatic diagnostics injection** after `write`/`edit`/`bash`, so the agent sees
   its own breakage without asking. This is the reason a *port* is warranted rather
   than only a tool: the runtime itself is a consumer.

**Opt-in**, matching OpenCode: no server spawns unless the `lsp` config enables it.

**Adapter:** new package `internal/lsp/` (analog to `internal/embed/`), implementing
`ports.LanguageService` directly.

## Hard rule: no LSP wire type crosses the port
No type from `go.lsp.dev/protocol` or `go.lsp.dev/jsonrpc2` may appear in a
`ports.LanguageService` signature or in any type it returns. `protocol.Diagnostic` is
converted to the neutral `ports.Diagnostic` inside `internal/lsp/`.

This is ADR-0005's provider-neutrality rule applied to a second wire protocol: the
core must not learn LSP any more than it learns the Anthropic wire. Per the house
rule that every hard rule arrives with a regression test, it is enforced by
`internal/ports/lsp_leak_guard_test.go`, which fails the build if an exported
identifier in `internal/ports/` references a `go.lsp.dev` type or if any package
outside `internal/lsp/` imports one — and it is mirrored into `AGENTS.md` and
`CLAUDE.md`.

Note: `internal/embed` redeclares its own `Embedder` interface instead of importing
the port. That is a wart, not a pattern; `internal/lsp` does not copy it.

## Why a library, when `internal/mcp` is stdlib-only
Hand-rolling was the default expectation (MCP set that precedent). Two findings
overrode it:

1. **The MCP framing does not carry over.** `internal/mcp/jsonrpc.go` is
   newline-delimited JSON; LSP requires `Content-Length` header framing.
2. **MCP ignores server-initiated notifications**, because no MCP flow needed them.
   LSP diagnostics arrive *exclusively* as pushed notifications, so the transport must
   demux notifications alongside responses. This is precisely the part of the MCP
   transport that cannot be reused.

`go.lsp.dev/jsonrpc2 v1.0.1` is pure-Go, BSD-3, published June 2026, requires Go 1.26
(the repo is on 1.26), and its only core dependency is
`github.com/go-json-experiment/json`. `go.lsp.dev/protocol v1.0.1` supplies the LSP
type set, which is large and not worth hand-maintaining. The cgo-free build gate and
`TestNoBannedDirectDeps` both continue to run.

What *is* reused from MCP is the process lifecycle shape: spawn, pending-request map,
demux goroutine, fail-all-waiters on exit.

## Safety posture
- **Opt-in.** No `lsp` config, no subprocess.
- **Never in fake mode.** `opts.Fake` sessions spawn nothing. This is a correctness
  rule, not a test convenience: change 0025 regressed the runtime suite from 0.9s to
  159s when a shipped default reached the network during tests.
- **Non-fatal.** A missing or broken server yields a named warning and a fully
  functional session (the `startMCPServers` pattern).
- **Gated.** LSP tools are read-only but still pass `PolicyGate` on the autonomous
  path.
- **Bounded.** Injected diagnostics are capped in count and length and pass through
  `Budgeter.Fit`; they must never crowd out retrieved context.
- **Async.** The post-edit refresh is fired from `OnToolResult`, which runs on the
  agent goroutine — it is never awaited by the loop.

## Consequences
- `PortNames()` returns eleven; the count test and the "ten seams" package doc move
  with it.
- Two new pinned, pure-Go dependencies.
- The runtime gains its first *automatic* context enrichment driven by a tool
  side-effect, which is a new kind of coupling between the loop and the context
  assembler. It is bounded by the cap and by the Budgeter.

## Explicitly not in scope
Completions; code actions / apply-edit / rename (they mutate — they need their own
gating story); formatting, semantic tokens, inlay hints; auto-installing language
servers; workspace-wide diagnostics.

## Rollback
`git revert` the milestone commits. No schema change, no on-disk state. Removing the
`lsp` config block makes the port and adapter inert.
