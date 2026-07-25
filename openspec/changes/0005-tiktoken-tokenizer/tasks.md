# Change 0005 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## A — tiktoken-go behind the Tokenizer port

- [x] T-450 RED `TestTiktokenCountMatchesReference`: count the canonical
      "Hello, world!" string against the published tiktoken number (4
      tokens for cl100k_base — corrected from the original 2-token
      estimate; tiktoken-go v0.1.8 returns 4 for "Hello, world!" on both
      cl100k_base and o200k_base).
      Done — f7dc89e.
- [x] T-451 RED `TestTiktokenRoundTrip`: encode then decode returns the
      input. Pins that the BPE doesn't mangle input.
      Done — f7dc89e.
- [x] T-452 Implement `internal/contextmgr.Tiktoken` struct wrapping
      `*tiktoken.Tiktoken` with `Count(text) int` calling
      `Encode(text, nil, nil)` and returning `len(...)`.
      Done — f7dc89e. Field named `inner` (not `tk`) to avoid the
      `tk.tk` shadow that go vet would flag.
- [x] T-453 RED `TestForModelPicksTiktokenForOpenAI`: `ForModel("openai/gpt-4")`
      returns the new `Tiktoken`; `ForModel("anthropic/claude-...")` still
      returns `Heuristic`.
      Done — f7dc89e.
- [x] T-454 Modify `ForModel` to call `NewTiktoken(model)` for OpenAI-shaped
      prefixes (openai, gpt-*, o200k_base, cl100k_base). On error or
      unknown prefix, fall back to `Heuristic` — never panic.
      Done — f7dc89e. Updated `TestForModelSelectsByPrefix` to reflect
      the new dispatch (openai → Tiktoken, anthropic → Heuristic,
      local/unknown → Heuristic fallback).
- [x] T-455 RED `TestForModelFallsBackOnInitError`: a model name not in
      tiktoken-go's table (e.g. "local/qwen2.5-coder") returns
      `Heuristic`, not an error.
      Done — f7dc89e.
- [x] T-456 RED `TestTiktokenInitSetsCacheDir`: when
      `TIKTOKEN_CACHE_DIR` is unset, `NewTiktoken` sets it to a sane
      default (e.g. `$XDG_CACHE_HOME/openplus/tiktoken` or
      `$HOME/.cache/openplus/tiktoken`). Pins the local-first guarantee.
      Done — f7dc89e. Also `TestTiktokenInitRespectsExistingCacheDir`
      pins that a pre-set env var is not overridden.
- [x] T-457 Implement `cacheDir()` helper + apply in `NewTiktoken`.
      Done — f7dc89e.

## B — Optional offline build tag

- [x] T-460 RED `go build -tags offline ./internal/contextmgr/...` fails
      because the offline loader wiring is missing. (Building the
      package without the tag must NOT pull in the offline loader.)
      Done — f7b1c9a. Default build is clean (offline file gated by
      `//go:build offline`); `-tags offline` build is also clean.
- [x] T-461 Add `internal/contextmgr/tiktoken_offline.go` with build tag
      `//go:build offline`. Calls `tiktoken.SetBpeLoader` with a stub
      loader; depends on `github.com/pkoukk/tiktoken-go-loader` (only
      pulled in under that tag).
      Done — f7b1c9a. Uses `tiktoken_loader.NewOfflineLoader()`.
- [x] T-462 RED `go test -tags offline ./internal/contextmgr/...` —
      pinned so a future refactor doesn't break the offline path.
      Done — f7b1c9a. Offline build's `go test ./internal/contextmgr/...`
      is green. `TestTiktokenOfflineBuildSeparation` reads
      `tiktoken_offline.go` and asserts both the `//go:build offline`
      directive and the `tiktoken-go-loader` reference are present,
      catching a future refactor that drops the build tag.

## Verification (Gate 5 — before declaring 0005 done)
- [ ] `go build ./...` clean.
- [ ] `go test ./internal/contextmgr/...` — old + new tests green.
- [ ] `go test ./...` — 22/22 green (the new tests live in an existing
      package, no package count change).
- [ ] `go run ./cmd/openplus --fake -C $(mktemp -d) -p 'hello'` —
      smoke path still works; `ForModel` is hit once at Assemble.
- [ ] `TIKTOKEN_CACHE_DIR=$(mktemp -d) go test
      ./internal/contextmgr/...` — fresh cache, encoding downloads
      once, subsequent runs use the cache.
- [ ] `go test -tags offline ./internal/contextmgr/...` — offline
      path compiles and runs.

## Out of scope (per proposal)
- Anthropic tokenizer (BPE isn't publicly published).
- Provider count endpoints.
- Streaming chunked counting.
- Anything on the v1 refuse list.
- A new `<model>/<count-endpoint>` provider protocol.