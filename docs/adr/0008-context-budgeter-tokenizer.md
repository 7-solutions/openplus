# ADR-0008 — Context budgeter, tokenizer, and reconstruction

**Status:** Accepted

## Context
The deepest MiMoCode subsystem is context management: checkpoint when nearing the
window, then reconstruct context from checkpoint + project memory + task progress +
retained recent messages, with importance-ranked, budgeted injection.

## Decision
A `Budgeter` owns a token budget per turn and decides what enters context. Token
counts come from a `Tokenizer` port (tiktoken-go for OpenAI-family; a calibrated
heuristic for Anthropic until an exact counter is wired). A `Checkpointer` writes
structured snapshots (`checkpoint.md`) and drives reconstruction when the live
window crosses a high-water mark.

Injection order (highest priority first, truncated to budget):
system → active task + progress → reconstructed checkpoint → hybrid-retrieved memory
(ADR-0003) → retained recent messages.

## Consequences
- (+) Long-horizon sessions survive window limits without losing the current task.
- (−) Token estimates are approximate across providers; treat the budget as a
  soft ceiling with a safety margin, and add per-provider calibration tests.
