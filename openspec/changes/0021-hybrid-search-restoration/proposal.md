# Change 0021 — Hybrid Search Restoration (FTS5 shadow index)

> Status: PROPOSED. Awaiting Gate 1 approval. **No code before approval.**

## Why
Change 0020 shipped vector-only retrieval because the Turso libturso binary
doesn't ship the `fts5` module — neither in `turso-go v0.2.2` (archived, our
current pin) nor in `turso.tech/database/tursogo v0.7.1` (canonical latest,
probed 2026-07-25: the driver's own `TestFTS` fails with `unknown module name
'fts'`). Native FTS (Tantivy) is described in Turso's "Beyond FTS5" blog but is
not compiled into any published Go binding's libturso as of this writing.

The pre-0020 design was a hybrid FTS5+vector RRF pipeline. Losing the lexical
half hurts recall on keyword-heavy queries (function names, identifiers, exact
phrases) where vector similarity is a poor proxy. The user chose to restore
hybrid search by **vendoring a separate cgo-free FTS5-capable SQLite** alongside
Turso for the lexical shadow index.

## What Changes
- **Add `modernc.org/sqlite` (transpiled C→Go, cgo-free, FTS5-capable)** as the
  lexical-index engine. Probed v1.54.0 at runtime: `CREATE VIRTUAL TABLE USING
  fts5`, `INSERT`, `MATCH ... ORDER BY bm25(...)` all work; `CGO_ENABLED=0`
  build clean; sqlite 3.53.3.
- **New internal adapter `internal/memory/fts.go`** — a derived FTS5 shadow
  index over `(rowid, text)`. It is NOT a new port; it is an implementation
  detail of the memory package behind the existing `MemoryStore` port. Keeps
  the change local to `internal/memory/`; no core package imports a concrete
  SQLite driver.
- **Wire the shadow index into `Store`** as an optional collaborator:
  - `Open(path, WithFTS())` functional option opens a shadow DB at
    `<path>.fts.db` (or `:memory:` when the primary is `:memory:`).
  - `Write` indexes into the shadow after the chunk commits (best-effort, like
    prune — a shadow failure never loses the primary write).
  - `pruneToMaxEntries` deletes the pruned rowids from the shadow too.
  - `Search` RRF-fuses vector KNN (Turso) + bm25 (shadow) when the shadow is
    present; vector-only when absent (backward-compatible default).
- **Driver-name isolation**: Turso registers as `"turso"`, modernc as
  `"sqlite"` — no collision; both coexist via `database/sql` in one binary.
- **`TestNoBannedDirectDeps` is unaffected** — its ban list is
  `asg017/sqlite-vec-go-bindings` + `ncruces/go-sqlite3`. `modernc.org/sqlite`
  is not banned. No guard amendment needed.

## Architecture (ports & adapters — hexagonal)
This change adds a concrete adapter (`internal/memory/fts.go`, modernc-backed)
behind the **existing** `MemoryStore` port. It does NOT add an 11th port: the
shadow index is an internal collaborator of `Store`, not a top-level seam. Core
packages continue to depend only on `ports.MemoryStore`; nothing in core imports
`modernc.org/sqlite`.

```
                  ports.MemoryStore (unchanged interface)
                           │
                 memory.Store (composes two engines)
                  ┌────────┴────────┐
            Turso (db)        ftsIndex (shadow, modernc)
            chunks + vector    chunks_fts(rowid, text)
            vector_distance_cos  MATCH + bm25
                  └────────┬────────┘
                       RRF fusion
```

The shadow DB is a pure **derived index**: reconstructable from `chunks` at any
time. A rebuild helper (`(Store).RebuildFTS()`) is included so a corrupt or
missing shadow is recoverable without touching the primary.

## Why modernc over ncruces
- `ncruces/go-sqlite3` was force-removed in change 0020 and is on the
  `TestNoBannedDirectDeps` ban list. Re-introducing it would require amending
  that regression guard, directly contradicting the 0020 directive.
- `modernc.org/sqlite` is a fresh dependency with no such baggage; transpiled
  C→Go (ccgo), no WASM runtime, no cgo. Runtime-probed FTS5-capable.
- Both are cgo-free; modernc's transpiled form has a larger binary footprint but
  avoids the WASM loader, which is simpler for this derived-index role.

## ADR
**ADR-0014 (proposed): Hybrid search via dual-engine composition.** Primary
store = Turso (vector KNN); lexical shadow index = `modernc.org/sqlite` FTS5.
RRF-fused. Restores the pre-0020 hybrid contract without depending on Turso
shipping fts5, and without re-introducing the banned ncruces/sqlite-vec stack.
Ships at `docs/adr/0014-hybrid-search-restoration.md`.

## Alternatives considered
1. **Wait for Turso to ship native FTS** — rejected by user choice. Unknown
   timeline; v0.7.1 (3 days old) still lacks it. (The DSN enablement path
   `?experimental=index_method` is known for when it lands; this change's
   shadow-index design is forward-compatible — it can be dropped for native FTS
   in a later change without touching `Search`'s RRF harness.)
2. **Re-target Turso to v0.7.1 first** — independent of FTS; deferred to its own
   change (0022 candidate) to keep this slice focused. The shadow-index design
   works identically on v0.2.2 (current pin) since Turso's role (vector) is
   unchanged.
3. **Vendor ncruces back for FTS5** — rejected: contradicts the 0020 force-remove
   directive and the active regression guard.

## Scope (explicitly OUT of this change)
- Turso driver re-target (v0.2.2 → v0.7.1). Separate change.
- Native Tantivy FTS via Turso. Blocked upstream; future change.
- Chunking improvements / richer splitting. Pre-existing backlog.
- Tunable RRF weights / per-field boosting. Future enhancement.

## Rollback
`git revert HEAD` restores vector-only. The shadow index lives in a sidecar
file (`<path>.fts.db`); reverting the code leaves the sidecar orphaned but
harmless (and deletable). No schema migration on the primary Turso DB — its
`chunks` table is untouched by this change.
