package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ProtocolVersion is the MCP revision this client speaks. A server that needs a
// different one says so in its initialize result; we report the mismatch rather
// than guessing at compatibility.
const ProtocolVersion = "2025-06-18"

// ClientName identifies OpenPlus to servers in the handshake.
const ClientName = "openplus"

// Transport carries JSON-RPC to one server. Call awaits a response; Notify sends
// a message that expects none. Both honor ctx: a hung server must never block a
// turn indefinitely.
//
// Implementations: stdioTransport (subprocess) and httpTransport (streamable
// HTTP). The Client is transport-agnostic.
type Transport interface {
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	Notify(ctx context.Context, method string, params json.RawMessage) error
	Close() error
}

// ToolDesc is one tool a server advertises.
type ToolDesc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Client speaks MCP to one server.
type Client struct {
	// Name is the config key for this server; it prefixes its tool names and
	// appears in every error, so a failure points at a specific server.
	Name string

	transport Transport

	mu          sync.Mutex
	initialized bool
	serverInfo  string
}

// NewClient wraps a Transport as an MCP client. The caller must call Initialize
// before listing or calling tools.
func NewClient(name string, t Transport) *Client {
	return &Client{Name: name, transport: t}
}

// Initialize performs the MCP handshake: an initialize request, then the
// initialized notification. The notification is only sent once the server has
// answered — sending it early would announce readiness we do not have.
func (c *Client) Initialize(ctx context.Context) error {
	params, err := json.Marshal(map[string]any{
		"protocolVersion": ProtocolVersion,
		"clientInfo":      map[string]string{"name": ClientName, "version": "0"},
		// Tools only: this change does not consume resources, prompts or
		// sampling, so it does not claim to support them.
		"capabilities": map[string]any{},
	})
	if err != nil {
		return fmt.Errorf("mcp: %s: encode initialize: %w", c.Name, err)
	}

	res, err := c.transport.Call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("mcp: %s: initialize: %w", c.Name, err)
	}

	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(res, &init); err != nil {
		return fmt.Errorf("mcp: %s: initialize result: %w", c.Name, err)
	}

	if err := c.transport.Notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("mcp: %s: initialized notification: %w", c.Name, err)
	}

	c.mu.Lock()
	c.initialized = true
	c.serverInfo = strings.TrimSpace(init.ServerInfo.Name + " " + init.ServerInfo.Version)
	c.mu.Unlock()
	return nil
}

// ServerInfo reports the server's self-reported name and version, empty before
// the handshake.
func (c *Client) ServerInfo() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverInfo
}

// ready reports whether the handshake completed.
func (c *Client) ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

// ListTools returns the server's advertised tools. It refuses before the
// handshake rather than sending a request the server is entitled to reject.
func (c *Client) ListTools(ctx context.Context) ([]ToolDesc, error) {
	if !c.ready() {
		return nil, fmt.Errorf("mcp: %s: tools/list before initialize", c.Name)
	}
	res, err := c.transport.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: tools/list: %w", c.Name, err)
	}
	var out struct {
		Tools []ToolDesc `json:"tools"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("mcp: %s: tools/list result: %w", c.Name, err)
	}
	return out.Tools, nil
}

// CallTool invokes one tool and returns its content as text.
//
// Two distinct failures both surface as errors: a JSON-RPC error (the call did
// not happen) and an isError result (the tool ran and reported failure). The loop
// needs to see both as failures, so neither is returned as ordinary output.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if !c.ready() {
		return "", fmt.Errorf("mcp: %s: tools/call before initialize", c.Name)
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	params, err := json.Marshal(map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", fmt.Errorf("mcp: %s.%s: encode arguments: %w", c.Name, name, err)
	}

	res, err := c.transport.Call(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("mcp: %s.%s: %w", c.Name, name, err)
	}

	var out struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			MimeType string `json:"mimeType"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", fmt.Errorf("mcp: %s.%s: result: %w", c.Name, name, err)
	}

	var b strings.Builder
	for _, item := range out.Content {
		if item.Type == "text" {
			b.WriteString(item.Text)
			continue
		}
		// Non-text content cannot be handed to the model as text, but saying
		// nothing would look like an empty result. Describe it instead.
		fmt.Fprintf(&b, "[%s content", item.Type)
		if item.MimeType != "" {
			fmt.Fprintf(&b, " %s", item.MimeType)
		}
		b.WriteString(" omitted]")
	}

	if out.IsError {
		text := strings.TrimSpace(b.String())
		if text == "" {
			text = "the tool reported an error with no detail"
		}
		return "", fmt.Errorf("mcp: %s.%s: %s", c.Name, name, text)
	}
	return b.String(), nil
}

// Close shuts the transport down (killing a subprocess, closing a connection).
func (c *Client) Close() error { return c.transport.Close() }
