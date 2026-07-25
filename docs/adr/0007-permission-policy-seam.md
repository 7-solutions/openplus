# ADR-0007 — Permission gate as a control-plane policy seam (ES256 claims)

**Status:** Accepted

## Context
Every tool call — especially `bash` and file writes — passes through a permission
layer (MiMoCode: allow/ask/deny rules, `external_directory` prompts,
`--dangerously-skip-permissions`). This is also the natural seam to hand policy to a
control plane, matching the data-plane/control-plane split used elsewhere (gitd/gitfrok).

## Decision
The permission layer is middleware around `Tool.Execute`. Local mode uses
allow/ask/deny rules (last-matching-rule-wins) compatible with `opencode.json`
`permission`. Server mode: a Node control plane mints a **short-lived ES256 claim**
scoping which repos/paths/tools the agent may touch; the Go agent **verifies and
enforces** — it never decides policy.

```go
type PolicyGate interface { Permit(ctx, ToolCall) (Decision, error) } // allow|ask|deny
```

## Consequences
- (+) Same policy model local and hosted; the agent proves it is allowed, it doesn't decide.
- (+) `deny` always blocks; forced-ask on destructive ops auto-rejects after timeout.
- (−) Server mode requires a JWKS/rotation story (deferred until hosted mode exists).
