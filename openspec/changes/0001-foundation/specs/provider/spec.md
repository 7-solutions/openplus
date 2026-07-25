# Provider (delta)

## ADDED Requirements

### Requirement: Neutral request/response model
The system SHALL define a single Request/Event model with neutral content Blocks that
both the Anthropic and OpenAI-compatible adapters marshal to and from.

#### Scenario: Round-trip tool call across adapters
- **WHEN** the same neutral request with a tool schema is sent via either adapter
- **THEN** each adapter emits its native tool-call form and parses the response back to neutral

### Requirement: Dual first-class adapters
The system SHALL provide Anthropic Messages and OpenAI-compatible (Chat Completions)
adapters selected by the `provider/model` prefix.

#### Scenario: Local model via base URL
- **WHEN** a provider sets `options.baseURL`/`options.apiKey`
- **THEN** the OpenAI-compatible adapter serves it unchanged

> Full requirements: `openspec/specs/provider/spec.md`.
