# Change 0005 — tiktoken-go behind the Tokenizer port (PLAN)

## Why
Change 0001 ships the **Tokenizer port** (`internal/contextmgr/tokenizer.go`) with
a single implementation, `Heuristic`, that estimates token cost by
characters-per-token. That works as a soft ceiling for ADR-0008's budget,
but the heuristic is wrong on the edges — code, technical jargon, and
non-English prose are under- or over-counted. For OpenAI-compatible
endpoints the real BPE is available client-side via `tiktoken-go`, which
runs the same encoder OpenAI uses to bill. Pinning that behind the same
port tightens the budget without changing any caller.

0001 explicitly deferred this in `tasks.md` (T-060 note):

> tiktoken-go is deferred behind the port — it fetches its BPE table
> from `openaipublic.blob.core.windows.net` at first use, which breaks
> the local-first guarantee and offline token counting. An exact
> counter (embedded BPE, or a provider count endpoint) drops in behind
> `Tokenizer` with no caller changes.

The blocker the note named (network fetch on first use) is now
**solvable inside the change**: tiktoken-go supports `TIKTOKEN_CACHE_DIR`
for persistent local caching, and `SetBpeLoader` + an offline loader
(`github.com/pkoukk/tiktoken-go-loader`) for fully offline operation.
0005 picks one path (cached download by default, with the offline
loader as an opt-in build tag) so the local-first guarantee holds.

## What changes
Adds capability `tokenizer-exact` — a second implementation of the
existing `Tokenizer` port, plus the assembly-time wiring to pick it.

- `internal/contextmgr/tiktoken.go` (new): `Tiktoken` struct with a
  `*tiktoken.Tiktoken` from `github.com/pkoukk/tiktoken-go`. Implements
  `Count(text string) int` by calling `Encode(text, nil, nil)` and
  returning the slice length. Exposes a constructor
  `NewTiktoken(model string) (*Tiktoken, error)` that picks the
  encoding by model (cl100k_base for gpt-4/gpt-3.5, o200k_base for
  gpt-4o).
- `internal/contextmgr/tokenizer.go` (modified): `ForModel` returns
  the new `Tiktoken` when the configured model is OpenAI-shaped; the
  `Heuristic` is kept as the fallback for `anthropic/*`, `local/*`,
  unknown prefixes, and any case where the tiktoken init fails.
- `go.mod`: add `github.com/pkoukk/tiktoken-go` (latest tagged,
  v0.1.8). No transitive cgo; tiktoken-go is pure Go.
- `internal/contextmgr/tiktoken_loader_offline.go` (new, build-tag
  `offline`): wires `tiktoken.SetBpeLoader` to the offline loader
  when the build tag is set, so the BPE is read from embedded files
  instead of the network. Documented in the godoc; default build uses
  the cached-download path.
- Tests in `internal/contextmgr/tiktoken_test.go`:
  - `TestTiktokenCountMatchesReference`: count the canonical
    "Hello, world!" string against the published tiktoken number.
  - `TestTiktokenRoundTrip`: encode then decode returns the input.
  - `TestForModelPicksTiktokenForOpenAI`: `ForModel("openai/gpt-4o")`
    returns the new `Tiktoken`; `ForModel("anthropic/...")` returns
    `Heuristic`.
  - `TestForModelFallsBackOnInitError`: a bogus model string falls
    back to `Heuristic` rather than panicking.
- The existing `Tokenizer` port signature is unchanged. Every caller
  (`Budgeter`, `CountMessages`, `contextmgr` tests) keeps working
  through the interface.

## Why this is a separate change (not 0004 / 0002)
- 0002 explicitly limited its scope to "the smallest surface needed to
  see it work end to end" — wiring, not new implementations.
- 0004 is the operational hardening of 0002's surface (env vars,
  timeouts, integration tests). Adding a new external dependency
  wasn't its scope.
- The change introduces a new third-party import (`tiktoken-go`),
  which is a dependency-surface event ADR-worthy on its own. Doing it
  in its own change keeps the audit trail clean.

## Non-goals (explicitly out of scope)
- **Anthropic tokenizer** — Anthropic's BPE isn't publicly published.
  Anthropic stays on the heuristic. If a count endpoint shows up,
  that's a follow-up.
- **Provider count endpoints** — both OpenAI and Anthropic offer
  token-count API calls. Wiring those is a follow-up; this change
  is strictly the local BPE path.
- **Streaming chunked counting** — `Encode` on the full assembled
  context runs in one call. For budgets up to 200k tokens that's a
  few ms per `Budgeter.Fit` call; well within the budget for a
  per-turn operation. Streaming is unnecessary.
- **Anything on the v1 refuse list** (voice/ASR, Max Mode, MCP
  marketplace, web/share UI, hosted server, goja `.js` workflows).
- **A `<model>/<count-endpoint>` provider protocol** — separate change.

## Governing decisions
- **ADR-0008** (context budgeter / tokenizer / reconstruction) — this
  change tightens the existing soft-ceiling estimator without changing
  the priority order or the reconstruction logic.
- No new ADRs.

## Risk
- **First-run download.** tiktoken-go downloads its BPE tables from
  `openaipublic.blob.core.windows.net` on the first use unless
  `TIKTOKEN_CACHE_DIR` is set or `SetBpeLoader` overrides the loader.
  The change sets `TIKTOKEN_CACHE_DIR` to `$XDG_CACHE_HOME/openplus/tiktoken`
  (with sensible fallbacks) so a single download persists across
  processes; without internet on first run, `ForModel` falls back to
  `Heuristic` rather than failing the assembly.
- **Encoding drift.** OpenAI ships new encodings as models evolve.
  `EncodingForModel` returns an error for unknown model names; the
  test pins the fallback path so a future `gpt-99` doesn't panic.
- **Encoding for non-OpenAI prefixes.** `ForModel("local/qwen2.5-coder")`
  still uses the heuristic — the model's BPE isn't in tiktoken-go's
  table. Same behavior as before the change for non-OpenAI families.

## Verification
1. `go build ./...` clean.
2. `go test ./internal/contextmgr/...` — new tests green, old tests
   unchanged (the port signature is stable).
3. `go test ./...` — **23/23 green** (22 packages + the new
   contextmgr tests don't add a package, just new tests).
4. `go run ./cmd/openplus --fake -C $(mktemp -d) -p 'hello'` — the
   smoke path still works; `ForModel` is hit once at Assemble.
5. `TIKTOKEN_CACHE_DIR=$(mktemp -d) go test ./internal/contextmgr/...`
   — fresh cache, encoding downloads once, subsequent tests use the
   cache.
6. `go test -tags offline ./internal/contextmgr/...` — the offline
   build path; asserts the `SetBpeLoader` wiring compiles when the
   loader is present.

## Approval
Per house Gate 1, implementation begins only after this proposal +
the delta spec + tasks are approved. The third-party dependency is
the load-bearing risk; the verification block exercises both the
online (cached) and offline paths so a refusal on either is
detectable before commit.