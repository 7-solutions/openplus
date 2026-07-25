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
	"path/filepath"
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

	// TUI configures the front-end's appearance (change 0017).
	TUI TUI

	// Max configures Max Mode, the best-of-N sampler (change 0016).
	Max Max

	// MCP maps a server name to its declaration (change 0015). Empty means no
	// MCP servers. The name prefixes that server's tool names.
	MCP map[string]MCPServer

	// LSP configures the language servers behind the LanguageService port
	// (change 0026, ADR-0017). Opt-in: absent means no server is ever spawned.
	LSP LSP
}

// LSP is the language-server configuration (ADR-0017). It is opt-in: a session
// spawns nothing unless Configured reports true.
type LSP struct {
	// Enabled turns the LanguageService on. Off by default — a coding agent
	// must not start subprocesses the user did not ask for.
	Enabled bool

	// Servers maps a file extension (including the dot, e.g. ".go") to the
	// language server that handles it.
	Servers map[string]LSPServer
}

// LSPServer is one language server: the command to run and its arguments. The
// user supplies the binary; OpenPlus never downloads a toolchain.
type LSPServer struct {
	Command string
	Args    []string
}

// Configured reports whether LSP should actually run. Both halves are required:
// enabling with no servers would spawn nothing, and servers without the flag is
// a declaration the user has not switched on.
func (l LSP) Configured() bool { return l.Enabled && len(l.Servers) > 0 }

// ServerFor resolves the language server for a file path by extension. The
// second result is false when no server is declared for it — callers must not
// treat a zero-value LSPServer as runnable, since its empty Command would be
// spawned as garbage.
func (l LSP) ServerFor(path string) (LSPServer, bool) {
	ext := filepath.Ext(path)
	if ext == "" {
		return LSPServer{}, false
	}
	srv, ok := l.Servers[ext]
	if !ok || srv.Command == "" {
		return LSPServer{}, false
	}
	return srv, true
}

// MCP transport names. Anything else is a config error: silently defaulting would
// start a server the user did not describe.
const (
	MCPTransportStdio = "stdio"
	MCPTransportHTTP  = "http"
)

// MCPServer is one declared MCP server (change 0015, ADR-0010). Stdio fields and
// HTTP fields are mutually exclusive in practice; Transport selects which apply.
//
// Note what this is: a stdio server is an arbitrary executable and an http server
// an arbitrary endpoint, both named by the user. Their tools go through the
// PolicyGate like any other.
type MCPServer struct {
	// Name is the config key, carried here so an error can name the server.
	Name string
	// Transport is MCPTransportStdio or MCPTransportHTTP.
	Transport string

	// Command, Args, Env and Dir describe a stdio subprocess.
	Command string
	Args    []string
	Env     map[string]string
	Dir     string

	// URL and Headers describe a streamable-HTTP endpoint.
	URL     string
	Headers map[string]string
}

// Max is the Max Mode configuration (change 0016, ADR-0011). Max Mode is opt-in
// per invocation; this only sets its defaults.
type Max struct {
	// Samples is the default N for /max. Zero means orchestrate.DefaultSamples;
	// a value above the cap is clamped at use, with the clamp reported.
	Samples int
	// Model is the judge model id ("<provider>/<model>"). Empty judges with the
	// session's model — an independent judge is better, but requiring one would
	// make Max Mode unusable on a single-model setup.
	Model string
}

// TUI is the front-end configuration (change 0017, ADR-0012).
type TUI struct {
	// Theme names the initial palette ("default", "deutan", "protan",
	// "tritan"). Empty means the front-end's default; an unknown name falls
	// back to it with a warning rather than failing the session, because an
	// appearance setting must never block work.
	Theme string
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
		TUI:          TUI{Theme: doc.TUI.Theme},
		Max:          Max{Samples: doc.Max.Samples, Model: doc.Max.Model},
	}
	cfg.Embedder.applyEnvOverrides()
	cfg.Memory.applyEnvOverrides()

	servers, err := parseMCP(doc.MCP)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	cfg.MCP = servers

	cfg.LSP = parseLSP(doc.LSP.Enabled, doc.LSP.Servers)

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

