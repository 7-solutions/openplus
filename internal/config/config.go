// Package config loads the OpenCode-compatible configuration surface
// (opencode.json + AGENTS.md) into neutral config values (ADR-0001).
//
// It reads only the parts the rest of the system needs to select a provider
// adapter and resolve its options: instructions, the default model string, the
// provider table (options.baseURL/apiKey with {env:VAR} expansion), and the
// permission rules. No provider-specific type escapes this package.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// Config is the parsed, env-expanded configuration.
type Config struct {
	// Instructions is the list of project-instruction files (e.g. AGENTS.md)
	// whose contents the agent prepends to the system prompt.
	Instructions []string

	// Model is the default "<provider>/<model>" string. The prefix selects the
	// provider adapter (ADR-0005); the suffix is the provider-native model id.
	Model string

	// Providers maps a provider id (anthropic, openai, local, custom…) to its
	// resolved options. The id is the opencode.json key under "provider".
	Providers map[string]Provider

	// Permission is the parsed permission ruleset (ADR-0007).
	Permission Permission

	// Embedder configures the embedding model used for memory (ADR-0004).
	// Absent means memory is disabled.
	Embedder Embedder

	// Memory configures the on-disk memory store (ADR-0003).
	Memory Memory

	// Context configures the token budget and window (ADR-0008).
	Context Context

	// Coordination configures the subagent symbol coordinator (change 0013).
	Coordination Coordination
}

// Embedder is the embedding-model configuration. Memory is only enabled when
// Configured reports true.
type Embedder struct {
	Model   string
	BaseURL string
	APIKey  string
	// Timeout bounds a single Embed call when no caller-supplied http.Client
	// is set. Zero means embed.DefaultTimeout (30s).
	Timeout time.Duration
}

// Configured reports whether enough is set to embed. A model is required: it
// names the vector space, and a store built against the wrong one is worse than
// no store at all.
func (e Embedder) Configured() bool { return e.Model != "" }

// applyEnvOverrides layers OPENPLUS_EMBED_* env vars on top of the file values.
// Precedence: env wins if non-empty, file is the default. Called by Load after
// the file is parsed; not part of the public surface because the override only
// matters at config-resolution time, not after a caller mutates Embedder.
func (e *Embedder) applyEnvOverrides() {
	if v := os.Getenv("OPENPLUS_EMBED_MODEL"); v != "" {
		e.Model = v
	}
	if v := os.Getenv("OPENPLUS_EMBED_BASE_URL"); v != "" {
		e.BaseURL = v
	}
	if v := os.Getenv("OPENPLUS_EMBED_API_KEY"); v != "" {
		e.APIKey = v
	}
}

// Memory is the memory-store configuration.
type Memory struct {
	// Path is the database file, relative to the project root when not absolute.
	Path string
	// AutoOpen creates the file (and its parent directory) if it does not
	// exist. Default false: a missing path is a configuration error, not a
	// silent side effect. Set true in opencode.json to opt in.
	AutoOpen bool
	// MaxEntries caps the stored chunks; oldest are pruned first on each
	// write. Zero means unbounded.
	MaxEntries int
}

// applyEnvOverrides layers OPENPLUS_MEMORY_PATH on top of the file value.
// Same precedence model as the embedder env overrides (T-402): env wins
// when non-empty, file is the default. Called by Load after the file is
// parsed.
func (m *Memory) applyEnvOverrides() {
	if v := os.Getenv("OPENPLUS_MEMORY_PATH"); v != "" {
		m.Path = v
	}
}

// Context is the context-window configuration (ADR-0008).
type Context struct {
	// Budget is the soft token ceiling for assembled context.
	Budget int
	// Window is the model's full context window, used for the checkpoint
	// high-water mark.
	Window int
}

// Coordination is the subagent symbol-coordinator configuration (change 0013).
type Coordination struct {
	// Backend selects the coordinator: "native" (default, ships with OpenPlus),
	// "grit" (external binary), or "none" (disable coordinated fan-out).
	Backend string
}

// Provider is one configured model backend.
type Provider struct {
	// ID is the opencode.json key (anthropic, openai, local, …).
	ID string

	// Name is the optional human label (opencode.json provider.<id>.name).
	Name string

	// BaseURL is options.baseURL after {env:VAR} expansion. Empty means the
	// provider's default endpoint (e.g. api.anthropic.com for the anthropic id).
	BaseURL string

	// APIKey is options.apiKey after {env:VAR} expansion.
	APIKey string

	// Models maps the provider-native model id to its display name.
	Models map[string]Model
}

// Model is one model entry under a provider.
type Model struct {
	Name string
}

// Permission is the parsed permission ruleset. Tools maps a tool name to its
// decision string ("allow"/"ask"/"deny"); Paths maps a glob path pattern to its
// decision string. The full rule engine (glob matching, last-match-wins,
// forced-ask timeout) is T-022 — this struct only holds the raw parsed rules.
type Permission struct {
	Tools map[string]string
	Paths map[string]string
}

