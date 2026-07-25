// Package tool defines the Tool port (ADR-0001 loop design). Every builtin
// (read, write, edit, bash, glob, grep) and every MCP-exposed capability
// implements this same interface; the agent loop never calls anything else.
package tool

import (
	"context"
	"encoding/json"
)

// Tool is one callable capability exposed to the model.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // JSON Schema for the tool's input
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// Registry resolves tool calls by name.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.tools[t.Name()] = t
	}
	return r
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}
