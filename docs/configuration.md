# Configuration

OpenPlus reads two files from your project root:

- **`opencode.json`** — settings (the same surface OpenCode uses)
- **`AGENTS.md`** — project instructions, prepended to the system prompt

Both are optional. With neither, OpenPlus runs with defaults and no project context.

> `opencode.json` is strict JSON — **comments are not allowed**. The examples below
> use `//` for explanation only; strip them before use.

## Full example

```json
{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["AGENTS.md"],
  "model": "anthropic/claude-sonnet-5",

  "provider": {
    "anthropic": {
      "options": { "apiKey": "{env:ANTHROPIC_API_KEY}" }
    },
    "openai": {
      "options": { "apiKey": "{env:OPENAI_API_KEY}" }
    },
    "local": {
      "name": "Local (OpenAI-compatible)",
      "options": { "baseURL": "http://localhost:11434/v1", "apiKey": "ollama" },
      "models": {
        "qwen2.5-coder": { "name": "Qwen2.5 Coder (local)" }
      }
    }
  },

  "permission": {
    "bash": "ask",
    "write": "ask",
    "external_directory": { "/tmp/**": "ask" }
  },

  "embedder": {
    "model": "nomic-embed-text",
    "baseURL": "http://localhost:11434/v1",
    "apiKey": "{env:EMBED_KEY}"
  },

  "memory": { "path": ".openplus/memory.db", "autoOpen": true, "maxEntries": 5000 },
  "context": { "budget": 120000, "window": 200000 },

  "lsp": {
    "enabled": true,
    "servers": {
      ".go": { "command": "gopls" },
      ".ts": { "command": "typescript-language-server", "args": ["--stdio"] }
    }
  },

  "mcp": {
    "my-server": { "transport": "stdio", "command": "my-mcp-server", "args": ["--flag"] }
  },

  "max": { "samples": 3, "model": "anthropic/claude-sonnet-5" },
  "tui": { "theme": "default" },
  "coordination": { "backend": "file" }
}
```

## `{env:VAR}` expansion

Any string value may reference an environment variable. Keep secrets out of the file
and out of version control:

```json
{ "apiKey": "{env:ANTHROPIC_API_KEY}" }
```

## `model`

`"<provider>/<model>"`. The prefix selects the adapter from the `provider` table.

```json
{ "model": "anthropic/claude-sonnet-5" }
{ "model": "local/qwen2.5-coder" }
```

Override per run with `--model` or `OPENPLUS_MODEL`.

## `provider`

| Field | Meaning |
|---|---|
| `name` | Display name (optional) |
| `options.baseURL` | Endpoint. Omit for Anthropic's default; required for OpenAI-compatible servers |
| `options.apiKey` | API key, usually `{env:...}` |
| `models` | Optional per-model display names |

Anything exposing an OpenAI-compatible `/v1/chat/completions` works: Ollama, vLLM,
LM Studio, OpenRouter, llama.cpp.

## `permission`

Controls what the agent may do without asking. Values: `allow`, `ask`, `deny`.

```json
{ "permission": { "bash": "ask", "write": "ask", "external_directory": { "/tmp/**": "ask" } } }
```

Defaults are conservative. Loosen deliberately — `"bash": "allow"` lets the model run
any command it composes.

## `embedder` — enables memory

**Memory is off unless an embedder is configured.** A model name is required: it
names the vector space, and a store built against the wrong one is worse than no
store.

```json
{ "embedder": { "model": "nomic-embed-text", "baseURL": "http://localhost:11434/v1" } }
```

| Field | Meaning |
|---|---|
| `model` | Embedding model (required) |
| `baseURL` | OpenAI-compatible embeddings endpoint |
| `apiKey` | Key if the endpoint needs one |
| `timeout` | Per-call timeout; default 30s |

Env overrides: `OPENPLUS_EMBED_MODEL`, `OPENPLUS_EMBED_BASE_URL`,
`OPENPLUS_EMBED_API_KEY`.

## `memory`

| Field | Default | Meaning |
|---|---|---|
| `path` | `.openplus/memory.db` | Database location |
| `autoOpen` | `false` | Open memory at session start |
| `maxEntries` | unlimited | Prune oldest beyond this count |

