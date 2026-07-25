# Change 0016 — Tasks (COMPLETE)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## H0 — Decision
- [x] T-1600 Write `docs/adr/0011-max-mode.md`: the ADR-0002 Max Mode trigger has
      fired; best-of-N pick-one (no merge), pure orchestration over the unchanged
      `Provider` port, provider-neutral, N bounded + explicit. ADR-0002 stays
      Accepted.

## H1 — Sampler (internal/orchestrate)
- [x] T-1610 `Candidate` type + `Sampler.Sample(ctx, req, n)`: run n tool-free
      `Provider.Stream` generations via `Runner.RunAll`, collect in stable order.
      Red: n=3 against `provider.Fake` → 3 candidates 0..2.
- [x] T-1611 Bounded parallelism: n > MaxParallel runs at most MaxParallel at once;
      no goroutine leak. Red: assert concurrency cap + all goroutines exit.
- [x] T-1612 Partial failure: one generation erroring still returns the others with
      the failure recorded on that candidate. Red: mix of ok/err scripts.

## H2 — Ranker (internal/orchestrate)
- [x] T-1620 `Ranker` (Provider + Model, sibling of `Judge`): `Rank(ctx, prompt,
      []Candidate) (best int, rationale string, err error)`. Red: Fake judge answers
      "best is candidate 1" → best=1.
- [x] T-1621 Parse judge output to an index; genuine tie → lowest index. Red: equal-best
      answer → best=0.
- [x] T-1622 Unparseable / out-of-range judge answer → error, no silent default.
      Red: garbage text and index 5-of-3 both error.

## H3 — MaxMode composer
- [x] T-1623 `MaxMode.Run(ctx, req, n) (Candidate, error)`: sample → rank → return
      the single best. Red: 3 sampled, judge picks 2 → candidate 2 returned.
- [x] T-1624 Ranking failure surfaces an error (not silent candidate 0). Red: bad
      judge → Run errors.

## H4 — Bounds + config
- [x] T-1625 N default (3) and maximum cap enforced; over-cap clamped + reported.
      Red: `/max 99` → clamped to cap, clamp reported.
- [x] T-1626 Provider-neutrality: orchestration imports no provider-specific package;
      only neutral types cross the boundary. Red: a build/import test gating it.

## H5 — Runtime surface
- [x] T-1627 `/max [n] [prompt...]` command in `builtinCommands`: runs MaxMode,
      returns the winning answer; opt-in only (normal turns unaffected). Config
      `max: { samples, model }` optional. Integration test through the real Session
      with a Fake provider.

## H6 — Gate
- [x] T-1628 Advisor pass (resolve every finding); update knowledge graph + memory.
      Update `AGENTS.md` refuse-list: `Max Mode (best-of-N + judge)` → "shipped (0016)."
