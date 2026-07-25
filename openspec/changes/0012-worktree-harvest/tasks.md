# Change 0012 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## G0 — Coordinator port (internal/orchestrate)
- [x] T-1200 `Coordinator` port: `Available() bool`,
      `Claim(ctx, agent, intent string, symbols []string) (Claim, error)`,
      `Done(ctx, agent string) error`, `Release(ctx, agent string) error`.
      `Claim` carries the granted worktree dir, or a refusal naming the blocking
      holder. Plus `FakeCoordinator` for tests.
- [x] T-1201 `NoCoordinator`: the always-unavailable implementation, so the
      uncoordinated path has a real object rather than a nil check.

## G1 — grit adapter (internal/orchestrate)
- [x] T-1210 `GritCoordinator{RepoRoot, Bin}`. `Available` reports whether the
      binary resolves. `Claim` shells `grit claim -a <agent> -i <intent> <syms…>`
      and derives the worktree path grit created; `Done` shells `grit done -a
      <agent>`; `Release` best-effort releases locks.
- [x] T-1211 Parse as little as possible: decide on exit status, surface stderr
      verbatim on failure. A blocked claim is distinguished from a hard error, so
      "someone holds this" does not read as "grit is broken".
- [x] T-1212 Adapter tests skip when `grit` is absent, matching how the worktree
      tests already skip without `git`. One test asserts `Available()` is false
      with a deliberately bogus `Bin`, which runs everywhere.

## G2 — Coordinated fan-out (internal/runtime)
- [x] T-1220 `Session.Coordinator` (default `NoCoordinator`) and
      `SubagentTask{Prompt, Symbols []string}` so a caller states what each
      subagent will edit.
- [x] T-1221 `Session.FanoutCoordinated(ctx, tasks []SubagentTask)`: claim per
      task, skip and report the blocked ones, run the granted ones in grit's
      worktree, then `Done` each. Locks release even when a subagent fails.
- [x] T-1222 Report: merged / blocked / failed per subagent, and a leading line
      stating that coordinated mode commits and merges.
- [x] T-1223 `/subagents --coordinated <prompt>#<sym,sym> | …` opts in. Without
      the flag, `/subagents` is byte-identical to change 0011. Unavailable
      coordinator reports why and falls back rather than failing.

## Verification (Gate 5 — before declaring 0012 done)
- [x] `go build ./...` clean; `go test ./...` green (22/22); `-race` clean on
      `internal/orchestrate` and `internal/runtime`.
- [x] `gofmt -l .` empty; `go vet ./...` clean; `CGO_ENABLED=0 go build ./...`
      green, and `go.mod` contains no grit entry — it is an external CLI, not a
      build dependency.
- [x] With no `grit` installed: `/subagents` behaves exactly as in 0011, and
      `--coordinated` reports the missing binary with its install URL and the
      uncoordinated fallback, exiting 1.
- [x] Fake coordinator: granted claim runs, blocked claim does not run and is
      reported with its holder, `Done` is called per granted subagent, locks
      release on subagent failure.
- [x] Two tasks claiming different symbols in one file are both granted — the
      reason for using grit at all
      (`TestFanoutCoordinatedDifferentSymbolsBothRun`).
- [x] `/help` lists the flag; a plain prompt still runs a normal turn.
- [x] **grit is NOT installed in this environment**, so `TestGritEndToEnd` is
      recorded as SKIP, not passed. Verified explicitly with `-run TestGrit -v`:
      6 adapter tests PASS, 1 SKIP. The claim→work→done cycle against the real
      binary is therefore **unexercised** and should be run once grit is
      installed.

## Discovered during implementation
- grit is **Rust** (271k of it, tree-sitter based). Confirmed before speccing, and
  it is what forced the CLI-behind-a-port shape: importing it would break the
  cgo-free single-static-binary rule outright.
- The adapter **derives** the worktree path (`.grit/worktrees/<agent>`) rather than
  scraping stdout. A stable path convention is a smaller dependency on grit's
  output format than parsing it, and grit's CLI is young.
- Blocked-vs-error is decided from output text, and a block is honored on **either**
  exit status: grit versions differ on whether a refused claim exits non-zero, and
  treating "wait your turn" as "the tool is broken" would be the worse failure.
- `--coordinated` is honored only in **leading** position, so the string appearing
  inside a prompt cannot silently switch modes.

## Out of scope (per proposal — each needs its own change)
Reimplementing grit's AST locking or merge · bundling the grit binary · Azure/S3
backends · inferring symbols from prompt text · the other ADR-0006 built-in
workflows · compose persistence · anything on the v1 refuse list.
