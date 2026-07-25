# Orchestration Specification (delta — change 0016)

## Purpose
Best-of-N generation with a judge pick: produce N candidate answers to a prompt in
parallel (tool-free), have a judge model select the best, and return only that one.
Adds a `Sampler`, a `Ranker`, and a `MaxMode` composer to `internal/orchestrate`,
surfaced by `/max`. The `Provider` port is unchanged; this is orchestration over the
neutral model surface.

## Requirements

### Requirement: N candidates are sampled in parallel
`Sampler.Sample` SHALL run N tool-free generations of the same request through
`Runner.RunAll`, bounded by `MaxParallel`, and collect them as `Candidate`s in a
stable order.

#### Scenario: N candidates returned in order
- **WHEN** `Sample(ctx, req, 3)` is called with a working provider
- **THEN** exactly 3 `Candidate`s are returned, indexed 0..2 in completion-friendly order

#### Scenario: Parallelism is bounded
- **WHEN** N exceeds `MaxParallel`
- **THEN** no more than `MaxParallel` generations run at once

#### Scenario: A failing candidate does not lose the others
- **WHEN** one generation errors
- **THEN** the other candidates are still returned and the failure is recorded on that candidate

### Requirement: A judge ranks and picks one
`Ranker.Rank` SHALL ask a judge model to choose the best candidate and return its
index; a non-parseable or out-of-range answer SHALL be an error.

#### Scenario: Best index is returned
- **WHEN** the judge model answers "best is candidate 1" for three candidates
- **THEN** `Rank` returns `best=1` with a rationale

#### Scenario: Garbage judge answer is an error
- **WHEN** the judge model returns unparseable text or an index outside the candidate set
- **THEN** `Rank` returns an error rather than defaulting silently

#### Scenario: A genuine tie picks the lowest index
- **WHEN** the judge declares candidates 0 and 1 equal-best
- **THEN** `best=0` is returned

### Requirement: MaxMode composes sample then rank
`MaxMode.Run` SHALL sample N candidates, rank them, and return exactly the single
best `Candidate`; if sampling or ranking fails it SHALL return an error.

#### Scenario: Best candidate is returned
- **WHEN** `Run(ctx, req, 3)` samples three and the judge picks index 2
- **THEN** the candidate at index 2 is returned

#### Scenario: Ranking failure surfaces an error
- **WHEN** the judge answer is unparseable
- **THEN** `Run` returns an error; it does not silently return candidate 0

### Requirement: N is bounded and explicit
`/max` SHALL accept an explicit N, default to the configured value (else 3), and
enforce a maximum cap; it SHALL never run on a normal turn.

#### Scenario: Default N when none given
- **WHEN** `/max <prompt>` is invoked with no N and no config
- **THEN** MaxMode runs with N=3

#### Scenario: Over-cap N is clamped and reported
- **WHEN** `/max 99 <prompt>` exceeds the configured maximum
- **THEN** N is clamped to the maximum and the clamp is reported to the user

### Requirement: MaxMode is provider-neutral
`Sampler`, `Ranker`, and `MaxMode` SHALL depend only on the neutral `Provider.Stream`
surface and neutral `Request`/`Event`/`Candidate` types; no provider-specific type
escapes `internal/provider`.

#### Scenario: Only neutral types cross the boundary
- **WHEN** MaxMode runs against any adapter
- **THEN** orchestration imports no provider-specific package