Storage is a Turso vector column plus a modernc FTS5 shadow index, fused with
Reciprocal Rank Fusion. **Chunk text and embeddings never leave your machine.**

Env override: `OPENPLUS_MEMORY_PATH`.

## `context`

| Field | Meaning |
|---|---|
| `budget` | Soft token ceiling for assembled context |
| `window` | The model's context window |

The budgeter fits system prompt, task state, checkpoint, retrieved memory, and recent
messages in priority order, dropping from the tail.

## `lsp` — code intelligence

**Opt-in.** No language server starts unless `enabled` is true *and* at least one
server is declared.

```json
{
  "lsp": {
    "enabled": true,
    "servers": {
      ".go":  { "command": "gopls" },
      ".ts":  { "command": "typescript-language-server", "args": ["--stdio"] },
      ".py":  { "command": "pyright-langserver", "args": ["--stdio"] },
      ".rs":  { "command": "rust-analyzer" }
    }
  }
}
```

Keys are file extensions **including the dot**. You supply the binary — OpenPlus
never downloads a toolchain.

Behavior:

- Servers start **lazily**, on first use of a file they handle
- A missing binary is a warning; the session continues without LSP for that language
- A failed server is not retried, so a missing binary costs one process attempt
- After the agent edits a file, diagnostics are refreshed asynchronously and a
  bounded section is added to its context
- Five tools become available: `lsp_diagnostics`, `lsp_hover`, `lsp_definition`,
  `lsp_symbols`, `lsp_references`

## `mcp` — Model Context Protocol servers

Tools from each server join the registry namespaced `<server>.<tool>` and pass the
same permission gate as builtins.

```json
{
  "mcp": {
    "local-tools": { "transport": "stdio", "command": "my-mcp-server", "args": ["--stdio"] },
    "remote": {
      "transport": "http",
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer {env:MCP_TOKEN}" }
    }
  }
}
```

| Field | Applies to | Meaning |
|---|---|---|
| `transport` | both | `"stdio"` or `"http"` (required) |
| `command`, `args`, `env`, `dir` | stdio | Subprocess to run |
| `url`, `headers` | http | Endpoint and headers |

A broken server is skipped with a warning rather than failing the session.

### Default docs source

**When you declare no MCP servers at all**, OpenPlus connects
[Context7](https://context7.com) so the model can read current library
documentation. Declaring any server of your own replaces this default.

- Disable entirely: `OPENPLUS_DEFAULT_DOCS=0`
- Higher rate limits: set `CONTEXT7_API_KEY`

## `max` — best-of-N

```json
{ "max": { "samples": 3, "model": "anthropic/claude-sonnet-5" } }
```

Defaults for `/max`: how many candidates to sample and which model judges them.

## `tui`

```json
{ "tui": { "theme": "default" } }
```

`/theme` lists available themes. Themes are color-vision aware.

## `coordination`

```json
{ "coordination": { "backend": "file" } }
```

How parallel subagents coordinate exclusive access to symbols.

## CLI flags

| Flag | Meaning |
|---|---|
| `-p <prompt>` | Run one turn and exit |
| `-C <dir>` | Project root (default `.`) |
| `--model <id>` | Override the configured model |
| `--config <path>` | Path to `opencode.json` |
| `--fake` | Scripted fake provider; no API key, no network |
| `--goal <text>` | Stop when the goal is judged met |
| `--version` | Print version and exit |

## Environment variables

| Variable | Effect |
|---|---|
| `OPENPLUS_MODEL` | Override the model |
| `OPENPLUS_FAKE=1` | Force the fake provider |
| `OPENPLUS_GOAL` | Set the goal |
| `OPENPLUS_MEMORY_PATH` | Override the memory database path |
| `OPENPLUS_EMBED_MODEL` / `_BASE_URL` / `_API_KEY` | Override embedder settings |
| `OPENPLUS_DEFAULT_DOCS=0` | Disable the Context7 default |
| `CONTEXT7_API_KEY` | Context7 key for higher rate limits |
| `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` | Referenced via `{env:...}` in the config |

## `AGENTS.md`

Whatever `instructions` lists is prepended to the system prompt. Use it for the
things a newcomer would need to know: architecture, conventions, hard rules, how to
run the tests.

```json
{ "instructions": ["AGENTS.md", "docs/conventions.md"] }
```
