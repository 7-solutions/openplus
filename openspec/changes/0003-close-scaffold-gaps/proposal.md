# Change 0003 — Close T-010 / T-011 scaffolding gaps + align TUI/runner user-turn contract (PLAN)

## Why
Two things landed on the same commit-ready batch.

### Part A — Stale scaffolding notes in 0001
The 0001 tasks file marks **T-010** ("Neutral Request/Event/Block model") and
**T-011** ("SSE reader") as `[~]` scaffolded, with the inline note that
"adapter-specific event parsing (`content_block_delta`, `chat.completion.chunk`)
is still open" and "round-trip JSON tests still open (adapters don't exist yet
to round-trip against)."

That note is stale. Both adapters now exist, the SSE reader is fully wired, and
the contract test (`internal/provider/contract_test.go`) already exercises
exactly those parsing paths:

- `content_block_delta` with `text_delta` and `input_json_delta` (anthropic
  stream, exercised at `TestContractRoundTrip` line 173).
- `chat.completion.chunk` with `tool_calls` and `finish_reason` (openai stream,
  exercised at the same test).
- Round-trip from neutral Request → native wire → neutral ToolCall, plus a
  separate `TestContractRoundTripToolResult` for the assistant tool-call +
  tool-result turn pair.
- A byte-for-byte equality test (`TestContractNeutralOutputIsEqualAcrossAdapters`,
  line 219) that pins the provider-neutrality invariant.

The `go test ./...` run on 2026-07-25 is green for 19/20 packages including
`internal/provider`, `internal/provider/anthropic`, `internal/provider/openaicompat`.
(The 20th, `internal/tui`, has an unrelated failure — Part B below.)

### Part B — TUI / runner user-turn contract mismatch
`internal/tui/model_test.go:68 TestSubmitAppendsUserAndStartsBusy` fails with
`history = []`. The test expects `submit()` to append a
`provider.Message{Role: provider.RoleUser, Blocks: [{Text: "hello there"}]}` to
`m.history`. Current `submit()` (model.go:184) only saves the text to
`m.pendingInput` and logs the `❯ …` prefix to `m.log`.

**The failing test is wrong.** The model does *not* own user-turn assembly —
`runtime.Session.Run(ctx, userMsg, history)` does, at
`internal/runtime/turn.go:52` (`append(history, userMessage(userMsg))`). The
test was written assuming the model owns history, but the runtime's API
treats `userMsg` as a fresh user string and `history` as prior turns.

Pinned by the runtime's tests:

- `internal/runtime/turn_test.go:160 TestAssembleContextCarriesHistory` —
  prior history plus a new `userMsg` produces a history whose last message
  is `userMsg`.
- `internal/runtime/turn_test.go:184 TestRunDrivesTheLoop` —
  `s.Run(ctx, "hello", nil)` produces a history whose first turn is
  `"hello"`.
- `internal/tui/runner_test.go:21 stubRunner.Run` (pre-fix) returned
  `append(history, assistant)` — i.e. preserved the history it got,
  without injecting a user message from `input`. The stub and the real
  runner disagreed on where the user turn comes from; the real runner
  owned it, the stub didn't.

If `submit()` were changed to append the user message to `m.history`, the
runtime would emit it twice on the wire — silent double-count caught only
by reading the contract. So the right fix is to **align the stub with the
runtime's contract and rewrite the test**, not to change `submit()`.

## What changes

### Part A — bookkeeping only
- `openspec/changes/0001-foundation/tasks.md`: flip T-010 and T-011 from `[~]`
  to `[x]`. Remove the stale scaffolding note under M1. Refresh the
  verification stamp with today's green set.
- No production code, no new tests, no port/adapter/capability added.

### Part B — align the TUI test fixture with the runtime's contract
- `internal/tui/runner_test.go stubRunner.Run`: append
  `userMessage(input)` to the history it returns, mirroring
  `runtime.Session.Run`'s contract at `turn.go:52`. The stub now produces
  the same `[user, assistant]` shape the real runner does.
