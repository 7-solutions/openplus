# Skills Specification

## Purpose
Reusable instruction sets discovered by name and BM25 relevance, invocable with `/`.
Governed by ADR-0002.

## Requirements

### Requirement: Discovery and override order
The system SHALL scan builtin skills then user skill directories
(`.opencode/skills`, `~/.claude/skills`, `~/.opencode/skills`), with user skills
discovered later overriding builtins of the same name.

#### Scenario: Project overrides builtin
- **WHEN** a project defines a skill with the same `name` as a builtin
- **THEN** the project skill is used

### Requirement: BM25 skill search
The system SHALL rank skills by exact name, alias, and BM25 relevance; high-confidence
matches load automatically, uncertain matches are ranked for the agent to assess.

#### Scenario: Explicit invocation
- **WHEN** the user types `/<skill-name>`
- **THEN** that skill loads regardless of ranking

#### Scenario: Multi-skill orchestration
- **WHEN** two or more skills are named in one message
- **THEN** all load and a multi-skill orchestration plan is injected
