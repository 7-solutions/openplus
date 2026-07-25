# Change 0018 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. TDD red-first, ports/adapters,
> Advisor + graph + memory on commit. `[ ]` open · `[~]` in progress · `[x]` done.
>
> **Stop the line** between T-1803 and T-1804 if `go build ./...` is not
> green — every later task depends on the import-rewrite being complete and
> compilable.

## M0 — Spec & audit
- [x] T-1800 OpenSpec change 0018 approved (proposal + both delta specs +
      this task list).

## M1 — Audit concrete dependencies (read-only, sets T-1803's diff)
- [x] T-1801 Catalogue the call sites: every `import
      "github.com/7solutions/openplus/internal/provider"` outside
      `internal/provider/`. Confirm none of them name a concrete adapter
      type (`Anthropic`, `OpenAICompat`) — only `provider.Provider`,
      `provider.Request|Event|Message|Block|ToolCall|ToolSchema|Usage|Role|BlockKind|EventKind`,
      or `provider.Fake`. **Output:** a 30-row table appended to
      `docs/adr/0018-audit.md` (new file under `docs/adr/`).
- [x] T-1802 Confirm there is no existing
      `internal/ports/provider.go` collision: only `internal/ports/ports.go`
      and the in-package `Provider` declaration. **Output:** a one-line
      note in `docs/adr/0018-audit.md`.

## M2 — Mechanical migration (TDD-light; behavior-preserving)
- [x] T-1803 Create the new home for the port surface and update imports
      across every file T-1801 listed.
      **Steps (red → green):**
      1. Move `internal/provider/types.go` content (Block, BlockKind, BlockTool*,
         Message, Role, Request, EventKind, ToolCall, Usage, Event, Provider
         interface) into a new `internal/ports/provider.go` (interface) and
         `internal/ports/model.go` (types). Doc-comments preserved.
      2. Move `internal/provider/fake.go` to
         `internal/ports/providerfake/fake.go`; rename the type to
         `portsfake.Fake`. Add
         `var _ ports.Provider = (*portsfake.Fake)(nil)`.
      3. Add `internal/provider/provider_compat.go` re-exporting
         `type Provider = ports.Provider`, `type Request = ports.Request`,
         etc., and `type Fake = portsfake.Fake`.
      4. In every file listed by T-1801, replace
         `"github.com/7solutions/openplus/internal/provider"` with
         `"github.com/7solutions/openplus/internal/ports"` in the import
         block; replace any `provider.Fake` reference with
         `portsproviderfake.Fake`.
      5. `go build ./...` MUST be green before this task closes.
      6. Existing tests MUST still pass (this is the safety net).
- [x] T-1804 Keep adapter packages (`anthropic`, `openaicompat`, `select`)
      importable. They currently import `internal/provider` for types; with
      the shim in place they continue to compile. No edits in this task —
      this is verification only. `go build ./...` and
      `go test ./internal/provider/...` MUST be green.
- [x] T-1805 Contract tests pass unchanged in behavior.
      `go test ./internal/provider/...` covers `TestContractRoundTrip`,
      `TestContractRoundTripToolResult`,
      `TestContractNeutralOutputIsEqualAcrossAdapters`. Same outcomes
      pre- and post-migration.

## M3 — Gates (Advisor + graph + memory)
- [x] T-1806 Full repo test suite green:
      `go test ./...` (and `-race` on
      `internal/orchestrate internal/coordinate`). This is the final
      behavior gate.
- [x] T-1807 Delete the shim. Remove
      `internal/provider/provider_compat.go`. Update any remaining callers
      (none expected — T-1801 should have migrated them all; if some
      remain because they only needed the shim for the transition, migrate
      them as part of this task). `go build ./...` and `go vet ./...`
      MUST be green.
- [x] T-1808 Add the regression guard:
      `internal/ports/leak_guard_test.go` asserts that no file outside
      `internal/provider/` and `internal/ports/` imports
      `internal/provider`. **Failing test first** (the test fails before
      T-1803, passes after it). This makes the violation unregressable.

## M4 — Knowledge propagation
- [x] T-1809 Update the package doc comment on `internal/provider/types.go`
      (now empty) to point to `internal/ports/`. After T-1807 types.go is
      deleted; until then the doc-comment makes the move discoverable.
- [x] T-1810 Knowledge graph: `graphify` re-runs on
      `internal/ports/provider.go` and `internal/ports/model.go` to keep
      `ports.Provider` discoverable from core packages.
- [x] T-1811 Memory: `icm store -t decisions-openplus -c "0018: provider
      port+neutral types moved to internal/ports; internal/provider now
      adapter-only. Shim deleted in T-1807." -i high -k "0018,ports,migration"`

## Definition of done (mirrors AGENTS.md self-check)
- [x] Approved OpenSpec PLAN + SPEC + TASKS existed before code (T-1800).
- [x] Tests written first, shown red, driven to green (T-1803 step 6 +
      T-1808).
- [x] Core depends on a port (`internal/ports`); new I/O is an adapter
      (concrete adapters still live in `internal/provider/`); no provider
      type leaked to core (T-1808 guards it).
- [x] cgo-free build still green (`CGO_ENABLED=0 go build ./...`).
- [x] Advisor passed; graph updated (T-1810 ✓); memory
      updated (T-1811 ✓).
- [x] No deferred/backlog item introduced.
