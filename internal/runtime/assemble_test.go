package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/embed"
	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/provider"
	"github.com/7solutions/openplus/internal/provider/anthropic"
	"github.com/7solutions/openplus/internal/provider/openaicompat"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// project writes a minimal opencode.json plus AGENTS.md and returns the root.
func project(t *testing.T, configJSON string) string {
	t.Helper()
	root := t.TempDir()
	if configJSON != "" {
		write(t, root, "opencode.json", configJSON)
	}
	write(t, root, "AGENTS.md", "House rule: the build is cgo-free.")
	return root
}

const anthropicConfig = `{
  "instructions": ["AGENTS.md"],
  "model": "anthropic/claude-sonnet-5",
  "provider": {"anthropic": {"options": {"apiKey": "{env:TEST_ANTHROPIC_KEY}"}}},
  "permission": {"bash": "ask", "write": "ask"}
}`

func TestAssembleSelectsAdapterFromConfig(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_KEY", "sk-ant-test")
	s, err := Assemble(project(t, anthropicConfig), Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	a, ok := s.Provider.(*anthropic.Adapter)
	if !ok {
		t.Fatalf("Provider = %T, want *anthropic.Adapter", s.Provider)
	}
	if a.APIKey != "sk-ant-test" {
		t.Errorf("resolved apiKey = %q", a.APIKey)
	}
}

func TestAssembleSelectsOpenAICompatWithBaseURL(t *testing.T) {
	cfg := `{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1", "apiKey": "ollama"}}}
}`
	s, err := Assemble(project(t, cfg), Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	oc, ok := s.Provider.(*openaicompat.Adapter)
	if !ok {
		t.Fatalf("Provider = %T, want *openaicompat.Adapter", s.Provider)
	}
	if oc.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("baseURL = %q", oc.BaseURL)
	}
}

// TestAssembleMissingCredentialFailsClearly is the spec scenario: no silent
// unauthenticated request, no nil-pointer panic.
func TestAssembleMissingCredentialFailsClearly(t *testing.T) {
	cfg := `{
  "model": "anthropic/claude-sonnet-5",
  "provider": {"anthropic": {"options": {"apiKey": "{env:DEFINITELY_UNSET_KEY}"}}}
}`
	_, err := Assemble(project(t, cfg), Options{})
	if err == nil {
		t.Fatal("expected an error when the API key cannot be resolved")
	}
	if !errors.Is(err, ErrMissingCredential) {
		t.Errorf("err = %v, want ErrMissingCredential", err)
	}
	if !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("error should name the provider: %v", err)
	}
}

func TestAssembleLocalEndpointNeedsNoCredential(t *testing.T) {
	// A local endpoint with a baseURL is legitimately keyless.
	cfg := `{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}}
}`
	if _, err := Assemble(project(t, cfg), Options{}); err != nil {
		t.Fatalf("a keyless local endpoint should assemble: %v", err)
	}
}

func TestAssembleFakeOptionNeedsNoConfig(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble with Fake: %v", err)
	}
	if _, ok := s.Provider.(*provider.Fake); !ok {
		t.Fatalf("Provider = %T, want *provider.Fake", s.Provider)
	}
}

func TestAssembleUnknownModelPrefixErrors(t *testing.T) {
	cfg := `{"model": "mystery/model", "provider": {"mystery": {"options": {"apiKey": "k"}}}}`
	if _, err := Assemble(project(t, cfg), Options{}); err == nil {
		t.Fatal("expected an error for a prefix with no adapter")
	}
}

func TestAssembleNoModelConfiguredErrors(t *testing.T) {
	cfg := `{"provider": {"anthropic": {"options": {"apiKey": "k"}}}}`
	_, err := Assemble(project(t, cfg), Options{})
	if err == nil {
		t.Fatal("expected an error when no model is configured")
	}
	if !errors.Is(err, ErrNoModel) {
		t.Errorf("err = %v, want ErrNoModel", err)
	}
}

func TestAssembleModelOptionOverridesConfig(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_KEY", "k")
	cfg := `{
  "model": "anthropic/claude-sonnet-5",
  "provider": {
    "anthropic": {"options": {"apiKey": "{env:TEST_ANTHROPIC_KEY}"}},
    "local": {"options": {"baseURL": "http://localhost:11434/v1"}}
  }
}`
	s, err := Assemble(project(t, cfg), Options{Model: "local/qwen2.5-coder"})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if _, ok := s.Provider.(*openaicompat.Adapter); !ok {
		t.Fatalf("Provider = %T, want the overridden adapter", s.Provider)
	}
	if s.Model != "local/qwen2.5-coder" {
		t.Errorf("Model = %q", s.Model)
	}
}

