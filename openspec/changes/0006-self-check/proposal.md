# Change 0006 — AGENTS.md self-check pass (PLAN)

## Why
The `AGENTS.md` self-check block names six invariants the project must
hold before declaring work "done":

> ## Self-check before "done"
> - [ ] Approved OpenSpec PLAN + SPEC + TASKS existed before code
> - [ ] Tests written first, shown red, driven to green
> - [ ] Core depends on a port; new I/O is an adapter; no provider type leaked
> - [ ] cgo-free build still green
> - [ ] Advisor passed; graph updated; memory updated
> - [ ] No deferred/backlog item introduced

This change audits each item against the current tree (`HEAD b642b86`,
22 packages green, 0001–0005 all landed), produces evidence for the items
that pass, and **closes the two honest gaps** the audit surfaces.

## Audit at HEAD `b642b86`

| # | Item | Status | Evidence |
|---|---|---|---|
| 1 | Approved OpenSpec PLAN + SPEC + TASKS before code | ✅ Pass | 0001–0005 each followed the gate: proposal.md + tasks.md + spec.md landed before any production code. Gate 1 stopped between sub-scopes. |
| 2 | Tests first, red, driven to green | ✅ Pass | Every change used red-first TDD. Honored reds: T-010/T-011, T-403 (timeout), T-405 (ErrDimensionDrift), T-407 (FallbackTo), T-410 (AutoOpen), T-450 (Hello world count was wrong — corrected to 4), TestMainConfigFlagMissing (renamed). Regression guards added where the underlying code was already correct. |
| 3 | Core on ports; no provider type leaked | ✅ Pass | `grep -r 'provider\.AnthropicAdapter\|provider\.OpenAICompatAdapter' --include='*.go'` finds zero matches outside `internal/provider/`. Only `provider.Fake` (test seam) and `provider` (neutral port types) appear elsewhere. |
| 4 | cgo-free build green | ✅ Pass | `CGO_ENABLED=0 go build ./...` succeeds. Dependencies: `github.com/ncruces/go-sqlite3` (WASM on wazero), `github.com/pkoukk/tiktoken-go` (pure Go). |
| 5 | Advisor passed; graph updated; memory updated | ❌ **GAP** | No `graphify` advisor review has run on any change. **No `icm store` calls have fired *this session*** despite `CLAUDE.md`'s mandatory triggers. Pre-existing project memory is substantial (63 entries under `context-openplus` from prior sessions; 1 under `decisions-openplus`; 1 under `errors-resolved`) — the gap is *this session's* work, not the project's. |
| 6 | No deferred/backlog item introduced | ✅ Pass | 0003–0005 add embedder config deltas, memory config deltas, cmd wiring deltas, tiktoken-go, and offline loader. None touches voice/ASR, Max Mode, MCP marketplace, web/share UI, hosted server, or goja `.js`. |

**Two honest gaps in item 5.** Both close inside this change.

## What changes

### Sub-scope A — Advisor review per change
For each landed change (0001, 0002, 0003, 0004, 0005), run a `graphify`
advisor pass and capture the findings. Findings that name a real
defect must be fixed inside this change (or escalated to a follow-up
0007 if the scope is large); findings that are advisory notes can be
recorded as durable knowledge.

- 0001-foundation: 32 tasks landed before this session; advisor pass is
  largely a regression-check against `HEAD b642b86` (which contains all
  the fixes since).
- 0002-live-wiring: integration slice; review the runtime.Session godoc
  and the assemble_test coverage.
- 0003-close-scaffold-gaps: scaffolding cleanup; trivial.
- 0004-config-integration: largest surface — embedder env, memory
  deltas, cmd wiring, integration tests. Reviewer should focus on the
  cross-subsystem seams.
- 0005-tiktoken-tokenizer: third-party dep added. Reviewer should
  check the offline build path and the cache-dir default.

If a finding produces a defect fix, it ships as its own commit
following the existing one-slice = one-commit pattern.

