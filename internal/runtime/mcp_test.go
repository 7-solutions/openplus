package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/provider"
)

// buildMCPEchoServer compiles the MCP test server used by internal/mcp and
// returns its path, so the wiring is exercised against a real subprocess server.
func buildMCPEchoServer(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "mcp", "testdata", "echoserver", "main.go")
	bin := filepath.Join(t.TempDir(), "echoserver")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build mcp test server: %v\n%s", err, out)
	}
	return bin
}

// mcpProject writes a project whose config declares one stdio MCP server.
func mcpProject(t *testing.T, extraPermission string) string {
	t.Helper()
	bin := buildMCPEchoServer(t)
	perm := `"permission": {"bash": "ask"` + extraPermission + `}`
	cfg := fmt.Sprintf(`{
  "model": "fake/fake",
  %s,
  "mcp": {"ci": {"transport": "stdio", "command": %q}}
}`, perm, bin)
	return project(t, cfg)
}

// T-1520: a configured server's tools are registered alongside the builtins and
// are callable through the registry.
func TestAssembleRegistersMCPTools(t *testing.T) {
	s, err := Assemble(mcpProject(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	defer s.Close()

	tl, ok := s.Tools.Get("ci.echo")
	if !ok {
		names := make([]string, 0)
		for _, x := range s.Tools.All() {
			names = append(names, x.Name())
		}
		t.Fatalf("ci.echo not registered; have %v", names)
	}
	// The builtins are still there.
	if _, ok := s.Tools.Get("read"); !ok {
		t.Error("registering MCP tools dropped the builtins")
	}
	// Its schema reached the model surface.
	var sawSchema bool
	for _, sc := range s.ToolSchemas {
		if sc.Name == "ci.echo" {
			sawSchema = true
		}
	}
	if !sawSchema {
		t.Error("ci.echo missing from ToolSchemas")
	}

	out, err := tl.Execute(context.Background(), json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "from subprocess" {
		t.Fatalf("Execute = %q", out)
	}
}

// T-1520: an MCP tool is gated exactly like a builtin — a permission rule written
// against its namespaced name decides.
func TestMCPToolIsPermissionGated(t *testing.T) {
	s, err := Assemble(mcpProject(t, `, "ci.echo": "deny"`), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	defer s.Close()

	got, err := s.Gate.Permit(context.Background(), provider.ToolCall{
		Name:  "ci.echo",
		Input: json.RawMessage(`{"text":"hi"}`),
	})
	if err != nil {
		t.Fatalf("Permit: %v", err)
	}
	if got != policy.Deny {
		t.Fatalf("gate decision = %v, want Deny", got)
	}
}

// T-1520: a server that cannot start is reported by name and skipped; the session
// still assembles, because one broken server must not cost the user their tools.
func TestAssembleMCPFailureIsNonFatal(t *testing.T) {
	root := project(t, `{
  "model": "fake/fake",
  "mcp": {"broken": {"transport": "stdio", "command": "openplus-no-such-binary-xyz"}}
}`)
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble should tolerate a broken server: %v", err)
	}
	defer s.Close()

	if len(s.MCPWarnings) == 0 {
		t.Fatal("a failed server should be reported")
	}
	if !strings.Contains(strings.Join(s.MCPWarnings, " "), "broken") {
		t.Errorf("warning should name the server: %v", s.MCPWarnings)
	}
	if _, ok := s.Tools.Get("read"); !ok {
		t.Error("builtins missing after a failed MCP server")
	}
}

// T-1515/T-1520: Close stops every started server, and is safe to call twice.
func TestSessionCloseStopsMCPServers(t *testing.T) {
	s, err := Assemble(mcpProject(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(s.MCPClients()) != 1 {
		t.Fatalf("clients = %d, want 1", len(s.MCPClients()))
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// A call after teardown fails rather than hanging.
	if tl, ok := s.Tools.Get("ci.echo"); ok {
		if _, err := tl.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
			t.Error("an MCP tool should fail after the session closed")
		}
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
