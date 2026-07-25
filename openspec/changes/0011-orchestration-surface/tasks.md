# Change 0011 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## F0 — Subagent fan-out (ADR-0002 #4)
- [x] T-1100 `Session.Fanout(ctx, prompts []string) ([]orchestrate.Result, error)`:
      builds one `orchestrate.Task` per prompt, each running a fresh agent turn in
      its isolated dir, and returns `Runner.RunAll`'s results (input order).
      `MaxSubagents` caps the task count; `MaxSubagentParallel` caps concurrency.
- [x] T-1101 The subagent gate resolves from rules alone: wrap `Session.Rules` so
      Ask never blocks on a prompt nobody is watching, while explicit denials
      still deny. Assert both.
- [x] T-1102 Worktree isolation: use `orchestrate.WorktreeIsolator` rooted at the
      project when it is a git repo, and run in place otherwise (a non-repo
      project must still work). Assert no worktree survives a fan-out, including
      when a task fails.
- [x] T-1103 `/subagents <prompt> | <prompt> | …` — pipe-separated so a prompt can
      contain spaces. Reports the count before running, refuses more than
      `MaxSubagents`, and errors with usage on none.

## F1 — Prompt phases (ADR-0006)
- [x] T-1110 `promptPhase` implements `orchestrate.Phase`: `Run` executes its
      prompt as an agent turn and returns the assistant's text. The first
      production implementation of the interface.
- [x] T-1111 Hand-off: a phase publishes its output so the next phase's prompt can
      reference it via `State`. Assert phase two sees phase one's output.

## F2 — Workflow invocation
- [x] T-1120 `Session.Workflows map[string]orchestrate.Workflow` + one built-in
      assembled from `promptPhase`, so the engine is exercised end to end.
- [x] T-1121 `/workflow <name>` runs it and returns the `Report`; unknown names
      list the registered ones. `/workflows` lists them, honestly when empty.
- [x] T-1122 Assert a phase that keeps failing exhausts its retry budget and the
      report names it.

## Verification (Gate 5 — before declaring 0011 done)
- [x] `go build ./...` clean; `go test ./...` green for every package (22/22, 146
      runtime tests); `-race` clean on `internal/runtime`.
- [x] `gofmt -l .` empty; `go vet ./...` clean; `CGO_ENABLED=0 go build ./...`
      green.
- [x] Fan-out results follow input order even when completion order differs
      (`TestFanoutResultsFollowInputOrder`, with a provider that makes the first
      request slowest).
- [x] A failing subagent does not lose its siblings.
- [x] No worktree directory survives a fan-out — asserted in tests on a real git
      repo, on both the success and failure paths, and confirmed at the CLI
      (`git worktree list` shows only the primary afterwards).
- [x] Concurrency cap holds under more tasks than the cap.
- [x] `/subagents` with no prompts, and with too many, both error usefully.
- [x] `/workflow` on the built-in runs both phases in order; on an unknown name
      it lists the known ones.
- [x] `/help` lists the new commands; a plain prompt still runs a normal turn.

## Discovered during implementation
- `provider.Fake` plays a **fixed script** and returns an empty turn once it runs
  out, which a fan-out or a multi-phase workflow exhausts immediately. Tests that
  need every call answered use an `alwaysProvider` instead. This also explains why
  `--fake /workflow review` shows an empty second phase at the CLI: the script ran
  out, not a hand-off bug (`TestPromptPhaseHandsOffOutput` proves the hand-off
  against a provider that answers every call).
- Subagents get their **own tool registry rooted at their worktree**, so a `glob`
  or `grep` inside a subagent searches its own checkout rather than the primary
  one. Without this, worktree isolation would be cosmetic.
- Subagents deliberately do **not** forward `OnEvent`/`OnToolResult`: several
  parallel subagents streaming into one transcript would interleave into noise.
  Their output is reported when the fan-out merges.
- `Ask` degrades to **Deny** in a subagent, not Allow: an unattended agent should
  not gain permissions a watched one would have to request.

## Out of scope (per proposal — each needs its own change)
The other three ADR-0006 built-in workflows (`deep-research`, `fact-check`,
`research-experiment`) · nested orchestration · merging file edits from parallel
worktrees back into the primary checkout · compose persistence · anything on the
v1 refuse list.
