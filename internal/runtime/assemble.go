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

	"github.com/7solutions/openplus/internal/compose"
	"github.com/7solutions/openplus/internal/config"
	"github.com/7solutions/openplus/internal/contextmgr"
	"github.com/7solutions/openplus/internal/embed"
	"github.com/7solutions/openplus/internal/improve"
	"github.com/7solutions/openplus/internal/memo"
	"github.com/7solutions/openplus/internal/memory"
	"github.com/7solutions/openplus/internal/orchestrate"
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
	// ConfigPath overrides the default <root>/opencode.json. Empty means
	// use the default. Used by --config / -c in the CLI.
	ConfigPath string
	// Goal, when non-empty, makes Run consult Session.Judge after the
	// agent loop returns. Empty Goal skips the judge entirely. Used by
	// --goal in the CLI (T-440..T-445).
	Goal string
	// Judge is the optional goal / stop-condition evaluator (ADR-0006).
	// Nil disables judging even when Goal is set, preserving pre-0007
	// behavior for callers that want a goal field but no judge yet.
	Judge *orchestrate.Judge
	// MaxJudgeIterations caps the number of judge consults in a single
	// Run when the judge keeps replying UNMET. Zero or negative falls
	// back to DefaultMaxJudgeIterations (3).
	MaxJudgeIterations int
}

// DefaultMaxJudgeIterations is the default cap for judge consults.
const DefaultMaxJudgeIterations = 3

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

	// Goal is the stop-condition text (Change 0007). Empty disables
	// judging; Run then terminates when the agent's tool-call count hits
	// zero (the pre-0007 behavior).
	Goal string
	// Judge is the optional goal / stop-condition evaluator (ADR-0006).
	// When Goal is non-empty and Judge is non-nil, Run consults Judge
	// after the agent loop returns. MET stops; UNMET appends feedback
	// to history and loops; the loop is bounded by MaxJudgeIterations.
	Judge *orchestrate.Judge
	// MaxJudgeIterations caps the judge loop. Zero or negative falls
	// back to DefaultMaxJudgeIterations (3).
	MaxJudgeIterations int

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

	// Checkpointer writes and restores checkpoint.md (ADR-0008). Nil when no
	// context window is configured, which disables checkpointing end to end.
	Checkpointer *contextmgr.Checkpointer
	// Tasks is the task tree, restored from the checkpoint at assembly and
	// written back on every checkpoint (milestone subsystem #3).
	Tasks contextmgr.TaskTree

	// OnEvent and OnToolResult are the front-end render hooks, forwarded to the
	// agent loop on every Run. Nil means no rendering (the non-interactive path).
	OnEvent      func(provider.Event)
	OnToolResult func(call provider.ToolCall, result provider.Block)

	// OnCheckpointError reports a failed checkpoint write. The turn itself
	// succeeded, but the session is no longer durable, so the operator needs to
	// know. Nil drops the report rather than failing the turn.
	OnCheckpointError func(error)

	// KeepRecent bounds how many trailing messages survive compaction
	// (change 0010). Zero uses DefaultKeepRecent.
	KeepRecent int
	// OnCompact reports a compaction as (before, after) message counts, so a
	// front-end can tell the user rather than the context shrinking invisibly.
	// Nil is a no-op.
	OnCompact func(before, after int)

	// Memo is the file-based memory surface: MEMORY.md, notes.md, and
	// tasks/<id>/progress.md under the project root (ADR-0002 #1). /dream
	// appends extracted facts here.
	Memo memo.Files

	// Compose is the active compose session, nil until /compose starts one
	// (ADR-0002 #6). It lives for the process; persisting a phase machine across
	// invocations is deliberately out of scope for change 0009.
	Compose *compose.Session

	// History accumulates this session's turns, so /dream has a transcript to
	// extract from and /distill has runs to mine.
	History []provider.Message
	// Runs records each turn's tool sequence for /distill.
	Runs []improve.Run

	// extraCommands holds session-local command registrations. Builtins always
	// win, so a registration cannot hijack /help.
	extraCommands map[string]Command
}

// maxJudgeIterations returns the effective cap on judge consults.
func (s *Session) maxJudgeIterations() int {
	if s.MaxJudgeIterations > 0 {
		return s.MaxJudgeIterations
	}
	return DefaultMaxJudgeIterations
}

// Assemble builds a Session from a project root. It fails rather than degrade:
// a missing credential, an unknown model prefix, or an unreadable project is an
// error, not a silently reduced session.
func Assemble(root string, opts Options) (*Session, error) {
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("runtime: project root %q is not a readable directory", root)
	}

	// Env overrides apply on top of Options (T-425). Precedence:
	// env > Options > file. This matches OPENAI_API_KEY-style tools.
	if v := os.Getenv("OPENPLUS_MODEL"); v != "" {
		opts.Model = v
	}
	if os.Getenv("OPENPLUS_FAKE") == "1" {
		opts.Fake = true
	}

	pc, err := config.LoadProjectContextWithConfig(root, opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	base := opts.BaseSystemPrompt
	if base == "" {
		base = DefaultBaseSystemPrompt
	}

	s := &Session{
		Root:               root,
		Config:             pc.Config,
		SystemPrompt:       pc.SystemPrompt(base),
		Goal:               opts.Goal,
		Judge:              opts.Judge,
		MaxJudgeIterations: opts.MaxJudgeIterations,
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

	// File memory (ADR-0002 #1): MEMORY.md and friends live at the project root.
	s.Memo = memo.Files{Root: root}

	// Checkpointing (ADR-0008). A window is required: without one there is no
	// high-water mark to cross, so the feature stays off rather than guessing.
	s.assembleCheckpointer(pc.Config.Context.Window)

	return s, nil
}

// assembleCheckpointer wires the Checkpointer and restores the task tree from
// any existing checkpoint. A malformed checkpoint degrades to an empty tree: a
// corrupt file must not stop the session from starting, since the whole point of
// the feature is resilience.
func (s *Session) assembleCheckpointer(window int) {
	if window <= 0 {
		return
	}
	s.Checkpointer = &contextmgr.Checkpointer{Root: s.Root, Window: window}

	cp, err := s.Checkpointer.Read()
	if err != nil {
		return // unreadable checkpoint: start clean
	}
	s.Tasks = cp.Tasks
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