// TestAssembleIncludesProjectInstructions is the spec scenario for AGENTS.md.
func TestAssembleIncludesProjectInstructions(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true, BaseSystemPrompt: "You are OpenPlus."})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if !strings.Contains(s.SystemPrompt, "cgo-free") {
		t.Errorf("AGENTS.md content missing from the system prompt: %q", s.SystemPrompt)
	}
	if !strings.HasPrefix(s.SystemPrompt, "You are OpenPlus.") {
		t.Errorf("base prompt must come first: %q", s.SystemPrompt)
	}
}

func TestAssembleRegistersBuiltinTools(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	for _, name := range []string{"read", "write", "edit", "bash", "glob", "grep"} {
		if _, ok := s.Tools.Get(name); !ok {
			t.Errorf("builtin %q not registered", name)
		}
	}
	// the neutral schemas handed to the model must match the registry
	if len(s.ToolSchemas) != len(s.Tools.All()) {
		t.Errorf("ToolSchemas = %d, registry = %d", len(s.ToolSchemas), len(s.Tools.All()))
	}
}

// TestAssembleGateFromConfigPermissions is the spec scenario for ask rules.
func TestAssembleGateFromConfigPermissions(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_KEY", "k")
	s, err := Assemble(project(t, anthropicConfig), Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	// the rule table says Ask, straight from permission.bash
	got, err := s.Rules.Permit(t.Context(), provider.ToolCall{Name: "bash", Input: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Permit: %v", err)
	}
	if got != policy.Ask {
		t.Fatalf("bash rule = %v, want Ask (from permission.bash)", got)
	}
	// a tool with no rule falls through to allow
	got, _ = s.Rules.Permit(t.Context(), provider.ToolCall{Name: "read", Input: []byte(`{}`)})
	if got != policy.Allow {
		t.Errorf("read rule = %v, want Allow", got)
	}

	// With no prompter wired, Ask must degrade to Deny rather than silently run.
	got, _ = s.Gate.Permit(t.Context(), provider.ToolCall{Name: "bash", Input: []byte(`{}`)})
	if got != policy.Deny {
		t.Errorf("unwired Ask = %v, want Deny (safe default)", got)
	}
}

// TestSetPrompterMakesAskInteractive proves the front-end seam: once a prompter
// is wired, an Ask rule consults it instead of denying.
func TestSetPrompterMakesAskInteractive(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_KEY", "k")
	s, err := Assemble(project(t, anthropicConfig), Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	s.SetPrompter(approvingPrompter{})

	got, err := s.Gate.Permit(t.Context(), provider.ToolCall{Name: "bash", Input: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Permit: %v", err)
	}
	if got != policy.Allow {
		t.Fatalf("approved Ask = %v, want Allow", got)
	}
}

// TestSetPrompterUnderSkipIsNoop proves --dangerously-skip-permissions does not
// become interactive just because a prompter exists.
func TestSetPrompterUnderSkipIsNoop(t *testing.T) {
	cfg := `{
  "model": "local/m",
  "provider": {"local": {"options": {"baseURL": "http://x/v1"}}},
  "permission": {"bash": "ask"}
}`
	s, err := Assemble(project(t, cfg), Options{SkipPermissions: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	s.SetPrompter(refusingPrompter{})
	// under skip, an ask rule is allowed without consulting anything
	got, _ := s.Gate.Permit(t.Context(), provider.ToolCall{Name: "bash", Input: []byte(`{}`)})
	if got != policy.Allow {
		t.Fatalf("skip-mode ask = %v, want Allow", got)
	}
}

// TestAssembleSkipPermissionsKeepsExplicitDenials is the spec scenario for the
// skip flag.
func TestAssembleSkipPermissionsKeepsExplicitDenials(t *testing.T) {
	cfg := `{
  "model": "local/m",
  "provider": {"local": {"options": {"baseURL": "http://x/v1"}}},
  "permission": {"bash": "deny", "write": "ask"}
}`
	s, err := Assemble(project(t, cfg), Options{SkipPermissions: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	// explicit deny survives the skip flag
	got, _ := s.Gate.Permit(t.Context(), provider.ToolCall{Name: "bash", Input: []byte(`{}`)})
	if got != policy.Deny {
		t.Errorf("bash decision = %v, want Deny even with skip", got)
	}
	// an ask rule becomes allow (no prompting under skip)
	got, _ = s.Gate.Permit(t.Context(), provider.ToolCall{Name: "write", Input: []byte(`{}`)})
	if got != policy.Allow {
		t.Errorf("write decision = %v, want Allow under skip", got)
	}
}

// --- T-101: optional subsystems ---

// TestAssembleWithoutEmbedderHasNoMemory is the spec scenario for optional
// subsystems.
func TestAssembleWithoutEmbedderHasNoMemory(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if s.Memory != nil {
		t.Error("no embedder configured, so there should be no memory store")
	}
	// and the session is still usable
	if s.Provider == nil || s.Tools == nil || s.Gate == nil {
		t.Fatal("session incomplete without memory")
	}
}

func TestAssembleWithEmbedderOpensMemory(t *testing.T) {
	root := project(t, `{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}},
  "embedder": {"model": "nomic-embed-text", "baseURL": "http://localhost:11434/v1"},
  "memory": {"autoOpen": true}
}`)
	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if s.Memory == nil {
		t.Fatal("an embedder is configured, so memory should be open")
	}
	t.Cleanup(func() { _ = s.Close() })
	// vec0 must actually be available in the opened store
	if v, err := s.Memory.VecVersion(); err != nil || v == "" {
		t.Fatalf("VecVersion = %q, err = %v", v, err)
	}
}

func TestAssembleDiscoversSkills(t *testing.T) {
	root := project(t, "")
	write(t, root, ".claude/skills/deploy/SKILL.md",
		"---\nname: deploy\ndescription: Deploy the service to kubernetes\n---\nsteps here")
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if s.Skills == nil {
		t.Fatal("skill index not assembled")
	}
	if _, ok := s.Skills.Find("deploy"); !ok {
		t.Error("project skill not discovered")
	}
}

func TestAssembleBudgeterUsesModelTokenizer(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_KEY", "k")
	s, err := Assemble(project(t, anthropicConfig), Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if s.Budgeter.Tokenizer == nil {
		t.Fatal("budgeter has no tokenizer")
	}
	// an anthropic model must get the anthropic-calibrated estimate
	if got := s.Budgeter.Tokenizer.Count("some text to measure"); got <= 0 {
		t.Fatalf("Count = %d", got)
	}
	if s.Budgeter.Budget <= 0 {
		t.Error("a positive default budget is expected")
	}
}

func TestAssembleMissingProjectRootErrors(t *testing.T) {
	if _, err := Assemble(filepath.Join(t.TempDir(), "nope"), Options{Fake: true}); err == nil {
		t.Fatal("expected an error for a nonexistent project root")
	}
}

// approvingPrompter approves every Ask; refusingPrompter refuses.
type approvingPrompter struct{}

func (approvingPrompter) Ask(context.Context, provider.ToolCall) (bool, error) { return true, nil }

type refusingPrompter struct{}

func (refusingPrompter) Ask(context.Context, provider.ToolCall) (bool, error) { return false, nil }

func TestSessionCloseIsSafeWithoutMemory(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close without memory: %v", err)
	}
}

// --- Change 0004 / T-410: Memory.AutoOpen defaults to false ---
//
// Today the runtime auto-creates the memory file via sqlite's create-on-open
// behavior, so a missing path never errors. After T-412 it must — the file
// only materializes when the operator opts in via memory.autoOpen: true. The
// risk (per the proposal) is silent side-effect creation; the fix is to make
// creation explicit.

func TestAssembleMemoryMissingPathFailsByDefault(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "nope", "memory.db") // parent does not exist either
	cfg := fmt.Sprintf(`{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}},
  "embedder": {"model": "nomic-embed-text", "baseURL": "http://localhost:11434/v1"},
  "memory": {"path": %q}
}`, missing)
	write(t, root, "opencode.json", cfg)
	write(t, root, "AGENTS.md", "House rule: the build is cgo-free.")

	_, err := Assemble(root, Options{})
	if err == nil {
		t.Fatal("expected an error when memory path is missing and autoOpen is unset")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want errors.Is(_, os.ErrNotExist)", err)
	}
}

// --- Change 0004 / T-411: Memory.AutoOpen=true creates the file ---

func TestAssembleMemoryAutoOpenCreatesPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "fresh", "memory.db") // parent and file both missing
	cfg := fmt.Sprintf(`{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}},
  "embedder": {"model": "nomic-embed-text", "baseURL": "http://localhost:11434/v1"},
  "memory": {"path": %q, "autoOpen": true}
}`, target)
	write(t, root, "opencode.json", cfg)
	write(t, root, "AGENTS.md", "House rule: the build is cgo-free.")

	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatalf("Assemble with autoOpen=true: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.Memory == nil {
		t.Fatal("autoOpen=true: Session.Memory must be non-nil")
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("autoOpen=true: file should exist at %q: %v", target, err)
	}
}

// TestAssembleConfigPathOverride: runtime.Options.ConfigPath overrides the
// default <root>/opencode.json lookup. The override must reach the
// config loader so a project can keep its config anywhere.
func TestAssembleConfigPathOverride(t *testing.T) {
	// Two configs: one in the project root (must be ignored), one at
	// the explicit override path (must be used). The override contains a
	// sentinel value only it carries.
	override := filepath.Join(t.TempDir(), "elsewhere", "my-config.json")
	if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, []byte(`{
  "model": "local/sentinel-9c41",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}}
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// The root has a *different* opencode.json with a model that is NOT
	// "sentinel-9c41". If the override works, Session.Model is sentinel-9c41.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(`{
  "model": "local/should-be-ignored"
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Assemble(root, Options{ConfigPath: override})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.Model != "local/sentinel-9c41" {
		t.Errorf("Model = %q, want local/sentinel-9c41 (override did not win)", s.Model)
	}
}

// TestAssembleConfigPathMissingErrors: --config /missing.json must fail
// clearly rather than silently fall back to the default <root>/opencode.json.
func TestAssembleConfigPathMissingErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")
	root := t.TempDir()
	_, err := Assemble(root, Options{ConfigPath: missing})
	if err == nil {
		t.Fatal("expected an error when --config points at a missing file")
	}
}
//
// Env wins over opencode.json's memory.path. The store must open at the
// env path; the file value is irrelevant. Same precedence model as
// OPENPLUS_EMBED_* (T-402).

func TestAssembleMemoryEnvPathOverride(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "env-memory.db")
	t.Setenv("OPENPLUS_MEMORY_PATH", envPath)

	root := project(t, `{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}},
  "embedder": {"model": "nomic-embed-text", "baseURL": "http://localhost:11434/v1"},
  "memory": {"autoOpen": true}
}`)

	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.Memory == nil {
		t.Fatal("Session.Memory must be non-nil")
	}
	// the store must have opened at the env path
	if _, err := os.Stat(envPath); err != nil {
		t.Errorf("env path %q not used: %v", envPath, err)
	}
	// and NOT at the configured path under <root>/.openplus/memory.db
	configPath := filepath.Join(root, ".openplus", "memory.db")
	if _, err := os.Stat(configPath); err == nil {
		t.Errorf("configured path %q should not exist when env wins", configPath)
	}
}

// TestAssembleMemoryMaxEntriesPropagated: configuring memory.maxEntries
// in opencode.json must reach the store's SetMaxEntries. Without this
// wiring the config knob is a no-op.
func TestAssembleMemoryMaxEntriesPropagated(t *testing.T) {
	root := project(t, `{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}},
  "embedder": {"model": "nomic-embed-text", "baseURL": "http://localhost:11434/v1"},
  "memory": {"autoOpen": true, "maxEntries": 3}
}`)
	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// The field is unexported; verify behaviorally by writing 5 chunks and
	// checking only 3 remain.
	s.Memory.Embedder = embedForCapTest()
	for i := 0; i < 5; i++ {
		if _, err := s.Memory.Write(context.Background(), "x", "t"); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	var n int
	if err := s.Memory.DB().QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3 (memory.maxEntries must reach the store)", n)
	}
}

// embedForCapTest returns a deterministic 4-dim embedder for cap tests.
// Same shape as the one in turn_test.go but kept inline so assemble_test
// stays self-contained.
func embedForCapTest() embed.Embedder { return capTestEmbedder{dim: 4} }

type capTestEmbedder struct{ dim int }

func (f capTestEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = make([]float32, f.dim)
	}
	return out, nil
}
func (f capTestEmbedder) Dim() int { return f.dim }
