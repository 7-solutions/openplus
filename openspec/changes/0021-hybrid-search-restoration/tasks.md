# Change 0021 — Tasks

> One task = one vertical slice = one PR. `[ ]` open · `[~]` in progress · `[x]` done.
> Gate order per openplus-build skill: Spec → Tests (red) → Implement (green) → Review → Commit.

## M0 — Spec (Gate 1: STOP for approval)
- [x] T-2100 PLAN (`proposal.md`) + TASKS shipped. Awaiting approval before any code.

## M1 — Dependency
- [x] T-2101 `go get modernc.org/sqlite@v1.54.0`. Verify it appears in `go.mod` require block and that `CGO_ENABLED=0 go build ./...` stays clean. Confirm `TestNoBannedDirectDeps` still passes (modernc is NOT on the ban list).

## M2 — FTS shadow-index adapter (TDD: red first)
- [x] T-2102 Write `internal/memory/fts_test.go` with failing tests: `openFTS`, `index`, `delete`, `search` (MATCH + bm25), `close`. Show red.
- [x] T-2103 Implement `internal/memory/fts.go` to green: `type ftsIndex struct{ db *sql.DB }`, `openFTS(path string) (*ftsIndex, error)` creating `chunks_fts USING fts5(text)`, `(f) index(ctx, id, text)`, `(f) delete(ctx, ids)`, `(f) search(ctx, query, k) (map[int64]float64, error)` returning id→bm25-derived rank score, `(f) close()`. Driver name `"sqlite"`.

## M3 — Wire into Store (TDD: red first)
- [x] T-2104 Write `internal/memory/store_fts_test.go` (red): `Open(path, WithFTS())` opens both engines; `Write` populates the shadow; `pruneToMaxEntries` deletes from the shadow; `Search` returns hybrid-ranked results (lexical boost on keyword queries). Show red.
- [x] T-2105 Implement the wiring to green:
  - Add `OpenOption` functional-option type and `WithFTS()` to `store.go`.
  - `Store` gains `fts *ftsIndex` (nil = vector-only, backward compatible).
  - `Open(path, opts...)` opens the shadow at derived path (`<dir>.fts.db` / `:memory:`).
  - `Write`: after commit, `s.fts.index(ctx, id, text)` (best-effort).
  - `pruneToMaxEntries`: after deleting from chunks, `s.fts.delete(ctx, ids)`.
  - `Search`: when `s.fts != nil`, fuse vector-KNN scores + bm25 scores via the existing RRF harness.
  - Add `(Store).RebuildFTS(ctx) error` — reconstructs the shadow from `chunks`.

## M4 — Hybrid search tests (TDD: red first)
- [x] T-2106 `internal/memory/search_test.go` — restore the golden hybrid-ranking test: a keyword query (e.g. "rust") ranks the two rust chunks above the non-rust chunks via the lexical boost, AND a semantic query still ranks by vector. Show red (currently vector-only can't pass the lexical assertion).
- [x] T-2107 Update existing memory tests that call `Open(":memory:")` to use `Open(":memory:", WithFTS())` where hybrid behavior is asserted; keep `Open(":memory:")` vector-only tests unchanged (backward-compat proof).

## M5 — Verify (Gates 2-4)
- [x] T-2108 `go build ./...` clean.
- [x] T-2109 `CGO_ENABLED=0 go build ./...` clean (modernc is cgo-free; Turso purego unchanged).
- [x] T-2110 `go test ./...` 26/26 green.
- [x] T-2111 `go test -race ./internal/memory/... ./internal/orchestrate/... ./internal/coordinate/... ./internal/runtime/... ./internal/ports/...` clean.
- [x] T-2112 `internal/ports/leak_guard_test.go` still passes — no core package imports `modernc.org/sqlite` directly.
- [x] T-2113 `TestNoBannedDirectDeps` still passes (modernc not banned; ncruces/sqlite-vec still absent).

## M6 — Commit + propagate (Gate 5)
- [ ] T-2114 Single commit `feat(memory): restore hybrid FTS5+vector search via modernc shadow index (0021)`. Push.
- [x] T-2115 Ship `docs/adr/0014-hybrid-search-restoration.md`.
- [ ] T-2116 Update `MEMORY.md` ADR list + discovered-knowledge entry.
- [ ] T-2117 ICM `decisions-openplus` store (high).

## Notes for the implementer
- **Driver names**: `"turso"` (primary) and `"sqlite"` (modernc shadow) coexist — no collision.
- **`:memory:` shadow**: when the primary is `:memory:`, the shadow is too; they are independent in-memory DBs linked only by the rowids the Write path passes. This is correct — the shadow is a derived index.
- **Best-effort shadow writes**: a shadow failure must NOT fail the primary Write (same discipline as prune). The shadow is reconstructable via `RebuildFTS`.
- **Do NOT re-introduce ncruces or sqlite-vec** — `TestNoBannedDirectDeps` will fail the build. modernc.org/sqlite is the only new dep.
- **Forward-compat**: when Turso ships native FTS, the shadow adapter can be swapped for a Turso-backed one behind the same internal interface — Search's RRF harness is unchanged.

## Rollback
`git revert HEAD` restores vector-only. Sidecar `<path>.fts.db` becomes orphan but harmless.
