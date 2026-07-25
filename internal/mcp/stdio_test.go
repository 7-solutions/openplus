package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildEchoServer compiles testdata/echoserver and returns the binary's path.
// Compiling is slower than a shell script but keeps the test portable, and the
// transport is then exercised against a real subprocess.
func buildEchoServer(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "echoserver", "main.go")
	bin := filepath.Join(t.TempDir(), "echoserver")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}
	return bin
}

// T-1513: handshake, list and call all work over a real subprocess.
func TestStdioTransportRoundTrip(t *testing.T) {
	bin := buildEchoServer(t)

	tr, err := NewStdio(context.Background(), StdioConfig{Command: bin})
	if err != nil {
		t.Fatalf("NewStdio: %v", err)
	}
	c := NewClient("sub", tr)
	defer c.Close()

	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if info := c.ServerInfo(); !strings.Contains(info, "echo") {
		t.Errorf("ServerInfo = %q", info)
	}

	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "sub.echo" {
		t.Fatalf("tools = %+v", tools)
	}
	out, err := tools[0].Execute(context.Background(), json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "from subprocess" {
		t.Fatalf("Execute = %q", out)
	}
}

// T-1513: an unknown method's JSON-RPC error surfaces as a Go error.
func TestStdioTransportServerError(t *testing.T) {
	tr, err := NewStdio(context.Background(), StdioConfig{Command: buildEchoServer(t)})
	if err != nil {
		t.Fatalf("NewStdio: %v", err)
	}
	defer tr.Close()

	if _, err := tr.Call(context.Background(), "nope/nope", nil); err == nil {
		t.Fatal("unknown method should error")
	}
}

// T-1515: closing the transport stops the subprocess — no leaked process.
func TestStdioTransportCloseStopsProcess(t *testing.T) {
	tr, err := NewStdio(context.Background(), StdioConfig{Command: buildEchoServer(t)})
	if err != nil {
		t.Fatalf("NewStdio: %v", err)
	}
	pid := tr.PID()
	if pid <= 0 {
		t.Fatalf("PID = %d", pid)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !tr.Exited() {
		t.Error("subprocess still running after Close")
	}
	// Close is idempotent: session teardown may run after an error path already
	// closed the transport.
	if err := tr.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	// A call after close fails rather than hanging.
	if _, err := tr.Call(context.Background(), "initialize", nil); err == nil {
		t.Error("Call after Close should error")
	}
}

// T-1515: cancelling the context the transport was started with stops the
// subprocess, so a cancelled session leaves nothing behind.
func TestStdioTransportContextCancelStopsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	tr, err := NewStdio(ctx, StdioConfig{Command: buildEchoServer(t)})
	if err != nil {
		t.Fatalf("NewStdio: %v", err)
	}
	defer tr.Close()

	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for !tr.Exited() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !tr.Exited() {
		t.Error("subprocess survived context cancellation")
	}
}

// T-1516: a call against a server that never answers aborts on the context.
func TestStdioTransportCallHonorsContext(t *testing.T) {
	// `cat` reads stdin and echoes bytes back verbatim, so a JSON-RPC request is
	// never answered with a valid response — a stand-in for a hung server.
	tr, err := NewStdio(context.Background(), StdioConfig{Command: "cat"})
	if err != nil {
		t.Skipf("cat unavailable: %v", err)
	}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := tr.Call(ctx, "initialize", nil); err == nil {
		t.Fatal("a hung server should fail the call")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("call took %v to abort", elapsed)
	}
}

// T-1519: a command that does not exist is an error at start, not at first call.
func TestStdioTransportMissingCommand(t *testing.T) {
	if _, err := NewStdio(context.Background(),
		StdioConfig{Command: "openplus-no-such-binary-xyz"}); err == nil {
		t.Fatal("a missing command should fail to start")
	}
}
