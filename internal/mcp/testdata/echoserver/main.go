// Command echoserver is a minimal JSON-RPC 2.0 MCP server used by the stdio
// transport tests. It answers initialize, tools/list and tools/call over
// stdin/stdout with newline-delimited frames, and exits when stdin closes.
//
// It lives under testdata so the main build never compiles it; the test builds it
// into a temp binary.
package main

import (
	"bufio"
	"encoding/json"
	"os"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	initResult = `{"protocolVersion":"2025-06-18","serverInfo":{"name":"echo","version":"1"}}`
	listResult = `{"tools":[{"name":"echo","description":"echo back","inputSchema":` +
		`{"type":"object","properties":{"text":{"type":"string"}}}}]}`
	callResult = `{"content":[{"type":"text","text":"from subprocess"}]}`
)

func main() {
	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	enc := json.NewEncoder(out)
	for {
		line, err := in.ReadString('\n')
		if err != nil {
			return
		}
		var req request
		if json.Unmarshal([]byte(line), &req) != nil {
			continue
		}
		if len(req.ID) == 0 {
			continue // a notification expects no response
		}

		res := response{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			res.Result = json.RawMessage(initResult)
		case "tools/list":
			res.Result = json.RawMessage(listResult)
		case "tools/call":
			res.Result = json.RawMessage(callResult)
		default:
			res.Error = &rpcError{Code: -32601, Message: "unknown method " + req.Method}
		}
		if enc.Encode(res) != nil {
			return
		}
		out.Flush()
	}
}
