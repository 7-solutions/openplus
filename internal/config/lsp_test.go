package config

import "testing"

func TestLoadParsesLSPBlock(t *testing.T) {
	p := writeFixture(t, "opencode.json", `{
  "model": "local/m",
  "lsp": {
    "enabled": true,
    "servers": {
      ".go": {"command": "gopls"},
      ".ts": {"command": "typescript-language-server", "args": ["--stdio"]}
    }
  }
}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.LSP.Enabled {
		t.Error("lsp.enabled = false, want true")
	}
	if len(cfg.LSP.Servers) != 2 {
		t.Fatalf("len(servers) = %d, want 2", len(cfg.LSP.Servers))
	}
	if got := cfg.LSP.Servers[".go"].Command; got != "gopls" {
		t.Errorf(".go command = %q, want gopls", got)
	}
	ts := cfg.LSP.Servers[".ts"]
	if ts.Command != "typescript-language-server" {
		t.Errorf(".ts command = %q", ts.Command)
	}
	if len(ts.Args) != 1 || ts.Args[0] != "--stdio" {
		t.Errorf(".ts args = %v, want [--stdio]", ts.Args)
	}
}

func TestLSPAbsentIsZero(t *testing.T) {
	p := writeFixture(t, "opencode.json", `{"model": "local/m"}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LSP.Enabled {
		t.Error("absent lsp block must not be enabled")
	}
	if len(cfg.LSP.Servers) != 0 {
		t.Errorf("absent lsp block has servers: %v", cfg.LSP.Servers)
	}
}

// TestLSPConfigured: opt-in requires BOTH the flag and at least one server —
// enabling with no servers would spawn nothing and should not read as active.
func TestLSPConfigured(t *testing.T) {
	cases := []struct {
		name string
		lsp  LSP
		want bool
	}{
		{"zero", LSP{}, false},
		{"enabled but no servers", LSP{Enabled: true}, false},
		{"servers but not enabled", LSP{Servers: map[string]LSPServer{".go": {Command: "gopls"}}}, false},
		{"enabled with a server", LSP{Enabled: true, Servers: map[string]LSPServer{".go": {Command: "gopls"}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lsp.Configured(); got != tc.want {
				t.Errorf("Configured() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLSPServerForExtension: routing is by file extension, and an unknown
// extension resolves to nothing rather than a zero-value server that would be
// spawned as an empty command.
func TestLSPServerForExtension(t *testing.T) {
	l := LSP{Enabled: true, Servers: map[string]LSPServer{".go": {Command: "gopls"}}}

	if srv, ok := l.ServerFor("main.go"); !ok || srv.Command != "gopls" {
		t.Errorf("ServerFor(main.go) = %+v, %v; want gopls", srv, ok)
	}
	if _, ok := l.ServerFor("README.md"); ok {
		t.Error("ServerFor(README.md) resolved a server; want none")
	}
	if _, ok := l.ServerFor("noext"); ok {
		t.Error("ServerFor(noext) resolved a server; want none")
	}
}
