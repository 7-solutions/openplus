# Change 0026 — Tasks

> One task = one vertical slice = one PR. `[ ]` open · `[~]` in progress · `[x]` done.
> Gate order per openplus-build skill: Spec → Tests (red) → Implement (green) → Review → Commit.
>
> **Five milestones, each independently green.** Do not start M(n+1) until M(n)'s
> verify task passes. This change is the largest so far; the milestone boundaries are
> the mechanism that keeps it reviewable.

## M0 — Spec (Gate 1: STOP for approval)
- [x] T-2600 PLAN (`proposal.md`) + ports spec delta + TASKS + ADR-0017 drafted.
      Awaiting approval before any code.

## M1 — Port + config + fake (no I/O)
- [x] T-2601 Write `internal/ports/lsp_test.go` (red): `PortNames()` contains
      `"LanguageService"` and has length 11; `var _ LanguageService =
      FakeLanguageService{}` compiles; the fake returns its canned diagnostics.
      Update the count in `internal/ports/ports_test.go` (10→11) and the `want` map.
      Show red. **Done** — also renamed `TestAllTenPortsAreDeclared` →
      `TestAllElevenPortsAreDeclared`.
- [x] T-2602 Implement to green in `internal/ports/lsp.go`: the `LanguageService`
      interface + neutral `Diagnostic`, `Location`, `Symbol`, `Severity` types;
      appended `"LanguageService"` to `PortNames()`; declared `FakeLanguageService`
      and added its line to the compile-time assertion block; package doc updated
      ("ten seams" → "eleven seams"). Positions are 1-based (human/compiler
      convention); the adapter will convert from LSP's 0-based UTF-16 positions.
- [x] T-2603 Write `internal/config/lsp_test.go` (red), mirroring
      `internal/config/embedder_test.go`: parses an `lsp` block (`enabled`, `servers`
      map of extension → `{command, args}`); absent block is the zero value;
      `Configured()` predicate; `ServerFor()` extension routing. Show red.
- [x] T-2604 Implement the config to green in `internal/config/config.go`: `LSP` field
      on `Config`, the `LSP`/`LSPServer` structs, `Configured()` (requires **both**
      the flag and ≥1 server), `ServerFor(path)` (unknown extension → no server, so a
      zero-value entry is never spawned as an empty command), `parseLSP` + the raw
      JSON structs. **Deviation from parseMCP:** a server entry with no command is
      *dropped*, not a load error — LSP is an optional enhancement and one bad entry
      must not make the config unloadable.
- [x] T-2605 **M1 verify.** `go test ./internal/ports/... ./internal/config/...` green;
      `CGO_ENABLED=0 go build ./...` clean; full suite 26 pkgs green;
      `go.mod`/`go.sum` untouched (no dependency yet, as specced).

## M2 — LSP client adapter (`internal/lsp/`)
- [x] T-2606 `go get go.lsp.dev/jsonrpc2@v1.0.1`; `go mod tidy`. **Deviation:**
      `go.lsp.dev/protocol` was fetched but then dropped by `go mod tidy` — see
      T-2608. The change adds **one** direct dependency, not two.
      `CGO_ENABLED=0 go build ./...` clean; `TestNoBannedDirectDeps` passes.
- [x] T-2607 Write `internal/lsp/client_test.go` (red) against a **fake language
      server over an in-process `net.Pipe`** (no real gopls, no process spawn):
      `initialize`/`initialized` handshake; a pushed
      `textDocument/publishDiagnostics` notification lands in the per-path cache with
      0-based→1-based conversion and a root-relative path; hover; definition
      converting to a neutral `ports.Location`; documentSymbol mapping LSP kind 12 →
      `"func"`; idempotent `Close`. Show red.
- [x] T-2608 Implement `internal/lsp/client.go` + `internal/lsp/wire.go` to green:
      spawn (or, in tests, an injected `io.ReadWriteCloser`), handshake,
      `didOpen`/`didChange`, the four request surfaces, and the **notification
      handler** caching diagnostics per path.
      **Deviation:** the LSP wire shapes are declared locally in `wire.go` rather than
      imported from `go.lsp.dev/protocol`. We consume a deliberately small slice of
      the protocol, and local structs document exactly which fields the adapter
      depends on — `go mod tidy` then removed `protocol` and `uri` entirely, halving
      the new dependency surface. `wire.go` is the single neutrality boundary: every
      conversion to a `ports.*` type lives there.
      Judgment calls recorded: unknown/absent severity → `SeverityError` (a problem
      the agent cannot rank should be loud, not demoted); `hoverContents` accepts all
      three historical LSP hover shapes; paths crossing the port are **root-relative**
      so the model never sees the developer's absolute layout.
- [x] T-2609 Write `internal/lsp/manager_test.go` (red): port implemented at compile
      time; unknown extension is a clean no-op on every surface; **no process spawned
      before first use**; a missing binary is a named warning, not a panic; a failed
      server is **not retried** (no fork+exec storm); disabled config starts nothing
      and warns nothing; idempotent `Shutdown`. Show red.
