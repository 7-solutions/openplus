# Change 0024 — RRF tuning (configurable weights + K)

> Status: PROPOSED. Awaiting Gate 1 approval. **No code before approval.**

## Why
Change 0021 restored hybrid search with **fixed** Reciprocal Rank Fusion:
both halves contribute an identical `1/(rrfK+rank)` with `rrfK=60`, fused by
plain map addition. That equal-weight default is a reasonable starting point
but is not tunable — there is no way to favor the lexical half for
keyword-heavy queries (identifiers, exact phrases) or the vector half for
semantic queries, and no way to adjust the rank-damping constant.

This change exposes the RRF knobs so retrieval quality can be tuned per
deployment. It is the **enabling step**: empirical tuning (finding optimal
weights) requires a retrieval-quality eval harness that does not yet exist;
making the weights configurable is what makes that possible later, and it
ships a useful capability now.

## What Changes
- **New `RRFConfig` type** on the memory package:
  ```go
  type RRFConfig struct {
      K             float64 // rank-damping constant; standard 60. Lower = steeper.
      VectorWeight  float64 // weight on the vector-KNN half.
      LexicalWeight float64 // weight on the lexical-bm25 half.
  }
  ```
- **`DefaultRRF()`** returns `{K:60, VectorWeight:1, LexicalWeight:1}` — the
  current equal-weight behavior. `Open` initializes the store to this default
  unconditionally, so `Open(path)` and `Open(path, WithFTS())` are
  byte-for-byte unchanged (backward compatible).
- **`WithRRF(cfg RRFConfig) OpenOption`** overrides the default. The caller
  sets all three fields explicitly (no zero-value magic — `LexicalWeight:0`
  means "disable the lexical half", not "use default").
- **Weighted fusion** in `Search`: the vector loop contributes
  `VectorWeight/(K+rank)`; the FTS half contributes `LexicalWeight * (1/(K+rank))`.
  The `rrfK` package const is replaced by `s.rrf.K`.
- **`ftsIndex.search` gains an `rrfK float64` parameter** (it currently hardcodes
  the package const). It stays a pure lexical ranker — it returns raw
  `1/(rrfK+rank)` contributions; the Store applies the lexical weight at fusion
  time. Keeps the weight ownership in one place (`Search`).
- **No schema change, no new port, no new dep.** Pure local change to
  `internal/memory/{search,store,fts}.go`. The `MemoryStore` port interface is
  untouched.

## Architecture (ports & adapters — hexagonal)
**No port change.** This refines the ADR-0014 fusion's *implementation*, not
the `MemoryStore` seam. `RRFConfig` is a config type on `memory.Store` (an
internal collaborator), consumed only via `Open` options. No core package
sees it.

## Why per-half weights, not a single alpha
A single `alpha ∈ [0,1]` (mixing `alpha*vector + (1-alpha)*lexical`) is the
normalized form; per-half weights are strictly more expressive (an alpha is
just `{VectorWeight: alpha, LexicalWeight: 1-alpha}`). Per-half weights also
let a deployment boost *both* (e.g. `{2, 2}` scales scores without changing
ranking — useful for downstream thresholding) or disable one (`{1, 0}` =
vector-only without dropping the shadow index).

## Scope (explicitly OUT of this change)
- **Empirical tuning / optimal-weight discovery.** Needs a retrieval-quality
  eval harness (annotated query→relevant-doc pairs) that doesn't exist.
  Building that is a separate, larger change. This change only provides the
  knobs; a follow-up can wire them to an eval-driven search.
- **Per-field boosting** (title vs body). The shadow index is a single `text`
  column; per-field boosting is deferred until the schema gains fields.
- **Runtime-adaptive weights** (per-query weighting). The config is set at
  `Open` time; per-query tuning is a future enhancement.

## Alternatives considered
1. **Single `alpha` mixing weight** — rejected: less expressive than per-half
   weights; an alpha is a special case of per-half weights.
2. **Empirical tuning now** — rejected: no eval harness; would be guessing.
3. **Hardcode "better" defaults than 1/1/60** — rejected: without an eval
   harness, any non-default is an unsubstantiated guess. Ship the knobs with
   the proven-neutral default; tune later with evidence.

## Rollback
`git revert HEAD`. No schema migration — the chunks table and the FTS shadow
are untouched; only the fusion formula and a config type change.
