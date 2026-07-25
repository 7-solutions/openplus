# ADR-0006 — Workflow runtime: Go-native phase engine (defer JS compatibility)

**Status:** Accepted

## Context
MiMoCode workflows are deterministic JS scripts in a sandbox. In pure Go there is a
fork: embed `goja` (pure-Go JS) to run MiMoCode `.js` workflows verbatim, or define
workflows as Go-native phase definitions.

## Decision
Ship a **Go-native** `Workflow` engine: a workflow is an ordered set of phases with
bounded retries, structured hand-off between phases, and opt-in parallel fan-out into
isolated **git worktrees**. Built-ins mirror MiMoCode: `compose`, `deep-research`,
`fact-check`, `research-experiment`.

```go
type Phase interface { Name() string; Run(ctx, *State) (Output, error) }
type Workflow struct { Phases []Phase; MaxRetries int; Parallel bool }
```

Defer `goja` behind a port. Add it only on an explicit trigger: a user needs to run
existing `.mimocode/workflows/*.js` unchanged.

## Consequences
- (+) No JS runtime in the default build; leaner, fully typed, easy to test.
- (−) Not drop-in compatible with MiMoCode's `.js` workflow files (deferred, not lost).
