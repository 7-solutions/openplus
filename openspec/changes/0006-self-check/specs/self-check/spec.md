# AGENTS.md self-check pass — audit invariants (delta — change 0006)

> Change 0006 audits the project against `AGENTS.md`'s self-check block
> and closes the two honest gaps it surfaces. The base invariants are
> unchanged; this file documents the **evidence** the audit must
> produce for each item.

## Purpose
Make the project's state visible against every item in `AGENTS.md`'s
self-check block. Pass with durable evidence for the items that already
hold; close the two gaps in item 5 (advisor review + memory persistence).

## Requirements

### Requirement: Six self-check items each have evidence
The change SHALL produce an `AUDIT.md` that, for each of the six items
in `AGENTS.md`'s self-check block, names the evidence that proves the
item holds at HEAD.

#### Scenario: Item 1 — Approved OpenSpec PLAN + SPEC + TASKS existed before code
- **WHEN** the audit reads the commit graph
- **THEN** every code commit is preceded (in time) by a docs commit
  landing the relevant `proposal.md` + `tasks.md` + `specs/<cap>/spec.md`
- **AND** the AUDIT.md cites at least one such trio per change

#### Scenario: Item 2 — Tests first, red, driven to green
- **WHEN** the audit inspects the commit history of a landed change
- **THEN** the test files modified in that change's first commit land
  RED (build-fail or assertion-fail) and a later commit in the same
  change makes them GREEN
- **AND** the AUDIT.md cites the red-then-green commits for at least
  one slice per sub-scope

#### Scenario: Item 3 — Core on ports; no provider type leaked
- **WHEN** the audit greps for `provider.AnthropicAdapter` and
  `provider.OpenAICompatAdapter` outside `internal/provider/`
- **THEN** no matches are returned
- **AND** the AUDIT.md embeds the grep output

#### Scenario: Item 4 — cgo-free build green
- **WHEN** the audit runs `CGO_ENABLED=0 go build ./...`
- **THEN** the build succeeds
- **AND** the AUDIT.md embeds the exit code

#### Scenario: Item 5 — Advisor passed; graph updated; memory updated
- **WHEN** the audit runs `graphify` (or manual spec-review) on each
  landed change (0001, 0002, 0003, 0004, 0005)
- **THEN** advisor findings are recorded in the AUDIT.md with
  classification: defect (→ T-6xx task in this change) vs advisory
  note (→ durable knowledge)
- **AND** `icm store` has been called for each landed change (one
  entry per change), each durable decision, and each resolved error
- **AND** `icm list --topic context-openplus` returns at least 5 entries

#### Scenario: Item 6 — No deferred/backlog item introduced
- **WHEN** the audit greps 0003, 0004, 0005 changes for any of:
  voice/ASR, Max Mode, MCP marketplace, web/share UI, hosted server,
  goja `.js`
- **THEN** no matches are returned
- **AND** the AUDIT.md cites the grep

### Requirement: ICM backfill for this session
The change SHALL call `icm store` for every change shipped this session
(0003, 0002, 0004, 0005) plus the durable decisions captured in
session memory, plus the resolved errors.

#### Scenario: One entry per change
- **WHEN** the change runs the ICM backfill script
- **THEN** `icm list --topic context-openplus` shows exactly one entry
  per landed change (0001 through 0005)
- **AND** each entry's content names the change ID and the headline

#### Scenario: One entry per durable decision
- **WHEN** the change captures the durable decisions from the session
  checkpoint (§7 of `openspec/changes/0006-self-check/checkpoint.md`)
- **THEN** `icm list --topic decisions-openplus` shows at least one
  entry per durable decision
- **AND** each entry's importance is `high`

#### Scenario: One entry per resolved error
- **WHEN** the change captures the resolved errors from the session
  (§8 of `openspec/changes/0006-self-check/checkpoint.md`)
- **THEN** `icm list --topic errors-resolved` shows the entries
- **AND** each entry names the error and the fix

### Requirement: AUDIT.md is durable
The audit document SHALL be committed to `openspec/changes/0006-self-check/AUDIT.md`
and SHALL contain a "Self-check" section that maps 1:1 to the six items
in `AGENTS.md`.

#### Scenario: A future contributor can re-run the audit
- **WHEN** the contributor reads `openspec/changes/0006-self-check/AUDIT.md`
- **THEN** the document names the exact commands used to gather
  evidence for each item, so a future run can reproduce the checks

## Out of scope (per proposal)
- Changing the AGENTS.md self-check criteria.
- Installing new skills or graphify infrastructure.
- Silently fixing defects surfaced by the advisor pass — each
  finding becomes a T-6xx task or escalates to a follow-up change.