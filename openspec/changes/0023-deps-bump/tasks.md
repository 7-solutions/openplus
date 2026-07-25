# Change 0023 — Tasks

> One task = one vertical slice = one PR. `[ ]` open · `[~]` in progress · `[x]` done.
> Playbook per the durable dep-bump discipline from change 0019 (REVERTED, lessons preserved).

## M0 — Spec (Gate 1: STOP for approval)
- [x] T-2300 PLAN (`proposal.md`) + TASKS shipped. Awaiting approval before any action.

## M1 — Snapshot (the audit artifact; mandatory)
- [x] T-2301 Captured pre-bump state to `SNAPSHOT.md`: `go list -m all` + build-clean confirmation + git HEAD `45286c9` (pre-bump `go.mod`/`go.sum` restorable via `git show 45286c9:go.mod`). Watch-items marked inline.

## M2 — Bump
- [x] T-2302 `go get -u ./...`.
- [x] T-2303 `go mod tidy`.
- [x] T-2304 `TestNoBannedDirectDeps` passes — ncruces, sqlite-vec, and the archived turso-go path did NOT reappear as direct deps.

## M3 — Verify (Gates 2-4; cgo-free is first-class)
- [x] T-2305 `go build ./...` clean.
- [x] T-2306 `CGO_ENABLED=0 go build ./...` clean — **the gate 0019's break escaped. PASSED** (purego alpha→stable did not break the Turso driver).
- [x] T-2307 `go test -count=1 ./...` 26/26 green. Memory suite exercises both Turso (purego) and the modernc shadow; no regression.
- [x] T-2308 `go test -race` on memory/orchestrate/coordinate/runtime/ports clean.
- [x] T-2309 leak guard + `TestNoBannedDirectDeps` both pass.
- [x] T-2310 Smoke: `openplus --version` → `openplus dev`; Turso `:memory:` open + `vector32()` round-trip → `sqlite_version=3.50.4 OK`.

## M4 — Diff review (the audit step)
- [x] T-2311 Diffed bumped `go.mod` against SNAPSHOT. Notable jumps (for commit message): purego v0.10.0-alpha.2→v0.10.2 (the watch-item; clean), mattn/go-runewidth v0.0.19→v0.0.27, mattn/go-isatty v0.0.20→v0.0.24, modernc/libc v1.74.1→v1.74.3, charmbracelet/{colorprofile v0.4.1→v0.4.3, x/ansi v0.11.6→v0.11.7}, golang.org/x/text v0.39.0→v0.40.0, plus routine clipperhouse/dlclark/go-sourcemap/pprof/go-colorful. `clipperhouse/stringish` dropped (no longer required). `ncruces/go-strftime` (stale indirect, NOT the banned go-sqlite3) remains — allowed. **Zero production code changed.**

## M5 — Commit + propagate (Gate 5)
- [ ] T-2312 Single commit `chore(deps): bump pinned dependencies to latest stable (0023)`. Push (await explicit push instruction).
- [ ] T-2313 ICM `decisions-openplus` store (medium — routine bump; note any watch-item that surfaced).

## Notes for the implementer
- **Lockfile-only.** If any `.go` file needs editing to absorb a bump, STOP — that's scope creep; propose it as change 0024 and revert the bump. Do not bundle code changes into a deps bump.
- **The cgo-free build is the canary that 0019 lacked.** If T-2306 goes red, capture the exact symbol error verbatim (per the durable rule: a runtime/compiler error naming a symbol is ground truth) before deciding investigate-vs-revert.
- **purego alpha→stable (watch-item #1)** is the highest-risk bump. If the memory tests fail after the bump with a libturso/purego loading error, the fix is to pin purego back to the alpha in a follow-up, NOT to force it — the alpha is what Turso v0.7.1 was tested against.
- **Do NOT touch** Go toolchain version, production code, or architectural guards.
- **The banned-dep list is load-bearing**: if `go get -u` somehow resurrects ncruces/sqlite-vec/archived-turso-go as a direct dep, `TestNoBannedDirectDeps` will fail — that's the guard doing its job; investigate why, don't weaken the guard.

## Rollback
`git revert HEAD`. SNAPSHOT.md preserves the pre-bump lockfile for a clean restore.
