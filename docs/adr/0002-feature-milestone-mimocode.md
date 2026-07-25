# ADR-0002 — Feature milestone = MiMoCode's added subsystems

**Status:** Accepted

## Context
"Done" needs a concrete target. MiMoCode = OpenCode + a defined set of harness
features. Those features are near-productizations of the discipline we already
enforce by hand (spec-first, TDD, memory-on-every-change, T-### tasks, SKILL.md).

## Decision
The v1 feature milestone is parity with MiMoCode's *added* subsystems, each built
behind a port:
1. Persistent memory (project memory, notes, task progress).
2. Intelligent context management (checkpoints + reconstruction + budgeted injection).
3. Tree task tracking (T1, T1.1 …) tied to checkpoints.
4. Subagent orchestration (parallel, cancellable, background).
5. Goal / stop-condition with an independent judge model.
6. Compose mode (spec→ship phase machine) with TDD-per-task.
7. Deterministic workflows (fixed phases, bounded retries, git-worktree parallelism).
8. Skills with BM25 discovery + `/skill` invocation.
9. Self-improvement: `/dream` (traces→memory) and `/distill` (workflows→skills).

## Non-goals for v1 (defer-behind-port; see backlog triggers)
Voice input/ASR, Max Mode (best-of-N + judge), MCP plugin marketplace, web/share UI,
multi-tenant server mode. Each enters on its documented trigger only.

## Consequences
- (+) A measurable definition of done, mapped to backlog milestones M0–M9.
- (−) Context reconstruction (#2) is the deepest subsystem; budget time accordingly.