- [x] T-2610 Implement `internal/lsp/manager.go` to green, implementing
      `ports.LanguageService`. Failure policy: every surface degrades to an empty
      result with a **nil error** — an optional enhancement must not abort a tool
      call. The extension is marked failed *before* the start attempt so one missing
      binary costs exactly one fork+exec.
- [x] T-2611 **M2 verify.** `go test ./internal/lsp/...` green (13 tests, no real
      language server spawned, 0.010s); `-race` clean; `CGO_ENABLED=0 go build ./...`
      clean; full suite 27 pkgs green; leak guard + `TestNoBannedDirectDeps` pass.

## M3 — Model-callable tools
- [x] T-2612 Write `internal/tool/lsp_test.go` (red) against `FakeLanguageService`:
      metadata (name/description/valid JSON-Schema object) for all five; rendering of
      severity + `file:line:col`; **a clean file returns an explicit "no diagnostics"
      statement, not an empty string** (an empty result reads to the model as a failed
      call); location and symbol rendering; malformed JSON and missing `path` are
      clean errors; hover/definition/references reject a missing line. Show red.
- [x] T-2613 Implement `internal/tool/lsp.go` to green — `LSPTools(ls)` plus the five
      `tool.Tool` implementations delegating to a `ports.LanguageService`. Output is
      plain `file:line:col message` text, not JSON: the model reads these the way a
      developer reads compiler output. Column defaults to 1 when omitted; a zero line
      is rejected (positions are 1-based, so 0 means absent).
- [x] T-2614 Write `internal/runtime/lsp_assemble_test.go` (red): no `lsp_*` tool with
      LSP disabled, with no lsp config at all, or in a fake session; all five
      registered **and present in `ToolSchemas`** when enabled in a real session.
      Show red.
- [x] T-2615 Implement the wiring to green in `internal/runtime/assemble.go`:
      `Session.LanguageService` field, `assembleLanguageService(opts)` returning the
      tools, called from `Assemble`; registered **only when `Configured()` and not
      `opts.Fake`**. `Session.Close` now shuts the language servers down — a leaked
      subprocess outlives the session that started it.
      Note: registration does not depend on a server actually starting (servers are
      lazy), which the test asserts by configuring a binary that does not exist.
- [x] T-2616 **M3 verify.** `go test ./internal/tool/... ./internal/runtime/...`
      green; runtime suite **0.884s** (was 0.911s — no regression);
      `CGO_ENABLED=0 go build ./...` clean; full suite 27 pkgs; guards pass.

## M4 — Auto-inject diagnostics after edits
- [x] T-2617 Write `internal/runtime/diagnostics_inject_test.go` (red), 8 cases:
      section present after an edit with position and message; **absent** with no
      LanguageService; **absent for a clean file** (silence is the right signal for
      working code — an empty heading would read as a failed lookup); capped with the
      remainder summarized as `(N more not shown)`; repeated edits of one file query
      the server **once** (dedupe); the user's `OnToolResult` render callback still
      fires; read-only tools schedule nothing; the hook is **nil** when neither a
      LanguageService nor a user callback needs it. Show red.
- [x] T-2618 Implement the async refresh to green in `internal/runtime/diagnostics.go`:
      `toolResultHook()` composes the user's render callback with the diagnostics
      trigger, filtered to `write`/`edit`/`bash`. The hook does the cheapest possible
      thing on the agent goroutine — record a path under a mutex — then fires
      `refreshDiagnosticsAsync`, which the loop never awaits.
      Two judgment calls: the refresh uses `context.Background()`, **not** the tool
      call's context, which is cancelled when the call returns and would abort the
      refresh we just started; and `bash` records no path (it has none) so it
      refreshes whatever is already pending — a build or formatter changes files we
      cannot enumerate from the call site.
- [x] T-2619 Implement the injection to green: `diagnosticsSection()` renders a
      bounded block prepended to `in.Memory` in `AssembleContext`, so `Budgeter.Fit`
      accounts for it and `renderSystem` emits it verbatim — no budgeter surgery
      needed. Diagnostics **lead** the memory pool: what the agent broke a moment ago
      outranks a retrieved chunk, and the budgeter drops from the tail. Caps:
      20 diagnostics, 300 chars per message. A file that gets fixed is deleted from
      the cache rather than reported as empty.
- [x] T-2620 **M4 verify.** `go test ./internal/runtime/...` green (8 new cases);
      **`-race` clean (2.010s)** — the async refresh is why this gate exists;
      `CGO_ENABLED=0 go build ./...` clean; full suite 27 pkgs green.

## M5 — Docs, hard rule, gates, commit
- [x] T-2621 Ship `docs/adr/0017-language-service-port.md`. Status moved
      Proposed → Accepted.
