package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-1511: frames are newline-delimited JSON-RPC 2.0 messages; a round-trip
// preserves method, id and params.
func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	out := Request{
		JSONRPC: Version,
		ID:      json.RawMessage(`7`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"echo"}`),
	}
	if err := EncodeFrame(&buf, out); err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("frame is not newline-terminated: %q", buf.String())
	}
	if strings.Count(strings.TrimSuffix(buf.String(), "\n"), "\n") != 0 {
		t.Errorf("frame contains an embedded newline: %q", buf.String())
	}

	var got Request
	if err := DecodeFrame(bufio.NewReader(&buf), &got); err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if got.Method != out.Method || string(got.ID) != "7" {
		t.Fatalf("round-trip lost data: %+v", got)
	}
	if string(got.Params) != `{"name":"echo"}` {
		t.Errorf("params = %s", got.Params)
	}
}

// T-1511: a malformed frame is an error, not a zero value.
func TestDecodeFrameMalformed(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("{not json}\n"))
	var got Response
	if err := DecodeFrame(r, &got); err == nil {
		t.Fatal("malformed frame should error")
	}
}

// T-1511: an empty line is skipped rather than reported as a broken frame — some
// servers pad their output.
func TestDecodeFrameSkipsBlankLines(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("\n\n" + `{"jsonrpc":"2.0","id":1,"result":{}}` + "\n"))
	var got Response
	if err := DecodeFrame(r, &got); err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if string(got.ID) != "1" {
		t.Fatalf("id = %s", got.ID)
	}
}

// T-1511: a JSON-RPC error response reports its code and message.
func TestResponseError(t *testing.T) {
	var res Response
	if err := json.Unmarshal([]byte(
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"no such method"}}`), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Error == nil {
		t.Fatal("error not parsed")
	}
	if !strings.Contains(res.Error.Error(), "no such method") ||
		!strings.Contains(res.Error.Error(), "-32601") {
		t.Errorf("Error() = %q, want code and message", res.Error.Error())
	}
}

// T-1510: this package stays cgo-free and standard-library-only apart from the
// project's own packages. An MCP SDK dependency would be the easy way to break
// the pure-Go build guarantee (ADR-0001), so the gate is a test.
func TestNoExternalOrCgoDependencies(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(path, "github.com/7-solutions/openplus/") {
				continue
			}
			if path == "C" {
				t.Errorf("%s: imports C; the default build is cgo-free", f)
				continue
			}
			// A standard-library path has no dot in its first element.
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") {
				t.Errorf("%s: third-party import %q; internal/mcp is standard-library only", f, path)
			}
		}
	}
}
