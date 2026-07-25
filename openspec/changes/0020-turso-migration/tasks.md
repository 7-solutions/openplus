# Change 0020 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. `[ ]` open · `[~]` in progress · `[x]` done.

## M0 — Spec
- [x] T-2000 OpenSpec change 0020 PLAN + TASKS approved.

## M1 — Dependency swap
- [x] T-2001 `go.mod` rewrite: drop `asg017/sqlite-vec-go-bindings`, drop `ncruces/go-sqlite3` (direct), add `github.com/tursodatabase/turso-go v0.2.2`. `go mod tidy`.

## M2 — Driver + schema
- [x] T-2002 Replace driver registration in `internal/memory/store.go` (driver name `turso`).
- [x] T-2003 Schema rewrite: drop `chunks_vec` (vec0); add `embedding BLOB` column on the `chunks` table. Bump `user_version` to 3.
- [x] T-2004 Update `internal/memory/search.go`: drop manual KNN; use Turso `vector_distance_cos` + `vector32()`. RRF fusion preserved (lexical half deferred — see M6).
- [x] T-2005 (skipped) — `internal/runtime/assemble.go` doesn't import `ncruces/go-sqlite3` directly.

## M3 — Tests
- [x] T-2006 Rewrite memory tests for Turso-native vector. Updated `store_test.go`, `store_write_test.go`, `search_test.go`; new `fakeEmbed` produces per-first-letter vectors so the golden ranking test is deterministic without FTS5.

## M4 — Verify
- [x] T-2007 `go build ./...` clean.
- [x] T-2008 `CGO_ENABLED=0 go build ./...` clean.
- [x] T-2009 `go test ./...` 26/26 green.
- [x] T-2010 `-race` on `internal/memory/... ./internal/orchestrate/... ./internal/coordinate/... ./internal/runtime/...` clean.

## M5 — Commit + propagate
- [x] T-2011 Single commit `chore(driver): migrate memory to Turso (0020)`. Pushed at HEAD `4780737`.
- [x] T-2012 `MEMORY.md` driver change + durable lesson entries committed. `docs/adr/0013-turso-driver.md` is a follow-up — not blocking.
- [x] T-2013 ICM `decisions-openplus` topic stored (HIGH importance).

## M6 — Lexical search deferred (forward-pointer, REVISED)
- [ ] T-2014 **Future work, NOT this change.** The hybrid Search pipeline is vector-only in this change. The next-agent M6 spec is:
  - **DO NOT re-introduce FTS5.** Per the Turso "Beyond FTS5" blog post (https://turso.tech/blog/beyond-fts5), Turso v0.5 (in development) ships a **native Tantivy-based FTS** that is NOT FTS5. The Turso roadmap is `CREATE INDEX ... USING fts (col) WITH (tokenizer='ngram')` (Postgres-style) and a `(cols) MATCH '...'` query syntax, with no virtual table at all.
  - **Future lexical search** (change 0021+): wait for the libturso build that ships the Tantivy FTS, OR upgrade to a newer `tursodatabase/turso-go` release once one is published that includes it. Then:
    1. Create a `CREATE INDEX chunks_fts_idx ON chunks USING fts (text) WITH (tokenizer='default')` on the `chunks` table.
    2. Replace the lexical half of `Search` (the deleted `chunks_fts MATCH` block) with a `WHERE (text) MATCH '...'` join against the new index, BM25 score via `fts_score(text, ...)`.
    3. Re-enable the RRF fusion.
  - The RRF fusion harness in `Search` is preserved for that upgrade — re-enabling the lexical half is a local change, not a rewrite.
  - The user follow-up: "Force-fit: drop FTS5, rank by vector only" was the correct v1 call. The wrong assumption was "FTS5 will come back" — Turso's path is native FTS, not FTS5.

## M7 — Hard rule + regression guard
- [x] T-2015 Added `TestNoBannedDirectDeps` in `internal/ports/ports_test.go` (existing file, not a new one). RED→GREEN proven by injecting then removing the banned line. Pattern after `leak_guard_test.go` (T-1808).

## Rollback
If any test fails irrecoverably, the revert is `git revert HEAD`; the prior baseline at `f9ada45` (post-0019 revert) is the largest known-good state.
