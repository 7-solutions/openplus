package config

import "testing"

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
