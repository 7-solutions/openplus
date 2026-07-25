# Change 0005 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## A — tiktoken-go behind the Tokenizer port

- [ ] T-450 RED `TestTiktokenCountMatchesReference`: count the canonical
      "Hello, world!" string against the published tiktoken number (2
      tokens for cl100k_base). RED until `Tiktoken` exists.
- [ ] T-451 RED `TestTiktokenRoundTrip`: encode then decode returns the
      input. Pins that the BPE doesn't mangle input.
- [ ] T-452 Implement `internal/contextmgr.Tiktoken` struct wrapping
      `*tiktoken.Tiktoken` with `Count(text) int` calling
      `Encode(text, nil, nil)` and returning `len(...)`.
- [ ] T-453 RED `TestForModelPicksTiktokenForOpenAI`: `ForModel("openai/gpt-4")`
      returns the new `Tiktoken`; `ForModel("anthropic/claude-...")` still
      returns `Heuristic`.
- [ ] T-454 Modify `ForModel` to call `NewTiktoken(model)` for OpenAI-shaped
      prefixes (openai, gpt-*, o200k_base, cl100k_base). On error or
      unknown prefix, fall back to `Heuristic` — never panic.
- [ ] T-455 RED `TestForModelFallsBackOnInitError`: a model name not in
      tiktoken-go's table (e.g. "local/qwen2.5-coder") returns
      `Heuristic`, not an error.
- [ ] T-456 RED `TestTiktokenInitSetsCacheDir`: when
      `TIKTOKEN_CACHE_DIR` is unset, `NewTiktoken` sets it to a sane
      default (e.g. `$XDG_CACHE_HOME/openplus/tiktoken` or
      `$HOME/.cache/openplus/tiktoken`). Pins the local-first guarantee.
- [ ] T-457 Implement `cacheDir()` helper + apply in `NewTiktoken`.

## B — Optional offline build tag

- [ ] T-460 RED `go build -tags offline ./internal/contextmgr/...` fails
      because the offline loader wiring is missing. (Building the
      package without the tag must NOT pull in the offline loader.)
- [ ] T-461 Add `internal/contextmgr/tiktoken_offline.go` with build tag
      `//go:build offline`. Calls `tiktoken.SetBpeLoader` with a stub
      loader; depends on `github.com/pkoukk/tiktoken-go-loader` (only
      pulled in under that tag).
- [ ] T-462 RED `go test -tags offline ./internal/contextmgr/...` —
      pinned so a future refactor doesn't break the offline path.

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