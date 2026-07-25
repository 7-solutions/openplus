# Change 0001 — Tasks (BACKLOG)

> One task = one vertical slice = one PR. TDD red-first, ports/adapters, Advisor +
> graph + memory on commit. `[ ]` open · `[~]` in progress · `[x]` done.

## M0 — Foundation
- [x] T-001 Scaffold module `github.com/7solutions/openplus`, `cmd/openplus`, CI, lint.
- [x] T-002 Config loader: parse `opencode.json` (provider/model/permission).
- [ ] T-003 `AGENTS.md` loader + project-context assembly.
- [ ] T-004 Define all ten ports as interfaces with no-op fakes for tests.

## M1 — Provider core (ADR-0005)
- [~] T-010 Neutral Request/Event/Block model — **scaffolded**: `internal/provider/types.go`.
      Round-trip JSON tests still open (adapters don't exist yet to round-trip against).
- [~] T-011 SSE reader — **scaffolded**: `internal/provider/sse.go` (stdlib `bufio`, generic
      frame decode) + `sse_test.go`. Adapter-specific event parsing (content_block_delta,
      chat.completion.chunk) is still open.
- [x] T-012 Anthropic Messages adapter: blocks, tools, streaming, thinking.
- [x] T-013 OpenAI-compatible adapter (Chat Completions): tool_calls, chunk SSE.
- [x] T-014 Prefix-based adapter selection from `provider/model`.
- [x] T-015 Contract tests: same neutral request across both adapters.
- [x] T-016 (added) `provider.Fake` — deterministic scripted provider for testing the
      loop with zero network/deps. `internal/provider/fake.go`.

## M2 — Agent loop + tools + policy (ADR-0001, ADR-0007)
- [x] T-020 Tool port + builtins: read, write, edit/str_replace, glob, grep.
- [x] T-021 `bash` tool with streamed output + cancellation.
- [x] T-022 PolicyGate — glob rules, last-match-wins, Ask via Prompter (ctx timeout), safe deny default.
- [x] T-023 Agent loop with tool-use iteration + streaming — **scaffolded**:
      `internal/agent/loop.go`, proven by `loop_test.go` (tool-use-then-finish,
      denied-call-does-not-execute). Runnable smoke test: `cmd/openplus/main.go`.
- [x] T-024 `--dangerously-skip-permissions` (allow-all base, explicit rules win).

> **Scaffold status (verified 2026-07-25):** T-010/T-011/T-016/T-020/T-022/T-023 have
> real, stdlib-only Go source under `cmd/` and `internal/`. Build green on Go 1.26.5:
> `go build ./...` clean, `go test ./...` 3 passed (5 packages), `go run ./cmd/openplus`
> smoke test passes (echo tool loop: assistant→toolcall→result→done).

## M3 — TUI (Crush base, ADR-0001)
- [x] T-030 Bubble Tea shell: input, streaming output, tool-event view.
- [x] T-031 Diff view for edits; permission prompt component.

## M4 — Memory (ADR-0003, ADR-0004)
- [x] T-040 ncruces/go-sqlite3 wiring + sqlite-vec (ncruces) load; `vec_version()` test.
- [x] T-041 Embedder port + local OpenAI-compatible adapter; dim pinning.
- [x] T-042 Hybrid store: FTS5 + vec0 tables; chunk + embed on write.
- [x] T-043 RRF fusion + top-k retrieval; golden ranking tests.
- [x] T-044 MEMORY.md / notes.md / tasks/<id>/progress.md read-write + resume inject.

## M5 — Skills (ADR-0002)
- [x] T-050 SkillIndex discovery + override scan order.
- [x] T-051 BM25 ranking + auto-load threshold; `/skill` explicit invocation.

## M6 — Context management (ADR-0008)
- [x] T-060 Tokenizer port + per-family calibrated heuristics + calibration tests.
      **Note:** tiktoken-go is deferred behind the port — it fetches its BPE table
      from `openaipublic.blob.core.windows.net` at first use, which breaks the
      local-first guarantee and offline token counting. An exact counter (embedded
      BPE, or a provider count endpoint) drops in behind `Tokenizer` with no
      caller changes.
- [x] T-061 Budgeter: priority-ordered injection to budget.
- [x] T-062 Checkpointer: write checkpoint.md; reconstruct on high-water mark.
- [x] T-063 Task tree (T1/T1.1) persisted + restored across checkpoints.

## M7 — Orchestration (ADR-0006)
- [x] T-070 Subagent runner: parallel, cancellable, git-worktree isolation.
- [x] T-071 Go-native Workflow engine: phases + bounded retries + hand-off.
- [x] T-072 Goal/stop-condition with independent judge model.

## M8 — Compose (ADR-0002)
- [x] T-080 Compose phase machine: grill→spec→implement→verify→review→finish.
- [x] T-081 TDD-per-task gate + Advisor review gate wired into review phase.

## M9 — Self-improvement
- [ ] T-090 `/dream`: traces→memory extraction + stale-entry pruning.
- [ ] T-091 `/distill`: repeated-workflow mining → skill/subagent/command scaffolds.

## Deferred (add-on-trigger only — do not build)
voice/ASR · Max Mode · MCP marketplace · web/share UI · hosted server mode · goja JS workflows.
