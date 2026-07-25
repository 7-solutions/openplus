# Ports (delta)

## ADDED Requirements

### Requirement: LanguageService port
The package `internal/ports/` SHALL declare a `LanguageService` port interface as the
eleventh seam the core depends on. It exposes read-only code intelligence:
`Diagnostics`, `Hover`, `Definition`, `DocumentSymbols`, `References`, and `Shutdown`.

**Constraint:** `internal/ports/` must not import `internal/lsp/` in either direction
(no cycle, no leak).

#### Scenario: Port count is asserted
- **WHEN** `PortNames()` is called after this change
- **THEN** it returns eleven names including `"LanguageService"`, and
  `TestAllTenPortsAreDeclared` (renamed to match) asserts the count is 11

#### Scenario: Core depends on the port, not the adapter
- **WHEN** a core package needs diagnostics
- **THEN** it depends on `ports.LanguageService`; the concrete client in
  `internal/lsp/` is constructed only by the runtime wiring layer

### Requirement: Neutral code-intelligence types
The result types (`Diagnostic`, `Location`, `Symbol`, `Severity`) SHALL be declared in
`internal/ports/` and be free of any LSP-wire type. No type from `go.lsp.dev/protocol`
or `go.lsp.dev/jsonrpc2` may appear in a `LanguageService` signature or in any type it
returns.

**Rationale:** this is the provider-neutrality hard rule applied to a second wire
protocol. The core must not learn LSP any more than it learns the Anthropic wire.

#### Scenario: LSP type leaks into a port signature
- **WHEN** any exported identifier in `internal/ports/` references a `go.lsp.dev` type
- **THEN** the regression guard fails the build

#### Scenario: Conversion happens at the adapter boundary
- **WHEN** a language server returns a `protocol.Diagnostic`
- **THEN** `internal/lsp/` converts it to `ports.Diagnostic` before it crosses the port

### Requirement: Test fake for the port
`internal/ports/` SHALL declare a `FakeLanguageService` with a compile-time assertion,
following the `FakeEmbedder` pattern, so tests can exercise LSP-dependent code with no
subprocess.

#### Scenario: Fake satisfies the port at compile time
- **WHEN** the ports package compiles
- **THEN** `var _ LanguageService = FakeLanguageService{}` enforces the contract

### Requirement: Opt-in activation, never in fake mode
A language server SHALL be started only when the `lsp` config enables it AND the
session is not a fake session.

#### Scenario: Disabled by default
- **WHEN** no `lsp` config block is present
- **THEN** no server process is spawned, no LSP tools are registered, and no
  diagnostics section is injected

#### Scenario: Fake session spawns nothing
- **WHEN** a session is assembled with `Fake: true`
- **THEN** no language server process is spawned regardless of the `lsp` config

### Requirement: Non-fatal degradation
A language server that is missing, fails to start, or fails to initialize SHALL
produce a named warning and leave the rest of the session fully functional.

#### Scenario: Configured server binary is absent
- **WHEN** `lsp.servers[".go"].command` names a binary that does not exist
- **THEN** assembly records a warning naming the server and the session continues
  without LSP tools for that extension

### Requirement: Bounded diagnostics injection
Diagnostics injected into the model's context SHALL be capped in count and length and
SHALL be accounted for by the Budgeter.

#### Scenario: A file with many errors
- **WHEN** a touched file produces more diagnostics than the cap
- **THEN** the injected section is truncated to the cap and the remainder is
  summarized as a count, never dropped silently

#### Scenario: Budget accounting
- **WHEN** the diagnostics section is assembled
- **THEN** it passes through `Budgeter.Fit` with the rest of the turn's context