### Sub-scope B — ICM memory persistence
Per `~/.claude/CLAUDE.md`, every significant task completion and every
architecture decision must produce an `icm store` call. **This session**
shipped 5 changes (0003, 0002, 0004 sub-scopes A/B/C/D, 0005) and made
dozens of architecture decisions — none were persisted to ICM by this
session.

The pre-existing project memory in ICM is substantial (63 entries under
`context-openplus` from prior sessions; 1 entry under `decisions-openplus`;
1 under `errors-resolved`); the gap is *this session's* work, not the
project's. The backfill targets this session only.

- One `icm store` per **this session's** change under topic
  `context-openplus`, importance `high`, summarizing the change in
  2-4 sentences.
- One `icm store` per durable decision surfaced this session, under
  topic `decisions-openplus`, importance `high`.
- One `icm store` per error resolved this session (T-410 AutoOpen
  build error, T-450 reference count off-by-2, tiktoken field shadow),
  under topic `errors-resolved`, importance `medium`.

### Sub-scope C — Documentation of audit results
A new file `openspec/changes/0006-self-check/AUDIT.md` records the
evidence for items 1, 2, 3, 4, 6 (already-passing) and the resolution
for item 5. The file becomes the durable reference for the next
self-check pass.

## Why this is a separate change (not just a fix-up commit)
Two reasons:
- The AGENTS.md self-check is a project-wide invariant, not a
  per-feature property. Recording it as its own change makes the audit
  visible in the commit history and lets a future contributor
  `git log --grep=0006` to find it.
- The fix-up work (advisor findings + ICM backfill) is substantial and
  warrants its own proposal + tasks file under the house gate.

## Non-goals (explicitly out of scope)
- Changing the AGENTS.md self-check criteria. The six items are the
  bar; this change makes the project meet the bar.
- A "graphify skill" installation or graphify-rig setup. The advisor
  pass uses whatever the agent already has access to (`/graphify`
  command from `~/.claude/skills/graphify/SKILL.md` if available; else
  manual review against the spec).
- Fixing pre-existing defects discovered by the advisor. Each
  finding becomes its own task; this change only **surfaces and
  escalates** them, not fixes them silently.

## Governing decisions
- ADR-0001 (Crush base, config compatibility)
- ADR-0002 (MiMoCode feature milestone)
- ADR-0008 (context budgeter / tokenizer / reconstruction) — relevant
  only if the advisor pass touches contextmgr.

No new ADRs.

## Risk
- **Advisor pass finds a real defect that requires a non-trivial fix.**
  Mitigation: each finding becomes a T-6xx task in this change; if the
  fix is large, escalate to a follow-up change with its own proposal.
  No silent fixes in this change.
- **ICM backfill becomes verbose.** Mitigation: one entry per change,
  not one per commit. Cap the prose at 2-4 sentences per entry.
- **`graphify` skill unavailable** (only present on systems with the
  installed skill). Mitigation: fall back to manual review against the
  spec, with the audit document naming exactly which evidence was
  checked.

## Verification
1. `openspec/changes/0006-self-check/AUDIT.md` exists and names
   evidence for all six self-check items.
2. `go build ./...` clean (item 4 re-confirmed after any advisor
   fixes).
3. `go test ./...` 22/22 green (item 2 re-confirmed).
4. `CGO_ENABLED=0 go build ./...` clean (item 4 cgo-free).
5. `grep -r 'provider\.AnthropicAdapter\|provider\.OpenAICompatAdapter' --include='*.go' .` returns zero matches
   outside `internal/provider/` (item 3 re-confirmed).
6. `icm list --topic context-openplus` shows ≥ 1 new entry per
   landed change in this session (0003, 0002, 0004, 0005), filterable
   by date.
7. `icm list --topic decisions-openplus` shows ≥ 1 new entry from
   this session's durable decisions.
8. `icm list --topic errors-resolved` shows ≥ 1 new entry from
   this session's resolved errors.

## Approval
Per house Gate 1, implementation begins only after this proposal +
the delta spec + tasks are approved.