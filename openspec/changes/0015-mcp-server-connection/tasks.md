# Change 0015 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## H0 — Decision
- [ ] T-1500 Write `docs/adr/0010-mcp-server-connection.md`: the ADR-0002 MCP
      trigger has fired for the **connection + tool proxy** phase; pure-Go
      JSON-RPC over stdio + streamable HTTP, cgo-free, behind the unchanged `Tool`
      port; MCP tools inherit the PolicyGate. The registry/marketplace stays
      deferred. ADR-0002 stays Accepted.

## H1 — Protocol core (internal/mcp)
- [ ] T-1510 Add no cgo-bearing deps; assert `CGO_ENABLED=0 go build ./...` green.
      Red: a build test gating the new package.
- [ ] T-1511 `Transport` interface + JSON-RPC 2.0 framing: `Request`/`Response`/
      `Error`, newline-delimited over stdio. Red: encode/decode round-trip; malformed
      frame → error.
- [ ] T-1512 `Client.initialize`/`initialized` handshake + `tools/list` +
      `tools/call` against a fake in-process `Transport`. Red: handshake → list →
      call round-trip; server error → Go error.

## H2 — Transports
- [ ] T-1513 stdio transport: spawn subprocess (`os/exec`), pipe JSON-RPC. Red:
      handshake + call against a tiny JSON-RPC echo subprocess (testdata script or
      compiled helper).
- [ ] T-1514 streamable-HTTP transport: POST requests, SSE/streams for
      server→client, against `httptest.Server`. Red: handshake + call round-trip;
      non-200 / broken stream → error.
- [ ] T-1515 Lifecycle: every started server stopped (stdio: kill+wait; http:
      close) on session end + on ctx done. Red: no leaked process/connection after
      teardown (assert subprocess exited, conn closed).
- [ ] T-1516 Cancellation: a `tools/call` honors ctx; hung server aborts to a
      cancel/deadline error. Red: call with cancelled/in-short-timeout ctx returns
      promptly.

## H3 — Tool adapter + schema
- [ ] T-1517 `mcpTool` adapter: implements `ports.Tool`; name `<server>.<tool>`;
      `Execute` forwards to `tools/call`, maps result to neutral tool result.
      Red: call → neutral result; server error → Go error.
- [ ] T-1518 `inputSchema` (JSON-Schema) → neutral tool schema translation; reject
      unsupported constructs up front, naming server+tool. Red: supported shape maps;
      unsupported shape → registration error before any call.

## H4 — Config
- [ ] T-1519 `mcp` section in `config.Load`: stdio (`command/args/env`) and http
      (`url/headers`) entries, `{env:VAR}` expansion; unknown transport → named
      error. Red: parse both transports; bad transport errors.

## H5 — Runtime wiring
- [ ] T-1520 At `assemble`: start configured servers, register their tools into the
      session `tool.Registry` alongside built-ins; stop on session end. Integration
      test through the real Session: one fake server in `opencode.json` → its tool is
      registered, callable, and passes the PolicyGate.

## H6 — Gate
- [ ] T-1521 Advisor pass (resolve every finding); update knowledge graph + memory.
      Update `AGENTS.md` refuse-list: `MCP marketplace` → "connection+proxy shipped
      (0015); registry deferred."
