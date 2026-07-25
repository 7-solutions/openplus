package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestLoadInstructionsReadsAgentsMD(t *testing.T) {
	root := t.TempDir()
	write(t, root, "AGENTS.md", "# Rules\nBuild is cgo-free.\n")

	got, err := LoadInstructions(root, []string{"AGENTS.md"})
	if err != nil {
		t.Fatalf("LoadInstructions: %v", err)
	}
	if !strings.Contains(got, "cgo-free") {
		t.Fatalf("instructions = %q", got)
	}
	// the source file is named so the model knows where a rule came from
	if !strings.Contains(got, "AGENTS.md") {
		t.Errorf("assembled context should name its source: %q", got)
	}
}

func TestLoadInstructionsMultipleFilesInOrder(t *testing.T) {
	root := t.TempDir()
	write(t, root, "AGENTS.md", "first rules")
	write(t, root, "EXTRA.md", "second rules")

	got, err := LoadInstructions(root, []string{"AGENTS.md", "EXTRA.md"})
	if err != nil {
		t.Fatalf("LoadInstructions: %v", err)
	}
	if strings.Index(got, "first rules") > strings.Index(got, "second rules") {
		t.Errorf("instruction order not preserved:\n%s", got)
	}
}

func TestLoadInstructionsMissingFileSkipped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "AGENTS.md", "present")

	got, err := LoadInstructions(root, []string{"AGENTS.md", "ABSENT.md"})
	if err != nil {
		t.Fatalf("a missing instruction file should not error: %v", err)
	}
	if !strings.Contains(got, "present") {
		t.Errorf("instructions = %q", got)
	}
}

func TestLoadInstructionsNoneConfiguredDefaultsToAgentsMD(t *testing.T) {
	root := t.TempDir()
	write(t, root, "AGENTS.md", "implicit default")

	got, err := LoadInstructions(root, nil)
	if err != nil {
		t.Fatalf("LoadInstructions: %v", err)
	}
	if !strings.Contains(got, "implicit default") {
		t.Errorf("AGENTS.md should be read even when instructions are unset: %q", got)
	}
}

func TestLoadInstructionsEmptyWhenNothingExists(t *testing.T) {
	got, err := LoadInstructions(t.TempDir(), []string{"AGENTS.md"})
	if err != nil {
		t.Fatalf("LoadInstructions: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty instructions, got %q", got)
	}
}

// TestLoadInstructionsRejectsEscape guards against an instruction path walking
// out of the project root.
func TestLoadInstructionsRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadInstructions(root, []string{"../outside.md"}); err == nil {
		t.Fatal("an instruction path escaping the root must be rejected")
	}
}

func TestProjectContextCombinesConfigAndInstructions(t *testing.T) {
	root := t.TempDir()
	write(t, root, "AGENTS.md", "the house rules")
	write(t, root, "opencode.json", `{
  "instructions": ["AGENTS.md"],
  "model": "anthropic/claude-sonnet-5",
  "provider": {"anthropic": {"options": {"apiKey": "k"}}}
}`)

	pc, err := LoadProjectContext(root)
	if err != nil {
		t.Fatalf("LoadProjectContext: %v", err)
	}
	if pc.Config == nil {
		t.Fatal("Config not loaded")
	}
	if pc.Config.Model != "anthropic/claude-sonnet-5" {
		t.Errorf("model = %q", pc.Config.Model)
	}
	if !strings.Contains(pc.Instructions, "the house rules") {
		t.Errorf("instructions = %q", pc.Instructions)
	}
}

func TestProjectContextWorksWithoutConfigFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, "AGENTS.md", "rules only, no opencode.json")

	pc, err := LoadProjectContext(root)
	if err != nil {
		t.Fatalf("a project without opencode.json should still load: %v", err)
	}
	if pc.Config == nil {
		t.Fatal("expected a zero-value Config rather than nil")
	}
	if !strings.Contains(pc.Instructions, "rules only") {
		t.Errorf("instructions = %q", pc.Instructions)
	}
}

func TestProjectContextInvalidConfigErrors(t *testing.T) {
	root := t.TempDir()
	write(t, root, "opencode.json", "{not json")
	if _, err := LoadProjectContext(root); err == nil {
		t.Fatal("malformed opencode.json should error rather than be ignored")
	}
}

func TestProjectContextSystemPromptPrefixesInstructions(t *testing.T) {
	root := t.TempDir()
	write(t, root, "AGENTS.md", "always run the tests")

	pc, err := LoadProjectContext(root)
	if err != nil {
		t.Fatal(err)
	}
	got := pc.SystemPrompt("You are OpenPlus.")
	if !strings.HasPrefix(got, "You are OpenPlus.") {
		t.Errorf("base prompt must come first:\n%s", got)
	}
	if !strings.Contains(got, "always run the tests") {
		t.Errorf("project instructions missing from the system prompt:\n%s", got)
	}
}

func TestSystemPromptWithoutInstructionsIsJustTheBase(t *testing.T) {
	pc := ProjectContext{Config: &Config{}}
	if got := pc.SystemPrompt("base only"); got != "base only" {
		t.Fatalf("SystemPrompt = %q, want %q", got, "base only")
	}
}
