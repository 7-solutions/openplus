# Provider (delta)

## MODIFIED Requirements

### Requirement: Neutral request/response model
The system SHALL define a single `Request`/`Event` model with neutral content
Blocks (Text, ToolCall, ToolResult, Thinking, Image) that both adapters
marshal to and from.

**Former location:** `internal/provider/types.go`.

**New location:** `internal/ports/model.go` (the neutral types) and
`internal/ports/provider.go` (the `Provider` interface). The package `internal/provider`
now exports *only* adapter implementations (Anthropic Messages, OpenAI-compatible
Chat Completions, prefix-select) and the SSE helper. The neutral types must
not reappear in `internal/provider/*` after this change.

#### Scenario: Round-trip tool call across adapters
- **WHEN** the same neutral request with a tool schema is sent via either adapter
- **THEN** each adapter emits its native tool-call form and parses the response back to neutral

### Requirement: Dual first-class adapters
The system SHALL provide Anthropic Messages and OpenAI-compatible (Chat Completions)
adapters selected by the `provider/model` prefix.

**Constraint added:** Both adapters live in `internal/provider/` and depend on
`internal/ports/` for the neutral types. Adding a new backend means creating
a new sub-package of `internal/provider/` and wiring it into the prefix-select
adapter — never touching `internal/ports/` or any core package.

#### Scenario: Local model via base URL
- **WHEN** a provider sets `options.baseURL`/`options.apiKey`
- **THEN** the OpenAI-compatible adapter serves it unchanged

### Requirement: Test fake at the port boundary
The scripted `Fake` provider SHALL live at the port boundary so tests do not
import a concrete adapter package.

**Former location:** `internal/provider/fake.go`.

**New location:** `internal/ports/providerfake/fake.go` (exported as
`portsfake.Fake`). Implements the canonical `ports.Provider` interface.

#### Scenario: Test calls a portsfake.Fake without touching `internal/provider/`
- **WHEN** a test in `internal/agent`, `internal/runtime`, `internal/tui`, or
  `internal/orchestrate` constructs a scripted provider
- **THEN** it imports `portsproviderfake` (or — during the transition window —
  `internal/provider` via shim) and satisfies the port without depending on
  any adapter implementation

### Requirement: Provider-neutral core (package-level decoupling)
The core packages SHALL import `internal/ports` for any provider type or
interface. Importing `internal/provider` from core is forbidden except during
the transition window in this change; any post-merge import of `internal/provider`
from outside `internal/provider/` itself fails the gates in T-1808.

#### Scenario: No adapter packages leak past `internal/provider/` (existing)
- **WHEN** `go build ./...` runs with the migration in T-1803 complete
- **THEN** no file under `internal/agent|orchestrate|runtime|tui|improve|contextmgr|policy|tool|memory|composition|ports/**` (excluding tests that legitimately use `portsproviderfake`) imports `internal/provider`

### Requirement: opencode.json compatibility (unchanged)
Provider and model configuration SHALL be read from `opencode.json`
`provider.<id>.options.baseURL/apiKey` and `model` = `<provider>/<model>`.

#### Scenario: Prefix selects adapter
- **WHEN** `model` is `anthropic/…` vs `openai/…` vs `custom/…`
- **THEN** the matching adapter is selected by prefix with no code change

> Full requirements (foundation): `openspec/specs/provider/spec.md`.
