package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// Echo is a trivial builtin used to prove the loop end-to-end without
// touching the filesystem or a shell. Real builtins (read/write/edit/bash/
// glob/grep) follow this same shape — see T-020/T-021 in the backlog.
type Echo struct{}

func (Echo) Name() string        { return "echo" }
func (Echo) Description() string { return "Echoes back the given text. For scaffold/testing only." }
func (Echo) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
}

func (Echo) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("echo: invalid input: %w", err)
	}
	return args.Text, nil
}
