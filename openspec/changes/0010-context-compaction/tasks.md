# Change 0010 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## E0 — Make the write outcome observable
- [x] T-1000 `maybeCheckpoint` reports whether it wrote. Compaction depends on
      that outcome, and today the function swallows it. Signature becomes
      `(wrote bool)`; a failed write still goes to `OnCheckpointError` and returns
      false.
- [x] T-1001 Re-scope the 0008 purity test rather than deleting it: rename to make
      "no checkpoint → no change" the assertion, and add its counterpart, "failed
      write → no change". The invariant is narrowed, not abandoned.

## E1 — Compact
- [x] T-1010 `KeepRecent` (default `DefaultKeepRecent`) bounds how many trailing
      messages survive. `compact(history)` returns a marker message followed by
      the last `KeepRecent`; a history at or under the keep-count is returned
      unchanged.
- [x] T-1011 The marker is a user-role message whose text names `checkpoint.md`
      and says material was compacted — unmistakably not conversation. Assert a
      reader can distinguish it.
- [x] T-1012 `Run` compacts only when `maybeCheckpoint` reports a successful
      write, and returns the compacted history. `Session.History` is updated to
      match, so `/dream` sees the same thing the caller does.
- [x] T-1013 `OnCompact func(before, after int)` reports the shrink, so a
      front-end can tell the user. Nil is a no-op.

## E2 — Mid-turn safety
- [x] T-1020 Assert the judge loop inside one `Run` sees full history: compaction
      happens after the loop exits, never between rounds.

## Verification (Gate 5 — before declaring 0010 done)
- [x] `go build ./...` clean; `go test ./...` green for every package — 22/22,
      118 runtime tests, `-race` clean.
- [x] `gofmt -l .` empty; `go vet ./...` clean; `CGO_ENABLED=0 go build ./...`
      green.
- [x] Failed checkpoint write leaves history untouched — the ordering property
      (`TestRunFailedWriteLeavesHistoryUntouched`, which also asserts no
      compaction marker appears and `OnCompact` never fires).
- [x] Crossing the mark shrinks history and the marker names `checkpoint.md`.
- [x] The newest messages survive, in order
      (`TestCompactKeepsMarkerPlusRecent`).
- [x] A session with no window configured returns history identical to today
      (`TestRunHistoryUntouchedWithoutCheckpointing`).
- [x] `go run ./cmd/openplus --fake -C $(mktemp -d) -p 'hello'` unchanged.
- [x] Verified the hook fires on a real long history: 22 → 7 messages.

## Discovered during implementation
- `OnCompact` was reachable in the runtime but wired to **no front-end**, so
  compaction would have happened invisibly — the exact risk the proposal names.
  T-1013 was only half done. Both front-ends now report it: the one-shot path to
  stderr, the TUI via a new `NoticeMsg` that lands in the transcript.
  `OnCheckpointError` had the same gap and is wired alongside it.
- `NoticeMsg` flushes any half-rendered assistant text first, so a notice cannot
  interleave with the message it interrupted.
- A single one-shot turn produces ~2 messages, well under the keep-count, so it
  never compacts. That is correct, but it means a naive CLI smoke test cannot
  exercise compaction — the property needs a seeded history to observe.

## Out of scope (per proposal — each needs its own change)
Summarization-model calls · compaction without a checkpoint · mid-turn compaction
· subagent and workflow surface (slice 3) · compose persistence across processes ·
anything on the v1 refuse list.
