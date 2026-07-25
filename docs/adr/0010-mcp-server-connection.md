# ADR-0010 — MCP server connection: the ADR-0002 trigger has fired

**Status:** Accepted

## Context
ADR-0002 listed "MCP plugin marketplace" as a deferred v1 non-goal, behind the
`Tool` port, with no documented trigger. The trigger now fires for the
**connection + tool proxy** phase: connect to configured Model Context Protocol
servers and surface their tools to the agent as plain `Tool`s.

A registry/marketplace browser remains deferred — this decision covers servers the
user declares in `opencode.json`, not discovery or installation.

## Decision
Add an MCP adapter behind the **unchanged** `Tool` port. A new package
(`internal/mcp`) is the only place that knows MCP exists: it speaks JSON-RPC 2.0
over a `Transport` — stdio (spawned subprocess) **or** streamable HTTP (SSE) —
performs the `initialize`/`initialized` handshake, lists tools (`tools/list`), and
proxies `tools/call`. Each remote tool becomes a `tool.Tool` adapter registered in
the session `tool.Registry` and named `<server>.<tool>`.

The turn loop, the `Tool` port, and the PolicyGate are not modified. An MCP tool is
a normal tool: it passes the "Permission gate on every tool call" requirement like
any built-in. A configured server is **not** a trust boundary.

The implementation is pure Go (`os/exec` + `net/http`), no vendor SDK, no cgo. The
cgo-free property is re-asserted by a build gate, not assumed. Only `tools/list` and
`tools/call` are wired; MCP resources, prompts, and sampling are out of scope.

ADR-0002 remains **Accepted**. This ADR records that its MCP clause has begun: the
connection phase ships; the marketplace phase stays deferred behind its own trigger.

## Consequences
- (+) The agent gains an external tool ecosystem without a port change — the
  hexagonal seam held.
- (+) MCP tools inherit permission gating and auditability for free.
- (−) MCP servers run arbitrary code / call arbitrary URLs; the PolicyGate is the
  sole in-process mitigation, documented not hidden.
- (−) Subprocess and connection lifecycle is now the binary's responsibility; leaks
  are prevented by teardown tests, not by the OS.
- (−) The marketplace/registry is still unbuilt; users must declare servers by hand.
