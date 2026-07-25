# ADR-0005 — Provider port: neutral domain model + Anthropic and OpenAI-compatible adapters

**Status:** Accepted

## Context
The feature set is the milestone, but the model layer must speak **both** the
Anthropic Messages API and the OpenAI-compatible endpoint as first-class citizens.
The two wire protocols disagree exactly where an agent harness lives: tool calling
and streaming. A pure-Go rebuild also means we replace the AI SDK OpenCode/MiMoCode
lean on — this adapter is ours to own.

## Decision
One provider-neutral domain model behind a `Provider` port; one adapter per wire
format. The loop, tools, memory, and compose never learn which provider is live.
The `provider/model` string prefix selects the adapter (same convention as
`opencode.json`).

```go
type Provider interface { Stream(ctx, Request) (<-chan Event, error) }
// Request.Messages carry neutral Blocks: Text | ToolCall | ToolResult | Thinking | Image
// Event.Kind: TextDelta | ToolCallStart | ToolArgsDelta | TurnEnd | Usage
```

Adapters:
- **anthropic** — `tool_use`/`tool_result` blocks, top-level `system`, `input_schema`,
  `message_*`/`content_block_*` SSE, `cache_control`, thinking.
- **openaicompat** — `tool_calls[]` with stringified `function.arguments`,
  `role:"tool"` results, system message, `chat.completion.chunk` SSE, `[DONE]`.

## Decision detail
Target **Chat Completions** for the OpenAI-compatible adapter (not Responses). One
adapter then unlocks OpenAI + Ollama + vLLM + LM Studio + OpenRouter + Groq + MiMo's
hosted endpoint. Add a native Responses adapter later only for OpenAI-specific features.

## Consequences
- (+) Highest-leverage adapter (openaicompat) covers all local/self-hosted models.
- (+) Config `provider.<id>.options.baseURL/apiKey` maps straight from opencode.json.
- (−) Both API surfaces move; pin adapters against current docs and add contract tests.
