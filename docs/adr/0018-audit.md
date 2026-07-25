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

## T-1801: catalogue of import sites (as-shipped, verified post-migration)

Re-verified against the tree after commit `7b91e1a` landed the migration.
The pre-migration "migration list" below is retained for history; the
**as-shipped** state follows.

### Pre-migration migration list (what T-1803 rewrote — historical)

These files imported `internal/provider` before 0018. T-1803 rewrote every
one to `internal/ports` (and `provider.Fake` → `portsfake.Fake`). No logic
changed.

- Group A — `*_test.go` using `provider.Fake`: `agent/loop_test.go`,
  `improve/dream_test.go`, `orchestrate/{goal,maxmode}_test.go`,
  `runtime/{accumulate,commands_behavior,compact,integration,max_cmd}_test.go`.
- Group B — core packages, neutral types only: `agent/loop.go`,
  `contextmgr/{budget,checkpoint,tokenizer}.go`, `improve/dream.go`,
  `orchestrate/{goal,maxmode}.go`, `policy/policy.go`,
  `runtime/{commands_builtin,fanout,turn,workflow}.go`, `tui/{model,prompt}.go`.
- Group C — `*_test.go` using neutral types only (full list in git history).
- Group D — `cmd/openplus/main.go` and `internal/ports/{ports,ports_test}.go`.

### As-shipped verification (2026-07-26)

Greps re-run against the current tree:

1. **Bare `internal/provider` import outside `internal/provider/`** —
   `grep -rn '".../internal/provider"' --include=*.go .` excluding
   `internal/provider/` returns **zero Go imports**. The only hit is
   `internal/ports/leak_guard_test.go`, and that is a *string literal*
   the guard matches against, not an import. ✓
2. **51 files** import `internal/ports` (was 0 pre-migration). Sampled
   Group B files (`agent/loop.go`, `contextmgr/budget.go`,
   `orchestrate/goal.go`, `policy/policy.go`, `runtime/turn.go`,
   `tui/model.go`): each imports `ports` once, `provider` zero times. ✓
3. **Group A tests** now use `portsfake ".../internal/ports/providerfake"`
   and `&portsfake.Fake{...}` — confirmed in `agent/loop_test.go`,
   `runtime/accumulate_test.go`, `orchestrate/goal_test.go`. ✓
4. **`cmd/openplus/main.go`** — zero `internal/provider` imports (only
   comments and flag help text mention "provider"). Wiring delegated to
   `runtime`. ✓

### Sanctioned adapter-surface imports (NOT leaks)

The leak_guard regression (T-1808) bans the **bare** `internal/provider`
package from `internal/` and `cmd/`; it permits two narrower surfaces,
both present and intentional:

- `internal/runtime/assemble.go:30` imports
  `selectadapter ".../internal/provider/select"` and calls
  `selectadapter.Select(model, cfg)` at line 391. `select` is the **adapter
  registry** — the composition seam, not a concrete backend. This is the
  sanctioned way core obtains a `ports.Provider` from config.
- `internal/runtime/assemble_test.go:16-17` imports the leaf adapters
  `provider/anthropic` and `provider/openaicompat` and type-asserts
  `*anthropic.Adapter` / `*openaicompat.Adapter` (lines 55, 73, 153) to
  prove `Select` routes to the right backend. This is **test-only wiring
  verification**; it does not construct adapters in production core paths.
  The guard does not flag subpath imports in `internal/`, so this is
  allowed by design.

No **non-test** core file constructs a concrete adapter type
(`Anthropic`, `OpenAICompat`). The 0006 self-check item-3 conclusion
holds at the type level; 0018 closes it at the package level for the
bare adapter import.

### Layout (as-shipped)

- `internal/ports/`: `ports.go` (port interfaces incl. `Provider`),
  `model.go` (all neutral types: `Block`, `BlockKind`, `Role`, `Message`,
  `ToolSchema`, `Request`, `EventKind`, `ToolCall`, `Usage`, `Event`),
  `ports_test.go`, `leak_guard_test.go` (T-1808),
  `providerfake/fake.go` (`portsfake.Fake`).
- `internal/provider/`: `sse.go` (adapter-only SSE helper) + the three
  adapter subpackages `anthropic/`, `openaicompat/`, `select/` +
  `contract_test.go`. `types.go`, `fake.go`, and the T-1803 transition
  shim are **deleted** (T-1807 done — `test -f internal/provider/provider.go`
  → no shim).

## Closed: T-1801 ✓, T-1802 ✓

Both re-verified against the shipped tree on 2026-07-26. The migration
landed in commit `7b91e1a`; the leak guard (`internal/ports/leak_guard_test.go`)
enforces the package boundary going forward.
