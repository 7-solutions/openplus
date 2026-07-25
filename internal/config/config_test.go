package config

import (
	"os"
	"path/filepath"
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
