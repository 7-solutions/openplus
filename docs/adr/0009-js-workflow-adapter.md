# ADR-0009 — JS workflow adapter: the goja trigger has fired

**Status:** Accepted

## Context
ADR-0006 shipped a Go-native `Workflow` engine and deliberately **deferred** goja behind
the `Phase`/`Workflow` port, with an explicit gate: add goja only when *"a user needs to
run existing `.mimocode/workflows/*.js` unchanged."* That trigger now fires: a JS adapter
is wanted so a `.js` workflow loads into the same engine the Go-native built-ins use.

## Decision
Add `github.com/dop251/goja` as the JS adapter behind the **unchanged** `Workflow`/`Phase`
port. A new package (`internal/jsworkflow`) is the only place that knows JS exists; it
compiles a `.js` source into an `orchestrate.Workflow`, and each JS phase function becomes
a `Phase` whose `Run` threads the engine's `State` through a `state` shim. The engine, the
port, the report shape, and the `/workflow` command are not modified — a JS workflow
enters through `/workflow load <path>` and runs through the existing path.

goja is chosen because it is **pure Go** (no cgo), preserving ADR-0001's cgo-free
single-binary rule. That property is re-asserted by a build gate, not assumed.

ADR-0006 remains **Accepted**. This ADR records that its deferral clause has ended: the
JS runtime now exists, as an adapter, exactly where the port said it would go.

The JS surface is deliberately narrow and sandboxed: CommonJS-shaped
`module.exports = { name, maxRetries?, phases:[{name, run(state)}] }`, synchronous, with
only `module`/`exports`/`state`/`console.log` bound. No `require`, fs, or net — host I/O
from JS is a separate trigger with its own threat model.

## Consequences
- (+) JS workflows run inside the same engine, with the same retry budget, hand-off,
  cancellation, and report semantics as Go-native phases.
- (+) The deferral ends without a port change — the hexagonal seam held.
- (−) A pure-Go JS runtime joins the binary. The cgo-free property is asserted, not free.
- (−) The JS contract is defined here (no MiMoCode `.js` reference existed to copy); a
  reviewer can reject the shape before code lands.
