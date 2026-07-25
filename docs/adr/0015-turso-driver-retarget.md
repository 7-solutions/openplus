# ADR-0015: Turso driver pinned to the canonical vanity path

Date: 2026-07-25
Status: Accepted (change 0022)
Refines: ADR-0013 (driver pin)

## Context
Change 0020 (ADR-0013) resolved the user's "use v0.7.1" directive to the
**wrong module path**: `github.com/tursodatabase/turso-go v0.2.2`. That path is
the **archived standalone binding** — its own README states "moved back to the
original turso repo" — and ships an older libturso (sqlite 3.47.0). It receives
no further updates.

A module-proxy probe during change 0021 revealed the **canonical live path**:
`turso.tech/database/tursogo`, a vanity import resolving to
`github.com/tursodatabase/turso` (the monorepo) at subdirectory `bindings/go`.
Latest stable as of 2026-07-25: **v0.7.1** (released 2026-07-22). This is the
path Turso actively maintains and where future fixes — including the native
(Tantivy) FTS described in the "Beyond-FTS5" blog — will land.

Staying on the archived path was a latent risk: any future Turso fix lands only
on the canonical path, never the archived one.

## Decision
Pin the memory driver to the canonical path:

- **Import** (`internal/memory/store.go`): `_ "turso.tech/database/tursogo"`.
- **go.mod**: `turso.tech/database/tursogo v0.7.1` (direct). The v0.7.1
  libturso binary arrives transitively as
  `github.com/tursodatabase/turso-go-platform-libs v0.7.1` (indirect).
- **Regression guard**: `github.com/tursodatabase/turso-go` (the archived
  standalone path) is added to `TestNoBannedDirectDeps` in
  `internal/ports/ports_test.go`, so the change-0020 wrong-path mistake cannot
  silently recur. The guard's matcher was hardened to **token-boundary** matching
  (`depTokenInLine`) so the banned short path does not substring-collide with the
  legitimate `.../turso-go-platform-libs` dependency.

## Why this is a drop-in, not a rewrite
The driver is consumed solely through `database/sql` via `sql.Open("turso", ...)`.
v0.7.1 registers under the same driver name `"turso"` and serves the same vector
SQL surface, runtime-verified during change 0021's scratch probe:
`vector32('[...]')` insert + `vector_distance_cos()` KNN → correct results;
`sqlite_version()` = 3.50.4; `CGO_ENABLED=0` build clean. The change-0021
modernc.org/sqlite FTS5 shadow index is independent of the Turso driver and is
unaffected.

## Consequences
- The archived path is now a build-time banned direct dep; re-pinning it fails
  `go test ./internal/ports/...`.
- Newer libturso (sqlite 3.50.4 vs 3.47.0). On-disk schema format is shared
  across the two builds for this schema, so no migration is needed.
- One behavioral note: `turso-go-platform-libs` is a transitive dep carrying the
  prebuilt libturso binary for the host platform; it is extracted and loaded at
  runtime via purego (cgo-free invariant preserved).

## Alternatives considered
1. **Stay on v0.2.2** — rejected: archived, unmaintained, blocks future fixes.
2. **Bundle into change 0021** — rejected: one task = one slice = one PR.
3. **Pin v0.8.0-pre.1** — rejected: pre-release; house rule mandates latest stable.
