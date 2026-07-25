# Ports (delta)

## ADDED Requirements

### Requirement: Canonical Provider port
The package `internal/ports/` SHALL declare the `Provider` port interface as
the single canonical declaration.

**Constraint:** `internal/ports/` must not import `internal/provider/` in
either direction (no cycle, no leak).

#### Scenario: Double-declaration regression
- **WHEN** both `internal/ports/provider.go` and `internal/provider/types.go`
  declare a `Provider` interface at the end of the change
- **THEN** the post-merge gate (T-1807) fails

### Requirement: Neutral model types in `internal/ports/`
The neutral model types (`Request`, `Event`, `Message`, `Block`, `BlockKind`,
`Role`, `ToolSchema`, `ToolCall`, `Usage`, `EventKind`, `ToolResult*`) SHALL
live in `internal/ports/model.go`. The names are unchanged from their former
`internal/provider/types.go` definitions; the doc-comments are unchanged.

#### Scenario: External adapter against canonical types
- **WHEN** a future adapter is added in `internal/provider/<new>/`
- **THEN** it imports `internal/ports` and references `ports.Request`,
  `ports.Event`, etc. — never a copy in `internal/provider/`

### Requirement: Test fakes package
The scripted `Fake` provider SHALL live in `internal/ports/providerfake/`
(exported name `portsfake.Fake`) and implement `ports.Provider`.

**Migration note:** during the transition window in change 0018, the
`internal/provider` package retains a thin re-export shim so a partial
migration does not break the build. The shim is deleted in T-1807 once no
caller depends on it.

#### Scenario: Test fake satisfies port at compile time
- **WHEN** a test uses `portsfake.Fake`
- **THEN** a compile-time assertion `var _ ports.Provider = (*portsfake.Fake)(nil)`
  at the bottom of `ports/providerfake/fake.go` enforces it

### Requirement: Backwards-compatibility shim (temporary)
During change 0018, `internal/provider/provider_compat.go` MAY re-export the
neutral types and `*provider.Fake` under their old names so adapter packages
and any external caller do not need to update in lockstep.

The shim is removed by T-1807 at the end of the change. A `go vet` check
(`internal/provider/has_no_shim_after_t1807`) confirms it is gone.

#### Scenario: External consumer survives the migration window
- **WHEN** an external `cmd/*` or test imports `provider.Fake` or `provider.Message`
  during the change
- **THEN** the shim satisfies the import until T-1807 closes it

> Full ports requirements: `openspec/specs/ports/spec.md` (foundation).
