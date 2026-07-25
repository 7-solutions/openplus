# Change 0025 — Tasks

> One task = one vertical slice = one PR. `[ ]` open · `[~]` in progress · `[x]` done.
> Gate order per openplus-build skill: Spec → Tests (red) → Implement (green) → Review → Commit.

## M0 — Spec (Gate 1: STOP for approval)
- [x] T-2500 PLAN (`proposal.md`) + TASKS shipped + ADR-0016 drafted. Awaiting approval before any code.

## M1 — Default-server injection (TDD: red first)
- [x] T-2501 Write `internal/runtime/mcp_default_test.go` (red): `applyDefaultMCP`
      on empty `Config.MCP` injects exactly one `context7` entry, HTTP transport,
      URL `https://mcp.context7.com/mcp`; non-empty MCP → unchanged;
      `OPENPLUS_DEFAULT_DOCS=0` → no inject even when empty; `CONTEXT7_API_KEY` set →
      header present, unset → absent. Show red.
- [x] T-2502 Implement to green in `internal/runtime/mcp.go`:
      `defaultContext7Name`/endpoint/`openplusDefaultDocsEnv` consts,
      `defaultContext7Server() config.MCPServer`, `applyDefaultMCP(*config.Config)`.
- [x] T-2503 Wire `applyDefaultMCP(pc.Config)` in `internal/runtime/assemble.go`
      between `config.LoadProjectContextWithConfig` and the `Session` build, gated on
      `!opts.Fake` (a fake session is hermetic and must not dial real services), so
      `s.Config` stays honest and `startMCPServers` sees the injected server.

## M2 — `/docs` command (TDD: red first)
- [x] T-2504 Write `internal/runtime/commands_docs_test.go` (red): `/docs` registered
      in `builtinCommands`; arity error (`/docs` with no arg); not-connected error
      path (no `context7.resolve-library-id` in `s.Tools` → helpful message). Show red.
- [x] T-2505 Implement `cmdDocs` in `internal/runtime/commands_builtin.go` + register
      it. Parse `libraryName` (first token) + `query` (rest, default
      `"usage and API overview"`); call `context7.resolve-library-id` then
      `context7.query-docs` via `s.Tools.Get(...).Execute(ctx, json.RawMessage)`;
      returns docs text. No `PolicyGate` (direct user action). Library id parsed
      defensively via regex (`libraryIDRe`) to tolerate Context7 envelope changes.

## M3 — Docs + config example
- [x] T-2506 Ship `docs/adr/0016-default-docs-context7.md`.
- [x] T-2507 ~~Add a commented `mcp.context7` example block to `opencode.json`.~~
      **Deviated:** `config.Load` uses strict `json.Unmarshal` (no JSONC), so a
      comment would break parsing. The explicit-add recipe is documented in ADR-0016
      (endpoint + header) instead.
- [x] T-2508 Update `AGENTS.md` ADR index (ADR-0016 added). No `CLAUDE.md` change —
      it is a thin mirror for *hard rules*; this ADR introduces none.

## M4 — Verify (Gates 2-4)
- [x] T-2509 `go build ./...` clean.
- [x] T-2510 `CGO_ENABLED=0 go build ./...` clean (no new dep).
- [x] T-2511 `go test ./...` green (26 pkgs); new tests red→green; existing MCP tests
      unaffected (inject gated on `!opts.Fake`, so the suite stays hermetic + fast).
- [x] T-2512 `internal/ports/leak_guard_test.go` passes (Context7 is an MCP adapter,
      runtime-owned; no core imports `internal/provider`).
- [x] T-2513 `TestNoBannedDirectDeps` passes (no new Go dependency).

## M5 — Commit + propagate (Gate 5)
- [ ] T-2514 Single commit `feat(runtime): default docs source via Context7 MCP (0025)`. Push (await explicit push instruction).
- [ ] T-2515 ICM `decisions-openplus` store (high — first network default; note opt-out).

## Notes for the implementer
- **Injection is post-Load, pre-Session.** Do not mutate config inside `config.Load`
  or `parseMCP` — keep it a runtime use-site default (matches Budget/MemoryPath).
- **`/docs` is not gated.** It is direct user action, read-only. The autonomous agent
  loop calling `context7.*` tools IS gated as usual — only the slash shortcut isn't.
- **Context7 result shape** for `resolve-library-id`: a list of candidates with a
  `libraryId` field; take the top hit. Confirm the exact JSON shape against a live call
  during T-2505 (the endpoint is the source of truth; harden the parser defensively).
- **No new Go dependency.** The HTTP MCP client already exists
  (`internal/mcp/http.go`). If implementation seems to need one, STOP — scope creep.
- **Failure is non-fatal.** A unreachable Context7 server must produce a warning (the
  existing `startMCPServers` path already skips broken servers with a named warning),
  not crash the session.

## Rollback
`git revert HEAD`. No schema migration, no on-disk state.
