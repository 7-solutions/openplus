package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/7solutions/openplus/internal/tool"
)

// ToolNameSeparator joins a server name and its tool name. Namespacing is not
// cosmetic: two servers may both expose "search", and a permission rule is
// written against the full name.
const ToolNameSeparator = "."

// mcpTool adapts one server-side tool to the Tool port. It holds no protocol
// state of its own — every call goes back through the Client.
type mcpTool struct {
	client *Client
	remote string // the server's own name for the tool
	name   string // "<server>.<tool>", what the model sees
	desc   string
	schema json.RawMessage
}

func (t *mcpTool) Name() string            { return t.name }
func (t *mcpTool) Description() string     { return t.desc }
func (t *mcpTool) Schema() json.RawMessage { return t.schema }

// Execute forwards to the server's tools/call. The PolicyGate has already run by
// the time the loop reaches here — an MCP tool is gated exactly like a builtin.
func (t *mcpTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return t.client.CallTool(ctx, t.remote, input)
}

// Tools lists the server's tools and adapts them to the Tool port.
//
// A tool whose schema cannot be translated fails the whole listing rather than
// being dropped: a user who configured a server expects its tools, and silently
// serving a subset hides the problem until the model tries to call the missing one.
func (c *Client) Tools(ctx context.Context) ([]tool.Tool, error) {
	descs, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]tool.Tool, 0, len(descs))
	for _, d := range descs {
		remote := strings.TrimSpace(d.Name)
		if remote == "" {
			return nil, fmt.Errorf("mcp: %s: server advertised a tool with no name", c.Name)
		}
		schema, err := translateSchema(c.Name, remote, d.InputSchema)
		if err != nil {
			return nil, err
		}
		out = append(out, &mcpTool{
			client: c,
			remote: remote,
			name:   c.Name + ToolNameSeparator + remote,
			desc:   d.Description,
			schema: schema,
		})
	}
	return out, nil
}

// unsupportedKeywords are JSON-Schema constructs the neutral tool schema cannot
// express. They are rejected rather than stripped: dropping a $ref or a oneOf
// changes what the tool accepts, and the model would then be told a lie about the
// tool's contract.
var unsupportedKeywords = []string{"$ref", "oneOf", "anyOf", "allOf", "not"}

// translateSchema converts an MCP inputSchema to the neutral tool schema. The
// neutral schema is itself JSON Schema, so a representable shape passes through
// unchanged — the work here is validation, and rejection names the server and the
// tool so the operator knows which one to fix.
//
// An absent or empty schema means "no arguments", which is representable.
func translateSchema(server, name string, in json.RawMessage) (json.RawMessage, error) {
	reject := func(why string) error {
		return fmt.Errorf("mcp: %s.%s: inputSchema cannot be represented: %s", server, name, why)
	}

	if len(strings.TrimSpace(string(in))) == 0 || string(in) == "null" {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}

	var root map[string]any
	if err := json.Unmarshal(in, &root); err != nil {
		return nil, reject(fmt.Sprintf("not a JSON object (%v)", err))
	}

	// The model calls tools with named arguments, so the top level must be an
	// object. An empty schema is allowed and means "no arguments".
	if t, ok := root["type"]; ok {
		if s, isStr := t.(string); !isStr || s != "object" {
			return nil, reject(fmt.Sprintf("top-level type must be object, got %v", t))
		}
	} else if len(root) > 0 {
		if err := checkUnsupported(root, reject); err != nil {
			return nil, err
		}
		return nil, reject("top-level schema has no object type")
	}

	if err := checkUnsupported(root, reject); err != nil {
		return nil, err
	}

	// Normalize so every schema handed to a provider is an explicit object with a
	// properties member, which some adapters require even with no arguments.
	if _, ok := root["type"]; !ok {
		root["type"] = "object"
	}
	if _, ok := root["properties"]; !ok {
		root["properties"] = map[string]any{}
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, reject(err.Error())
	}
	return out, nil
}

// checkUnsupported walks the schema rejecting constructs the neutral schema
// cannot express, at any depth.
func checkUnsupported(node any, reject func(string) error) error {
	switch v := node.(type) {
	case map[string]any:
		for _, kw := range unsupportedKeywords {
			if _, ok := v[kw]; ok {
				return reject("uses " + kw)
			}
		}
		for _, child := range v {
			if err := checkUnsupported(child, reject); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := checkUnsupported(child, reject); err != nil {
				return err
			}
		}
	}
	return nil
}
