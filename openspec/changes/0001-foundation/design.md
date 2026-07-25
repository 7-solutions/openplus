# Change 0001 — Design

## Module layout
```
cmd/openplus/                 # entrypoint, wires ports -> adapters
internal/agent/           # the loop (ADR-0001)
internal/provider/        # Provider port
  anthropic/              #   Messages adapter (ADR-0005)
  openaicompat/           #   Chat Completions adapter (ADR-0005)
internal/tool/            # Tool port + builtins: read, write, edit, bash, glob, grep
internal/policy/          # PolicyGate: allow/ask/deny + ES256 verify (ADR-0007)
internal/memory/          # MemoryStore (ncruces+sqlite-vec), FTS5+vec0, RRF (ADR-0003)
internal/embed/           # Embedder port + local adapter (ADR-0004)
internal/context/         # Tokenizer, Budgeter, Checkpointer (ADR-0008)
internal/skills/          # SkillIndex: discovery + BM25 (ADR-0002)
internal/compose/         # spec->ship phase machine (ADR-0002)
internal/orchestrate/     # subagents, workflows, goal-judge, task tree (ADR-0006)
internal/config/          # opencode.json + AGENTS.md loader (ADR-0001)
internal/tui/             # Bubble Tea front-end (Crush base)
```

## Ports (the seams)
Provider · Embedder · MemoryStore · Tool · SkillIndex · Tokenizer · Budgeter ·
Checkpointer · PolicyGate · Workflow. The core depends only on these; every external
system is an adapter. New capability = new adapter, not a refactor.

## The loop (heart)
```go
for {
    resp := provider.Stream(ctx, req)         // ADR-0005
    history = append(history, resp.Assistant())
    calls := resp.ToolCalls()
    if len(calls) == 0 { return }             // done (or goal-judge, ADR-0006)
    for _, c := range calls {
        if !policy.Permit(ctx, c) { history = append(history, denied(c)); continue }
        history = append(history, tool.Run(ctx, c))
    }
    ctx = budgeter.Fit(ctx, history)          // reconstruct/inject, ADR-0008
}
```

## Test strategy
Contract tests per provider adapter (recorded fixtures for Anthropic + one
OpenAI-compatible target). Memory: golden RRF ranking tests. Budgeter: per-provider
token-estimate calibration tests. All red-first per house TDD.
