// Package runtime is the composition root (change 0002): it assembles the
// subsystems built in change 0001 into a live session.
//
// The runtime owns no behavior. It resolves configuration, picks adapters, and
// hands ports to the agent loop — every decision about *how* something works
// stays in the package that owns it. That is what keeps this from becoming a god
// object as more subsystems come online.
package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/7solutions/openplus/internal/config"
	"github.com/7solutions/openplus/internal/contextmgr"
	"github.com/7solutions/openplus/internal/embed"
	"github.com/7solutions/openplus/internal/memory"
	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/provider"
	selectadapter "github.com/7solutions/openplus/internal/provider/select"
	"github.com/7solutions/openplus/internal/skills"
	"github.com/7solutions/openplus/internal/tool"
)

// Sentinel errors from assembly.
var (
	// ErrNoModel means no model was configured or passed.
	ErrNoModel = errors.New("runtime: no model configured")
	// ErrMissingCredential means the selected provider has no resolvable API
	// key and is not a local endpoint.
	ErrMissingCredential = errors.New("runtime: provider credential missing")
)

// Defaults for values a project need not configure.
const (
	// DefaultBudget is the assembled-context token ceiling when unset.
	DefaultBudget = 120_000
	// DefaultWindow is the assumed context window when unset.
	DefaultWindow = 200_000
	// DefaultMemoryPath is where the memory database lives when unset.
	DefaultMemoryPath = ".openplus/memory.db"
	// DefaultBaseSystemPrompt is used when the caller passes none.
	DefaultBaseSystemPrompt = "You are OpenPlus, a coding agent."
	// MaxAutoSkills bounds how many skills auto-load into one turn.
	MaxAutoSkills = 3
)

// Options are the operator-supplied knobs, overriding configuration.
type Options struct {
	// Model overrides the configured model ("<provider>/<model>").
	Model string
	// SkipPermissions applies --dangerously-skip-permissions: an allow-all base
	// where explicit rules still win.
	SkipPermissions bool
	// Fake uses the scripted fake provider, so the binary runs with no
	// credential (the offline smoke path).
	Fake bool
	// BaseSystemPrompt precedes the project instructions.
	BaseSystemPrompt string
}

// Session is an assembled, ready-to-run agent session. Fields are ports, not
// concrete adapters, except where a caller legitimately needs the concrete type
// (Memory, for its lifecycle).
type Session struct {
	Root  string
	Model string

	Config       *config.Config
	SystemPrompt string

	Provider    provider.Provider
	Tools       *tool.Registry
	ToolSchemas []provider.ToolSchema

	// Gate authorizes tool calls. Until a Prompter is wired, an Ask rule
	// degrades to Deny — safe, but not interactive.
	Gate policy.Gate
	// Rules is the decision table behind Gate, exposed so a caller can inspect
	// what a rule decides independently of how Ask is resolved.
	Rules *policy.Rules

	// Memory is nil when no embedder is configured.
	Memory   *memory.Store
	Embedder embed.Embedder

	Skills   *skills.Index
	Budgeter contextmgr.Budgeter

	// OnEvent and OnToolResult are the front-end render hooks, forwarded to the
	// agent loop on every Run. Nil means no rendering (the non-interactive path).
	OnEvent      func(provider.Event)
	OnToolResult func(call provider.ToolCall, result provider.Block)
}

// Assemble builds a Session from a project root. It fails rather than degrade:
// a missing credential, an unknown model prefix, or an unreadable project is an
// error, not a silently reduced session.
func Assemble(root string, opts Options) (*Session, error) {
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("runtime: project root %q is not a readable directory", root)
	}

	pc, err := config.LoadProjectContext(root)
	if err != nil {
		return nil, err
	}

	base := opts.BaseSystemPrompt
	if base == "" {
		base = DefaultBaseSystemPrompt
	}

	s := &Session{
		Root:         root,
		Config:       pc.Config,
		SystemPrompt: pc.SystemPrompt(base),
	}

	// Tools: the full builtin set, plus their neutral schemas for the model.
	s.Tools = tool.NewRegistry(
		tool.Read{}, tool.Write{}, tool.Edit{}, tool.Bash{},
		tool.Glob{Root: root}, tool.Grep{Root: root},
	)
	s.ToolSchemas = toolSchemas(s.Tools)

	// Provider.
	if err := s.assembleProvider(opts); err != nil {
		return nil, err
	}

	// Policy gate from the configured permission rules.
	gate, rules, err := buildGate(pc.Config.Permission, opts.SkipPermissions)
	if err != nil {
		return nil, err
	}
	s.Gate, s.Rules = gate, rules

	// Optional: memory + embedder.
	if err := s.assembleMemory(); err != nil {
		return nil, err
	}

	// Skills: standard scan order, lowest priority first.
	s.Skills = skills.NewIndex(skillRoots(root)...)
	if _, err := s.Skills.Discover(); err != nil {
		return nil, err
	}

	// Context budgeter, calibrated for the selected model.
	budget := pc.Config.Context.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	s.Budgeter = contextmgr.Budgeter{
		Tokenizer: contextmgr.ForModel(s.Model),
		Budget:    budget,
	}

	return s, nil
}

