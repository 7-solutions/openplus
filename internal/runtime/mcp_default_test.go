package runtime

import (
	"testing"

	"github.com/7solutions/openplus/internal/config"
)

// TestApplyDefaultMCP_InjectsWhenEmpty: a Config with no MCP servers gains
// exactly one Context7 entry on the http transport at the canonical endpoint.
func TestApplyDefaultMCP_InjectsWhenEmpty(t *testing.T) {
	t.Setenv("OPENPLUS_DEFAULT_DOCS", "") // not disabled
	t.Setenv("CONTEXT7_API_KEY", "")      // no key → no header

	cfg := &config.Config{}
	applyDefaultMCP(cfg)

	if len(cfg.MCP) != 1 {
		t.Fatalf("len(MCP) = %d, want 1", len(cfg.MCP))
	}
	srv, ok := cfg.MCP[defaultContext7Name]
	if !ok {
		t.Fatalf("missing %q server; have %v", defaultContext7Name, cfg.MCP)
	}
	if srv.Transport != config.MCPTransportHTTP {
		t.Errorf("transport = %q, want %q", srv.Transport, config.MCPTransportHTTP)
	}
	if srv.URL != defaultContext7Endpoint {
		t.Errorf("URL = %q, want %q", srv.URL, defaultContext7Endpoint)
	}
	if len(srv.Headers) != 0 {
		t.Errorf("headers = %v, want empty when CONTEXT7_API_KEY unset", srv.Headers)
	}
}

// TestApplyDefaultMCP_NoOpWhenNonEmpty: any user MCP config suppresses the
// default — auto-inject-if-empty only.
func TestApplyDefaultMCP_NoOpWhenNonEmpty(t *testing.T) {
	t.Setenv("OPENPLUS_DEFAULT_DOCS", "")
	t.Setenv("CONTEXT7_API_KEY", "")

	cfg := &config.Config{
		MCP: map[string]config.MCPServer{
			"mine": {Name: "mine", Transport: config.MCPTransportHTTP, URL: "http://example/mcp"},
		},
	}
	applyDefaultMCP(cfg)

	if _, ok := cfg.MCP[defaultContext7Name]; ok {
		t.Errorf("default leaked into a config that already had user servers: %v", cfg.MCP)
	}
	if len(cfg.MCP) != 1 {
		t.Errorf("len(MCP) = %d, want 1 (untouched)", len(cfg.MCP))
	}
}

// TestApplyDefaultMCP_DisabledByEnv: the kill-switch suppresses injection even
// when MCP is empty.
func TestApplyDefaultMCP_DisabledByEnv(t *testing.T) {
	for _, val := range []string{"0", "false", "off"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv("OPENPLUS_DEFAULT_DOCS", val)
			t.Setenv("CONTEXT7_API_KEY", "")

			cfg := &config.Config{}
			applyDefaultMCP(cfg)

			if len(cfg.MCP) != 0 {
				t.Errorf("OPENPLUS_DEFAULT_DOCS=%q: len(MCP) = %d, want 0", val, len(cfg.MCP))
			}
		})
	}
}

// TestDefaultContext7Server_APIKeyHeader: a set CONTEXT7_API_KEY is forwarded as
// the Context7 auth header; absent key → no header.
func TestDefaultContext7Server_APIKeyHeader(t *testing.T) {
	t.Run("key set", func(t *testing.T) {
		t.Setenv("CONTEXT7_API_KEY", "sk-test-123")
		srv := defaultContext7Server()
		if got := srv.Headers["CONTEXT7_API_KEY"]; got != "sk-test-123" {
			t.Errorf("CONTEXT7_API_KEY header = %q, want %q", got, "sk-test-123")
		}
	})
	t.Run("key unset", func(t *testing.T) {
		t.Setenv("CONTEXT7_API_KEY", "")
		srv := defaultContext7Server()
		if _, ok := srv.Headers["CONTEXT7_API_KEY"]; ok {
			t.Errorf("CONTEXT7_API_KEY header present when env unset: %v", srv.Headers)
		}
	})
}
