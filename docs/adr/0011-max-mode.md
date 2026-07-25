# ADR-0011 — Max Mode: the ADR-0002 trigger has fired

**Status:** Accepted

## Context
ADR-0002 listed "Max Mode (best-of-N + judge)" as a deferred v1 non-goal with no
documented trigger. The trigger now fires: generate N candidate answers in parallel
and have a judge pick the best one.

The user-approved strategy is **pick-one** (the judge selects a single candidate).
Judge-merge (synthesizing one answer from many) is explicitly out of scope and
remains deferred.

## Decision
Add best-of-N to `internal/orchestrate`, behind the **unchanged** `Provider` port.
Three new types: a `Sampler` that runs N tool-free generations through the existing
`Runner.RunAll` (bounded by `MaxParallel`, proven in change 0011); a `Ranker` — a
sibling of the goal `Judge` (`internal/orchestrate/goal.go`) holding a `Provider` +
`Model` — that asks the judge model to pick the best candidate and return its index;
and a `MaxMode` composer that samples N, ranks, and returns the single best
`Candidate`.

Each candidate is one tool-free model turn (a draft); the fan-out is the same
`Runner` subagents already use. `/max [n] [prompt...]` is the only entry point —
Max Mode never multiplies cost on a normal turn. N defaults to 3, is capped, and is
always explicit. The judge's answer is parsed to an index; a non-parseable or
out-of-range answer is an error, never a silent default. Genuine ties break to the
lowest index.

The implementation is pure orchestration over the neutral `Provider.Stream` surface.
No provider-specific type escapes `internal/provider`. No cgo.

ADR-0002 remains **Accepted**. This ADR records that its Max Mode clause has fired,
scoped to pick-one; judge-merge stays deferred behind its own trigger.

## Consequences
- (+) Higher-quality answers on demand, reusing the proven fan-out + judge plumbing.
- (+) No port change and no provider coupling — the seam held.
- (−) `/max` costs N× a single turn; mitigated by explicit opt-in, a default of 3,
  a max cap, and surfaced cost.
- (−) Judge quality bounds Max Mode quality; a weak or indecisive judge degrades
  selection. Surfaces as an error on bad output, not a silent pick.
- (−) Judge-merge is still unbuilt; users wanting synthesis must wait for its trigger.
