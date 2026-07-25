# Change 0014 — goja JS workflow adapter behind the Workflow port (PLAN)

## Why
ADR-0006 shipped a Go-native `Workflow` engine and **deferred** goja behind the
`Phase`/`Workflow` port, with an explicit trigger: *"a user needs to run existing
`.mimocode/workflows/*.js` unchanged."* That trigger now fires. This change adds the
goja adapter — the one place that knows JS exists — so a `.js` workflow file compiles
into the same `orchestrate.Workflow` the built-ins already use. The engine, the port,
the `/workflow` command, and the report shape are unchanged: this is a new adapter,
which is the entire reason the port was drawn where it was.

The Go-native built-ins (`compose`, `deep-research`, …) stay Go. JS is opt-in: load a
file, get a workflow, run it through the same path.

## What I verified before designing
1. **The port already holds the shape.** `orchestrate.Workflow{Phases []Phase,
   MaxRetries int}` and `Phase.Run(ctx, *State) (string, error)` are exactly what a JS
   phase must produce. Verified by reading `internal/orchestrate/workflow.go` and its
   tests (hand-off, retry budget, cancellation between phases, nil-state).
2. **goja is pure Go.** `github.com/dop251/goja` is an ES5.1+ interpreter with no cgo
   dependency, so it preserves the ADR-0001 hard rule (cgo-free single binary). The
   cgo-free claim is **re-asserted by a build at T-1410**, not assumed from the README —
   same empirical bar change 0013 set.
3. **The runtime already registers workflows by name.** `s.Workflows
   map[string]orchestrate.Workflow` plus `/workflow run <name>` mean a JS-loaded
   workflow needs no new execution path — only a `load` that drops it into the map.
4. **No MiMoCode `.js` reference exists in this repo** (`.mimocodeagent/` has no
   workflows). So the JS contract below is **defined here**, not reverse-engineered.
   That is stated plainly so a reviewer can reject the shape before any code.

## What changes
Adds a JS adapter behind the existing `Workflow` port — no core engine change.

- `internal/jsworkflow`: `Compile(name, src) (orchestrate.Workflow, error)` and
  `LoadFile(path)`. The only package that imports goja. Produces an
  `orchestrate.Workflow`; each JS phase fn becomes a `Phase` whose `Run` threads a
  `state` shim over `orchestrate.State`.
- Runtime: `/workflow load <path>` compiles a file and registers it under its declared
  name in `s.Workflows`. Existing `/workflow run <name>` then runs it unchanged.
- `docs/adr/0009-js-workflow-adapter.md`: records that the ADR-0006 goja trigger fired.
- `AGENTS.md` refuse-list: the `goja .js workflow compatibility` line moves to
  "shipped (0014), behind `/workflow load`."

### The JS contract (defined by this change)
A workflow source is CommonJS-shaped:

```js
module.exports = {
  name: "deep-research",
  maxRetries: 2,                 // optional, defaults to 0
  phases: [
    { name: "query",     run: (state) => "output" },
    { name: "synthesize", run: (state) => { state.set("k", "v"); return "..." } },
  ],
};
```

- `state` shim: `{ last: string, get(key): string, set(key, value): void }` over the
  engine's `orchestrate.State`. `last` is the prior phase's output.
- A phase `run` returns its output (coerced to string); a **throw** is a phase error
  and is retried up to `maxRetries`, exactly like a Go phase.
- **Sandbox:** only `module`, `exports`, `state`, and `console.log` (→ host logger) are
  defined. `require`, `process`, `fetch`, filesystem, and network are **not** bound — a
  ReferenceError, by design. JS workflows are pure computation plus state hand-off.

## What this deliberately does not do
- **No host I/O hooks.** No `require`, no fs, no net from JS. A workflow that needs the
  model or tools calls a Go phase, not a JS one. Binding host capabilities is a
  separate trigger with its own threat model.
- **No auto-scan / discovery.** Workflows load explicitly via `/workflow load`. Scanning
  a directory on startup is deferred — silent registration is the wrong default.
- **No async.** goja runs synchronously; phases are sync. Promises/`await` are out.
- **No replacing the Go built-ins.** `compose` etc. stay Go-native. JS is additive.
- **No npm / module resolution.** One file, one workflow. `require` is undefined.

## Governing decisions
ADR-0006 (defer goja behind the port until this trigger) · ADR-0001 (cgo-free — goja is
pure Go, **re-asserted by build**) · ADR-0002 #7 (deterministic workflows). The
`Workflow`/`Phase` port is **unchanged**: this is the adapter the deferral promised.

## Risk
- **cgo leak.** A transitive goja dependency pulling cgo would break the single-binary
  promise. T-1410 asserts `CGO_ENABLED=0 go build ./...` stays green before any feature
  code lands.
- **Unbounded JS.** `while(true){}` in a phase would hang the engine. Mitigated by
  wiring `ctx.Done()` to goja's `vm.Interrupt` (T-1414) so a cancelled workflow aborts
  the script **mid-phase**, not only between phases. A wall-clock budget is the caller's
  ctx, not a hidden default.
- **Silent contract drift.** A `.js` missing `phases` or with a non-function `run` must
  error naming the defect — never compile to an empty workflow that "succeeds." Asserted
  in T-1411.
- **Sandbox escape.** goja is sandboxed unless the host binds globals. The only binding
  is the `state` shim. T-1415 asserts `require`/`process`/`fetch` are undefined.

## Verification
Compile is testable against source strings (valid → right phase names; malformed →
descriptive error). The phase adapter is testable for output + state hand-off
round-trip. Cancellation is testable by a JS infinite loop aborted by a cancelled ctx.
The sandbox is testable by asserting host globals are absent. End-to-end is testable by
loading a two-phase example, running it, and checking the Report — plus the runtime
integration: `/workflow load` then `/workflow run` through the real Session.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks are
approved (house Gate 1).
