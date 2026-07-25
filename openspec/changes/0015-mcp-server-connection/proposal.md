# Change 0015 — MCP server connection + tool proxy (PLAN)

## Why
`MCP marketplace` has sat on the refuse-in-v1 list since `0001-foundation`
(proposal §"Deferred") and ADR-0002 ("MCP plugin marketplace"). The deferral was
behind the `Tool` port, with no documented trigger. That trigger now fires: connect
to configured Model Context Protocol servers and surface their tools to the agent
like any built-in tool.

This change ships **connection + tool proxy** — the first of two phases the user
approved. A registry/marketplace browser is explicitly **deferred** (see "What this
deliberately does not do"); it is a later change with its own trigger.

## What I verified before designing
1. **The Tool port already holds the shape.** `ports.Tool` (`internal/ports/ports.go`)
   and `tool.Registry` (`internal/tool/tool.go`, wired at `runtime/assemble.go`)
   register tools by name and execute them through the agent loop's
   `executeOne` (`internal/agent/loop.go`). An MCP server's tools become `tool.Tool`
   adapters dropped into that registry — no new execution path.
2. **Every tool call already passes the PolicyGate.** The agent-loop spec's
   "Permission gate on every tool call" requirement covers MCP tools for free the
   moment they are plain `Tool`s. An MCP tool is not a privilege bypass.
3. **Config is JSON-shaped and env-expandable.** `config.Load`
   (`internal/config/config.go`) parses `opencode.json` into a `Config` struct with
   `{env:VAR}` expansion. An `mcp` section fits the existing `rawConfig` pattern; no
   new loader.
4. **MCP is JSON-RPC 2.0 over two transports.** stdio (subprocess) and streamable
   HTTP (SSE). Both are implementable in pure Go over `os/exec` + `net/http` — no
   SDK, no cgo. The cgo-free claim is **re-asserted by a build at T-1510**, not
   assumed.
5. **No MCP code exists yet** — this is greenfield adapter work, all behind the
   `Tool` port.

## What changes
Adds an MCP adapter behind the **unchanged** `Tool` port — no core loop change.

- `internal/mcp`: the only package that knows MCP exists. A `Client` speaks
  JSON-RPC 2.0 over a `Transport` (stdio subprocess **or** streamable HTTP). It
  performs `initialize`/`initialized`, lists tools (`tools/list`), and proxies
  `tools/call`. Each remote tool becomes a `tool.Tool` adapter whose `Execute`
  forwards to `tools/call` and maps the JSON result to the neutral tool result.
- Config: `opencode.json` gains an `mcp` map — `{ name: { transport: "stdio",
  command, args, env } | { transport: "http", url, headers? } }`. Parsed in
  `config.Load`; servers are materialized at session assemble.
- Runtime: at `assemble`, each configured server is started, its tools discovered
  and registered into the session `tool.Registry` alongside the built-ins; servers
  are stopped on session end. MCP tools carry their server name in the tool name
  (`server.tool`) so they are auditable.
- `docs/adr/0010-mcp-server-connection.md`: records the trigger firing; the
  registry/marketplace stays deferred.
- `AGENTS.md` refuse-list: `MCP marketplace` → "connection+proxy shipped (0015);
  registry deferred."

### The MCP contract (defined by this change)
- Transport: stdio = spawned subprocess, newline-delimited JSON-RPC over
  stdin/stdout; http = streamable HTTP per the MCP spec (POST for requests,
  SSE/streams for server→client). Both gated behind one `Transport` interface.
- Lifecycle: a server that fails `initialize` is reported by name and skipped
  (never crashes the session); a server that dies mid-session surfaces its tools'
  calls as errors.
- Schema: MCP tool `inputSchema` (JSON-Schema) is translated to the provider's
  neutral tool schema at registration; a schema that cannot be represented is
  rejected with the tool/server named.

## What this deliberately does not do
- **No registry / marketplace browser.** No browse, install, or remote catalog.
  Servers are declared in `opencode.json` by the user. That is a later change.
- **No resources / prompts / sampling MCP surfaces.** Only `tools/list` +
  `tools/call`. Resources and prompts are separate triggers.
- **No auto-trust.** MCP tools go through the `PolicyGate` like every tool. A
  server is not a trust boundary; the user still approves calls per policy.
- **No outbound network beyond declared servers.** No telemetry, no update
  checks, no fetching server lists.
- **No vendor MCP SDK.** Pure-Go JSON-RPC over stdio/http, cgo-free.

## Governing decisions
ADR-0002 (deferred MCP marketplace — trigger now fires, connection phase) ·
ADR-0001 (cgo-free — pure-Go JSON-RPC, **re-asserted by build**) · the agent-loop
"Permission gate on every tool call" requirement (MCP tools inherit it). The
`Tool` port is **unchanged**: this is an adapter feeding the existing registry.

## Risk
- **Subprocess / connection lifecycle.** Leaked processes or sockets on session
  end. T-1515 asserts every started server is stopped (stdio) or closed (http) on
  session teardown, including cancel/kill on context done.
- **Unbounded blocking.** A hung server stalls a tool call. Every `tools/call`
  carries the turn `ctx`; a non-responsive server is aborted, surfacing a
  deadline/cancel error.
- **Schema translation gaps.** MCP `inputSchema` shapes the neutral schema cannot
  express must be rejected up front (tool named), not silently coerced. T-1513.
- **cgo leak.** A transitive dependency pulling cgo breaks the single binary.
  T-1510 gates the dependency with a cgo-free build.
- **Trust surface.** MCP servers run arbitrary code / call arbitrary URLs. The
  PolicyGate is the mitigation; documented in the ADR, not hidden.

## Verification
The `Client` is testable against an in-process fake `Transport` (initialize
handshake, tools/list, tools/call round-trip, error mapping). The stdio transport
is testable against a tiny JSON-RPC echo subprocess; the http transport against an
`httptest.Server`. The tool adapter is testable for schema translation + result
mapping. End-to-end is testable by configuring one fake server in `opencode.json`,
assembling a session, and confirming its tool is registered, callable, and gated.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks are
approved (house Gate 1).
