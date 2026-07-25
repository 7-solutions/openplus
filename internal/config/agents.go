package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultInstructionFile is read when opencode.json names no instruction files.
// It is the canonical location for project rules (see the repo's own AGENTS.md).
const DefaultInstructionFile = "AGENTS.md"

// DefaultConfigFile is the config file looked for under a project root.
const DefaultConfigFile = "opencode.json"

// ResolveConfigPath returns the config file a root plus an optional override
// resolves to, whether or not it exists. An empty override means
// <root>/opencode.json; a relative override is taken relative to root. Callers
// that need to write the config (e.g. /theme persisting a palette) use this to
// find the same file the session was loaded from.
func ResolveConfigPath(root, configPath string) string {
	if configPath == "" {
		return filepath.Join(root, DefaultConfigFile)
	}
	if !filepath.IsAbs(configPath) {
		return filepath.Join(root, configPath)
	}
	return configPath
}

// ProjectContext is a project's assembled configuration and instructions —
// everything the agent needs to know about *this* repo before its first turn
// (T-003).
type ProjectContext struct {
	// Root is the project root the context was loaded from.
	Root string
	// Config is the parsed opencode.json. Never nil: a project without the file
	// gets a zero-value Config.
	Config *Config
	// Instructions is the concatenated content of the instruction files, each
	// under a header naming its source file.
	Instructions string
}

// LoadProjectContext loads opencode.json (when present) and the instruction
// files it names, defaulting to AGENTS.md. A missing opencode.json is fine — a
// project can be configured entirely by its AGENTS.md — but a malformed one is
// an error rather than a silent fallback to defaults.
func LoadProjectContext(root string) (ProjectContext, error) {
	pc := ProjectContext{Root: root, Config: &Config{}}

	configPath := ResolveConfigPath(root, "")
	if _, err := os.Stat(configPath); err == nil {
		cfg, err := Load(configPath)
		if err != nil {
			return ProjectContext{}, err
		}
		pc.Config = cfg
	} else if !os.IsNotExist(err) {
		return ProjectContext{}, fmt.Errorf("config: stat %s: %w", configPath, err)
	}

	instructions, err := LoadInstructions(root, pc.Config.Instructions)
	if err != nil {
		return ProjectContext{}, err
	}
	pc.Instructions = instructions

	return pc, nil
}

// LoadProjectContextWithConfig is LoadProjectContext with an explicit
// config path (T-422). Empty configPath means "use the default
// <root>/opencode.json with lenient missing-file semantics". A non-empty
// path is treated as an explicit override — a missing or malformed file
// there is an error, because --config is a deliberate operator choice
// and a typo should surface as a clear failure.
//
// Callers that always want the strict behavior can call this function
// with a non-empty path; callers that want lenient default-path behavior
// call LoadProjectContext directly.
func LoadProjectContextWithConfig(root, configPath string) (ProjectContext, error) {
	if configPath == "" {
		return LoadProjectContext(root)
	}
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(root, configPath)
	}
	pc := ProjectContext{Root: root, Config: &Config{}}

	cfg, err := Load(configPath)
	if err != nil {
		return ProjectContext{}, fmt.Errorf("config: --config %s: %w", configPath, err)
	}
	pc.Config = cfg

	instructions, err := LoadInstructions(root, pc.Config.Instructions)
	if err != nil {
		return ProjectContext{}, err
	}
	pc.Instructions = instructions

	return pc, nil
}

// LoadInstructions reads each instruction file relative to root, in order,
// concatenating them under a header naming the source so the model can tell
// which file a rule came from. Missing files are skipped (a project need not
// have every file); a path escaping root is an error.
func LoadInstructions(root string, files []string) (string, error) {
	if len(files) == 0 {
		files = []string{DefaultInstructionFile}
	}

	var b strings.Builder
	for _, rel := range files {
		path, err := resolveInRoot(root, rel)
		if err != nil {
			return "", err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue // not every named file has to exist
			}
			return "", fmt.Errorf("config: read %s: %w", path, err)
		}
		if strings.TrimSpace(string(body)) == "" {
			continue
		}
		fmt.Fprintf(&b, "# Project instructions (%s)\n", rel)
		b.WriteString(strings.TrimSpace(string(body)))
		b.WriteString("\n\n")
	}

	return strings.TrimSpace(b.String()), nil
}

// SystemPrompt appends the project instructions to a base system prompt. The
// base comes first so the agent's own identity and rules outrank project
// instructions that might contradict them.
func (pc ProjectContext) SystemPrompt(base string) string {
	if pc.Instructions == "" {
		return base
	}
	if base == "" {
		return pc.Instructions
	}
	return base + "\n\n" + pc.Instructions
}

// resolveInRoot joins rel onto root, rejecting any path that escapes it.
//
// Containment is decided on absolute paths. A relative root needs it: with
// root ".", filepath.Join(".", "AGENTS.md") is "AGENTS.md" — the "./" prefix is
// gone, so a textual prefix check against "." rejects a file that is plainly
// inside the project. That broke every default `openplus -p ...` invocation,
// while absolute roots (what the tests used) worked fine.
//
// The returned path keeps the caller's root form, relative or absolute, so
// error messages and logs read the way the user invoked the tool.
func resolveInRoot(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("config: instruction path %q must be relative to the project root", rel)
	}
	path := filepath.Join(root, rel)

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("config: resolve project root %q: %w", root, err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("config: resolve instruction path %q: %w", rel, err)
	}
	if absPath != absRoot && !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("config: instruction path %q escapes the project root", rel)
	}
	return path, nil
}
