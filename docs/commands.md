# Commands

Slash commands run locally. They cost no model round-trip unless the command itself
asks the model to do something.

`/help` prints the live list — it is generated from the dispatch table, so it is
never out of date.

## Reference

### Docs

| Command | Does |
|---|---|
| `/docs <library> [query]` | Fetch current documentation for a library via Context7 |

```
/docs react hooks
/docs turso vector search
```

Resolves the library id, then queries its docs. Requires the Context7 MCP server,
which is connected by default when you declare no MCP servers of your own. Direct
user action, so it is not gated — but the model calling `context7.*` tools on its
own still is.

### Skills

| Command | Does |
|---|---|
| `/skills` | List discoverable skills |
| `/skill <name>` | Load a skill by name |

Skills are markdown instructions, each at `<dir>/<name>/SKILL.md`. Discovery scans,
lowest priority first, so a project skill overrides a personal one of the same name:

1. `~/.claude/skills/`
2. `<project>/.opencode/skills/`
3. `<project>/.claude/skills/`

Relevant skills are also auto-loaded into context by BM25 ranking, without being
asked for.

### Compose — the spec-to-ship pipeline

A phase machine that walks a feature from idea to merged.

| Command | Does |
|---|---|
| `/compose <feature>` | Start a compose session |
| `/grill <notes>` | Record interrogation notes, advance to spec |
| `/spec <body>` | Write the spec document |
| `/approve-spec` | Approve it (the gate before code) |
| `/task <id> <title>` | Add an implement-phase task |
| `/red <id>` | Record a failing test for a task |
| `/green <id>` | Record the production code, mark the task green |
| `/verify` | Record the verify-phase pass |
| `/advisor` | Record that the Advisor review ran |
| `/finding <id> <detail>` | Record an Advisor finding |
| `/resolve <id>` | Resolve a finding |
| `/advance` | Advance to the next phase |
| `/phase` | Report the current phase |

The order is enforced: you cannot reach `/green` without a `/red` first. That is the
point — it encodes the TDD gate rather than trusting anyone to remember it.

### Orchestration

| Command | Does |
|---|---|
| `/subagents [--coordinated] <prompt>[#sym] \| ...` | Run prompts as parallel subagents in isolated git worktrees |
| `/workflows` | List registered workflows |
| `/workflow <name>` | Run a registered workflow |
| `/workflow load <path>` | Load and run a `.js` workflow file |
| `/max [n] <prompt>` | Sample n candidates, judge them, answer once |

```
/subagents add retry to the http client | write its tests
/subagents --coordinated refactor auth#Login | update callers#Login
/max 5 design the cache eviction policy
```

Each subagent gets its own git worktree, so parallel edits cannot collide. With
`--coordinated`, `#symbol` suffixes take exclusive locks on those symbols.

### Code intelligence

Available when `lsp.enabled` is configured. These are model-callable tools rather
than slash commands — the agent invokes them itself:

| Tool | Does |
|---|---|
| `lsp_diagnostics` | Compiler and linter problems in a file |
| `lsp_hover` | Signature and docs for the symbol at a position |
| `lsp_definition` | Where a symbol is defined |
| `lsp_symbols` | Declarations in a file, with line numbers |
| `lsp_references` | Every use of a symbol |

Diagnostics are also injected automatically after the agent edits a file, so it sees
its own breakage without asking.

### Self-improvement

| Command | Does |
|---|---|
| `/dream` | Mine this session's trace for durable facts, write them to memory |
| `/distill [name]` | Find repeated tool sequences, package one as a skill/subagent/command |

`/dream` is for knowledge; `/distill` is for procedure. Run `/dream` at the end of a
session that taught you something non-obvious, and `/distill` when you notice
yourself doing the same five steps for the third time.

### Appearance

| Command | Does |
|---|---|
| `/theme` | List available themes |
| `/theme <name>` | Switch theme (persisted to `opencode.json`) |

Themes are designed for color-vision accessibility; the palette does not rely on
red/green distinction alone.

### Meta

| Command | Does |
|---|---|
| `/help` | List every available command |

## Builtin tools

The model calls these directly, subject to the permission gate:

| Tool | Does |
|---|---|
| `read` | Read a file |
| `write` | Write a file |
| `edit` | Replace a string in a file, returns a diff |
| `bash` | Run a shell command |
| `glob` | Find files by pattern (`**` supported) |
| `grep` | Search file contents |

MCP tools join this set namespaced `<server>.<tool>`. LSP tools appear as
`lsp_*` when configured.

## Extending

**Skills** are the supported extension point: add `<name>/SKILL.md` under
`.opencode/skills/` or `.claude/skills/` and it becomes loadable with `/skill <name>`
and eligible for auto-loading.

**Custom slash commands are not loadable yet.** `/distill` can scaffold one into
`.opencode/command/`, but nothing reads that directory back as a command at runtime —
the dispatch table is compiled in. Scaffolded commands serve as documentation for now.
