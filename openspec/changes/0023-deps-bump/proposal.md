# Change 0023 — Fresh dependency bump

> Status: PROPOSED. Awaiting Gate 1 approval. **No code before approval.**

## Why
AGENTS.md mandates "Latest-stable, lockfile-pinned. Bump only after green
tests." The last bump attempt (change 0019) was **reverted** because
`go get -u ./...` crossed the ncruces `sqlite3.Binary` removal (v0.21+) which
broke the cgo-free build via the sqlite-vec bindings. That trap is now
**permanently gone**: changes 0020/0022 removed and banned ncruces +
sqlite-vec-go-bindings (`TestNoBannedDirectDeps`), and re-targeted the memory
driver to Turso v0.7.1 (canonical) + modernc.org/sqlite v1.54.0 (FTS5 shadow).
A fresh bump is now safe on the axis that broke 0019.

`go list -m -u all` shows ~26 deps with updates available, all minor/patch.

## What Changes
- **Snapshot first** (the 0019 discipline, now mandatory): capture pre-bump
  `go.mod`, `go.sum`, and `go list -m all` to
  `openspec/changes/0023-deps-bump/SNAPSHOT.md` for diff-comparison audit.
- **`go get -u ./...`** then **`go mod tidy`**.
- **No production code change** is expected. If any code change is needed to
  absorb a bump, STOP — that's scope creep beyond a deps bump and needs its own
  change. A deps bump is lockfile-only.
- **Verify** with the full gate, with the cgo-free build as a first-class gate
  (the 0019 lesson: the default build hid the ABI break the cgo-free path exposed).

## Watch-items (from the `go list -m -u all` ground truth)
These are the bumps most likely to surface a problem; none are known-bad, all
are flagged for close attention during verification:
1. **`ebitengine/purego` v0.10.0-alpha.2 → v0.10.2** — the cgo-free FFI that
   Turso's libturso loads through. Currently pinned to an **alpha**; the bump
   overrides the version Turso v0.7.1 declared in its own go.mod with the
   stable release. The memory tests (which exercise the actual Turso driver)
   are the canary. This is the single highest-risk bump in the set.
2. **`mattn/go-runewidth` v0.0.19 → v0.0.27** and **`mattn/go-isatty` v0.0.20 → v0.0.24** —
   8-patch and 4-patch TUI jumps. The TUI tests are the canary.
3. **`modernc.org/libc` v1.74.1 → v1.74.3** — modernc internals (transitive of
   modernc.org/sqlite, the FTS5 shadow). Patch; low risk.
4. **`golang.org/x/{exp,mod,sync,text,tools}`** — standard x-packages; routine.
5. **`mattn/go-sqlite3` v1.14.42 → v1.14.48** — present as a *test* dep of the
   Turso driver (indirect); not used by OpenPlus production code. Safe.

## ADR
**None new.** This change operationalizes ADR-0001's "latest-stable,
lockfile-pinned" and refines the dep-bump playbook already durable from 0019.
The SNAPSHOT.md discipline is the durable artifact, not a new ADR.

## Why this is low-risk now (vs 0019)
- The ncruces `sqlite3.Binary` removal — the exact symbol that broke 0019 —
  can no longer fire: ncruces is a build-time banned direct dep, and no
  production code imports it.
- The two engines on the critical path (Turso via purego, modernc transpiled)
  are both cgo-free and have their own test coverage; a bump regression shows
  up in `go test ./internal/memory/...` before it reaches a user.
- No major-version jumps in the available-updates set (all minor/patch).

## Alternatives considered
1. **Stay pinned** — rejected: the latest-stable house rule exists; purego is
   currently on an *alpha*, which is itself a latent risk worth resolving.
2. **Selective bumps (only safe-looking ones)** — rejected: violates the spirit
   of "latest-stable" and leaves the lockfile half-updated. A clean
   `go get -u ./...` with full verification is the correct shape; if it breaks,
   that's signal to investigate, not to cherry-pick.
3. **Bundle a code change to absorb a bump** — rejected at the gate: a deps
   bump is lockfile-only. Any required code change gets its own follow-up change.

## Scope (explicitly OUT of this change)
- Any production code change to absorb a bump. (If one is required, STOP and
  propose it as change 0024.)
- Bumping the Go toolchain itself (currently 1.26.5). Separate change.
- New features, new ports, new adapters.

## Rollback
`git revert HEAD`. The SNAPSHOT.md preserves the exact pre-bump lockfile state
for a clean restore if any bump surfaces a latent issue after merge.
