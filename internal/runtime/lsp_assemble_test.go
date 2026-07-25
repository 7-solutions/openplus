package runtime

import (
	"testing"
)

// lspEnabledConfig carries a real provider block: the non-fake sessions below
// go through full provider selection before reaching the LSP wiring.
const lspEnabledConfig = `{
  "instructions": ["AGENTS.md"],
  "model": "anthropic/claude-sonnet-5",
  "provider": {"anthropic": {"options": {"apiKey": "{env:TEST_ANTHROPIC_KEY}"}}},
  "lsp": {
    "enabled": true,
    "servers": {".go": {"command": "definitely-not-a-real-binary"}}
  }
}`

const lspDisabledConfig = `{
  "instructions": ["AGENTS.md"],
  "model": "local/m",
  "lsp": {
    "servers": {".go": {"command": "definitely-not-a-real-binary"}}
  }
}`

var lspToolNames = []string{
	"lsp_diagnostics", "lsp_hover", "lsp_definition", "lsp_symbols", "lsp_references",
}

func hasTool(s *Session, name string) bool {
	_, ok := s.Tools.Get(name)
	return ok
}

// TestLSPToolsAbsentWhenDisabled is the opt-in guarantee at the tool surface:
// no lsp_* tool is advertised to the model unless LSP is configured.
func TestLSPToolsAbsentWhenDisabled(t *testing.T) {
	s, err := Assemble(project(t, lspDisabledConfig), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	for _, n := range lspToolNames {
		if hasTool(s, n) {
			t.Errorf("%s registered while LSP is disabled", n)
		}
	}
	if s.LanguageService != nil {
		t.Error("LanguageService set while LSP is disabled")
	}
}

// TestLSPToolsAbsentWithNoConfig: the common case — no lsp block at all.
func TestLSPToolsAbsentWithNoConfig(t *testing.T) {
	s, err := Assemble(project(t, `{"model":"local/m"}`), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	for _, n := range lspToolNames {
		if hasTool(s, n) {
			t.Errorf("%s registered with no lsp config", n)
		}
	}
}

// TestLSPToolsAbsentInFakeMode is the hermetic-test rule (the 0025 lesson): a
// fake session must never wire something that can spawn a process, even when
// the config asks for it.
func TestLSPToolsAbsentInFakeMode(t *testing.T) {
	s, err := Assemble(project(t, lspEnabledConfig), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	for _, n := range lspToolNames {
		if hasTool(s, n) {
			t.Errorf("%s registered in a fake session", n)
		}
	}
	if s.LanguageService != nil {
		t.Error("LanguageService set in a fake session")
	}
}

// TestLSPToolsRegisteredWhenEnabled: with LSP on and a real (non-fake) session,
// all five tools reach the model. The configured binary does not exist, which
// is deliberate — registration must not depend on a server actually starting,
// since servers start lazily on first use.
func TestLSPToolsRegisteredWhenEnabled(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_KEY", "sk-ant-test")
	s, err := Assemble(project(t, lspEnabledConfig), Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if s.LanguageService == nil {
		t.Fatal("LanguageService not wired while LSP is enabled")
	}
	for _, n := range lspToolNames {
		if !hasTool(s, n) {
			t.Errorf("%s not registered while LSP is enabled", n)
		}
	}
}

// TestLSPToolSchemasReachTheModel: registration is only useful if the schema
// list the provider receives includes them.
func TestLSPToolSchemasReachTheModel(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_KEY", "sk-ant-test")
	s, err := Assemble(project(t, lspEnabledConfig), Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	got := map[string]bool{}
	for _, sc := range s.ToolSchemas {
		got[sc.Name] = true
	}
	for _, n := range lspToolNames {
		if !got[n] {
			t.Errorf("%s missing from ToolSchemas", n)
		}
	}
}
