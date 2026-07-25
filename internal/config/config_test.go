package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture writes content to a temp file and returns its path.
func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

const fullFixture = `{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["AGENTS.md"],
  "model": "anthropic/claude-sonnet-5",
  "provider": {
    "anthropic": {
      "options": { "apiKey": "{env:ANTHROPIC_API_KEY}" }
    },
    "openai": {
      "options": { "apiKey": "{env:OPENAI_API_KEY}" }
    },
    "local": {
      "name": "Local (OpenAI-compatible)",
      "options": { "baseURL": "http://localhost:11434/v1", "apiKey": "ollama" },
      "models": {
        "qwen2.5-coder": { "name": "Qwen2.5 Coder (local)" }
      }
    }
  },
  "permission": {
    "bash": "ask",
    "write": "ask",
    "external_directory": { "/tmp/**": "ask", "/home/**": "allow" }
  }
}`

func TestLoadParsesProviders(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("OPENAI_API_KEY", "sk-test")
	p := writeFixture(t, "opencode.json", fullFixture)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.Instructions) != 1 || cfg.Instructions[0] != "AGENTS.md" {
		t.Fatalf("instructions = %v", cfg.Instructions)
	}
	if cfg.Model != "anthropic/claude-sonnet-5" {
		t.Fatalf("default model = %q", cfg.Model)
	}

	if got := cfg.Providers["anthropic"].APIKey; got != "sk-ant-test" {
		t.Fatalf("anthropic apiKey not expanded: %q", got)
	}
	if got := cfg.Providers["local"].BaseURL; got != "http://localhost:11434/v1" {
		t.Fatalf("local baseURL = %q", got)
	}
	if got := cfg.Providers["local"].Name; got != "Local (OpenAI-compatible)" {
		t.Fatalf("local name = %q", got)
	}
	if _, ok := cfg.Providers["local"].Models["qwen2.5-coder"]; !ok {
		t.Fatalf("local models missing qwen2.5-coder: %+v", cfg.Providers["local"].Models)
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("FOO_TOKEN", "abc123")
	cases := map[string]string{
		"{env:FOO_TOKEN}":           "abc123",
		"prefix-{env:FOO_TOKEN}-xz": "prefix-abc123-xz",
		"no-env":                    "no-env",
		"{env:MISSING_VAR}":         "",
	}
	for in, want := range cases {
		if got := expandEnv(in); got != want {
			t.Errorf("expandEnv(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseModel(t *testing.T) {
	cases := []struct {
		in         string
		prov, want string
		wantErr    bool
	}{
		{in: "anthropic/claude-sonnet-5", prov: "anthropic", want: "claude-sonnet-5"},
		{in: "local/qwen2.5-coder", prov: "local", want: "qwen2.5-coder"},
		{in: "anthropic", wantErr: true}, // no slash
		{in: "", wantErr: true},
	}
	for _, c := range cases {
		prov, model, err := ParseModel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseModel(%q): want error, got (%q,%q,nil)", c.in, prov, model)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseModel(%q): %v", c.in, err)
		}
		if prov != c.prov || model != c.want {
			t.Errorf("ParseModel(%q) = (%q,%q), want (%q,%q)", c.in, prov, model, c.prov, c.want)
		}
	}
}

func TestProviderForPrefixSelects(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "k")
	p := writeFixture(t, "opencode.json", fullFixture)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	prov, err := cfg.ProviderFor("anthropic/claude-sonnet-5")
	if err != nil {
		t.Fatalf("ProviderFor: %v", err)
	}
	if prov.ID != "anthropic" {
		t.Fatalf("provider id = %q", prov.ID)
	}

	if _, err := cfg.ProviderFor("unknown/model"); err == nil {
		t.Fatal("ProviderFor(unknown) should error")
	}
}

func TestLoadPermissionRules(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("OPENAI_API_KEY", "k")
	p := writeFixture(t, "opencode.json", fullFixture)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Permission.Tools["bash"] != "ask" {
		t.Fatalf("bash rule = %q", cfg.Permission.Tools["bash"])
	}
	if cfg.Permission.Paths["/tmp/**"] != "ask" {
		t.Fatalf("/tmp rule = %q", cfg.Permission.Paths["/tmp/**"])
	}
	if cfg.Permission.Paths["/home/**"] != "allow" {
		t.Fatalf("/home rule = %q", cfg.Permission.Paths["/home/**"])
	}
}

// T-1717: the optional "tui" section selects the initial palette. Absent means
// empty, which the front-end resolves to its default.
func TestLoadTUITheme(t *testing.T) {
	p := writeFixture(t, "opencode.json", `{"model":"a/b","tui":{"theme":"deutan"}}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TUI.Theme != "deutan" {
		t.Fatalf("TUI.Theme = %q, want deutan", cfg.TUI.Theme)
	}

	p2 := writeFixture(t, "opencode.json", `{"model":"a/b"}`)
	cfg2, err := Load(p2)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg2.TUI.Theme != "" {
		t.Fatalf("absent tui section should leave Theme empty, got %q", cfg2.TUI.Theme)
	}
}

// T-1718: /theme persists its choice. SetTUITheme rewrites only the tui.theme
// field — every other key in the file survives, because the file is the user's.
func TestSetTUITheme(t *testing.T) {
	p := writeFixture(t, "opencode.json", `{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-5",
  "permission": { "bash": "ask" }
}`)
	if err := SetTUITheme(p, "tritan"); err != nil {
		t.Fatalf("SetTUITheme: %v", err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load after set: %v", err)
	}
	if cfg.TUI.Theme != "tritan" {
		t.Fatalf("Theme = %q, want tritan", cfg.TUI.Theme)
	}
	if cfg.Model != "anthropic/claude-sonnet-5" {
		t.Fatalf("model lost: %q", cfg.Model)
	}
	if cfg.Permission.Tools["bash"] != "ask" {
		t.Fatalf("permission lost: %+v", cfg.Permission.Tools)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), "$schema") {
		t.Fatalf("unknown keys must survive; file = %s", raw)
	}

	// Switching again overwrites rather than duplicating.
	if err := SetTUITheme(p, "protan"); err != nil {
		t.Fatalf("SetTUITheme again: %v", err)
	}
	cfg, err = Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TUI.Theme != "protan" {
		t.Fatalf("Theme = %q, want protan", cfg.TUI.Theme)
	}

	// A missing file is created, not an error: a fresh project has no config yet.
	fresh := filepath.Join(t.TempDir(), "opencode.json")
	if err := SetTUITheme(fresh, "deutan"); err != nil {
		t.Fatalf("SetTUITheme(new file): %v", err)
	}
	cfg, err = Load(fresh)
	if err != nil {
		t.Fatalf("Load(new): %v", err)
	}
	if cfg.TUI.Theme != "deutan" {
		t.Fatalf("new file Theme = %q", cfg.TUI.Theme)
	}
}