// assembleProvider resolves the model string and selects its adapter.
func (s *Session) assembleProvider(opts Options) error {
	if opts.Fake {
		s.Model = opts.Model
		if s.Model == "" {
			s.Model = "fake/fake"
		}
		s.Provider = fakeProvider()
		return nil
	}

	model := opts.Model
	if model == "" {
		model = s.Config.Model
	}
	if model == "" {
		return fmt.Errorf("%w: set \"model\" in opencode.json or pass --model", ErrNoModel)
	}
	s.Model = model

	prov, err := s.Config.ProviderFor(model)
	if err != nil {
		return err
	}
	// A remote endpoint with no key would otherwise fail as an opaque 401 mid
	// turn; fail here with the provider named. A configured baseURL means a
	// local/self-hosted endpoint, which is legitimately keyless.
	if prov.APIKey == "" && prov.BaseURL == "" {
		return fmt.Errorf("%w: provider %q has no apiKey (set it in opencode.json, "+
			"or set options.baseURL for a local endpoint)", ErrMissingCredential, prov.ID)
	}

	p, err := selectadapter.Select(model, s.Config.Providers)
	if err != nil {
		return err
	}
	s.Provider = p
	return nil
}

// assembleMemory opens the store and embedder when an embedder is configured.
// Without one there is no memory: an unembedded store cannot answer a semantic
// query, so silently opening it would be worse than leaving it off.
func (s *Session) assembleMemory() error {
	if !s.Config.Embedder.Configured() {
		return nil
	}

	s.Embedder = &embed.Local{
		BaseURL: s.Config.Embedder.BaseURL,
		APIKey:  s.Config.Embedder.APIKey,
		Model:   s.Config.Embedder.Model,
		Timeout: s.Config.Embedder.Timeout,
	}

	path := s.Config.Memory.Path
	if path == "" {
		path = DefaultMemoryPath
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Root, path)
	}

	// AutoOpen defaults to false: a missing memory file is a configuration
	// error, not a silent side effect. Operators opt in via memory.autoOpen.
	if !s.Config.Memory.AutoOpen {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("runtime: memory %q: %w", path, err)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("runtime: memory dir: %w", err)
		}
	}

	store, err := memory.Open(path)
	if err != nil {
		return err
	}
	store.Embedder = s.Embedder
	if max := s.Config.Memory.MaxEntries; max > 0 {
		store.SetMaxEntries(max)
	}
	s.Memory = store
	return nil
}

// Close releases the session's resources. Safe to call when memory was never
// opened.
func (s *Session) Close() error {
	if s.Memory == nil {
		return nil
	}
	return s.Memory.Close()
}

// buildGate turns configured permission rules into a Gate, returning the
// underlying rule table alongside it. Without the skip flag the result is a
// Prompting gate so the front-end can resolve Ask via SetPrompter.
func buildGate(perm config.Permission, skip bool) (policy.Gate, *policy.Rules, error) {
	if skip {
		skipGate, err := policy.NewSkip(perm.Tools, perm.Paths)
		if err != nil {
			return nil, nil, err
		}
		return skipGate, skipGate.Rules, nil
	}
	rules, err := policy.NewRules(policy.Allow, perm.Tools, perm.Paths)
	if err != nil {
		return nil, nil, err
	}
	return &policy.Prompting{Rules: rules}, rules, nil
}

// SetPrompter wires an interactive prompter so Ask decisions can be resolved by
// the operator instead of degrading to Deny. It is a no-op under
// --dangerously-skip-permissions, where nothing prompts by design.
func (s *Session) SetPrompter(p policy.Prompter) {
	if prompting, ok := s.Gate.(*policy.Prompting); ok {
		prompting.Prompter = p
	}
}

// skillRoots is the skill scan order, lowest priority first: user-level skills
// are overridden by the project's own.
func skillRoots(root string) []string {
	var roots []string
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".claude", "skills"))
	}
	roots = append(roots,
		filepath.Join(root, ".opencode", "skills"),
		filepath.Join(root, ".claude", "skills"),
	)
	return roots
}

// toolSchemas converts a registry into the neutral schemas sent to the model.
func toolSchemas(r *tool.Registry) []provider.ToolSchema {
	all := r.All()
	out := make([]provider.ToolSchema, 0, len(all))
	for _, t := range all {
		out = append(out, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Schema(),
		})
	}
	return out
}

// fakeProvider is the scripted offline provider used by --fake: it answers one
// turn with text and stops, which is enough to prove the wiring end to end.
func fakeProvider() *provider.Fake {
	return &provider.Fake{Scripts: [][]provider.Event{
		{
			{Kind: provider.EventTextDelta, Text: "openplus runtime is wired (fake provider)"},
			{Kind: provider.EventTurnEnd},
		},
	}}
}
