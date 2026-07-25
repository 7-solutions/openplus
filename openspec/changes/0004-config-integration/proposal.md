# Change 0004 — Embedder + memory config deltas + cmd wiring + integration tests (PLAN)

## Why
Change 0002 shipped the baseline: `Embedder` and `Memory` config blocks,
`runtime.Assemble` that consumes them, `cmd/openplus` wired to the runtime,
and unit tests covering each layer in isolation. With that baseline green
(feb8321, c6addf4, 79ed516, 4ea958c), 0004 is the **delta** — the operational
features that make the baseline usable in real projects and the integration
tests that prove it stays usable.

Four sub-scopes, each a vertical slice on its own:

1. **Embedder config deltas** — env overrides, per-call timeout, dimension
   drift surfaced before the wire, and a fallback path when the endpoint
   fails.
2. **Memory config deltas** — auto-create on first write, max-entries cap,
   and the env-var knob for the on-disk path.
3. **`cmd/openplus` wiring deltas** — `--config` flag for alternate config
   paths, env overrides (`OPENPLUS_MODEL`, `OPENPLUS_FAKE`), `--version`,
   and a documented exit-code contract.
4. **End-to-end integration tests** — driving `runtime.Session.Run` through
   realistic scenarios (memory round-trip across two sessions, permission
   matrix, credential-missing exit code, `--fake` smoke against the full
   `Assemble → Run` path).

## What changes

### Sub-scope A — Embedder config deltas
- `internal/config/config.go`: add `Embedder.Timeout time.Duration`,
  `Embedder.Fallback []Embedder` (optional secondary endpoint), and an env
  override path that reads `OPENPLUS_EMBED_MODEL` / `OPENPLUS_EMBED_BASE_URL`
  / `OPENPLUS_EMBED_API_KEY` and applies them as a higher-priority layer on
  top of `opencode.json` (the file is the base, the env wins).
- `internal/embed/embed.go`: plumb the timeout into the `http.Client`
  (default 30s, configurable via the new `Embedder.Timeout`), surface
  dimension-drift as a `ErrDimensionDrift` sentinel so callers can
  distinguish it from a transport error, and add a `FallbackTo` method that
  tries the secondary endpoint on transport failure (not on dim drift — the
  fallback endpoint almost certainly uses a different model).
- Tests in `internal/config/embedder_test.go` (env overrides) and
  `internal/embed/embed_test.go` (timeout, dim drift, fallback).

### Sub-scope B — Memory config deltas
- `internal/config/config.go`: add `Memory.AutoOpen bool` (open the store
  with `os.Create` semantics on first write instead of failing closed when
  the file doesn't exist yet), `Memory.MaxEntries int` (cap on stored
  chunks; 0 = unbounded, kept for explicit-disable), and an env override
  `OPENPLUS_MEMORY_PATH`.
- `internal/runtime/assemble.go assembleMemory()`: respect `AutoOpen`,
  apply `MaxEntries` via `memory.Store.SetMaxEntries`, and fail with a
  clear error when `AutoOpen` is false and the path doesn't exist.
- Tests in `internal/config/embedder_test.go` (memory block deltas) and
  `internal/memory/` (store-level behavior).

### Sub-scope C — `cmd/openplus` wiring deltas
- `cmd/openplus/main.go`: add `--config / -c` flag (default
  `<root>/opencode.json`), `OPENPLUS_MODEL` env (overrides `--model` and the
  file), `OPENPLUS_FAKE=1` env (overrides `--fake`), `--version` flag
  printing the build version (a `var Version = "dev"` in `cmd/openplus`).
  Document the exit-code contract: 0 = clean, 2 = missing credential / no
  model, 1 = everything else (provider error, policy rejection, etc.).
- Tests in a new `cmd/openplus/main_test.go` driving `runOnce` / `runTUI`
  through public seams.

