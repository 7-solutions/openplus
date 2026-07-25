# Change 0014 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## H0 — Decision
- [x] T-1400 Write `docs/adr/0009-js-workflow-adapter.md`: the ADR-0006 goja trigger has
      fired; goja is the JS adapter behind the (unchanged) `Workflow` port; pure-Go,
      cgo-free. ADR-0006 stays Accepted — this records the deferral ending.

## H1 — Adapter core (internal/jsworkflow)
- [x] T-1410 Add `github.com/dop251/goja` (latest stable, pinned in `go.mod`). Assert
      `CGO_ENABLED=0 go build ./...` is green — cgo-free is the whole reason goja was
      chosen over a cgo runtime. Red: a build test gating the dependency.
- [x] T-1411 `Compile(name, src string) (orchestrate.Workflow, error)`: VM with
      `module`/`exports`, run src, read `{name, maxRetries?, phases:[{name, run}]}`.
      Missing `phases`/`name`, non-function `run`, or a load-time throw → descriptive
      error; never an empty Workflow. Red: valid src → phases named as declared;
      malformed → named error.
- [x] T-1412 `jsPhase.Run`: build `state` shim `{last, get(k), set(k,v)}` over
      `*orchestrate.State`, call JS `run`, coerce return to string output, map a thrown
      JS value to a Go error. Red: output + state hand-off round-trip; throw → error.
- [x] T-1413 Honor JS `maxRetries` → `Workflow.MaxRetries` (default 0). Red: a failing
      JS phase retries N extra times then fails the workflow with the right report.

## H2 — Safety
- [x] T-1414 Cancellation: wire `ctx.Done()` → goja `vm.Interrupt`; an interrupted
      script surfaces as a context error so the engine stops mid-phase. Red: a
      `while(true){}` phase aborts on a cancelled ctx, returns promptly.
- [x] T-1415 Sandbox: bind only `module`, `exports`, `state`, `console.log` (→ host
      logger). Red: `require`/`process`/`fetch` raise ReferenceError; `console.log`
      output is captured.

## H3 — File load + example
- [x] T-1416 `LoadFile(path) (orchestrate.Workflow, error)`: read + Compile; errors name
      the file. Red via `testdata`.
- [x] T-1417 `internal/jsworkflow/testdata/example.js`: two phases with hand-off,
      proving Compile → Run → Report end-to-end.

## H4 — Runtime surface
- [x] T-1418 Extend `/workflow` with `load <path>`: compile + register into
      `s.Workflows` under the declared name; return the registered name. Existing
      `/workflow run <name>` runs it unchanged. Integration test through the real
      Session: load → run → Report.OK.

## H5 — Gate
- [x] T-1419 Advisor pass (resolve every finding); update knowledge graph + memory.
      Update `AGENTS.md` refuse-list: `goja .js workflow compatibility` → "shipped
      (0014), behind `/workflow load`."