- `internal/tui/model_test.go:68 TestSubmitAppendsUserAndStartsBusy`:
  rewritten to drive the end-to-end flow (enter → submit → runTurn →
  `turnDoneMsg`) and assert that `m.history` ends up as `[user, assistant]`.
  The original intent (the user turn is recorded in history) is preserved
  via the path that actually produces it: the runner's return value, not
  `submit()`.
- **No change to `internal/tui/model.go submit()`** — the production code
  was correct. The failing test had drifted from the runtime contract.
- No port, no API change. `Runner.Run(ctx, input, history)` keeps the
  same signature. `runtime.Session.Run(ctx, userMsg, history)` keeps the
  same signature. The only thing that moves is which layer assembles the
  user message into history: the runner (correct), not the model (wrong).

## Why no new tests (Part A)
The two existing test files already cover what the stale note claimed was
missing. Adding a third layer of "neutral round-trip" tests would duplicate
coverage without raising the invariant. The proper test for "neutral JSON
round-trips" is to (a) `json.Marshal` a `Request`, (b) `json.Unmarshal` it, (c)
deep-equal the two — but the adapters already do that on every Stream call
(the wire recorder sees and parses the marshaled body). Adding an explicit
unit test for the marshal step alone would test the stdlib, not our code.

## Why the rewritten test is enough (Part B)
The rewritten `TestSubmitAppendsUserAndStartsBusy` asserts:
- after enter: `m.busy == true`, `m.input.Value() == ""`, `len(m.history) == 0`
  (history stays empty until the runner returns);
- after the `turnDoneMsg` is delivered: `m.busy == false`, `len(m.history) == 2`,
  `m.history[0]` is the user message, `m.history[1]` is the assistant reply.

It covers both halves of the seam — `submit()`'s capture (input is gone,
busy is set, history is untouched) and `turnDoneMsg`'s assignment
(history lands as `[user, assistant]`) — so a regression in either half
surfaces here. Adding "submit two in a row" or "submit then runTurn"
cases would expand into territory that belongs to the runtime tests in
0002, not the TUI unit tests here.

## Governing decisions
ADR-0001 (Crush base), ADR-0005 (provider-neutral domain model).
No new ADRs.

## Risk
- **Premature flip in Part A** if the adapters are silently regressed in a way
  the tests don't catch. Mitigation: re-run `go build ./... && go test ./...`
  before the Part A commit; paste output in the commit message. *Materialized
  outcome:* `go test ./...` was 19/20 green at Part A commit time; Part B's
  fix flipped it to 20/20.
- **Part B double-count (predicted here, materialized)** — `runTurn()`
  (`model.go:171`) reads `m.history` to pass to the runner. If `submit()`
  appended the user message to history *and* the runtime also appended it,
  the user turn would appear twice on the wire. Mitigation: T-211 verified
  the runtime's contract before touching `submit()`. *Materialized outcome:*
  the runtime does append the user message itself, so the fix went to the
  test fixture and the test itself, not to `submit()`. No double-count
  occurred because no `submit()` change landed.

## Verification
1. `go build ./...` — must be clean.
2. `go test ./...` — **must be 20/20 green** after this change. Part A's
   bookkeeping doesn't add packages; Part B's test-only changes must flip
   the TUI package from red to green.
3. `go test ./internal/tui/... -run TestSubmitAppendsUserAndStartsBusy -v` —
   confirm the specific failing test now passes.
4. `go test ./internal/runtime/...` — confirm the runtime's pinning tests
   (`TestAssembleContextCarriesHistory`, `TestRunDrivesTheLoop`) still
   pass after the TUI stub change.
5. `grep -n 'content_block_delta\|input_json_delta\|chat.completion.chunk' internal/provider` —
   must still show the parsing paths in both adapter tests and the contract
   test.

## Approval
Per house Gate 1, implementation begins only after this proposal + the
updated `0001-foundation/tasks.md` are approved. No capability delta; no
`specs/` file needed for either part — Part A is bookkeeping, Part B is a
bug fix to existing TUI behavior captured by an already-failing test.