# Change 0006 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. TDD where applicable; audit
> tasks are documentation tasks.
> `[ ]` open · `[~]` in progress · `[x]` done.

## A — Advisor review per change

- [ ] T-600 Run `graphify` (or manual spec-review) on 0001-foundation.
      Capture findings in `openspec/changes/0006-self-check/AUDIT.md` §0001.
- [ ] T-601 Run review on 0002-live-wiring. Capture in §0002.
- [ ] T-602 Run review on 0003-close-scaffold-gaps. Capture in §0003.
- [ ] T-603 Run review on 0004-config-integration. Capture in §0004.
      Focus on cross-subsystem seams (embedder env + memory deltas +
      cmd wiring + integration tests).
- [ ] T-604 Run review on 0005-tiktoken-tokenizer. Capture in §0005.
      Focus on third-party dep + offline build path + cache-dir default.
- [ ] T-605 For each finding: classify as **defect** (→ T-6xx task in
      this change) or **advisory** (→ durable knowledge entry in
      `decisions-openplus`). No silent fixes.

## B — Fix advisor-flagged defects (sub-scope A side effects)

- [ ] T-610..T-619 One task per defect-classified finding from T-605.
      Each task is a small slice: red test if applicable, then fix,
      then commit. If a finding is large, escalate to a follow-up
      change with its own proposal; do not bloat this change.

## C — ICM memory backfill

- [ ] T-620 `icm store` for 0001-foundation (32-task foundation +
      architecture decisions).
- [ ] T-621 `icm store` for 0002-live-wiring (composition root +
      Assemble/Run/Close contract).
- [ ] T-622 `icm store` for 0003-close-scaffold-gaps (T-211 finding:
      failing test was wrong, not the code).
- [ ] T-623 `icm store` for 0004-config-integration (embedder
      config deltas, memory config deltas, cmd wiring deltas, exit
      codes, integration tests).
- [ ] T-624 `icm store` for 0005-tiktoken-tokenizer (third-party dep
      behind Tokenizer port; offline build tag).
- [ ] T-625 `icm store` for the durable decisions from the session
      checkpoint §7 (one entry per durable pattern).
- [ ] T-626 `icm store` for the resolved errors from the session
      checkpoint §8 (T-410, T-450 reference count, tiktoken field shadow,
      etc.).
- [ ] T-627 Verify with `icm list --topic context-openplus` that all 5
      changes are present; `icm list --topic decisions-openplus` and
      `icm list --topic errors-resolved` are non-empty.

## D — AUDIT.md

- [ ] T-630 Author `openspec/changes/0006-self-check/AUDIT.md` with
      the Self-check section (1:1 mapping to AGENTS.md items 1-6)
      plus the per-change advisor findings.
- [ ] T-631 Embed evidence in AUDIT.md:
      - For item 1: commit graph excerpt showing proposal.md precedes
        first code commit per change.
      - For item 2: red-then-green commit pairs per sub-scope.
      - For item 3: the `grep -r 'provider\.AnthropicAdapter\|...'` output.
      - For item 4: `CGO_ENABLED=0 go build ./...` exit code.
      - For item 5: advisor findings + ICM backfill summary.
      - For item 6: the deferred-list grep output.
- [ ] T-632 Commit AUDIT.md in this change.

## Verification (Gate 5 — before declaring 0006 done)
- [ ] `openspec/changes/0006-self-check/AUDIT.md` exists and cites
      evidence for all six items.
- [ ] `go build ./...` clean (item 4 re-confirmed).
- [ ] `go test ./...` 22/22 green (item 2 re-confirmed).
- [ ] `CGO_ENABLED=0 go build ./...` clean (item 4 cgo-free).
- [ ] `grep -r 'provider\.AnthropicAdapter\|provider\.OpenAICompatAdapter'
      --include='*.go' .` returns zero matches outside `internal/provider/`
      (item 3 re-confirmed).
- [ ] `icm list --topic context-openplus` shows ≥ 5 entries
      (one per change).
- [ ] `icm list --topic decisions-openplus` non-empty.
- [ ] `icm list --topic errors-resolved` non-empty.

## Out of scope (per proposal)
- Modifying AGENTS.md's self-check criteria.
- Silent fixes for advisor findings (each becomes a task or
  escalates to a follow-up).
- New graphify infrastructure.