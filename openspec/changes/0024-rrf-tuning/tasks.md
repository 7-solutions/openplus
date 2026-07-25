# Change 0024 — Tasks

> One task = one vertical slice = one PR. `[ ]` open · `[~]` in progress · `[x]` done.
> Gate order per openplus-build skill: Spec → Tests (red) → Implement (green) → Review → Commit.

## M0 — Spec (Gate 1: STOP for approval)
- [x] T-2400 PLAN (`proposal.md`) + TASKS shipped. Awaiting approval before any code.

## M1 — RRFConfig type + option (TDD: red first)
- [x] T-2401 Wrote `internal/memory/rrf_test.go` (red): DefaultRRF()=={60,1,1}, Open applies default, WithRRF overrides, zero-is-explicit. Shown red.
- [x] T-2402 Implemented to green: `RRFConfig`, `DefaultRRF()`, `WithRRF(cfg)`, `Store.rrf`; `Open` sets `s.rrf = DefaultRRF()` before options. 4 config tests green.

## M2 — Weighted fusion (TDD: red first)
- [x] T-2403 Wrote `internal/memory/search_rrf_test.go` (red): (a) default-option == no-option score equality; (b) VectorWeight=10 → vector-only match wins; (c) LexicalWeight=10 → lexical-only match wins; (d) K controls steepness (ratio (K+1)/K). Shown red.
- [x] T-2404 Implemented to green in `search.go` + `fts.go`: `ftsIndex.search` gains `rrfK float64` param; vector loop `VectorWeight/(K+rank)`; FTS fusion `LexicalWeight * contribution`; removed the `rrfK` const. Updated 7 existing call sites to pass `DefaultRRF().K`. All RRF/FTS/hybrid/search tests green; `TestHybridSearchBoostsLexicalMatch` (+1/60) preserved under default.

## M3 — Verify (Gates 2-4)
- [x] T-2405 `go build ./...` clean.
- [x] T-2406 `CGO_ENABLED=0 go build ./...` clean.
- [ ] T-2407 `go test -count=1 ./...` green (existing hybrid tests incl. `TestHybridSearchBoostsLexicalMatch` must still pass under the default config — the +1/60 boost is preserved).
- [x] T-2408 `go test -race ./internal/memory/... ./internal/orchestrate/... ./internal/coordinate/... ./internal/runtime/... ./internal/ports/...` clean.
- [x] T-2409 Architectural guards: leak guard + `TestNoBannedDirectDeps` still pass (no dep change expected).

## M4 — Commit + propagate (Gate 5)
- [ ] T-2410 Single commit `feat(memory): configurable RRF weights and K (0024)`. Push (await explicit push instruction).
- [x] T-2411 Update `MEMORY.md` ADR-0014 entry to note the fusion is now tunable via `WithRRF`; record `DefaultRRF() = {60,1,1}`.
- [ ] T-2412 ICM `decisions-openplus` store (medium).

## Notes for the implementer
- **Backward compat is load-bearing.** `Open(path)` and `Open(path, WithFTS())` MUST produce identical rankings to pre-0024. The `TestHybridSearchBoostsLexicalMatch` test (which asserts +1/60) is the canary — if it breaks, the default isn't {60,1,1}.
- **No zero-value magic.** A caller passing `WithRRF(RRFConfig{})` gets `{0,0,0}` — that's their explicit choice (zeroes out everything). The default comes from `DefaultRRF()`, set by `Open` before options. Document this on `WithRRF`.
- **`ftsIndex.search` signature change** (adds `rrfK float64`) is internal — no external caller. Update the one call site in `search.go`.
- **No new dep, no schema change, no port change.** If the diff grows beyond `internal/memory/{rrf_test,rrf,search,fts,store}.go`, something is wrong — re-read the proposal.

## Rollback
`git revert HEAD`. No schema migration.
