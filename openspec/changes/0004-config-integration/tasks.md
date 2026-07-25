# Change 0004 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.
> Sub-scope markers (A/B/C/D) match the proposal; commit each slice as its own PR.

## A — Embedder config deltas
- [x] T-400 RED `TestEmbedderEnvOverridesFile`: write `opencode.json` with
      embedder.model=`file-model` and setenv `OPENPLUS_EMBED_MODEL=env-model`;
      `cfg.Embedder.Model` must be `env-model`.
      Done — 5684a03.
- [x] T-401 RED `TestEmbedderEnvOverridesOnBaseURL` and
      `TestEmbedderEnvOverridesOnAPIKey`: same pattern for the other two
      fields.
      Done — 5684a03.
- [x] T-402 Make the three env overrides pass. `Config.Load` (or a new
      `Config.Embedder.resolve()` helper) layers env on top of the parsed
      file. Same priority model as `OPENAI_API_KEY` in many tools: env
      wins, file is the default.
      Done — 5684a03.
- [x] T-403 RED `TestEmbedderTimeoutApplied`: drive `Local.Embed` against an
      `httptest.Server` that hangs; assert the call returns within the
      configured timeout (e.g. 50ms).
      Done — 3acf98c.
- [x] T-404 Add `Embedder.Timeout` (default 30s) and plumb it into the
      `http.Client` used by `Local`. `nil` `http.Client` becomes
      `&http.Client{Timeout: cfg.Embedder.Timeout}`.
      Done — 3acf98c.
- [x] T-405 RED `TestLocalErrDimensionDrift`: first `Embed` returns dim=4,
      second returns dim=8; assert `errors.Is(err, embed.ErrDimensionDrift)`.
      Done — 09199d7.
- [x] T-406 Introduce `ErrDimensionDrift` and surface it from `Local.Embed`.
      Done — 09199d7.
- [x] T-407 RED `TestLocalFallbackOnTransport`: first endpoint returns
      500 (transport-class failure); second endpoint returns 200; assert
      `vecs` from the second endpoint.
      Done — 471ee1c.
- [x] T-408 Add `FallbackTo(embed.Embedder)` method; only triggers on
      transport-class errors (network, 5xx, 429). Documented in the godoc.
      Done — 471ee1c.

## B — Memory config deltas
- [ ] T-410 RED `TestMemoryAutoOpenFalseMissingPathFails`: assemble with
      `memory.autoOpen=false` (default), path that doesn't exist; assert
      `runtime.Assemble` returns an error wrapping `os.ErrNotExist`.
- [ ] T-411 RED `TestMemoryAutoOpenTrueCreatesPath`: same path, but
      `"autoOpen": true`; assert the file is created and `Session.Memory`
      is non-nil.
- [ ] T-412 Add `Memory.AutoOpen bool` (default false) and respect it in
      `assembleMemory`.
- [ ] T-413 RED `TestMemoryEnvPathOverride`: setenv `OPENPLUS_MEMORY_PATH`,
      assemble; assert the store opens at that path, not the configured one.
- [ ] T-414 Apply the env override at the same priority as the embedder
      env overrides (T-402).
- [ ] T-415 RED `TestMemoryMaxEntriesZeroIsUnbounded`,
      `TestMemoryMaxEntriesCapEvictsOldest`: assemble with
      `memory.maxEntries=2`, write 5 chunks, assert 2 are retained and the
      2 retained are the most recent.
- [ ] T-416 Add `Memory.MaxEntries int` (0 = unbounded) and `memory.Store`
      gains `SetMaxEntries` that prunes oldest-first on each write. Keep
      the unbounded path zero-cost.

## C — `cmd/openplus` wiring deltas
- [ ] T-420 RED `TestMainVersion`: `run()` with `--version` flag returns
      `nil` and prints to stdout something matching `^openplus \S+$`; no
      assembly happens.
- [ ] T-421 Add `var Version = "dev"` in `cmd/openplus/main.go` and the
      `--version` flag. Wire `-ldflags '-X main.Version=v0.1.0'` in a docs
      note (not in the binary itself).
- [ ] T-422 RED `TestMainConfigFlag`: `run()` with `--config /tmp/x.json`
      reads that file; a sentinel value inside it (e.g. a unique model
      name) must reach the runtime. Cover both the success path and the
      "file not found" path that returns exit 1 with a clear message.
- [ ] T-423 Add `--config / -c` flag (default `<root>/opencode.json`).
- [ ] T-424 RED `TestMainEnvOverrides`:
      `OPENPLUS_MODEL=foo/bar OPENPLUS_FAKE=1` overrides the file and the
      `--fake` flag.
- [ ] T-425 Apply the two env vars in `run()` before `runtime.Assemble`.
      Document precedence: env > `--model`/`--fake` > `opencode.json`.
- [ ] T-426 RED `TestMainExitCodeContract`: missing credential returns
      exit 2; no model returns exit 2; provider error returns exit 1;
      successful `--fake` run returns exit 0. Use a temp subprocess via
      `os/exec` if needed; if the function isn't testable directly, lift
      the exit-code mapping into a small `func exitCode(err error) int`.
- [ ] T-427 Introduce `exitCode(err error) int` mapping `ErrMissingCredential`
      and `ErrNoModel` to 2, everything else to 1. Wire it through `main()`.

## D — Integration tests
- [ ] T-430 RED `TestIntegrationMemoryRoundTripAcrossSessions`: write via
      `s1.Run`, construct `s2` against the same path, `s2.Run` with a
      prompt whose embedding-relevant query term matches the prior
      exchange; assert the retrieval surfaces it via `s2.AssembleContext`.
- [ ] T-431 RED `TestIntegrationPermissionDenyStopsExecution`: configure
      `Permission.Tools["bash"] = "deny"`, drive `s.Run` with a scripted
      provider that issues a bash tool call, assert the call is rejected
      and the agent reports it without invoking the tool.
- [ ] T-432 RED `TestIntegrationCredentialMissingWrapsToExit2`: assemble
      against a remote provider with no apiKey; assert
      `errors.Is(err, runtime.ErrMissingCredential)` and that
      `exitCode(err) == 2`.
- [ ] T-433 RED `TestIntegrationFakeSmokeEndToEnd`: full path
      `Assemble(tempRoot, Options{Fake:true}) → Run("hi") → Close`. Assert
      returned history has at least one user and one assistant message and
      no error.

## Verification (Gate 5 — before declaring 0004 done)
- [ ] `go build ./...` clean.
- [ ] `go test ./...` green for every package (existing + new tests).
- [ ] `go test ./internal/runtime/... -run Integration -v` — the four
      integration scenarios pass.
- [ ] End-to-end smoke:
      `go run ./cmd/openplus --fake -C $(mktemp -d) -p 'say hello'`
      prints the scripted reply.
- [ ] `go run ./cmd/openplus --version` prints `openplus dev` (or the
      build version).
- [ ] `OPENPLUS_FAKE=1 go run ./cmd/openplus -C $(mktemp -d) -p 'say hello'`
      works without `--fake`.
- [ ] Exit-code contract documented in `cmd/openplus/main.go` godoc.

## Out of scope (per proposal)
Encryption at rest · shell completion · rate limiting / retries in the
adapter · anything on the v1 refuse list.