// Load reads and parses the opencode.json file at path, expanding {env:VAR}
// references in option values. Returns an error if the file cannot be read or
// is not valid JSON. Unknown fields are ignored (config is forward-compatible).
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var doc rawConfig
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg := &Config{
		Instructions: doc.Instructions,
		Model:        doc.Model,
		Providers:    make(map[string]Provider, len(doc.Provider)),
		Permission:   parsePermission(doc.Permission),
		Embedder: Embedder{
			Model:   doc.Embedder.Model,
			BaseURL: expandEnv(doc.Embedder.BaseURL),
			APIKey:  expandEnv(doc.Embedder.APIKey),
			Timeout: doc.Embedder.Timeout,
		},
		Memory: Memory{
			Path:       doc.Memory.Path,
			AutoOpen:   doc.Memory.AutoOpen,
			MaxEntries: doc.Memory.MaxEntries,
		},
		Context: Context{
			Budget: doc.Context.Budget,
			Window: doc.Context.Window,
		},
		Coordination: Coordination{Backend: doc.Coordination.Backend},
	}
	cfg.Embedder.applyEnvOverrides()
	cfg.Memory.applyEnvOverrides()

	for id, rp := range doc.Provider {
		cfg.Providers[id] = Provider{
			ID:      id,
			Name:    rp.Name,
			BaseURL: expandEnv(rp.Options.BaseURL),
			APIKey:  expandEnv(rp.Options.APIKey),
			Models:  parseModels(rp.Models),
		}
	}

	return cfg, nil
}

// ParseModel splits a "<provider>/<model>" string into its provider id and
// provider-native model id. The prefix selects the adapter (ADR-0005). Returns
// an error if the string has no "/" separator or is empty.
func ParseModel(model string) (providerID, modelID string, err error) {
	if model == "" {
		return "", "", fmt.Errorf("config: empty model")
	}
	idx := strings.IndexByte(model, '/')
	if idx <= 0 || idx == len(model)-1 {
		return "", "", fmt.Errorf("config: model %q must be <provider>/<model>", model)
	}
	return model[:idx], model[idx+1:], nil
}

// ProviderFor resolves the configured Provider for a "<provider>/<model>"
// string, selecting by prefix. Returns an error if the prefix has no provider
// configured.
func (c *Config) ProviderFor(model string) (Provider, error) {
	providerID, _, err := ParseModel(model)
	if err != nil {
		return Provider{}, err
	}
	p, ok := c.Providers[providerID]
	if !ok {
		return Provider{}, fmt.Errorf("config: no provider configured for %q", providerID)
	}
	return p, nil
}

// --- raw (on-the-wire) shapes ---

type rawConfig struct {
	Instructions []string                   `json:"instructions"`
	Model        string                     `json:"model"`
	Provider     map[string]rawProvider     `json:"provider"`
	Permission   map[string]json.RawMessage `json:"permission"`
	Embedder     struct {
		Model   string        `json:"model"`
		BaseURL string        `json:"baseURL"`
		APIKey  string        `json:"apiKey"`
		Timeout time.Duration `json:"timeout"`
	} `json:"embedder"`
	Memory struct {
		Path       string `json:"path"`
		AutoOpen   bool   `json:"autoOpen"`
		MaxEntries int    `json:"maxEntries"`
	} `json:"memory"`
	Context struct {
		Budget int `json:"budget"`
		Window int `json:"window"`
	} `json:"context"`
	Coordination struct {
		Backend string `json:"backend"`
	} `json:"coordination"`
}

type rawProvider struct {
	Name    string `json:"name"`
	Options struct {
		BaseURL string `json:"baseURL"`
		APIKey  string `json:"apiKey"`
	} `json:"options"`
	Models map[string]rawModel `json:"models"`
}

type rawModel struct {
	Name string `json:"name"`
}

func parseModels(raw map[string]rawModel) map[string]Model {
	if raw == nil {
		return nil
	}
	out := make(map[string]Model, len(raw))
	for id, m := range raw {
		out[id] = Model{Name: m.Name}
	}
	return out
}

// parsePermission splits permission rules into tool-name rules and path-glob
// rules. A scalar value ("ask") is a tool rule keyed by that field; an object
// value maps glob keys to decisions (the opencode.json "external_directory"
// shape). Unknown shapes are skipped, not fatal.
func parsePermission(raw map[string]json.RawMessage) Permission {
	p := Permission{Tools: map[string]string{}, Paths: map[string]string{}}
	for key, val := range raw {
		var s string
		if err := json.Unmarshal(val, &s); err == nil {
			p.Tools[key] = s
			continue
		}
		var globs map[string]string
		if err := json.Unmarshal(val, &globs); err == nil {
			for g, d := range globs {
				p.Paths[g] = d
			}
		}
	}
	return p
}

// envTokenRe matches a single {env:VAR_NAME} reference.
var envTokenRe = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces every {env:VAR} in s with os.Getenv("VAR"). A missing env
// var expands to the empty string. Non-matching text is left untouched.
func expandEnv(s string) string {
	return envTokenRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[len("{env:") : len(m)-len("}")]
		return os.Getenv(name)
	})
}
