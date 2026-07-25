# Change 0019 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. `[ ]` open · `[~]` in progress · `[x]` done.

## M0 — Spec
- [x] T-1900 OpenSpec change 0019 approved (proposal + this task list).

## M1 — Snapshot
- [ ] T-1901 Capture pre-bump state → `openspec/changes/0019-deps-bump/SNAPSHOT.md`
      (`go.mod`, `go.sum`, `go list -m all`).

## M2 — Bump
- [ ] T-1902 `go get -u ./...` — refresh all 20 modules.
- [ ] T-1903 `go mod tidy` — canonicalize the graph.

## M3 — Verify (stop-the-line at any red)
- [ ] T-1904 `go build ./...` green.
- [ ] T-1905 `go test ./...` green.
- [ ] T-1906 `CGO_ENABLED=0 go build ./...` green.
- [ ] T-1907 `-race` on `internal/memory`, `internal/coordinate`, `internal/runtime`.

## M4 — Commit + propagate
- [ ] T-1908 `go.mod` directive line stays at `1.26` (or whichever is required).
- [ ] T-1909 Single commit `chore(deps): bump pinned dependencies (0019)`.
- [ ] T-1910 Push to origin/main. CI re-validates.

## Out of scope (note for the reviewer)
- Big-bang refactors (no port moves, no major version migrations not required
  by the bump).
- Pinning/unpinning policy changes.
- Toolchain bump beyond 1.26 (no such release available at the time of the
  change).
