# Tokenizer port — tiktoken-go implementation (delta — change 0005)

> Change 0005 sits on top of the 0001 foundation. The Tokenizer port and its
> `Heuristic` implementation are already shipped (0001/M6, T-060). This file
> documents the second implementation that lands behind the same port, plus
> the assembly-time wiring to pick it.

## Purpose
Provide an exact BPE-based token counter for OpenAI-shaped model prefixes,
implementing the same `Tokenizer` interface as the existing `Heuristic`.
Drop-in behind the port — no caller changes.

## Requirements

### Requirement: tiktoken-backed Tokenizer
The system SHALL provide a `Tokenizer` implementation backed by
`github.com/pkoukk/tiktoken-go`. The implementation counts tokens by running
the model's actual BPE encoder, matching what OpenAI bills.

#### Scenario: OpenAI-shaped prefix picks the tiktoken implementation
- **WHEN** `ForModel("openai/gpt-4")` is called
- **THEN** the returned `Tokenizer` is the new `Tiktoken` type, not `Heuristic`

#### Scenario: Anthropic prefix stays on Heuristic
- **WHEN** `ForModel("anthropic/claude-sonnet-5")` is called
- **THEN** the returned `Tokenizer` is `Heuristic` (Anthropic's BPE isn't
  publicly published)

#### Scenario: unknown prefix falls back to Heuristic
- **WHEN** `ForModel("local/qwen2.5-coder")` is called
- **THEN** the returned `Tokenizer` is `Heuristic`; no panic, no error

### Requirement: tiktoken count matches the reference
The new `Tiktoken.Count(text)` SHALL return the same token count as
`len(tiktoken.Encode(text, nil, nil))` for the chosen encoding.

#### Scenario: Hello, world!
- **WHEN** `Tiktoken{Encoding: cl100k_base}.Count("Hello, world!")` is called
- **THEN** it returns 2 (the published tiktoken count for cl100k_base on
  that string)

### Requirement: tiktoken round-trips losslessly
Encoding then decoding a string SHALL return the original text. This pins
that the BPE doesn't mangle input under the chosen encoding.

#### Scenario: encode then decode
- **WHEN** a string `s` is encoded and the result is decoded with the same
  encoding
- **THEN** the result equals `s`

### Requirement: cache directory defaults to a sane local path
When `TIKTOKEN_CACHE_DIR` is unset, `NewTiktoken` SHALL set it to a
project-local cache directory (under `$XDG_CACHE_HOME/openplus/tiktoken`
with `$HOME/.cache/openplus/tiktoken` as a fallback). The local-first
guarantee holds: the BPE is downloaded at most once per host.

#### Scenario: cache dir is set on first call
- **WHEN** `NewTiktoken("openai/gpt-4")` is called with `TIKTOKEN_CACHE_DIR`
  unset
- **THEN** `os.Getenv("TIKTOKEN_CACHE_DIR")` returns a path that ends in
  `openplus/tiktoken`

### Requirement: offline loader is opt-in via build tag
An offline BPE loader SHALL be wired via `tiktoken.SetBpeLoader` only when
the binary is built with `-tags offline`. The default build (no tag) does
NOT pull in the offline loader dependency.

#### Scenario: default build excludes the offline loader
- **WHEN** `go build ./...` is run without `-tags offline`
- **THEN** the resulting binary does not import `tiktoken-go-loader`

#### Scenario: offline build wires the loader
- **WHEN** `go build -tags offline ./...` is run
- **THEN** the resulting binary calls `tiktoken.SetBpeLoader(...)` and
  imports `tiktoken-go-loader`

### Requirement: callers see no change
Every existing caller of `Tokenizer.Count` and `Budgeter.Fit` continues
to work without modification. The port signature is unchanged.

#### Scenario: existing contextmgr tests stay green
- **WHEN** `go test ./internal/contextmgr/...` is run
- **THEN** the existing `TestBudgeterRespectsBudget`,
  `TestCheckpointRoundTrips`, etc. all stay green
- **AND** the new tiktoken tests are green too

## Out of scope (per proposal)
Anthropic tokenizer · provider count endpoints · streaming chunked
counting · v1 refuse list items · new provider protocol for counts.