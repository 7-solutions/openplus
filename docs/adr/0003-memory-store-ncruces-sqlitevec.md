# ADR-0003 — Memory store: ncruces/go-sqlite3 (cgo-free) + sqlite-vec, hybrid FTS5+vec0

**Status:** Accepted — **supersedes the tentative modernc.org/sqlite choice**

## Change (decision-as-diff)
```diff
- driver: modernc.org/sqlite (pure Go, FTS5 only)
+ driver: github.com/ncruces/go-sqlite3 (cgo-free, real SQLite via wazero WASM)
+ vector: github.com/asg017/sqlite-vec-go-bindings/ncruces (sqlite-vec as WASM)
+ retrieval: HYBRID — FTS5 (lexical/BM25) + vec0 (semantic KNN), fused with RRF
```

## Context
`sqlite-vec` is a pure-C loadable extension. Its Go bindings README states the CGO
binding "will NOT work with ... `modernc.org/sqlite`". So adopting sqlite-vec forces
the driver decision. Two viable drivers:
- **cgo:** `mattn/go-sqlite3` + `sqlite-vec-go-bindings/cgo` — native speed, but C
  toolchain and painful cross-compile; breaks the pure-Go / static-binary goal.
- **cgo-free (chosen):** `ncruces/go-sqlite3` (real SQLite compiled to WASM, run on
  the pure-Go `wazero` runtime) + `sqlite-vec-go-bindings/ncruces` — single static
  binary, trivial cross-compile, and it runs *real* SQLite so **FTS5 and vec0 live
  in the same database**.

## Decision
Use ncruces/go-sqlite3 + sqlite-vec (ncruces). Store each memory chunk in both an
FTS5 table and a `vec0` table. On query, run both and fuse with Reciprocal Rank
Fusion; hand top-k to the budgeted-injection step.

## Consequences
- (+) cgo-free static binary; FTS5 + vector in one file-backed store.
- (+) Better recall than MiMoCode's FTS5-only memory.
- (−) wazero adds small startup/interpret overhead — negligible at memory scale.
- (−) sqlite-vec is pre-v1 (pin the version) and vec0 is brute-force KNN — perfect
  for project-scoped memory (thousands of chunks), not a million-vector store.
