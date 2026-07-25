# ADR-0014: Hybrid search via dual-engine composition

Date: 2026-07-25
Status: Proposed (change 0021)
Supersedes: ADR-0003 (lexical half), refines ADR-0013

## Context
Change 0020 (ADR-0013) migrated the memory store to Turso and shipped
**vector-only** retrieval because Turso's libturso binary does not compile in
the `fts5` module. A runtime probe on 2026-07-25 confirmed this holds across
both published Go bindings:

- `github.com/tursodatabase/turso-go` v0.2.2 (archived) — no fts5.
- `turso.tech/database/tursogo` v0.7.1 (canonical latest) — the driver's own
  `TestFTS` fails with `unknown module name 'fts'`. The experimental-index-method
  gate lifts via DSN `?experimental=index_method`, but the `fts` module is not
  compiled into the libturso binary either binding ships.

Losing the lexical half hurts recall on keyword-heavy queries (identifiers,
exact phrases) where vector similarity is a poor proxy. The user chose to
restore hybrid search by vendoring a separate cgo-free FTS5-capable SQLite
alongside Turso.

## Decision
Compose two engines behind the existing `MemoryStore` port:

- **Primary**: Turso (`turso-go` v0.2.2) — `chunks(id, text, source, embedding)`
  with `vector32()` insert and `vector_distance_cos()` KNN. Source of truth.
- **Shadow**: `modernc.org/sqlite` (transpiled C→Go, cgo-free, FTS5-capable) —
  a derived `chunks_fts USING fts5(text)` index over `(rowid, text)`. Driver
  name `"sqlite"` does not collide with Turso's `"turso"`.
- **Fusion**: Reciprocal Rank Fusion. Each half contributes
  `1/(rrfK+rank)`; the Store sums into one score map. A chunk the vector half
  ranks outside its top-k can still surface via the lexical half.

The shadow is an **internal collaborator** of `memory.Store` (`fts *ftsIndex`),
not an 11th port. `Open(path)` stays vector-only (backward compatible);
`Open(path, WithFTS())` opts into hybrid. The shadow is a pure derived index,
reconstructable via `(Store).RebuildFTS`. Best-effort writes (a shadow failure
never loses a primary write, mirroring prune discipline).

## Why modernc over ncruces
- `ncruces/go-sqlite3` is on the `TestNoBannedDirectDeps` ban list (force-removed
  in change 0020). Re-introducing it contradicts that directive and would
  require amending the regression guard.
- `modernc.org/sqlite` is a fresh dependency with no such baggage; runtime-probed
  FTS5-capable (`CREATE VIRTUAL TABLE USING fts5`, `MATCH ... ORDER BY bm25`).
  cgo-free verified (`CGO_ENABLED=0`).

## Consequences
- One new direct dependency (`modernc.org/sqlite`); cgo-free invariant preserved.
- Two database files when FTS is enabled on a file-backed store
  (`<base>.db` + `<base>.fts.db`); the shadow is deletable/rebuildable.
- The architectural leak guard still holds: no core package imports
  `modernc.org/sqlite`; it lives only in `internal/memory/`.
- **Forward-compat**: when Turso ships a binding with native (Tantivy) FTS, the
  shadow adapter can be swapped for a Turso-backed one behind the same internal
  `ftsIndex` interface — `Search`'s RRF harness is unchanged.

## Alternatives considered
1. **Wait for Turso native FTS** — rejected by user choice; unknown timeline.
2. **Re-target Turso to v0.7.1 first** — independent; deferred to a separate
   change. The shadow design works on the current v0.2.2 pin.
3. **Re-introduce ncruces** — rejected (banned-dep guard + 0020 directive).
