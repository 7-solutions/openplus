// Package mcp connects to Model Context Protocol servers and exposes their tools
// through the unchanged Tool port (change 0015, ADR-0010).
//
// This package is the only place in OpenPlus that knows the MCP wire protocol:
// JSON-RPC 2.0 over either a stdio subprocess or streamable HTTP. Everything it
// hands the rest of the system is a neutral tool.Tool. It is written against the
// standard library only, so the build stays cgo-free.
//
// Security: an MCP server is arbitrary code (stdio) or an arbitrary endpoint
// (http) chosen by the user's config. Its tools are registered like any other, so
// they pass the PolicyGate — that gate is the only in-process mitigation, and MCP
// tools must never bypass it.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Version is the JSON-RPC version every message carries.
const Version = "2.0"

// Request is a JSON-RPC 2.0 request or notification. A notification has no ID
// and expects no response.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response. Exactly one of Result and Error is set.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if len(e.Data) > 0 {
		return fmt.Sprintf("jsonrpc error %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// EncodeFrame writes one newline-delimited JSON message. Compact encoding
// matters: an embedded newline would split one message across two frames.
func EncodeFrame(w io.Writer, msg any) error {
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("mcp: encode frame: %w", err)
	}
	if _, err := w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("mcp: write frame: %w", err)
	}
	return nil
}

// DecodeFrame reads one newline-delimited JSON message into msg, skipping blank
// padding lines. A malformed frame is an error rather than a zero value, so a
// caller cannot mistake garbage for an empty result.
func DecodeFrame(r *bufio.Reader, msg any) error {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF && strings.TrimSpace(line) != "" {
				// A final frame without its trailing newline is still a frame.
				return decodeLine(line, msg)
			}
			return err
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		return decodeLine(line, msg)
	}
}

func decodeLine(line string, msg any) error {
	if err := json.Unmarshal([]byte(line), msg); err != nil {
		return fmt.Errorf("mcp: decode frame %q: %w", strings.TrimSpace(line), err)
	}
	return nil
}
