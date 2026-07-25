# Provider Specification

## Purpose
Provider-neutral model layer with first-class Anthropic and OpenAI-compatible adapters.
Governed by ADR-0005.

## Requirements

### Requirement: Neutral request/response model
The system SHALL define a single `Request`/`Event` model with neutral content Blocks
(Text, ToolCall, ToolResult, Thinking, Image) that both adapters marshal to and from.

#### Scenario: Round-trip tool call across adapters
- **WHEN** the same neutral `Request` with a tool schema is sent via either adapter
- **THEN** the adapter emits the provider's native tool-call representation
- **AND** parses the provider's tool-call response back into a neutral `ToolCall`

### Requirement: Anthropic Messages adapter
The adapter SHALL map neutral blocks to `tool_use`/`tool_result` content blocks, a
top-level `system`, `input_schema` tool definitions, and parse `message_*` /
`content_block_*` SSE events including `input_json_delta`.

#### Scenario: Streaming tool arguments
- **WHEN** Anthropic streams `input_json_delta` fragments
- **THEN** the adapter accumulates them into a complete JSON tool input

### Requirement: OpenAI-compatible adapter (Chat Completions)
The adapter SHALL target Chat Completions, map neutral blocks to `tool_calls[]` with
stringified `function.arguments` and `role:"tool"` results, and parse
`chat.completion.chunk` deltas terminating on `[DONE]`.

#### Scenario: Local model via base URL
- **WHEN** a provider is configured with `options.baseURL` and `options.apiKey`
- **THEN** the same adapter serves Ollama / vLLM / LM Studio / OpenRouter / MiMo endpoints

### Requirement: opencode.json compatibility
Provider and model configuration SHALL be read from `opencode.json`
`provider.<id>.options.baseURL/apiKey` and `model` = `<provider>/<model>`.

#### Scenario: Prefix selects adapter
- **WHEN** `model` is `anthropic/…` vs `openai/…` vs `custom/…`
- **THEN** the matching adapter is selected by prefix with no code change
