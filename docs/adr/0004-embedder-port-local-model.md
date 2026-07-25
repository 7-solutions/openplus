# ADR-0004 — Embedder port with a local embedding model

**Status:** Accepted

## Context
`vec0` stores vectors; something must produce them for every chunk on write and
every query on read. That is a new hard dependency introduced by ADR-0003.

## Decision
Define an `Embedder` port. Default adapter targets a **local** model via the
OpenAI-compatible embeddings API (Ollama `nomic-embed-text` or `bge-m3`). The
embedding dimension is pinned to the model and recorded in the DB schema.

```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dim() int
}
```

## Consequences
- (+) Memory stays fully self-hosted; no chunk text leaves the box (privacy).
- (+) Reuses the OpenAI-compatible adapter machinery (ADR-0005).
- (−) Changing embedding model = re-embed the store (dimension change is a migration).
- Optional: quantize to int8/binary in vec0 to shrink storage if it ever matters.
