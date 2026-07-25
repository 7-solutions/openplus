# Change 0002 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## R0 — Composition root
- [x] T-100 `runtime.Assemble(root, Options)` → `*Session`: config + instructions,
      provider adapter by prefix, tool registry, policy gate from config
      permissions. Clear error when a credential is missing.
      Done — c6addf4 (config) + 79ed516 (assemble.go).
- [x] T-101 Optional subsystems: memory store + embedder when configured, skill
      index from the standard scan order, tokenizer/budgeter from the model.
      Done — 79ed516.

## R1 — Per-turn context
- [x] T-110 `Session.AssembleContext(userMsg)`: retrieve memory, auto-load
      skills, budget in ADR-0008 order, return the system prompt for the turn.
      Done — 79ed516.
- [x] T-111 `Session.Run`: drive the agent loop with the assembled context and
      persist the turn to memory when a store is configured.
      Done — 79ed516.

## R2 — Entrypoint
- [x] T-120 `cmd/openplus`: build the session from the runtime; `--fake` keeps
      the offline smoke path. TUI and non-TTY paths both use it.
      Done — 4ea958c.

## Verification (2026-07-25)
- [x] `go build ./...` clean.
- [x] `go test ./...` 20/20 green, including 9 tests in `internal/runtime/assemble_test.go`
      and 7 tests in `internal/runtime/turn_test.go`.
- [x] End-to-end smoke: `go run ./cmd/openplus --fake -C $(mktemp -d) -p 'say hello'`
      prints the scripted fake reply.

## Out of scope (needs its own change)
compose/`/dream`/`/distill` command surfaces · orchestration wiring · new ports
or adapters · anything on the v1 refuse list.
