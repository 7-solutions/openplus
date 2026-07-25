# Change 0022 — Turso driver re-target (archived → canonical)

> Status: PROPOSED. Awaiting Gate 1 approval. **No code before approval.**

## Why
Change 0020 resolved the user's "use v0.7.1" directive to **the wrong module
path**: `github.com/tursodatabase/turso-go v0.2.2`. That path is the **archived
standalone binding** — its own README says "moved back to the original turso
repo" — and its bundled libturso is old (sqlite 3.47.0).

The **canonical live path** is `turso.tech/database/tursogo`, a vanity import
that the Go proxy resolves to `github.com/tursodatabase/turso` (the monorepo)
at subdirectory `bindings/go`. Latest stable: **v0.7.1** (2026-07-22). This is
the version the user originally cited (`tursodatabase/turso/releases/tag/v0.7.1`
— the monorepo tag). v0.7.1's bundled libturso is newer (sqlite 3.50.4) and is
the path Turso actively maintains.

Staying on the archived path is a latent risk: it receives no updates, and any
future Turso fix (including native FTS, per the "Beyond-FTS5" blog) will land
on the canonical path, never the archived one.

## What Changes
- **Swap the import** in `internal/memory/store.go`: `_ "github.com/tursodatabase/turso-go"`
  → `_ "turso.tech/database/tursogo"`. (The only code-level change.)
- **Swap the go.mod require**: `github.com/tursodatabase/turso-go v0.2.2` →
  `turso.tech/database/tursogo v0.7.1`. `go mod tidy`.
- **Update 4 doc-comment references** in `store.go` (package doc line 2, driver
  registration comment line 38, ensureSchema comment line 330) to name the new
  path/version.
- **No behavioral change**: driver name stays `"turso"`; the vector contract
  (`vector32()`/`vector_distance_cos()` KNN) is identical on v0.7.1 (runtime-
  verified in change 0021's scratch probe); cgo-free preserved (purego). The
  modernc.org/sqlite FTS5 shadow (change 0021) is unaffected — it does not
  touch the Turso driver.
- **Regression guard**: add `github.com/tursodatabase/turso-go` (the archived
  path) to the `TestNoBannedDirectDeps` ban list, so the 0020 wrong-path
  mistake can never silently recur. Pattern matches the existing ncruces +
  sqlite-vec ban.

## Why this is low-risk (drop-in, not a rewrite)
The driver is consumed solely through `database/sql` via `sql.Open("turso",
...)`. The only thing that matters is that v0.7.1 registers under the same
driver name and serves the same SQL surface. Both were runtime-verified in
change 0021's probe:
- `sql.Open("turso", ":memory:")` → OK, `sqlite_version()` = 3.50.4.
- `vector32('[...]')` insert + `vector_distance_cos()` KNN → OK, correct
  nearest-neighbor result.
- `CGO_ENABLED=0` build → clean.

The existing `internal/memory/...` test suite (26 cases incl. the hybrid search
tests) is the red→green proof: it passes on v0.2.2 today and must still pass on
v0.7.1 after the swap.

## Architecture (ports & adapters — hexagonal)
**No port changes.** The `MemoryStore` port and the modernc FTS shadow adapter
are untouched. This change touches exactly one adapter import line in
`internal/memory/store.go`. No core package imports the Turso driver directly
(leak guard stays green); the change does not escape `internal/memory/`.

## ADR
**ADR-0015 (proposed):** Turso driver pinned to the canonical vanity path
`turso.tech/database/turso`, subdirectory `bindings/go` of the monorepo. The
archived `github.com/tursodatabase/turso-go` is banned as a direct dep. Ships
at `docs/adr/0015-turso-driver-retarget.md`.

## Alternatives considered
1. **Stay on v0.2.2** — rejected: archived, unmaintained, blocks all future
   Turso fixes (incl. native FTS) which land only on the canonical path.
2. **Bundle the re-target into change 0021** — rejected at spec time: one task
   = one vertical slice = one PR. 0021 shipped the shadow index on v0.2.2; this
   change is the isolated driver swap.
3. **Wait for v0.8.0** — v0.8.0-pre.1 exists but is pre-release; the
   "latest-stable, lockfile-pinned" house rule forbids pinning a pre-release.
   v0.7.1 is the latest stable.

## Scope (explicitly OUT of this change)
- Native Turso FTS (Tantivy). Still absent in v0.7.1's libturso (the driver's
  own `TestFTS` fails); change 0021's modernc shadow already covers lexical
  retrieval. Re-evaluate when a stable binding ships the `fts` module.
- RRF weight tuning, per-field boosting. Future enhancement.
- Bumping modernc.org/sqlite. Separate dep-bump change.

## Rollback
`git revert HEAD` restores v0.2.2. No schema migration — the chunks table and
the FTS shadow are byte-identical across the two libturso builds (sqlite 3.47
vs 3.50 share the on-disk format for this schema).
