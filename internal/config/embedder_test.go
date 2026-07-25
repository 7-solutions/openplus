package config

import (
	"os"
	"testing"
)

func TestLoadParsesEmbedderBlock(t *testing.T) {
	t.Setenv("EMBED_KEY", "sk-embed")
	p := writeFixture(t, "opencode.json", `{
  "model": "local/qwen2.5-coder",
  "embedder": {
    "model": "nomic-embed-text",
    "baseURL": "http://localhost:11434/v1",
    "apiKey": "{env:EMBED_KEY}"
  }
}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.Model != "nomic-embed-text" {
		t.Errorf("embedder model = %q", cfg.Embedder.Model)
	}
	if cfg.Embedder.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("embedder baseURL = %q", cfg.Embedder.BaseURL)
	}
	if cfg.Embedder.APIKey != "sk-embed" {
		t.Errorf("embedder apiKey not env-expanded: %q", cfg.Embedder.APIKey)
	}
}

func TestEmbedderAbsentIsZero(t *testing.T) {
	p := writeFixture(t, "opencode.json", `{"model": "local/m"}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.Model != "" {
		t.Fatalf("expected a zero Embedder, got %+v", cfg.Embedder)
	}
	if cfg.Embedder.Configured() {
		t.Error("an absent embedder must not report as configured")
	}
}

func TestEmbedderConfiguredRequiresModel(t *testing.T) {
	// a baseURL alone is not enough to embed — the model names the vector space
	p := writeFixture(t, "opencode.json", `{"embedder": {"baseURL": "http://x/v1"}}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.Configured() {
		t.Error("an embedder without a model must not report as configured")
	}
}

func TestEmbedderConfiguredWithModel(t *testing.T) {
	p := writeFixture(t, "opencode.json", `{"embedder": {"model": "nomic-embed-text"}}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Embedder.Configured() {
		t.Error("an embedder with a model should report as configured")
	}
}

func TestLoadParsesMemoryPath(t *testing.T) {
	p := writeFixture(t, "opencode.json", `{"memory": {"path": ".openplus/memory.db"}}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Memory.Path != ".openplus/memory.db" {
		t.Errorf("memory path = %q", cfg.Memory.Path)
	}
}

func TestLoadParsesContextBudget(t *testing.T) {
	p := writeFixture(t, "opencode.json", `{"context": {"budget": 120000, "window": 200000}}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Context.Budget != 120000 {
		t.Errorf("budget = %d", cfg.Context.Budget)
	}
	if cfg.Context.Window != 200000 {
		t.Errorf("window = %d", cfg.Context.Window)
	}
}

// --- Change 0004 / T-400..T-402: embedder env overrides ---

// TestEmbedderEnvModelOverride: OPENPLUS_EMBED_MODEL wins over the file.
func TestEmbedderEnvModelOverride(t *testing.T) {
	t.Setenv("OPENPLUS_EMBED_MODEL", "env-model")
	p := writeFixture(t, "opencode.json", `{
  "embedder": {"model": "file-model"}
}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.Model != "env-model" {
		t.Errorf("embedder model = %q, want env-model", cfg.Embedder.Model)
	}
}

// TestEmbedderEnvBaseURLOverride: OPENPLUS_EMBED_BASE_URL wins over the file.
func TestEmbedderEnvBaseURLOverride(t *testing.T) {
	t.Setenv("OPENPLUS_EMBED_BASE_URL", "http://env-host/v1")
	p := writeFixture(t, "opencode.json", `{
  "embedder": {"model": "m", "baseURL": "http://file-host/v1"}
}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.BaseURL != "http://env-host/v1" {
		t.Errorf("embedder baseURL = %q, want http://env-host/v1", cfg.Embedder.BaseURL)
	}
}

// TestEmbedderEnvAPIKeyOverride: OPENPLUS_EMBED_API_KEY wins over the file.
func TestEmbedderEnvAPIKeyOverride(t *testing.T) {
	t.Setenv("OPENPLUS_EMBED_API_KEY", "sk-env")
	p := writeFixture(t, "opencode.json", `{
  "embedder": {"model": "m", "apiKey": "sk-file"}
}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.APIKey != "sk-env" {
		t.Errorf("embedder apiKey = %q, want sk-env", cfg.Embedder.APIKey)
	}
}

// TestEmbedderEnvAbsentLeavesFileAlone: with no OPENPLUS_EMBED_* env set,
// the parsed file values flow through untouched.
func TestEmbedderEnvAbsentLeavesFileAlone(t *testing.T) {
	for _, k := range []string{"OPENPLUS_EMBED_MODEL", "OPENPLUS_EMBED_BASE_URL", "OPENPLUS_EMBED_API_KEY"} {
		old, had := os.LookupEnv(k)
		os.Unsetenv(k)
		if had {
			t.Setenv(k, old)
		}
	}
	p := writeFixture(t, "opencode.json", `{
  "embedder": {"model": "file-model", "baseURL": "http://f/v1", "apiKey": "sk-file"}
}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Embedder.Model != "file-model" || cfg.Embedder.BaseURL != "http://f/v1" || cfg.Embedder.APIKey != "sk-file" {
		t.Errorf("env-absent path mutated embedder: %+v", cfg.Embedder)
	}
}
