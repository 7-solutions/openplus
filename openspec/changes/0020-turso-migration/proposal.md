# Change 0020 — Migrate SQLite driver to Turso (Go bindings) + native vector (PLAN)

## Why
Change 0019 revealed that `asg017/sqlite-vec-go-bindings v0.1.x` (and v0.1.7-alpha.2) is ABI-incompatible with `ncruces/go-sqlite3 v0.21+`. The bindings reference `sqlite3.Binary`, which ncruces v0.21 removed in favor of a separate `go-sqlite3-wasm/v3` module. The bindings author has not tagged a release bridging the new ABI.

The user directive (2026-07-25): replace both with the Turso Go bindings (`github.com/tursodatabase/turso-go` @ v0.2.2, the latest published) and use Turso's native vector support — drop `sqlite-vec-go-bindings` entirely.

This is reversible: ncruces/go-sqlite3 stays in `go.mod` as an indirect dependency (Turso uses purego, not cgo) — but if a downstream package needs Direct dep access, we'd re-add. Net: clean separation.

## What changes
- `go.mod`:
  - **DROP** `github.com/asg017/sqlite-vec-go-bindings` (direct dep).
  - **DROP** `ncruces/go-sqlite3` (direct dep, becomes indirect or unused).
  - **ADD** `github.com/tursodatabase/turso-go v0.2.2` (direct dep).
- `internal/memory/store.go`:
  - Replace `ncruces/go-sqlite3`-based driver registration with `tursodatabase/turso-go` driver.
  - Schema: `chunks_fts` (FTS5) stays; `chunks_vec` (vec0) replaced with Turso's native vector index.
  - Schema versioning: bump `user_version` to 3.
- `internal/memory/search.go`:
  - Drop manual `vec0` KNN query — use Turso's native vector syntax.
  - FTS5 + RRF fusion stays the same shape (text-search unchanged).
- `internal/memory/store_test.go` + `internal/memory/store_write_test.go`:
  - Replace `sqlite-vec`-specific golden tests with Turso-native-vector golden tests.
  - FTS5 golden tests unchanged.
- `internal/runtime/assemble.go` (anywhere `ncruces/go-sqlite3` is referenced):
  - Replace import path.
- `docs/adr/`: add a new ADR describing the driver decision.
- `MEMORY.md` + project memory: update the durable "ncruces/go-sqlite3 is the WASM-backed driver" entry. Replace with Turso-is-the-driver.

## Governing decisions
- **ADR-0013** (NEW): Driver is Turso v0.2.2 (Go bindings). Native vector is the vector story. sqlite-vec-go-bindings is removed.
- ADR-0003 (memory is cgo-free) — Redone. Turso is cgo-free via purego; meets the same constraint.

## Risks
- **Turso v0.2.2 is older than the v0.7.1 Rust CLI.** The Go bindings are at v0.2.2; the database engine is at v0.7.1. The Go bindings embed a specific libturso build (likely the same 0.7.x line). Need to verify the vector-API is actually native (not sqlite-vec-rewrite).
- **Golden ranking tests are the project's primary quality gate for memory.** Rewriting them is the largest single risk surface. If the new vector path produces different ranking, the tests fail and the change is incomplete.
- **FTS5 + RRF fusion is fragile.** Schema migration from `chunks_vec` (vec0) to native vector index is a real SQL change, not just a Go change.
- **ncruces go-sqlite3 may stay as indirect** (from anywhere that imports it transitively) — `go mod tidy` cleans this up.

## Approval
**STOP** — implementation begins only after this PLAN + TASKS are approved (house Gate 1). The user has already approved the migration direction; this SPEC makes the destructive surface explicit and the tasks reviewable.

## Tasks
- T-2000 SPEC landed (this file).
- T-2001 Switch `go.mod` deps: drop `asg017/sqlite-vec-go-bindings`, drop `ncruces/go-sqlite3` (direct), add `github.com/tursodatabase/turso-go v0.2.2`. Run `go mod tidy`.
- T-2002 Replace driver registration in `internal/memory/store.go`. Use `tursodatabase/turso-go` driver. Test that `sql.Open("turso", ...)` works.
- T-2003 Update `internal/memory/store.go` schema: drop `chunks_vec` (vec0); add Turso-native vector index. Bump `user_version` to 3.
- T-2004 Update `internal/memory/search.go`: drop manual KNN query; use Turso native vector syntax. RRF fusion stays the same shape.
- T-2005 Update `internal/runtime/assemble.go` if it imports `ncruces/go-sqlite3` directly. If only middle-of-the-stack uses it, the change is in `store.go` only.
- T-2006 Rewrite memory golden tests: replace `sqlite-vec` golden assertions with Turso-native-vector assertions. FTS5 golden tests unchanged.
- T-2007 `go build ./...` clean.
- T-2008 `CGO_ENABLED=0 go build ./...` clean (Turso is purego, cgo-free).
- T-2009 `go test ./...` 26/26 green, including the rewritten memory tests.
- T-2010 `go test -race ./internal/memory/... ./internal/coordinate/... ./internal/runtime/...` clean.
- T-2011 Single commit `chore(driver): migrate memory to Turso (0020)`. Push to origin/main.
- T-2012 Update `MEMORY.md` to record the driver change. Update `docs/adr/`.
- T-2013 Update ICM `decisions-openplus` topic with the architectural facts.

## Definition of done
- Turso is the only driver.
- sqlite-vec-go-bindings is gone.
- All 26 packages green.
- cgo-free invariant holds.
- `-race` clean on memory + concurrency-heavy packages.
- Leak guard still active.
- Single commit + push.
- No new ADR-required changes beyond ADR-0013.
- No deferred/backlog item introduced.
