# ADR-0016: Context7 as the default docs source

Date: 2026-07-26
Status: Proposed (change 0025, awaiting Gate 1)
Uses: ADR-0010 (MCP server connection, change 0015)

## Context
OpenPlus ships MCP connection (ADR-0010) but no *default* MCP server, so out of the
box the model has no way to fetch up-to-date library documentation — it falls back to
(stale) training-data knowledge. OpenCode and MiMoCode both close this gap. The
feature matrix (`docs/feature-matrix.md`) lists docs lookup as an OpenPlus gap.

Context7 (Upstash) is a remote MCP server that returns current, versioned library
docs. Wiring it as a shipped default gives every fresh OpenPlus session a docs source
with zero configuration.

This is the **first OpenPlus default that makes an outbound network call on startup**.
It tensions the "local-by-default" privacy hard rule (which is specifically scoped to
memory + embeddings, not all network activity), so the decision and its opt-out are
recorded here.

## Decision
Make Context7 the default docs MCP server, **auto-injected only when the user has
declared zero MCP servers**, over the **remote streamable-HTTP transport**.

- **Endpoint:** `https://mcp.context7.com/mcp` (OpenPlus `http` MCP transport).
- **Auth:** optional `CONTEXT7_API_KEY` header, read from the env of the same name.
- **Injection point:** runtime use-site default applied to the loaded `Config` after
  `config.Load` and before `Assemble` builds the `Session` (same convention as
  `DefaultBudget` / `DefaultMemoryPath`). Not inside the config package.
- **Opt-out:** `OPENPLUS_DEFAULT_DOCS=0|false|off` disables the auto-inject.
- **`/docs` command:** a human-initiated shortcut (`resolve-library-id` →
  `query-docs`) that bypasses `PolicyGate` because it is direct, read-only user
  action — the same standing as a user typing a CLI lookup. Autonomous agent-loop
  calls to `context7.*` tools remain gated as usual.

## Why auto-inject-if-empty (not default-on / not opt-in)
- **Default-on for everyone** would dial out even for users who run their own MCP
  servers — too aggressive given the privacy rule.
- **Pure opt-in** (template only) loses the zero-config "it just works" UX.
- **Auto-inject-if-empty** gives fresh users docs for free, and the instant a user
  configures any `mcp.*` entry the default vanishes — they signal "I own the MCP
  surface" and OpenPlus respects that. Any user who wants Context7 *and* their own
  servers adds `mcp.context7` explicitly.

## Why remote HTTP (not stdio npx)
The stdio transport (`npx -y @upstash/context7-mcp`) adds a runtime node+npx
dependency, breaking the pure-Go, single-static-binary ethos (ADR-0001). The remote
HTTP endpoint serves the same Context7 backend with no local process and no node —
and OpenPlus's HTTP MCP client already exists (`internal/mcp/http.go`).

## Privacy posture
Context7 receives only a library name and a query string. No source code, memory
contents, or secrets are sent. The connection is read-only (docs fetch). Mitigations:
inject-only-if-empty, env kill-switch, and this ADR.

## Consequences
- Fresh sessions (`mcp` empty) gain `context7.resolve-library-id` and
  `context7.query-docs` tools and the `/docs` command.
- A startup network dial to context7.com occurs for those sessions; failure is
  non-fatal (existing `startMCPServers` skips broken servers with a named warning).
- No new Go dependency, no new port, no schema change.

## Rollback
`git revert HEAD`. No on-disk state.
