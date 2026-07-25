# Change 0009 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## D0 — Dispatcher
- [x] T-900 `Command{Name, Usage, Summary, Run func(*Session, string) (string,
      error)}` + a registry map. `Session.Dispatch(ctx, input)` returns
      `(output string, handled bool, err error)`: `handled` is false for input not
      beginning with `/`, so the caller falls through to a normal turn.
- [x] T-901 Unknown command returns an error naming it and listing the registered
      commands. `/help` lists them with their usage.

## D1 — Skills (ADR-0002 #8)
- [x] T-910 `/skill <name>` returns the skill body via `skills.Index.Find`;
      missing name errors and lists the discoverable names.
- [x] T-911 `/skills` lists discovered skills with descriptions; none found is a
      report, not an error.

## D2 — Compose (ADR-0002 #6)
- [x] T-920 `Session.Compose *compose.Session` + `/compose <feature>` starting one
      at grill. Starting a second while one is active reports the active feature
      rather than silently discarding it.
- [x] T-921 Phase verbs: `/grill <notes>`, `/spec <body>`, `/approve-spec`,
      `/task <id> <title>`, `/red <id>`, `/green <id>`, `/verify`, `/advisor`,
      `/finding <id> <detail>`, `/resolve <id>`, `/advance`. Each maps to the
      matching `compose.Session` method and surfaces `ErrGateBlocked` /
      `ErrTDDViolation` / `ErrWrongPhase` as readable text.
- [x] T-922 Every phase verb errors with "start a compose session first" when
      `Session.Compose` is nil.

## D3 — Dream into file memory (ADR-0002 #9 + #1)
- [x] T-930 `Session.Memo memo.Files` rooted at the project root.
- [x] T-931 `/dream` runs `improve.Dreamer.Extract` over the session transcript
      and appends each fact via `memo.Files.AppendMemory`, reporting the count.
      Empty extraction reports honestly and writes nothing; no transcript is an
      error. Requires a provider — reuses `Session.Provider` and `Session.Model`.
- [x] T-932 Assert existing `MEMORY.md` content survives: append only, never
      rewrite.

## D4 — Distill (ADR-0002 #9)
- [x] T-940 `Session.Runs []improve.Run` accumulated from each turn's tool calls,
      so `/distill` has material without a separate recorder.
- [x] T-941 `/distill` mines with `MinePatterns`, picks the top pattern, routes by
      `SuggestKind` to `ScaffoldSkill` / `ScaffoldCommand` / `ScaffoldSubagent`,
      and reports the path written. No qualifying pattern reports honestly; an
      existing file is refused, not overwritten.

## D5 — Wire the front-ends
- [x] T-950 The one-shot path (`-p`) dispatches before running a turn.
- [x] T-951 The TUI dispatches on submit, rendering command output in the
      transcript without a provider round-trip.

## Verification (Gate 5 — before declaring 0009 done)
- [x] `go build ./...` clean; `go test ./...` green for every package — 22/22,
      101 tests in `internal/runtime`.
- [x] `gofmt -l .` empty; `go vet ./...` clean; `CGO_ENABLED=0 go build ./...`
      green.
- [x] `/skill` on a real discovered skill returns its body (verified at the CLI).
- [x] `/dream` against the fake provider appends to a real `MEMORY.md`, and a
      pre-existing hand-written line is still present afterwards
      (`TestCmdDreamNeverRewritesExisting`).
- [x] `/distill` writes a scaffold that `skills.Index.Discover` then finds
      (`TestCmdDistillWritesDiscoverableScaffold`).
- [x] `/compose` + `/advance` without an approved spec reports the blocked gate
      and does not advance (`TestComposeSpecGateEnforcedThroughCommands`); the
      TDD and Advisor gates are likewise enforced through the surface.
- [x] `/nonsense` lists the known commands and exits 1.
- [x] A plain (non-slash) prompt still runs a normal turn.

## Discovered during implementation
- `Session.History` / `Session.Runs` were declared but never populated, so
  `/dream` and `/distill` would have dispatched and found nothing in real use.
  `Session.record` now captures both after each turn; a tool-free turn records no
  run, since an empty sequence would dilute the frequency counts `/distill`
  depends on. (`accumulate_test.go`.)
- `/help` cannot live in the `builtinCommands` literal: its Run reads the table it
  belongs to, which the compiler rejects as an initialization cycle. It is
  registered in an `init()` instead.
- `skillRoots` includes `~/.claude/skills`, so skill tests must redirect HOME —
  a developer's own user-level skills otherwise leak into assertions about what
  is discoverable. The discovery is correct in production; the isolation belongs
  in the tests.

## Out of scope (per proposal — each needs its own change)
Subagent and workflow surface (slice 3: `orchestrate.Runner`,
`WorktreeIsolator`, `Workflow`) · compose persistence across processes · live
context compaction · new engine behavior · anything on the v1 refuse list.
