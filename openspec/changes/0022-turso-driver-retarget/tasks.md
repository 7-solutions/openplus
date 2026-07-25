# Change 0022 — Tasks

> One task = one vertical slice = one PR. `[ ]` open · `[~]` in progress · `[x]` done.
> Gate order per openplus-build skill: Spec → Tests (red) → Implement (green) → Review → Commit.

## M0 — Spec (Gate 1: STOP for approval)
- [x] T-2200 PLAN (`proposal.md`) + TASKS shipped. Awaiting approval before any code.

## M1 — Re-target (the one code change)
- [x] T-2201 Swap the import in `internal/memory/store.go:30`: `_ "github.com/tursodatabase/turso-go"` → `_ "turso.tech/database/tursogo"`.
- [x] T-2202 `go.mod` swap: `go get turso.tech/database/tursogo@v0.7.1` then `go mod edit -droprequire github.com/tursodatabase/turso-go` then `go mod tidy`. Verify the archived path is fully gone from `go.mod` AND `go.sum`. (Note: `tursodatabase/turso-go-platform-libs v0.7.1` arrives as a legit indirect — the v0.7.1 libturso binary.)

## M2 — Doc-comment hygiene
- [x] T-2203 Updated the 4 doc-comment references in `store.go` (package doc, driver-registration comment, ensureSchema comment) to name `turso.tech/database/tursogo v0.7.1`; also corrected the now-stale "FTS5 not used / vector-only" ensureSchema comment to reflect that the lexical half lives in the 0021 modernc shadow. Historical OpenSpec docs (0020 proposal/tasks) and ADR-0014 untouched.

## M3 — Regression guard (the hard rule → its test)
- [x] T-2204 Added `github.com/tursodatabase/turso-go` (archived path) to `bannedDirectDeps` in `internal/ports/ports_test.go`. Hardened the matcher to **token-boundary** matching (`depTokenInLine`) so the banned short path does not substring-collide with the legit `.../turso-go-platform-libs`. RED→GREEN proven: injecting the archived dep fires the test; removing it passes despite platform-libs being present.

## M4 — Verify (Gates 2-4)
- [x] T-2205 `go build ./...` clean.
- [x] T-2206 `CGO_ENABLED=0 go build ./...` clean (purego preserved).
- [x] T-2207 `go test -count=1 ./...` 26/26 green (the existing memory suite is the red→green proof; a swap regression shows up here).
- [x] T-2208 `go test -race ./internal/memory/... ./internal/orchestrate/... ./internal/coordinate/... ./internal/runtime/... ./internal/ports/...` clean.
- [x] T-2209 `internal/ports/leak_guard_test.go` passes (no core pkg imports the Turso driver directly).
- [x] T-2210 `TestNoBannedDirectDeps` passes (now bans ncruces, sqlite-vec, AND the archived turso-go path; token-boundary matcher avoids collision with legit platform-libs).
- [x] T-2211 Sanity: `grep -rn "tursodatabase/turso-go" --include=*.go` returns only INTENTIONAL references — the store.go historical-re-target doc comment and the ports_test.go ban-list entry + matcher explanation. No stale imports/usages.

## M5 — Commit + propagate (Gate 5)
- [x] T-2212 Single commit `chore(driver): re-target Turso to canonical turso.tech/database/tursogo v0.7.1 (0022)`. Push (await explicit push instruction).
- [x] T-2213 Ship `docs/adr/0015-turso-driver-retarget.md`.
- [x] T-2214 Update `MEMORY.md` (driver pin in project context + ADR-0015 in decisions; mark the archived-path wrong-turn resolved).
- [x] T-2215 ICM `decisions-openplus` store (high).

## Notes for the implementer
- **The ONLY production-code change is one import line.** Everything else is go.mod, comments, the guard, and docs. If the diff grows beyond that, something is wrong — stop and re-read the proposal.
- **Driver name is `"turso"` on both v0.2.2 and v0.7.1** — no `driverName` constant changes, no call-site changes.
- **Do NOT touch** the 0020 OpenSpec proposal/tasks or ADR-0014. They are historical records; editing them rewrites history. ADR-0015 is where the new state is documented.
- **The modernc.org/sqlite shadow (0021) is independent** — it does not import or depend on the Turso driver path. It will not be affected by this swap.
- **If any memory test goes red after the swap**, the v0.7.1 libturso surface differs from the probe in a way that matters — capture the runtime error verbatim (per the durable rule: a runtime SQL error naming a symbol is ground truth) and decide amend-vs-revert before forcing it green.

## Rollback
`git revert HEAD` restores v0.2.2. No schema migration.
