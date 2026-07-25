# Embedder + memory config deltas, cmd wiring, integration tests (delta — change 0004)

> Change 0004 sits on top of 0002. The baseline (Embedder + Memory config
> blocks, runtime.Assemble wiring, cmd/openplus entrypoint, unit tests per
> layer) already shipped via feb8321, c6addf4, 79ed516, 4ea958c. This file
> documents the operational deltas that 0004 adds.

## Purpose
Harden the embedder and memory subsystems for real projects: env overrides
on top of `opencode.json`, timeouts and dimension-drift surfacing, a memory
auto-open flag, exit-code discipline on the CLI, and end-to-end integration
tests that prove the runtime survives realistic scenarios.

## Requirements

### Requirement: Embedder env overrides
The embedder configuration SHALL accept env-var overrides that take
precedence over `opencode.json`. Precedence: env > file > default.

#### Scenario: OPENPLUS_EMBED_MODEL wins over the file
- **WHEN** `opencode.json` sets `embedder.model` to one value and
  `OPENPLUS_EMBED_MODEL` is set to another
- **THEN** `Config.Embedder.Model` reports the env value

#### Scenario: OPENPLUS_EMBED_BASE_URL wins over the file
- Same pattern for `baseURL`.

#### Scenario: OPENPLUS_EMBED_API_KEY wins over the file
- Same pattern for `apiKey`.

#### Scenario: Absent env vars leave the file alone
- **WHEN** no `OPENPLUS_EMBED_*` env is set
- **THEN** `Config.Embedder` reflects only what `opencode.json` says

### Requirement: Embedder timeout
The embedder port SHALL support a per-call timeout, defaulting to 30s when
unset. A timeout returns a transport-class error; the embedder does not
swallow it silently.

#### Scenario: Hanging endpoint is bounded by the timeout
- **WHEN** the configured endpoint hangs longer than `Embedder.Timeout`
- **THEN** `Embed` returns within the timeout with an error wrapping
  `context.DeadlineExceeded`

### Requirement: Dimension drift surfaces as a typed error
The embedder port SHALL return `ErrDimensionDrift` (not a generic error)
when the endpoint returns a vector whose length disagrees with the pinned
dimension. The fallback path does NOT trigger on dimension drift.

#### Scenario: First call pins, second call drifts
- **WHEN** the first `Embed` returns 4-dim vectors and a later `Embed`
  returns 8-dim vectors
- **THEN** the later call returns an error wrapping `ErrDimensionDrift`

#### Scenario: Fallback does NOT catch drift
- **WHEN** `FallbackTo` is configured and the primary returns dim-drift
- **THEN** the error propagates to the caller; the fallback is not tried

### Requirement: Embedder fallback on transport failure
The embedder port SHALL retry against a configured fallback endpoint when
the primary returns a transport-class error (network failure, 5xx, 429).

#### Scenario: Primary 500, fallback 200
- **WHEN** the primary endpoint returns HTTP 500
- **THEN** the call succeeds against the fallback endpoint

#### Scenario: Primary 400, no fallback attempted
- **WHEN** the primary endpoint returns HTTP 400
- **THEN** the error propagates without consulting the fallback

### Requirement: Memory auto-open is opt-in
Memory stores SHALL be opened with `os.Open` semantics by default; the
`memory.autoOpen: true` flag in `opencode.json` SHALL switch the runtime
to `os.OpenFile(..., O_CREATE)`.

#### Scenario: autoOpen false and missing path
- **WHEN** `memory.autoOpen` is unset (or false) and the path does not exist
- **THEN** `runtime.Assemble` returns an error wrapping `os.ErrNotExist`

#### Scenario: autoOpen true and missing path
- **WHEN** `memory.autoOpen` is true and the path does not exist
- **THEN** the file is created and `Session.Memory` is non-nil

### Requirement: Memory path env override
`OPENPLUS_MEMORY_PATH` SHALL override `opencode.json`'s `memory.path`,
matching the precedence model used for the embedder env overrides.

#### Scenario: env path wins over the file
- **WHEN** `opencode.json` sets `memory.path` to one value and
  `OPENPLUS_MEMORY_PATH` is set to another
- **THEN** the store opens at the env path

### Requirement: Memory max-entries cap
The memory store SHALL respect a `memory.maxEntries` cap, pruning
oldest-first on each write. Zero means unbounded.

#### Scenario: maxEntries=2, write 5
- **WHEN** `memory.maxEntries` is 2 and 5 chunks are written
- **THEN** exactly 2 chunks remain, the 2 most recent

### Requirement: CLI config flag
`cmd/openplus` SHALL accept `--config / -c` to point at a non-default
`opencode.json`. The default remains `<root>/opencode.json`.

#### Scenario: --config /tmp/x.json
- **WHEN** `--config /tmp/x.json` is passed and `/tmp/x.json` exists
- **THEN** the runtime assembles from `/tmp/x.json`

#### Scenario: --config /missing.json
- **WHEN** `--config /missing.json` is passed and the file does not exist
- **THEN** the process exits non-zero with a clear error message

### Requirement: CLI env overrides
`OPENPLUS_MODEL` SHALL override `--model` and the file. `OPENPLUS_FAKE=1`
SHALL enable the fake provider without `--fake`. Precedence:
env > flag > file.

#### Scenario: OPENPLUS_MODEL=foo/bar
- **WHEN** `OPENPLUS_MODEL=foo/bar` is set and `--model baz/qux` is passed
- **THEN** the runtime assembles against `foo/bar`

#### Scenario: OPENPLUS_FAKE=1
- **WHEN** `OPENPLUS_FAKE=1` is set and `--fake` is not passed
- **THEN** the runtime uses the scripted fake provider

### Requirement: CLI version flag
`cmd/openplus --version` SHALL print `openplus <version>` to stdout and
exit 0 without assembling a session.

#### Scenario: --version
- **WHEN** `--version` is passed
- **THEN** stdout matches `^openplus \S+$` and the process exits 0

### Requirement: CLI exit-code contract
The process exit code SHALL follow: 0 = clean run, 2 = configuration
problem (missing credential, no model), 1 = everything else.

#### Scenario: Missing credential
- **WHEN** the runtime returns an error wrapping `runtime.ErrMissingCredential`
- **THEN** the process exits 2

#### Scenario: No model
- **WHEN** the runtime returns an error wrapping `runtime.ErrNoModel`
- **THEN** the process exits 2

#### Scenario: Provider error mid-turn
- **WHEN** a turn fails with a non-config error (transport, 5xx, parse)
- **THEN** the process exits 1

### Requirement: End-to-end integration tests
The runtime SHALL pass four integration scenarios that exercise the full
`Assemble → Run` path, not just per-layer unit tests.

#### Scenario: Memory round-trip across two Sessions
- **WHEN** Session A writes via `Run` and Session B (against the same
  memory path) calls `Run` with a relevant prompt
- **THEN** Session B's `AssembleContext` surfaces the original exchange
  via retrieval

#### Scenario: Permission deny stops tool execution
- **WHEN** `Permission.Tools["bash"] = "deny"` and the scripted provider
  issues a bash call
- **THEN** the tool does not execute and the agent reports the rejection

#### Scenario: Credential-missing wraps to exit 2
- **WHEN** a remote provider is configured without `apiKey`
- **THEN** `runtime.Assemble` returns an error wrapping
  `runtime.ErrMissingCredential`, and `exitCode(err) == 2`

#### Scenario: --fake end-to-end smoke
- **WHEN** `Assemble(tempRoot, Options{Fake:true}) → Run("hi") → Close`
- **THEN** the returned history has at least one user message and one
  assistant message, and no error is returned