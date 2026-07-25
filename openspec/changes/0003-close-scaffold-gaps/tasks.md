# Change 0003 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## D0 — Reconcile stale scaffolding notes (0001)
- [x] T-200 Update `openspec/changes/0001-foundation/tasks.md`:
  - flip **T-010** from `[~]` to `[x]`.
  - flip **T-011** from `[~]` to `[x]`.
  - remove the stale scaffolding note under M1 ("Adapter-specific event
    parsing … is still open"; "Round-trip JSON tests still open …").
  - rewrite the "Scaffold status (verified 2026-07-25)" block to read
    "All scaffolding closed" and replace the package list with today's
    green set: 20/20 packages green after Part B lands.
      Done — committed as 8366d27 on 2026-07-25. Verification stamp
      still reads "19/20" because Part B hadn't landed at commit time;
      superseded by a04f3f9 on the same day (Part B landed, 20/20 green).

## D1 — TUI submit() appends user message to history (red→green)
- [x] T-210 (already red) `internal/tui/model_test.go:68
      TestSubmitAppendsUserAndStartsBusy` — confirms `submit()` must append
      a `provider.Message{Role: provider.RoleUser, Blocks: [{Text: ...}]}` to
      `m.history`.
- [x] T-211 **BLOCKING FINDING — completed 2026-07-25**: the runtime
      explicitly appends the user message from `userMsg`, NOT from `history`.
      Evidence:
      - `internal/runtime/turn.go:52` —
        `in.Recent = append(append([]provider.Message{}, history...),
        userMessage(userMsg))`. `userMessage(userMsg)` at line 138 builds
        `{Role: provider.RoleUser, Blocks: [{Text: userMsg}]}`.
      - `internal/runtime/turn_test.go:160 TestAssembleContextCarriesHistory`
        pins this: passes `prior = [{Text: "earlier question"}]` plus
        `userMsg = "follow-up"`, asserts the returned history's last message
        is `"follow-up"` (line 176-179).
      - `internal/runtime/turn_test.go:184 TestRunDrivesTheLoop` pins the
        same contract at the `Run` level: `s.Run(ctx, "hello", nil)` →
        `hist[0].Blocks[0].Text == "hello"`.
      - `cmd/openplus/main.go:93` is the only external caller and passes
        `nil` for history with `prompt` as `userMsg`.
      - `internal/tui/runner_test.go:21 stubRunner.Run` preserves whatever
        `history` it received and appends an assistant reply — it does NOT
        build a user message from `input`.
      **Conclusion:** if `submit()` appends the user message to
      `m.history`, the runtime prepends it again — the user turn appears
      twice in the wire payload (history=[user, prior, …] →
      `turn.History=[user, prior, …, user]`). The risk called out in the
      proposal materializes.
      **The failing test is wrong.** The model does NOT own history;
      `Session.Run` owns it. `submit()` must not touch `history`.
      Fixing the test instead of the code is the right move — the existing
      test contract for `TestModelRunsTurnThroughRunner`
      (`runner_test.go:60-63`: first turn passes empty history) is what
      holds with the runtime's design.
- [x] T-212 **REPLACES the production-code fix.** Edit
      `internal/tui/model_test.go:68 TestSubmitAppendsUserAndStartsBusy` so
      it matches the runtime contract: history stays empty at submit time;
      the user turn enters history when `turnDoneMsg` arrives, because the
      real runner returns `history = [...prior, user, assistant]` (the
      runtime's `Session.Run` already appended it). To prove that the
      model joins that contract, the test should: (a) submit, (b) drive
      the stubRunner (with a reply), (c) deliver the resulting
      `turnDoneMsg`, (d) assert history is now `[user, assistant]`.
      That's the correct end-to-end shape; it covers the original intent
      ("user turn recorded") without contradicting the runtime.
      The `m.history` field continues to be populated by the
      `turnDoneMsg` handler at `model.go:119`.
      Plus: update `internal/tui/runner_test.go stubRunner.Run` to mirror
      the runtime's user-turn injection, so the stub and production
      share one user-turn contract.
      Done — committed as a04f3f9 on 2026-07-25.
- [x] T-213 `go test ./internal/tui/... -v` — must show 4/4 in this package
      green. The lone failure should flip green via T-212; the other three
      TUI tests must not regress.
      Done — 18/18 PASS in `internal/tui`; `go test ./...` 20/20 green.

## D2 — Evidence capture (in commit messages, not files)
- [x] T-220 Commit 1 (Part A only): paste the `go build ./...` clean output
      and the `go test ./...` line showing 19/20 green with the lone TUI
      failure called out.
      Done — in 8366d27 body.
- [x] T-221 Commit 2 (Part B): paste `go test ./...` showing 20/20 green
      and the before/after of
      `go test ./internal/tui/... -run TestSubmitAppendsUserAndStartsBusy -v`.
      Done — in a04f3f9 body.

## Out of scope
- 0002 live-wiring. That has its own proposal/tasks and is still awaiting
  Gate 1 approval.
- Any v1 refuse-list item.
- Wiring `submit()` through `Bubble Tea`'s `tea.Cmd` message chain to also
  render the user line via the existing `applyEvent` path. The current log
  append on line 187 (`"❯ " + m.pendingInput`) handles visibility; keeping
  history and log concerns separate matches the existing design.