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
- [ ] T-2011 Single commit `chore(driver): migrate memory to Turso (0020)`. Push.
- [ ] T-2012 `MEMORY.md` driver change + `docs/adr/0013-turso-driver.md`.
- [ ] T-2013 ICM `decisions-openplus` topic.

## M6 — FTS5 deferred (forward-pointer)
- [ ] T-2014 **Future work, NOT this change.** Turso v0.2.2's libturso does not ship the `fts5` module. The hybrid Search pipeline is vector-only in this change. When Turso ships a libturso build with FTS5 (or the project switches to a different driver), the chunks_fts virtual table can be re-added and the bm25 half of Search re-enabled. The RRF fusion harness is preserved so the upgrade is local.

## M7 — Hard rule + regression guard
- [ ] T-2015 Add a regression guard at `internal/ports/regression_test.go` (or equivalent) that fails the build if `github.com/asg017/sqlite-vec-go-bindings` or `github.com/ncruces/go-sqlite3` reappear as direct deps. Pattern after `leak_guard_test.go`.

## Rollback
If any test fails irrecoverably, the revert is `git revert HEAD`; the prior baseline at `f9ada45` (post-0019 revert) is the largest known-good state.
