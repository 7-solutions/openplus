# Change 0008 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## C0 — Assemble the Checkpointer
- [x] T-800 `Session.Checkpointer *contextmgr.Checkpointer` + `Session.Tasks
      contextmgr.TaskTree`. `Assemble` builds the Checkpointer from
      `config.Context.Window` (rooted at the project root); a zero window leaves
      it nil, which disables the feature end to end.
- [x] T-801 On assembly, read any existing `checkpoint.md` and restore
      `Session.Tasks` from it. An unreadable or malformed file restores an empty
      tree without failing assembly. (`assembleCheckpointer`.)

## C1 — Reconstruct into the assembled context
- [x] T-810 `AssembleContext` uses `Checkpointer.Reconstruct` as the base
      `contextmgr.Input` when a checkpoint exists, then layers retrieved memory
      and auto-loaded skills onto it. Without a checkpoint the current behavior is
      unchanged. (`Session.baseInput`.)
- [x] T-811 Live `recent` messages override the checkpoint's digest; the
      checkpoint summary lands in the `Checkpoint` section and the active task in
      `Task`, so `renderSystem` emits them in ADR-0008 order.
      **Note:** `Reconstruct` falls back to the checkpoint's own digest when
      handed nil recent, so `baseInput` clears `in.Recent` explicitly — a stale
      digest competing with live messages for the same budget is strictly worse
      than the live ones alone.

## C2 — Write on the high-water mark
- [x] T-820 `Session.Run` measures the assembled context (`Turn.Used`, from the
      Budgeter) and, after the turn completes, writes a checkpoint when
      `ShouldCheckpoint(used)` is true. The write happens after the turn so a
      crash mid-turn cannot record a half-finished state. (`maybeCheckpoint`,
      called after `persist`, past the judge loop.)
- [x] T-821 The checkpoint carries the summary, `Session.Tasks`, and the retained
      recent messages. A write failure goes to `Session.OnCheckpointError` rather
      than being dropped or failing the turn.
- [x] T-823 Summary is the retained transcript **verbatim, capped** at
      `SummaryCap` (8000) characters: keep the most recent whole messages that
      fit, and prepend a visible marker naming how many earlier messages were
      dropped. No model call, no editorial selection. The newest message is
      always kept even if it alone exceeds the cap — an empty summary would be a
      silent total loss.
- [x] T-822 Assert the returned history is byte-identical whether or not a
      checkpoint was written — the write must be observationally pure with
      respect to live context. (`TestRunHistoryIdenticalWithAndWithoutCheckpoint`,
      `reflect.DeepEqual` across a checkpointing and a non-checkpointing session.)

## Verification (Gate 5 — before declaring 0008 done)
- [x] `go build ./...` clean.
- [x] `go test ./...` green for every package — 22/22, 70 tests in
      `internal/runtime`. `-race` clean on that package.
- [x] `gofmt -l .` empty; `go vet ./...` clean; `CGO_ENABLED=0 go build ./...`
      green.
- [x] Two-session end-to-end: session A crosses the mark and writes
      `checkpoint.md` (summary carrying both sides of the exchange verbatim);
      session B in the same root reconstructs and exits 0.
- [x] A corrupt `checkpoint.md` (truncated mid-section) does not fail a turn —
      verified at the CLI, exit 0.
- [x] `go run ./cmd/openplus --fake -C $(mktemp -d) -p 'hello'` still works with
      no window configured, and writes no checkpoint (feature fully disabled).

## Out of scope (per proposal — each needs its own change)
Compose / `/dream` / `/distill` / `/skill` command surface (slice 2) · subagent
and workflow surface (slice 3) · summarization-model calls · compaction of the
live message history · anything on the v1 refuse list.