// SetTUITheme persists theme as tui.theme in the config file at path, leaving
// every other key intact — the file belongs to the user, so this is a targeted
// patch and not a rewrite from the parsed Config (which would drop keys this
// package does not model, e.g. "$schema"). A missing file is created: a fresh
// project has no opencode.json yet, and picking a theme should not require one.
//
// Keys are re-emitted in sorted order (Go marshals maps sorted); content is
// preserved, formatting is not.
func SetTUITheme(path, theme string) error {
	doc := map[string]json.RawMessage{}
	mode := os.FileMode(0o600)

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("config: parse %s: %w", path, err)
		}
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("config: read %s: %w", path, err)
	}

	// Merge into any existing tui object so a sibling tui setting survives.
	section := map[string]json.RawMessage{}
	if cur, ok := doc["tui"]; ok {
		if err := json.Unmarshal(cur, &section); err != nil {
			return fmt.Errorf("config: %s: \"tui\" is not an object: %w", path, err)
		}
	}
	name, err := json.Marshal(theme)
	if err != nil {
		return fmt.Errorf("config: encode theme: %w", err)
	}
	section["theme"] = name

	encoded, err := json.Marshal(section)
	if err != nil {
		return fmt.Errorf("config: encode tui section: %w", err)
	}
	doc["tui"] = encoded

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode %s: %w", path, err)
	}
	out = append(out, '\n')

	// Write via a temp file in the same directory and rename: a crash or a full
	// disk must not leave the user with a truncated opencode.json. Concurrent
	// writers are still last-one-wins, which is acceptable for a hand-edited
	// settings file but means an interactive edit racing /theme can be lost.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".opencode-*.json")
	if err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("config: chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: replace %s: %w", path, err)
	}
	return nil
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
	TUI struct {
		Theme string `json:"theme"`
	} `json:"tui"`
	Max struct {
		Samples int    `json:"samples"`
		Model   string `json:"model"`
	} `json:"max"`
	MCP map[string]rawMCPServer `json:"mcp"`
	LSP struct {
		Enabled bool                    `json:"enabled"`
		Servers map[string]rawLSPServer `json:"servers"`
	} `json:"lsp"`
}

type rawLSPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type rawMCPServer struct {
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	Dir       string            `json:"dir"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
}

// parseMCP validates every declared server and expands {env:VAR} in the fields
// that carry secrets or paths. An incomplete or unknown declaration is an error
// naming the server: starting a subprocess the user did not fully describe, or
// silently skipping one they did, are both worse than refusing to load.
func parseMCP(raw map[string]rawMCPServer) (map[string]MCPServer, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]MCPServer, len(raw))
	for name, rs := range raw {
		s := MCPServer{
			Name:      name,
			Transport: rs.Transport,
			Command:   expandEnv(rs.Command),
			Dir:       rs.Dir,
			URL:       expandEnv(rs.URL),
			Env:       expandEnvMap(rs.Env),
			Headers:   expandEnvMap(rs.Headers),
		}
		for _, a := range rs.Args {
			s.Args = append(s.Args, expandEnv(a))
		}

		switch s.Transport {
		case MCPTransportStdio:
			if s.Command == "" {
				return nil, fmt.Errorf("mcp server %q: stdio transport needs a \"command\"", name)
			}
		case MCPTransportHTTP:
			if s.URL == "" {
				return nil, fmt.Errorf("mcp server %q: http transport needs a \"url\"", name)
			}
		case "":
			return nil, fmt.Errorf("mcp server %q: missing \"transport\" (%q or %q)",
				name, MCPTransportStdio, MCPTransportHTTP)
		default:
			return nil, fmt.Errorf("mcp server %q: unknown transport %q (want %q or %q)",
				name, s.Transport, MCPTransportStdio, MCPTransportHTTP)
		}
		out[name] = s
	}
	return out, nil
}

// parseLSP converts the raw lsp block (change 0026, ADR-0017). Unlike parseMCP
// it does not reject a malformed entry: a server with no command is dropped
// rather than failing the load. LSP is an optional enhancement, so one bad
// entry must not make the whole config unloadable — ServerFor treats a missing
// extension as "no language server", which is a working state.
func parseLSP(enabled bool, raw map[string]rawLSPServer) LSP {
	out := LSP{Enabled: enabled}
	if len(raw) == 0 {
		return out
	}
	out.Servers = make(map[string]LSPServer, len(raw))
	for ext, rs := range raw {
		cmd := expandEnv(rs.Command)
		if cmd == "" {
			continue
		}
		s := LSPServer{Command: cmd}
		for _, a := range rs.Args {
			s.Args = append(s.Args, expandEnv(a))
		}
		out.Servers[ext] = s
	}
	return out
}

// expandEnvMap expands {env:VAR} in every value of a string map.
func expandEnvMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = expandEnv(v)
	}
	return out
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
