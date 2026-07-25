# Memory Specification

## Purpose
Persistent, self-hosted memory with hybrid lexical + semantic retrieval and budgeted
context injection. Governed by ADR-0003, ADR-0004, ADR-0008.

## Requirements

### Requirement: Hybrid store
The system SHALL persist each memory chunk in both an FTS5 table and a `vec0` table in
one ncruces/go-sqlite3 database, and retrieve via FTS5 + vec0 fused with RRF.

#### Scenario: Semantic recall on paraphrase
- **WHEN** a query paraphrases a stored decision without shared keywords
- **THEN** vec0 surfaces the chunk and RRF ranks it into the top-k

#### Scenario: Exact-identifier recall
- **WHEN** a query contains an exact identifier (e.g. `T-011`, a filename)
- **THEN** FTS5 surfaces the chunk even if embeddings rank it low

### Requirement: Local embedding
Embeddings SHALL be produced by the `Embedder` port using a local model; chunk text
MUST NOT leave the host by default.

#### Scenario: Dimension pinned to model
- **WHEN** the store is initialized
- **THEN** the vec0 column dimension equals `Embedder.Dim()` and is recorded in schema

### Requirement: Memory artifacts
The system SHALL maintain project memory (`MEMORY.md`), scratch notes (`notes.md`),
and per-task progress (`tasks/<id>/progress.md`), auto-injected on session resume.

#### Scenario: Resume without relearning
- **WHEN** a session resumes in a known project
- **THEN** project memory and open task progress are injected before the first turn

### Requirement: Self-improvement passes
`/dream` SHALL extract durable knowledge from recent traces into memory and prune
stale entries; `/distill` SHALL package repeated manual workflows into skills.

#### Scenario: Dream prunes superseded facts
- **WHEN** `/dream` runs and a stored fact is contradicted by recent work
- **THEN** the stale entry is removed and the new fact is written
