# Change 0018 — Extract Provider port & neutral types into `internal/ports` (PLAN)

## Why
AGENTS.md house rule: **"the core depends on ports, not on adapters."** ADR-0005
codifies this for the model layer. In practice, however, every core package
(`agent`, `runtime`, `tui`, `orchestrate`, `improve`, `contextmgr`, `policy`,
`tool`, `memory`) imports `internal/provider` — *not* to depend on a concrete
adapter, but because the **neutral types** (`Request`, `Event`, `Message`,
`Block`, `ToolCall`, `ToolSchema`, `Usage`, `Role`, `BlockKind`, `EventKind`)
live in that package alongside the adapter implementations. This is a
package-level coupling violation: a file that uses only `provider.Message` is
already reaching into the adapter package.

Change 0006 self-check (item 3) cleared this audit because no *concrete
adapter* (`anthropic.`, `openaicompat.`) leaks outside `internal/provider/`.
But it explicitly noted the resolution is **not yet hexagonal at the package
boundary**: any caller of `provider.Message` has a transitive dependency on
the SSE helper and the Anthropic / OpenAI-compatible adapters.

This change moves the port surface (the `Provider` interface, every neutral
type, and the test `Fake`) into `internal/ports/`. After it ships, the
`internal/provider` package contains *only* the concrete adapters (Anthropic,
OpenAI-compatible, prefix-select) and the SSE helper — the things an external
programmer would write to add a new backend. The core imports `internal/ports`
exclusively.

## What changes
- **Move** the `Provider` interface and every neutral type from
  `internal/provider/types.go` to `internal/ports/` (split across
  `internal/ports/provider.go` for the port/interface and `internal/ports/model.go`
  for the neutral types).
- **Move** `internal/provider/fake.go` (`Fake`) to `internal/ports/providerfake/fake.go`,
  re-exporting it as `portsfake.Fake` (or accepting a thin alias in
  `internal/provider` for back-compat).
- **Keep** concrete adapters (`anthropic`, `openaicompat`), the prefix-select,
  and the SSE helper in `internal/provider/`. They `import
  "internal/ports"` for the neutral types; they no longer export any.
- **Rewrite** every core-package import of `internal/provider` to import
  `internal/ports` (and `internal/ports/providerfake` where they used `Fake`).
  No behavior changes.
- **Add** an `internal/provider/provider.go` thin shim that re-exports the
  port types under their old names **for adapter packages only**, so the
  Anthropic / OpenAI-compatible adapters don't have to learn a new import path
  at the same time as core does. The shim is deleted in a follow-up
  change once no callers reference it.

## Governing decisions
- **ADR-0005** — provider-neutral loop; the model package is neutral.
- **AGENTS.md** — ports & adapters; core depends on ports.
- **Change 0006** self-check item 3 — already cleared at the *type* level;
  this change closes the *package* level.

## Impact (file-level)
- `internal/ports/ports.go`: gains the `Provider` interface (currently mirrored
  here; will be promoted to canonical).
- `internal/ports/ports.go`: gains neutral types via a new `internal/ports/model.go`.
- `internal/ports/providerfake/fake.go`: the `Fake` is a `ports.Provider`.
- `internal/provider/types.go`, `fake.go`: deleted after migration.
- `internal/provider/sse.go`, `anthropic/`, `openaicompat/`, `select/`: import
  path rewrites (and use the shim during transition).
- ~30 files in `agent`, `runtime`, `tui`, `orchestrate`, `improve`,
  `contextmgr`, `policy`, `tool`, `memory` packages and their tests: import
  rewrites only. **No call-site logic changes.**
- New: `openspec/specs/provider/` delta, `openspec/specs/ports/` delta.
- New: backward-compat note in `internal/ports/ports.go`.

## Compatibility
None of the public type names change (`Request`, `Event`, `Message`,
`ToolCall`, `ToolSchema`, `Usage`, `Block`, `Role`, `BlockKind`, `EventKind`,
`Provider`). The only consumer-facing path that moves is `Fake`'s import path
(`internal/provider` → `internal/ports/providerfake`). All producers in this
repo are migrated in the same change; an external consumer that imports
`provider.Fake` gets a one-line breakage in favor of the new path.

## What explicitly does NOT change
- No new ports are added. This is a **packaging** refactor of an existing
  port, not a new capability.
- `provider/contract_test.go` is rewritten to import `internal/ports`,
  keeping the same scenarios.
- Adapter behavior (`anthropic.`, `openaicompat.`) is preserved bit-for-bit.
  Tests cover this (T-1804, T-1805).
- The SSE helper stays in `internal/provider/sse.go` (not port-neutral; only
  useful to adapters).

## Risks
- **Large import-rewrite surface** (~30 files). Mitigation: mechanical change,
  exercised by `go build ./...` (T-1803) and full `go test ./...` (T-1806).
- **`Fake` API change** (package path). Mitigation: `internal/provider`
  shim re-exports it during transition (T-1807); deleted only after `go vet`
  reports zero in-package callers.
- **Lockfile churn.** No new dependencies; go.mod/go.sum untouched.

## Approval
**STOP** — implementation begins only after this PLAN, both deltas (provider
+ ports), and the task list are approved (house Gate 1).
