# Change 0016 — Max Mode: best-of-N with a judge pick (PLAN)

## Why
`Max Mode (best-of-N + judge)` has sat on the refuse-in-v1 list since
`0001-foundation` and ADR-0002. The deferral had no documented trigger. That trigger
now fires: generate N candidate answers to a prompt in parallel and have a judge
pick the best one, returning only that answer.

The user approved the **pick-one** strategy (not judge-merge). N candidates are
produced; the judge ranks them; the single best is returned. Judge-merge is out of
scope.

## What I verified before designing
1. **The fan-out primitive already exists.** `orchestrate.Runner.RunAll`
   (`internal/orchestrate/subagent.go`) runs a slice of `Task`s in parallel, bounded
   by `MaxParallel`, collecting `Result`s. Best-of-N is N `Task`s that each produce
   one candidate — no new concurrency.
2. **A judge already talks to a provider.** `orchestrate.Judge`
   (`internal/orchestrate/goal.go`) holds a `Provider` + `Model` and returns a
   `Verdict`. Best-of-N needs the same plumbing but a different question: "which of
   these N is best?" A `Ranker` sibling to `Judge` reuses the wiring, not the
   semantics.
3. **The model call is provider-neutral.** `Provider.Stream` + neutral
   `Request`/`Event` (`internal/provider/types.go`) are the only model surface. No
   provider type escapes `internal/provider`.
4. **The command surface accepts new commands.** `builtinCommands`
   (`internal/runtime/commands_builtin.go`) and the dispatch in `command.go` register
   slash commands; `/max` fits the existing pattern.
5. **No best-of/sample code exists yet** — greenfield, all in `internal/orchestrate`.

## What changes
Adds a `Sampler` + `Ranker` to `internal/orchestrate`, surfaced by a `/max` command,
behind the **unchanged** `Provider` port.

- `orchestrate.Sampler.Sample(ctx, req provider.Request, n int) ([]Candidate, error)`:
  runs N `Task`s through `Runner.RunAll`; each task is one `Provider.Stream` drained
  to a final assistant text (no tool iteration — pure generation). Bounded by the
  runner's `MaxParallel`.
- `orchestrate.Ranker`: holds `Provider` + `Model` (same shape as `Judge`). `Rank(ctx,
  prompt, []Candidate) (best int, rationale string, err error)` asks the judge model
  to choose the best candidate and return its index. Provider-neutral.
- `MaxMode`: composes Sampler + Ranker — sample N, rank, return the single best
  `Candidate`. `Run(ctx, req, n) (Candidate, error)`.
- Runtime: `/max [n] [prompt...]` runs MaxMode for the given n (default from config,
  else 3) on the prompt, streams/returns the winning answer. Config gains an optional
  `max: { samples: N, model: "<judge-model>" }`.
- `docs/adr/0011-max-mode.md`: records the trigger firing.
- `AGENTS.md` refuse-list: `Max Mode (best-of-N + judge)` → "shipped (0016)."

### The Max Mode contract (defined by this change)
- **Pick-one.** The judge returns exactly one candidate's index; ties resolved by
  lowest index. No merging, no synthesis.
- **Generation is tool-free.** Each candidate is one model turn with no tools — a
  draft. (A future trigger may run full agent turns; out of scope.)
- **Bounded parallelism + bounded N.** N is capped (default 3, max configurable) and
  fan-out respects `MaxParallel`; cost is N× the single-turn cost, surfaced to the
  caller, never hidden.
- **Deterministic on failure.** If ranking fails, MaxMode returns an error rather
  than silently picking candidate 0.

## What this deliberately does not do
- **No judge-merge.** The judge selects, never synthesizes. Merge is a separate
  trigger.
- **No tool-using candidates.** Each candidate is a single tool-free generation.
- **No automatic enablement.** Max Mode runs only via `/max`; it never silently
  multiplies cost on a normal turn.
- **No provider-specific sampling knobs.** Temperature/top-p for candidates stay at
  provider defaults; this is orchestration, not provider tuning.

## Governing decisions
ADR-0002 (deferred Max Mode — trigger now fires) · ADR-0001 (cgo-free — pure
orchestration) · the provider-neutrality hard rule (only `Provider.Stream` + neutral
types). The `Provider` port is **unchanged**.

## Risk
- **Cost.** N× tokens per `/max`. Mitigated by a documented default (3), a max cap,
  and explicit opt-in via command. T-1625 asserts the cap is enforced.
- **Judge bias / indecision.** A judge that won't commit returns no valid index.
  T-1623 asserts a non-parseable or out-of-range judge answer is an error, with
  lowest-index tiebreak only for genuine ties.
- **Goroutine / cost accounting.** Fan-out is bounded by `Runner.MaxParallel`
  (already proven in 0011). T-1622 re-asserts no goroutine leak.
- **Provider leak.** Any provider-specific type escaping `internal/provider` breaks
  neutrality. T-1626 asserts the orchestration packages depend only on neutral types.

## Verification
`Sampler` is testable against the `provider.Fake` (N candidates collected, parallelism
bounded). `Ranker` is testable against a Fake that returns a fixed "best is index K"
verdict (and a malformed one → error). `MaxMode` composes them: sample N → rank →
return candidate K, end-to-end. The `/max` command is an integration test through the
real Session with a Fake provider.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks are
approved (house Gate 1).
