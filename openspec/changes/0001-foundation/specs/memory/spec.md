# Memory (delta)

## ADDED Requirements

### Requirement: Hybrid store
The system SHALL persist each chunk in FTS5 and vec0 in one ncruces/go-sqlite3
database and retrieve via RRF-fused FTS5 + vec0.

#### Scenario: Semantic recall on paraphrase
- **WHEN** a query paraphrases a stored decision with no shared keywords
- **THEN** vec0 surfaces it and RRF ranks it into the top-k

### Requirement: Local embedding
Embeddings SHALL come from the Embedder port using a local model; chunk text MUST NOT
leave the host by default.

#### Scenario: Dimension pinned to model
- **WHEN** the store initializes
- **THEN** the vec0 dimension equals Embedder.Dim() and is recorded in schema

> Full requirements: `openspec/specs/memory/spec.md`.
