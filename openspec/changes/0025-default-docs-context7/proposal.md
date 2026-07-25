# Change 0025 — Default docs source (Context7)

> Status: PROPOSED. Awaiting Gate 1 approval. **No code before approval.**

## Why
The feature matrix (`docs/feature-matrix.md`) flags **library-docs lookup** as a gap
OpenPlus should close: OpenCode and MiMoCode both give the model up-to-date library
documentation. OpenPlus already ships MCP connection (change 0015, ADR-0010), so the
cheapest path is to make **Context7** (Upstash) the default docs MCP server — it
returns current, versioned library docs the model can cite instead of guessing from
stale training data.

This is **not LSP** (code intelligence: go-to-def, diagnostics, hover). LSP is a
separate, larger change (0026) — explicitly deferred.

## What Changes
- **Auto-inject-if-empty default.** When the user has declared **zero** MCP servers
  (`config.MCP` empty), OpenPlus injects a `context7` server at
  `https://mcp.context7.com/mcp` (streamable HTTP) before `startMCPServers` runs. The
  moment the user configures any `mcp.*` entry, the default disappears and they own
  the MCP surface. Zero-config for fresh users; no surprise once they take control.
- **Remote HTTP transport, no node.** Context7's streamable-HTTP endpoint is used, not
  the `npx @upstash/context7-mcp` stdio server. This adds **no runtime node/npx
  dependency** — consistent with OpenPlus's pure-Go, single-binary ethos.
- **Optional API key via env.** If `CONTEXT7_API_KEY` is set, it is sent as the
  `CONTEXT7_API_KEY` header (higher rate limits; free key at context7.com/dashboard).
  Basic use is key-less.
- **Env kill-switch.** `OPENPLUS_DEFAULT_DOCS=0|false|off` disables the auto-inject
  even when MCP is empty — for privacy-controlled environments that want no startup
  network dial.
- **`/docs <library> [query…]` command.** A human-initiated convenience that calls
  `context7.resolve-library-id` then `context7.query-docs` and returns the docs text.
  Not gated by `PolicyGate` — it is direct user action (read-only docs fetch), not an
  autonomous agent-loop tool call.

## Architecture (ports & adapters — hexagonal)
**No new port. No interface change.** This reuses the existing MCP machinery:
`config.MCPServer` (change 0015), `startMCPServers` (`internal/runtime/mcp.go`), and
`tool.Registry`. The default is a use-site fallback applied to the loaded `Config`
**after** `config.Load` and **before** `Assemble` builds the `Session` — the same
convention the repo already uses for `DefaultBudget` and `DefaultMemoryPath`. Context7
is an MCP adapter reached through the existing `Tool` port; the core never sees a
concrete type.

No `specs/` delta: consistent with changes 0022/0023/0024, an internal-behavior
change with no port or capability interface change ships `proposal.md` + `tasks.md`
only.

## Privacy
This is the **first OpenPlus default that dials out on startup**. It tensions the
"local-by-default" privacy hard rule (which is specifically about memory +
embeddings). Mitigations, all load-bearing:
1. **Inject-only-if-empty** — any user MCP config silently opts them out.
2. **Env kill-switch** (`OPENPLUS_DEFAULT_DOCS`) for locked-down environments.
3. **Read-only docs fetch** — Context7 receives a library name + query; no source
   code, no memory contents, no secrets leave the host.
4. **ADR-0016** records the decision and the opt-out.

## Context7 facts (verified 2026-07-26)
- Endpoint: `https://mcp.context7.com/mcp` (streamable HTTP → OpenPlus `http` transport)
- Auth header: `CONTEXT7_API_KEY` (optional)
- Tools: `resolve-library-id` (`libraryName`, `query`) → candidate library IDs;
  `query-docs` (`libraryId`, `query`) → docs text
- Namespaced in `s.Tools` as `context7.resolve-library-id` / `context7.query-docs`
  (MCP `ToolNameSeparator = "."`)

## Scope (explicitly OUT of this change)
- **LSP / code intelligence** — change 0026, separate ADR, separate gate.
- **A config field for the default** (e.g. `docs.default`) — env kill-switch suffices
  for v1; a discoverable config field is a future nicety.
- **Caching / offline docs** — Context7 is queried live each call.
- **Pointing the default at a different docs backend** — hard-coded to Context7 for v1.

## Alternatives considered
1. **Opt-out (default-on for everyone)** — rejected: dials out even for users with
   their own MCP config; too aggressive given the privacy rule.
2. **Opt-in (template + `/docs` only, no auto-connect)** — rejected: loses the
   zero-config "it just works" UX the user asked for.
3. **stdio `npx` transport** — rejected: adds a node/npx runtime dependency; breaks
   the single-binary ethos. Remote HTTP is cleaner.
4. **No `/docs` command (model uses the MCP tools directly)** — kept the command: it
   is the tangible, discoverable human surface for "default docs source."

## Rollback
`git revert HEAD`. No schema migration, no on-disk state. The default + one command
are pure runtime additions; removing them restores prior behavior exactly.
