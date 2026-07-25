package runtime

import (
	"context"
	"fmt"
	"sort"

	"github.com/7solutions/openplus/internal/config"
	"github.com/7solutions/openplus/internal/mcp"
	"github.com/7solutions/openplus/internal/tool"
)

// startMCPServers connects to every declared MCP server and returns their tools,
// adapted to the Tool port, plus a warning per server that could not be used.
//
// A broken server is skipped rather than fatal: it costs the user that server's
// tools, but taking down the whole session for one bad entry costs them all of
// them. Every failure is named so it cannot pass unnoticed.
//
// Servers are started in name order so the registered tool set (and any failure
// report) is reproducible.
func (s *Session) startMCPServers(ctx context.Context, servers map[string]config.MCPServer) ([]tool.Tool, []string) {
	if len(servers) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	var (
		tools    []tool.Tool
		warnings []string
	)
	for _, name := range names {
		decl := servers[name]
		client, err := dialMCP(ctx, decl)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("mcp server %q unavailable: %v", name, err))
			continue
		}

		if err := client.Initialize(ctx); err != nil {
			_ = client.Close()
			warnings = append(warnings, fmt.Sprintf("mcp server %q failed initialize: %v", name, err))
			continue
		}
		got, err := client.Tools(ctx)
		if err != nil {
			_ = client.Close()
			warnings = append(warnings, fmt.Sprintf("mcp server %q: %v", name, err))
			continue
		}

		s.mcpClients = append(s.mcpClients, client)
		tools = append(tools, got...)
	}
	return tools, warnings
}

// dialMCP builds the transport a declaration asks for. The mapping from config to
// transport lives here so internal/mcp stays independent of the config surface.
func dialMCP(ctx context.Context, decl config.MCPServer) (*mcp.Client, error) {
	switch decl.Transport {
	case config.MCPTransportStdio:
		env := make([]string, 0, len(decl.Env))
		for k, v := range decl.Env {
			env = append(env, k+"="+v)
		}
		sort.Strings(env)
		t, err := mcp.NewStdio(ctx, mcp.StdioConfig{
			Command: decl.Command,
			Args:    decl.Args,
			Env:     env,
			Dir:     decl.Dir,
		})
		if err != nil {
			return nil, err
		}
		return mcp.NewClient(decl.Name, t), nil

	case config.MCPTransportHTTP:
		return mcp.NewClient(decl.Name, mcp.NewHTTP(mcp.HTTPConfig{
			URL:     decl.URL,
			Headers: decl.Headers,
		})), nil

	default:
		// config.Load already rejects unknown transports; this guards a
		// hand-built Session.
		return nil, fmt.Errorf("unknown transport %q", decl.Transport)
	}
}

// MCPClients returns the connected MCP clients (diagnostics and tests).
func (s *Session) MCPClients() []*mcp.Client {
	return append([]*mcp.Client(nil), s.mcpClients...)
}

// closeMCP stops every MCP server this session started. It is idempotent, so a
// deferred Close after an error path is safe.
//
// Every server's Close is attempted even if an earlier one fails — a leaked
// subprocess is worse than a lost error message.
func (s *Session) closeMCP() error {
	clients := s.mcpClients
	s.mcpClients = nil

	var firstErr error
	for _, c := range clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("runtime: closing mcp server %q: %w", c.Name, err)
		}
	}
	return firstErr
}