- [x] T-2622 Write the hard-rule regression guard:
      `internal/ports/lsp_leak_guard_test.go`, two tests —
      `TestPortsDeclareNoLSPWireType` (nothing in `internal/ports/` may reference
      `go.lsp.dev/`) and `TestOnlyLSPAdapterImportsLSPWire` (only `internal/lsp/` may
      import it, walking `internal/` and `cmd/`).
      **RED→GREEN proven with real violations**, not asserted: adding
      `_ "go.lsp.dev/jsonrpc2"` to `internal/ports/lsp.go` failed the first test
      naming the file; adding the same import to `internal/tool/lsp.go` failed the
      second naming that file; both reverted → green.
- [x] T-2623 Hoisted the hard rule to **both** `AGENTS.md` (canonical, full wording +
      rationale) and `CLAUDE.md` (thin mirror), each naming
      `internal/ports/lsp_leak_guard_test.go`. Generalized the wording beyond LSP:
      *"Wire neutrality at every port — no wire type crosses a seam"*, with LSP as the
      instance, since it is ADR-0005's provider rule applied to a second protocol.
      Port list ten → eleven in both files; ADR-0017 added to the index;
      CLAUDE.md's "most recent change" pointer updated to 0026.
- [x] T-2624 Updated `docs/feature-matrix.md`: LSP row now "yes opt-in", added rows
      for surface breadth and automatic diagnostics feed, port count 10 → 11, new
      wire-neutrality row, and LSP removed from the gap list (replaced by the honest
      residual gap: no auto-detect/auto-install of servers).
- [x] T-2625 **Full gates.** `go build ./...`, `CGO_ENABLED=0 go build ./...`,
      `go vet ./...` all clean; `go test -count=1 ./...` **27 pkgs green, 0 FAIL**;
      `-race` clean on lsp/runtime/ports/memory/orchestrate; all six guards pass
      (provider leak ×2, banned deps, LSP leak ×2, eleven-port count).
- [x] T-2626 Manual smoke against a **real gopls v0.23.0** (installed with
      `go install golang.org/x/tools/gopls@latest` on user instruction). Drove
      `lsp.Manager` against a scratch module containing a deliberate `undefinedFunc()`
      call. Every surface verified end to end:

      ```
      DIAG       main.go:12:2 error: undefined: undefinedFunc
      HOVER      "func Greet(name string) string\nGreet returns a greeting for name."
      DEFINITION [{Path:main.go Line:6 Column:6}]
      SYMBOLS    [{Greet func L6} {main func L10}]
      ```

      This confirms against a real server what the fake could only assert:
      Content-Length framing, the initialize handshake, **asynchronously pushed
      diagnostics**, the 0-based→1-based position conversion (the error genuinely is
      at line 12 col 2), root-relative paths, and LSP symbol-kind 12 → `"func"`.
      No warnings; clean shutdown. Elapsed 0.64s.
      The smoke test itself was scratch and is not committed — it needs a real gopls
      on PATH, which unit tests must never require.
- [x] T-2627 Commits on `feat/0026-language-service-port`. **Deviation:** three
      commits, not five. `internal/runtime/assemble.go` carries both the M3 tool
      wiring and the M4 diagnostics state, so a strict five-way split would need
      backward edits and would produce at least one intermediate commit that does not
      build. Grouped as M1+M2 (`40d753e`, port + adapter), M3+M4 (`752c361`, tools +
      injection), M5 (`1b185b3`, guard + docs). **Each was verified to build and pass
      its packages standalone** (`git stash --keep-index`) before committing, so the
      history stays bisectable. Not pushed — awaiting instruction.
- [x] T-2628 ICM `decisions-openplus` store (high) — eleventh port, the generalized
      wire-neutrality hard rule and its proven guard, the library-over-hand-rolled
      reversal and why, the `context.Background()` refresh subtlety, and the unrun
      real-server smoke.

## Notes for the implementer
- **No `go.lsp.dev` type in a port signature.** This is the change's hard rule
  (T-2622). Convert inside `internal/lsp/`.
- **Do not copy `internal/embed`'s pattern** of redeclaring its port interface.
  Implement `ports.LanguageService` directly (as `internal/memory` and
  `internal/provider/*` do).
- **The MCP transport is not a template for framing.** `internal/mcp/jsonrpc.go` is
  newline-delimited; LSP is `Content-Length`. The MCP stdio *process lifecycle*
  (spawn, pending map, demux goroutine, fail-all-waiters on exit) IS worth copying.
- **MCP ignores server-initiated notifications; LSP must not.** Diagnostics arrive
  only that way.
- **Never spawn in fake mode.** Non-negotiable — see the 0025 regression.
- **Async refresh must never be awaited by the loop.** `OnToolResult` runs on the
  agent goroutine.
- **If the diff needs a new port beyond LanguageService, STOP** — that is scope creep;
  propose it separately.

## Rollback
`git revert` the milestone commits, newest first. No schema migration, no on-disk
state. Removing the `lsp` config block makes the port and adapter inert.
