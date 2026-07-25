# ADR-0018 — Audit snapshot (T-1801, T-1802)

Backs `openspec/changes/0018-provider-port-extraction`. Captures the
pre-migration state of every import of `internal/provider` and confirms
no name collisions for the moved types.

## T-1802: name-collision check

| Type        | Declared in `internal/ports/` | Declared in `internal/provider/` | Collision? |
|-------------|-------------------------------|-----------------------------------|------------|
| Provider    | yes (`ports.go:32`)            | yes (`types.go:113`)              | yes (mirror) |
| Request     | no                            | yes (`types.go:65`)               | no         |
| Event       | no                            | yes (`types.go:103`)              | no         |
| Message     | no                            | yes (`types.go:50`)               | no         |
| Block       | no                            | yes (`types.go:20`)               | no         |
| BlockKind   | no                            | yes (`types.go:9`)                | no         |
| Role        | no                            | yes (`types.go:42`)               | no         |
| ToolSchema  | no                            | yes (`types.go:58`)               | no         |
| ToolCall    | no                            | yes (`types.go:87`)               | no         |
| Usage       | no                            | yes (`types.go:94`)               | no         |
| EventKind   | no                            | yes (`types.go:74`)               | no         |

The only mirrored declaration is `Provider` — which is intentional today
(ports declare what they import; provider re-declares the surface). After
0018, the canonical home is `internal/ports`; the provider-package copy
survives only via the shim deleted at T-1807.

## T-1801: catalogue of import sites

### Files inside `internal/provider/` (leave alone until shim is removed)

These import siblings and `types.go` — they will be re-pointed at
`internal/ports` during T-1803 alongside the shim, but they are not
"core per AGENTS.md":

- `internal/provider/anthropic/anthropic.go`
- `internal/provider/anthropic/anthropic_test.go`
- `internal/provider/openaicompat/openaicompat.go`
- `internal/provider/openaicompat/openaicompat_test.go`
- `internal/provider/select/select.go`
- `internal/provider/select/select_test.go`
- `internal/provider/contract_test.go`
- `internal/provider/types.go` (self; moves out)

### Files outside `internal/provider/` that import `internal/provider` (the migration list)

Classified by what they actually use. **No file in this list constructs a
concrete adapter** (`Anthropic`, `OpenAICompat`, etc.). All use is either
the `Provider` interface, neutral types, or `provider.Fake` (test seam).

#### Group A — `*_test.go` files using `provider.Fake` (need portsfake)

- `internal/agent/loop_test.go`
- `internal/improve/dream_test.go`
- `internal/orchestrate/goal_test.go`
- `internal/orchestrate/maxmode_test.go`
- `internal/runtime/accumulate_test.go`
- `internal/runtime/commands_behavior_test.go`
- `internal/runtime/compact_test.go`
- `internal/runtime/integration_test.go`
- `internal/runtime/max_cmd_test.go`

#### Group B — core packages depending on neutral types only

- `internal/agent/loop.go`
- `internal/contextmgr/budget.go`
- `internal/contextmgr/checkpoint.go`
- `internal/contextmgr/tokenizer.go`
- `internal/improve/dream.go`
- `internal/orchestrate/goal.go`
- `internal/orchestrate/maxmode.go`
- `internal/policy/policy.go`
- `internal/runtime/assemble.go`
- `internal/runtime/commands_builtin.go`
- `internal/runtime/fanout.go`
- `internal/runtime/turn.go`
- `internal/runtime/workflow.go`
- `internal/tui/model.go`
- `internal/tui/prompt.go`

#### Group C — `*_test.go` files using neutral types only

- `internal/contextmgr/budget_test.go`
- `internal/contextmgr/checkpoint_test.go`
- `internal/contextmgr/tokenizer_test.go`
- `internal/orchestrate/goal_test.go`  (also in Group A; overlaps)
- `internal/policy/rules_test.go`
- `internal/policy/skip_test.go`
- `internal/runtime/assemble_test.go`
- `internal/runtime/checkpoint_test.go`
- `internal/runtime/checkpoint_write_test.go`
- `internal/runtime/command_test.go`
- `internal/runtime/fanout_test.go`
- `internal/runtime/mcp_test.go`
- `internal/runtime/turn_test.go`
- `internal/runtime/workflow_test.go`
- `internal/tui/dispatch_test.go`
- `internal/tui/model_test.go`
- `internal/tui/prompt_test.go`
- `internal/tui/runner_test.go`
- `internal/tui/theme_test.go`

#### Group D — `cmd/` and the ports package itself

- `cmd/openplus/main.go` — neutral types + adapter wiring (still allowed in
  `cmd/` until T-1807; should also move)
- `internal/ports/ports.go` — declares the `Provider` interface and re-exports
  `provider.Request` etc. into port-test fakes (the mirroring comment)
- `internal/ports/ports_test.go` — neutral types in tests

### Tally

- 1 file: `cmd/openplus/main.go` (production wiring)
- 14 files: core packages (Group B)
- ~20 files: tests (Groups A and C)
- 8 files: `internal/provider/*` siblings (Group B's adapter side)
- 2 files: `internal/ports/*` (the package itself; will become the canonical home)

**Total: ~45 files touched.** This matches the proposal's "~30+ import
rewrite surface". Every rewrite is a one-line import-path swap; no logic
changes. Group A additionally needs `provider.Fake` →
`portsproviderfake.Fake`.

## Confirmation: no concrete adapter leaks

`grep -rn "Anthropic\\.Adapter\\|OpenAICompat\\.Adapter\\|provider/anthropic\\.\\|provider/openaicompat\\." --include="*.go"` outside `internal/provider/` returns **zero matches**. The 0006 self-check item
3 conclusion holds: only the package boundary remains coupled, which is
what T-1803 closes.

## What this audit rules out

- **No new port is being introduced.** This change is a pure packaging move
  of an existing port.
- **No call-site refactor is required.** Only the import path changes.
- **`provider.Fake`'s name is unchanged.** Only its package changes.

## What this audit confirms as required

- **A back-compat shim during migration.** Without it, every Group-A test
  would need its `provider.Fake` reference rewritten in the same commit as
  the type's package moves. The shim (T-1803 step 3) allows the rewrite
  to be sequenced: types move first, shim holds, callers migrate
  incrementally, then shim deletes at T-1807. This is the change
  described in the proposal (under "Compatibility").

## Closed: T-1801 ✓, T-1802 ✓
