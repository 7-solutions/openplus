# Agent-loop Specification (delta — change 0015)

## Purpose
Connect to configured Model Context Protocol (MCP) servers and surface their tools
to the agent as plain `Tool`s in the session registry, behind the **unchanged**
`Tool` port. Two transports: stdio (subprocess) and streamable HTTP. No registry,
no resources/prompts — tools only. This adds an adapter and a config section; it
does not change the turn loop or the permission gate.

## Requirements

### Requirement: MCP servers are declared in config
The system SHALL read an `mcp` map from `opencode.json`, where each entry names a
server and either a stdio subprocess (command/args/env) or a streamable HTTP
endpoint (url/headers).

#### Scenario: stdio server declaration
- **WHEN** config declares `{ mcp: { ci: { transport: "stdio", command: "npx",
  args: ["-y", "@mcp/server"] } } }`
- **THEN** a session materializes a stdio MCP client for `ci` on assemble

#### Scenario: http server declaration
- **WHEN** config declares `{ mcp: { search: { transport: "http",
  url: "https://x/mcp" } } }`
- **THEN** a session materializes a streamable-HTTP MCP client for `search`

#### Scenario: Unknown transport is rejected
- **WHEN** an entry sets `transport: "grpc"`
- **THEN** config load errors naming the server and the bad transport

### Requirement: A server's tools become Tools in the registry
After initialize, each server's `tools/list` entries SHALL register as `tool.Tool`
adapters in the session `tool.Registry`, named `server.tool`, executable through the
existing loop path and subject to the existing PolicyGate.

#### Scenario: Tools are listed and registered
- **WHEN** a server completes initialize and reports tools `[a, b]`
- **THEN** the registry contains `<server>.a` and `<server>.b`, callable by the agent

#### Scenario: An MCP tool is permission-gated like any tool
- **WHEN** the model calls an MCP tool
- **THEN** the call passes through the PolicyGate before being forwarded to the server

### Requirement: tools/call forwards and maps results
An MCP tool's `Execute` SHALL forward to the server's `tools/call`, return its
result mapped to the neutral tool result, and surface a server error as a Go error.

#### Scenario: A call returns content
- **WHEN** `tools/call` returns content items
- **THEN** `Execute` returns the mapped neutral result to the loop

#### Scenario: A server error is surfaced
- **WHEN** `tools/call` returns an MCP error
- **THEN** `Execute` returns a Go error describing it; the loop records the failure

### Requirement: Servers are lifecycle-managed
The system SHALL start servers at assemble and stop/close them on session end; a
server that fails initialize is reported by name and skipped, not fatal.

#### Scenario: Failed initialize is non-fatal
- **WHEN** a server fails the initialize handshake
- **THEN** its tools are absent, the session continues, and the failure is reported by name

#### Scenario: Teardown stops every server
- **WHEN** a session ends
- **THEN** every started stdio subprocess is killed/cleaned and every http connection closed

### Requirement: Schema translation is total or rejected
An MCP tool's `inputSchema` SHALL be translated to the neutral tool schema; a shape
that cannot be represented SHALL be rejected at registration with the tool named,
never silently coerced.

#### Scenario: Unsupported schema is rejected up front
- **WHEN** an `inputSchema` uses a construct the neutral schema cannot express
- **THEN** registration errors naming the server and tool, before any call

### Requirement: A hung server is abortable
Every MCP call SHALL honor the turn context; a non-responsive server is aborted and
surfaces a deadline/cancellation error rather than blocking the loop.

#### Scenario: Context cancellation aborts a call
- **WHEN** a `tools/call` is in flight and its context is cancelled
- **THEN** the call returns a cancellation error promptly