### Sub-scope D — Integration tests
- `internal/runtime/integration_test.go` (new file): four scenarios
  driving `runtime.Session.Run` end to end:
  - Memory round-trip: write via `Session.Run`, then construct a *second*
    `Session` against the same memory path and assert retrieval surfaces
    the original exchange.
  - Permission matrix: assemble with `Permission.Tools["bash"] = "deny"`,
    `Session.Run` a prompt that triggers a tool call, assert the call is
    rejected without execution.
  - Credential-missing: assemble without a configured apiKey for a remote
    provider, assert `ErrMissingCredential` is returned (already covered by
    `assemble_test.go` but this test asserts the exit-code contract by
    checking the wrapping, not just the error type).
  - `--fake` smoke: full path from `Assemble(root, Options{Fake: true})` to
    `Session.Run` to `Session.Close`, with a temp project root and a
    scripted provider.

## Why this is a separate change (not 0002)
0002's proposal explicitly limited scope to "the smallest surface needed to
see it work end to end." Embedder env overrides, memory TTL, shell completion,
exit-code documentation, and end-to-end tests across two Session instances
are all *operational* concerns — they harden the runtime, they don't make it
work. Folding them into 0002 would have grown it past the integration-slice
budget the proposal named.

## Non-goals (explicitly out of scope)
- **Encryption at rest** for the memory store. That's a real concern but it
  introduces a key-management story (where does the key come from? what
  happens on rotate?) that needs its own ADR. Flagged in Risks.
- **Shell completion** for `cmd/openplus`. Substantial generator code, a
  separate spec.
- **Anything on the v1 refuse list** (voice/ASR, Max Mode, MCP marketplace,
  web/share UI, hosted server, goja `.js` workflows).
- **Provider-side rate limiting / retries**. That's an adapter concern, not
  the embedder. If a future adapter needs it, that's a separate change.

## Governing decisions
- ADR-0001 (Crush base, config compatibility).
- ADR-0003 (memory store). 0004 adds operational knobs on top, not new ports.
- ADR-0004 (embedder). 0004 adds env overrides and timeouts behind the
  existing `Embedder` interface.
- ADR-0008 (context budget). Unchanged.
- No new ADRs.

## Risk
- **Sub-scope C exit-code contract.** Documenting exit codes is a backwards-
  compatibility commitment. Any first-run script that grep'd stderr for
  specific text might break. Mitigation: keep error text identical, only add
  structured exit-code returns.
- **Sub-scope B `AutoOpen`** turns a failure mode (missing memory file)
  into a silent side effect (creates a file). That can mask config errors
  ("why is there a `.openplus/memory.db` in my home dir?"). Mitigation:
  default `AutoOpen` to `false`; require an explicit `"autoOpen": true` in
  the config block to enable it.
- **Sub-scope A fallback endpoint** is a feature, but it doubles the embedder
  surface area. Tests must pin the failure modes the fallback does NOT cover
  (dim drift, 4xx other than 5xx and 429). Mitigation: `FallbackTo` only
  triggers on `errors.Is(err, ErrTransport)`; everything else propagates.

## Verification
1. `go build ./...` — must be clean.
2. `go test ./...` — **must remain 20/20 green** plus the new tests added in
   each sub-scope. Final expected package count: 21 (adds
   `cmd/openplus` if it gets a test file, otherwise 20).
3. `go test ./internal/runtime/... -run Integration -v` — confirm the four
   integration scenarios pass.
4. `go run ./cmd/openplus --fake -C $(mktemp -d) -p 'say hello'` — smoke
   test that the wiring still works end to end.
5. `go run ./cmd/openplus --version` — prints `openplus dev` (or the build
   version).
6. `OPENPLUS_FAKE=1 go run ./cmd/openplus -C $(mktemp -d) -p 'say hello'` —
   env override path works without `--fake`.
7. `grep -n 'ErrMissingCredential\|ErrNoModel' cmd/openplus` — confirms the
   documented exit-code contract maps the two sentinel errors to exit 2.

## Approval
Per house Gate 1, implementation begins only after this proposal + the
delta spec + tasks are approved. The four sub-scopes are independent enough
to ship as four separate PRs (one per slice), each with its own red-first
tests and a green build, but they all belong to Change 0004.