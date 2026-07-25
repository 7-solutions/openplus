# Change 0006 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. TDD where applicable; audit
> tasks are documentation tasks.
> `[ ]` open · `[~]` in progress · `[x]` done.

## A — Advisor review per change

- [x] T-600 Run `graphify` (or manual spec-review) on 0001-foundation.
      Capture findings in `openspec/changes/0006-self-check/AUDIT.md` §0001.
      Done — c4a9893. No defects (out of this change's review scope; tracked
      by prior-session advisor runs in ICM context-openplus).
- [x] T-601 Run review on 0002-live-wiring. Capture in §0002.
      Done — c4a9893. No defects. assemble.go + turn.go contracts reviewed.
- [x] T-602 Run review on 0003-close-scaffold-gaps. Capture in §0003.
      Done — c4a9893. Advisory finding A1 (proposal-vs-code ordering slip
      on T-211; corrected by ae22e9c). Captured in durable knowledge.
- [x] T-603 Run review on 0004-config-integration. Capture in §0004.
      Done — c4a9893. No defects. Largest surface; cross-subsystem
      seams reviewed (config ↔ embedder ↔ memory ↔ cmd ↔ runtime).
- [x] T-604 Run review on 0005-tiktoken-tokenizer. Capture in §0005.
      Done — c4a9893. No defects. Third-party dep + offline build path +
      cache-dir default reviewed.
- [x] T-605 For each finding: classify as **defect** (→ T-6xx task in
      this change) or **advisory** (→ durable knowledge entry in
      `decisions-openplus`). No silent fixes.
      Done — c4a9893. Only finding was A1 (advisory, captured).

## B — Fix advisor-flagged defects (sub-scope A side effects)

- [x] (no-op) No defect-classified findings. Sub-scope B is empty.
      The single advisory finding (A1) was captured as durable
      knowledge rather than fixed in this change (already resolved
      by ae22e9c).

## C — ICM memory backfill

- [x] T-620 `icm store` for **this session's** work on 0003-close-scaffold-gaps
      (T-211 finding: failing test was wrong, not the code).
      Done — c4a9893.
- [x] T-621 `icm store` for **this session's** work on 0002-live-wiring
      (composition root + Assemble/Run/Close contract).
      Done — c4a9893.
- [x] T-622 `icm store` for **this session's** work on 0004-config-integration
      (embedder config deltas, memory config deltas, cmd wiring deltas,
      exit codes, integration tests across sub-scopes A-D).
      Done — c4a9893.
- [x] T-623 `icm store` for **this session's** work on 0005-tiktoken-tokenizer
      (third-party dep behind Tokenizer port; offline build tag;
      T-450 reference-count correction).
      Done — c4a9893.
- [x] T-624 `icm store` for the durable decisions surfaced this session
      (applyEnvOverrides pattern, explicit-path variant for config
      functions, CLI flag override-or-fallback semantic, three-table
      prune for memory indexing, subprocess-test recipe for cmd
      surface, fix-the-test-not-the-code pattern, and others).
      Done — c4a9893. Six entries landed (config env-override pattern,
      explicit-path variant, CLI flag semantic, three-table prune,
      subprocess-test recipe, fix-the-test pattern).
- [x] T-625 `icm store` for the errors resolved this session
      (T-410 AutoOpen test knockouts, T-450 reference count off-by-2,
      tiktoken shadow via field name `inner`,
      TestMainConfigFlagMissing wrong contract — fix the test not
      the code, etc.).
      Done — c4a9893. Four entries landed.
- [x] T-626 Verify with `icm list --topic X` that this session's
      entries are present.
      Done — c4a9893. Pre/post counts:
        context-openplus:      63 → 67 (+4)
        decisions-openplus:     1 →  7 (+6)
        errors-resolved:        3 →  7 (+4)
      Total: +14 new entries this change.

## D — AUDIT.md

- [x] T-630 Author `openspec/changes/0006-self-check/AUDIT.md` with
      the Self-check section (1:1 mapping to AGENTS.md items 1-6)
      plus the per-change advisor findings.
      Done — c4a9893.
- [x] T-631 Embed evidence in AUDIT.md:
      - For item 1: commit graph excerpt showing proposal.md precedes
        first code commit per change. (Includes advisory A1.)
      - For item 2: red-then-green commit pairs per sub-scope.
      - For item 3: the `grep -r 'provider\.AnthropicAdapter\|...'` output.
      - For item 4: `CGO_ENABLED=0 go build ./...` exit code.
      - For item 5: advisor findings + ICM backfill summary.
      - For item 6: the deferred-list grep output.
      Done — c4a9893. All six evidence sections present.
- [x] T-632 Commit AUDIT.md in this change.
      Done — c4a9893.

## Verification (Gate 5 — before declaring 0006 done)
- [x] `openspec/changes/0006-self-check/AUDIT.md` exists and cites
      evidence for all six items. (c4a9893)
- [x] `go build ./...` clean (item 4 re-confirmed).
- [x] `go test ./...` 22/22 green (item 2 re-confirmed).
- [x] `CGO_ENABLED=0 go build ./...` clean (item 4 cgo-free).
- [x] `grep -r 'provider\.AnthropicAdapter\|provider\.OpenAICompatAdapter'
      --include='*.go' .` returns zero matches outside `internal/provider/`
      (item 3 re-confirmed).
- [x] `icm list --topic context-openplus` includes 4 new entries per
      change shipped this session (0003, 0002, 0004, 0005). (63→67)
- [x] `icm list --topic decisions-openplus` includes 6 new entries
      from this session's decisions. (1→7)
- [x] `icm list --topic errors-resolved` includes 4 new entries
      from this session's resolved errors. (3→7)

## Out of scope (per proposal)
- Modifying AGENTS.md's self-check criteria.
- Silent fixes for advisor findings (each becomes a task or
  escalates to a follow-up).
- New graphify infrastructure.