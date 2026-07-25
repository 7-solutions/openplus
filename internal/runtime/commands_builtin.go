package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/7-solutions/openplus/internal/compose"
	"github.com/7-solutions/openplus/internal/config"
	"github.com/7-solutions/openplus/internal/improve"
	"github.com/7-solutions/openplus/internal/orchestrate"
	"github.com/7-solutions/openplus/internal/ports"
)

// builtinCommands is the dispatch table. Every entry closes a milestone
// subsystem that was previously unreachable (ADR-0002).
//
// /help lives in an init() rather than the literal: its Run reads the table it
// is a member of, which the compiler rejects as an initialization cycle.
var builtinCommands = map[string]Command{
	// --- skills (ADR-0002 #8) ---
	"skill": {
		Name: "skill", Usage: "/skill <name>", Summary: "load a skill by name",
		Run: (*Session).cmdSkill,
	},
	"skills": {
		Name: "skills", Usage: "/skills", Summary: "list discoverable skills",
		Run: (*Session).cmdSkills,
	},

	// --- compose (ADR-0002 #6) ---
	"compose": {
		Name: "compose", Usage: "/compose <feature>", Summary: "start a compose session",
		Run: (*Session).cmdCompose,
	},
	"grill": {
		Name: "grill", Usage: "/grill <notes>", Summary: "record grill notes and advance to spec",
		Run: (*Session).cmdGrill,
	},
	"spec": {
		Name: "spec", Usage: "/spec <body>", Summary: "write the feature spec document",
		Run: (*Session).cmdSpec,
	},
	"approve-spec": {
		Name: "approve-spec", Usage: "/approve-spec", Summary: "approve the written spec",
		Run: (*Session).cmdApproveSpec,
	},
	"task": {
		Name: "task", Usage: "/task <id> <title>", Summary: "add an implement-phase task",
		Run: (*Session).cmdTask,
	},
	"red": {
		Name: "red", Usage: "/red <id>", Summary: "record a failing test for a task",
		Run: (*Session).cmdRed,
	},
	"green": {
		Name: "green", Usage: "/green <id>", Summary: "record production code and mark a task green",
		Run: (*Session).cmdGreen,
	},
	"verify": {
		Name: "verify", Usage: "/verify", Summary: "record the verify-phase pass",
		Run: (*Session).cmdVerify,
	},
	"advisor": {
		Name: "advisor", Usage: "/advisor", Summary: "record that the Advisor review ran",
		Run: (*Session).cmdAdvisor,
	},
	"finding": {
		Name: "finding", Usage: "/finding <id> <detail>", Summary: "record an Advisor finding",
		Run: (*Session).cmdFinding,
	},
	"resolve": {
		Name: "resolve", Usage: "/resolve <id>", Summary: "resolve an Advisor finding",
		Run: (*Session).cmdResolve,
	},
	"advance": {
		Name: "advance", Usage: "/advance", Summary: "advance to the next compose phase",
		Run: (*Session).cmdAdvance,
	},
	"phase": {
		Name: "phase", Usage: "/phase", Summary: "report the current compose phase",
		Run: (*Session).cmdPhase,
	},

	// --- self-improvement (ADR-0002 #9, and #1's file memory) ---
	"dream": {
		Name: "dream", Usage: "/dream", Summary: "extract durable facts from this session into MEMORY.md",
		Run: (*Session).cmdDream,
	},
	"distill": {
		Name: "distill", Usage: "/distill [name]", Summary: "mine repeated tool sequences into a scaffold",
		Run: (*Session).cmdDistill,
	},

	// --- orchestration (ADR-0002 #4, #7) ---
	"subagents": {
		Name: "subagents", Usage: "/subagents [--coordinated] <prompt>[#sym] | …",
		Summary: "run prompts as parallel subagents in isolated worktrees",
		Run:     (*Session).cmdSubagents,
	},
	"workflow": {
		Name: "workflow", Usage: "/workflow <name> | load <path>", Summary: "run a registered workflow, or load a .js workflow file",
		Run: (*Session).cmdWorkflow,
	},
	"workflows": {
		Name: "workflows", Usage: "/workflows", Summary: "list registered workflows",
		Run: (*Session).cmdWorkflows,
	},

	// --- max mode (change 0016, ADR-0011) ---
	"max": {
		Name: "max", Usage: "/max [n] <prompt>",
		Summary: "answer once from n sampled candidates, judged (best-of-N)",
		Run:     (*Session).cmdMax,
	},

	// --- appearance (change 0017, ADR-0012) ---
	"theme": {
		Name: "theme", Usage: "/theme [name]", Summary: "list color themes, or switch to one",
		Run: (*Session).cmdTheme,
	},

	// --- docs (change 0025, ADR-0016) ---
	"docs": {
		Name: "docs", Usage: "/docs <library> [query]",
		Summary: "fetch up-to-date library docs via Context7",
		Run:    (*Session).cmdDocs,
	},
}

func init() {
	builtinCommands["help"] = Command{
		Name: "help", Usage: "/help", Summary: "list the available commands",
		Run: func(s *Session, _ string) (string, error) {
			return helpText(s.commands()), nil
		},
	}
}

// --- skills ---

func (s *Session) cmdSkill(args string) (string, error) {
	name := strings.TrimSpace(args)
	if name == "" {
		return "", fmt.Errorf("runtime: /skill needs a name; try /skills to list them")
	}
	if s.Skills == nil {
		return "", fmt.Errorf("runtime: no skill index available")
	}
	sk, ok := s.Skills.Find(name)
	if !ok {
		return "", fmt.Errorf("runtime: no skill named %q; discoverable: %s",
			name, strings.Join(s.skillNames(), ", "))
	}
	return fmt.Sprintf("# Skill: %s\n%s\n\n%s", sk.Name, sk.Description, sk.Body), nil
}

func (s *Session) cmdSkills(_ string) (string, error) {
	if s.Skills == nil {
		return "", fmt.Errorf("runtime: no skill index available")
	}
	all := s.Skills.All()
	if len(all) == 0 {
		return "no skills discovered (looked in .claude/skills and .opencode/skills)", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d skill(s):\n", len(all))
	for _, sk := range all {
		fmt.Fprintf(&b, "  %-24s %s\n", sk.Name, sk.Description)
	}
	return b.String(), nil
}

func (s *Session) skillNames() []string {
	if s.Skills == nil {
		return nil
	}
	all := s.Skills.All()
	names := make([]string, 0, len(all))
	for _, sk := range all {
		names = append(names, sk.Name)
	}
	sort.Strings(names)
	return names
}

// --- compose ---

// composeSession returns the active compose session, or an error telling the
// user to start one. Every phase verb goes through this.
func (s *Session) composeSession() (*compose.Session, error) {
	if s.Compose == nil {
		return nil, fmt.Errorf("runtime: no compose session; start one with /compose <feature>")
	}
	return s.Compose, nil
}

func (s *Session) cmdCompose(args string) (string, error) {
	feature := strings.TrimSpace(args)
	if feature == "" {
		return "", fmt.Errorf("runtime: /compose needs a feature name")
	}
	// Refuse to silently discard work in progress.
	if s.Compose != nil {
		return "", fmt.Errorf("runtime: already composing %q at phase %s; finish it first",
			s.Compose.Feature, s.Compose.Current())
	}
	s.Compose = compose.NewSession(s.Root, feature)
	return fmt.Sprintf("composing %q — phase %s. Next: /grill <notes>",
		feature, s.Compose.Current()), nil
}

func (s *Session) cmdGrill(args string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	if err := cs.CompleteGrill(args); err != nil {
		return "", err
	}
	return fmt.Sprintf("grill recorded — phase %s. Next: /spec <body>", cs.Current()), nil
}

func (s *Session) cmdSpec(args string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(args) == "" {
		return "", fmt.Errorf("runtime: /spec needs a spec body")
	}
	if err := cs.WriteSpec(args); err != nil {
		return "", err
	}
	return fmt.Sprintf("spec written to %s (not yet approved). Next: /approve-spec",
		cs.SpecPath()), nil
}

func (s *Session) cmdApproveSpec(_ string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	if err := cs.ApproveSpec(); err != nil {
		return "", err
	}
	return "spec approved. Next: /advance to implement", nil
}

func (s *Session) cmdTask(args string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	id, title, _ := strings.Cut(strings.TrimSpace(args), " ")
	if id == "" {
		return "", fmt.Errorf("runtime: /task needs an id and a title")
	}
	if err := cs.AddTask(id, strings.TrimSpace(title)); err != nil {
		return "", err
	}
	return fmt.Sprintf("task %s added. Next: /red %s (a failing test comes first)", id, id), nil
}

func (s *Session) cmdRed(args string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(args)
	if id == "" {
		return "", fmt.Errorf("runtime: /red needs a task id")
	}
	if err := cs.RecordFailingTest(id); err != nil {
		return "", err
	}
	return fmt.Sprintf("failing test recorded for %s. Next: /green %s", id, id), nil
}

// cmdGreen records production code and marks the task green. The TDD gate still
// refuses if no failing test was recorded first — the command surface does not
// weaken it.
func (s *Session) cmdGreen(args string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(args)
	if id == "" {
		return "", fmt.Errorf("runtime: /green needs a task id")
	}
	if err := cs.RecordProductionCode(id); err != nil {
		return "", err
	}
	if err := cs.MarkTaskGreen(id); err != nil {
		return "", err
	}
	return fmt.Sprintf("task %s is green", id), nil
}

func (s *Session) cmdVerify(_ string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	if err := cs.RecordVerifyPass(); err != nil {
		return "", err
	}
	return "verify pass recorded. Next: /advance to review", nil
}

func (s *Session) cmdAdvisor(_ string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	if err := cs.RecordAdvisorRun(); err != nil {
		return "", err
	}
	return "Advisor run recorded. Record findings with /finding, then /advance", nil
}

func (s *Session) cmdFinding(args string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	id, detail, _ := strings.Cut(strings.TrimSpace(args), " ")
	if id == "" {
		return "", fmt.Errorf("runtime: /finding needs an id and a detail")
	}
	cs.AddFinding(id, strings.TrimSpace(detail))
	return fmt.Sprintf("finding %s recorded (%d open)", id, len(cs.OpenFindings())), nil
}

func (s *Session) cmdResolve(args string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(args)
	if id == "" {
		return "", fmt.Errorf("runtime: /resolve needs a finding id")
	}
	if !cs.ResolveFinding(id) {
		return "", fmt.Errorf("runtime: no finding %q", id)
	}
	return fmt.Sprintf("finding %s resolved (%d still open)", id, len(cs.OpenFindings())), nil
}

func (s *Session) cmdAdvance(_ string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	before := cs.Current()
	if err := cs.Advance(); err != nil {
		// The gate refused: report it verbatim, and stay where we are.
		return "", fmt.Errorf("still at %s: %w", before, err)
	}
	return fmt.Sprintf("%s → %s", before, cs.Current()), nil
}

func (s *Session) cmdPhase(_ string) (string, error) {
	cs, err := s.composeSession()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "feature %q at phase %s\n", cs.Feature, cs.Current())
	for _, t := range cs.Tasks() {
		mark := " "
		if t.Green {
			mark = "x"
		}
		fmt.Fprintf(&b, "  [%s] %s %s\n", mark, t.ID, t.Title)
	}
	if open := cs.OpenFindings(); len(open) > 0 {
		fmt.Fprintf(&b, "  %d open finding(s)\n", len(open))
	}
	return b.String(), nil
}

// --- dream / distill ---

// cmdDream extracts durable facts from this session's transcript and appends them
// to MEMORY.md. Append-only by design: MEMORY.md is a file the user owns and may
// have hand-edited, so /dream adds to it and never rewrites it.
func (s *Session) cmdDream(_ string) (string, error) {
	if len(s.History) == 0 {
		return "", fmt.Errorf("runtime: nothing to extract from — this session has no history yet")
	}
	if s.Provider == nil {
		return "", fmt.Errorf("runtime: /dream needs a provider")
	}

	d := improve.Dreamer{Provider: s.Provider, Model: s.Model}
	facts, err := d.Extract(context.Background(), s.History)
	if err != nil {
		return "", fmt.Errorf("runtime: /dream: %w", err)
	}
	if len(facts) == 0 {
		return "nothing durable found in this session; MEMORY.md unchanged", nil
	}

	for _, f := range facts {
		if err := s.Memo.AppendMemory("- " + f); err != nil {
			return "", fmt.Errorf("runtime: /dream: appending to MEMORY.md: %w", err)
		}
	}
	return fmt.Sprintf("appended %d fact(s) to MEMORY.md", len(facts)), nil
}

// --- max mode ---

// cmdMax answers a prompt by sampling N candidates and returning the one an
// independent judge picks (change 0016). It is opt-in per invocation: a normal
// turn never pays for N generations.
//
// A leading integer is the sample count, but only when a prompt follows it — so
// "/max 3" is a missing prompt rather than a request to answer "3".
func (s *Session) cmdMax(args string) (string, error) {
	if s.Provider == nil {
		return "", fmt.Errorf("runtime: /max needs a provider")
	}

	n := s.MaxSamples
	prompt := strings.TrimSpace(args)
	head, rest, _ := strings.Cut(prompt, " ")
	if parsed, err := strconv.Atoi(head); err == nil {
		n = parsed
		prompt = strings.TrimSpace(rest)
	}
	if prompt == "" {
		return "", fmt.Errorf("runtime: /max needs a prompt (usage: /max [n] <prompt>)")
	}

	samples, note := orchestrate.ClampSamples(n)
	judgeModel := s.MaxModel
	if judgeModel == "" {
		judgeModel = s.Model
	}

	m := orchestrate.MaxMode{
		Sampler: orchestrate.Sampler{
			Provider: s.Provider,
			Runner:   orchestrate.Runner{MaxParallel: s.MaxSubagentParallel},
		},
		Ranker: orchestrate.Ranker{Provider: s.Provider, Model: judgeModel},
	}

	req := ports.Request{
		Model:  s.Model,
		System: s.SystemPrompt,
		Messages: []ports.Message{{
			Role:   ports.RoleUser,
			Blocks: []ports.Block{{Kind: ports.BlockText, Text: prompt}},
		}},
		// No tools: candidates answer, they do not act (the sampler enforces
		// this too; passing none makes the intent explicit here).
	}

	best, err := m.Run(context.Background(), req, samples)
	if err != nil {
		return "", fmt.Errorf("runtime: /max: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "best of %d (candidate %d)", samples, best.Index)
	if note != "" {
		fmt.Fprintf(&b, " — %s", note)
	}
	b.WriteByte('\n')
	if best.Rationale != "" {
		fmt.Fprintf(&b, "judge: %s\n", best.Rationale)
	}
	b.WriteByte('\n')
	b.WriteString(strings.TrimRight(best.Text, "\n"))
	return b.String(), nil
}

// --- appearance ---

// cmdTheme lists the front-end's color themes, or switches to one and persists
// the choice as tui.theme so it survives a restart (change 0017).
//
// The switch happens before the write: a persisted theme the front-end rejected
// would come back on the next start. A failed write is reported but does not undo
// the switch — the user asked for it, and the session should honor that.
func (s *Session) cmdTheme(args string) (string, error) {
	if s.Theme == nil {
		return "", fmt.Errorf("runtime: /theme needs an attached front-end " +
			"(themes are a TUI feature; this session is headless)")
	}
	names := s.Theme.ThemeNames()

	name := strings.TrimSpace(args)
	if name == "" {
		var b strings.Builder
		fmt.Fprintf(&b, "%d theme(s) — * marks the active one:\n", len(names))
		active := s.Theme.Theme()
		for _, n := range names {
			mark := " "
			if n == active {
				mark = "*"
			}
			fmt.Fprintf(&b, "  %s %s\n", mark, n)
		}
		b.WriteString("switch with /theme <name>")
		return b.String(), nil
	}

	known := false
	for _, n := range names {
		if n == name {
			known = true
			break
		}
	}
	if !known {
		return "", fmt.Errorf("runtime: unknown theme %q; available: %s",
			name, strings.Join(names, ", "))
	}
	if err := s.Theme.SetTheme(name); err != nil {
		return "", fmt.Errorf("runtime: /theme: %w", err)
	}
	if err := config.SetTUITheme(s.ConfigPath, name); err != nil {
		return fmt.Sprintf("theme %s is active, but it could not be saved: %v", name, err), nil
	}
	return fmt.Sprintf("theme %s is active and saved to %s", name, s.ConfigPath), nil
}

// --- orchestration ---

// coordinatedFlag opts a fan-out into symbol-coordinated mode.
const coordinatedFlag = "--coordinated"

// cmdSubagents fans prompts out as parallel subagents. Prompts are separated by
// "|" so an individual prompt can contain spaces; blank segments are dropped so a
// trailing or doubled separator is forgiving rather than an error.
//
// With --coordinated, each prompt must name the symbols it will edit
// (prompt#file.go::Func) and the fan-out claims them before running, merging each
// subagent's work on success. Without it, behavior is unchanged: isolated
// worktrees, text results, nothing written to the repository.
func (s *Session) cmdSubagents(args string) (string, error) {
	if rest, ok := cutFlag(args, coordinatedFlag); ok {
		tasks, err := parseCoordinatedTasks(rest)
		if err != nil {
			return "", err
		}
		results, err := s.FanoutCoordinated(context.Background(), tasks)
		if err != nil {
			return "", err
		}
		return CoordinatedReport(coordinatorBackend(s.Coordinator), results), nil
	}

	var prompts []string
	for seg := range strings.SplitSeq(args, "|") {
		if p := strings.TrimSpace(seg); p != "" {
			prompts = append(prompts, p)
		}
	}
	if len(prompts) == 0 {
		return "", fmt.Errorf("runtime: /subagents needs at least one prompt, " +
			"separated by | (e.g. /subagents write the docs | review the docs)")
	}

	results, err := s.Fanout(context.Background(), prompts)
	if err != nil {
		return "", err
	}
	return FanoutReport(prompts, results), nil
}

// cutFlag removes a leading flag from an argument string, reporting whether it was
// present. Only a leading position is honored, so a "--coordinated" appearing
// inside a prompt is treated as prompt text rather than silently changing mode.
func cutFlag(args, flag string) (string, bool) {
	trimmed := strings.TrimSpace(args)
	if !strings.HasPrefix(trimmed, flag) {
		return args, false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, flag)), true
}

// cmdDistill mines the session's recorded tool sequences and scaffolds the
// strongest pattern. The scaffold kind follows the pattern's shape, and an
// existing file is refused rather than overwritten.
func (s *Session) cmdDistill(args string) (string, error) {
	if len(s.Runs) == 0 {
		return "", fmt.Errorf("runtime: no recorded runs to mine — use some tools first")
	}

	patterns := improve.MinePatterns(s.Runs, improve.MineOptions{})
	if len(patterns) == 0 {
		return "no tool sequence recurred often enough to distill; nothing written", nil
	}
	top := patterns[0]

	name := strings.TrimSpace(args)
	if name == "" {
		name = strings.Join(top.Tools, "-")
	}

	kind := improve.SuggestKind(top)
	var (
		path string
		err  error
	)
	switch kind {
	case improve.KindSkill:
		path, err = improve.ScaffoldSkill(s.Root, name, top)
	case improve.KindSubagent:
		path, err = improve.ScaffoldSubagent(s.Root, name, top)
	default:
		path, err = improve.ScaffoldCommand(s.Root, name, top)
	}
	if err != nil {
		return "", fmt.Errorf("runtime: /distill: %w", err)
	}

	// A distilled skill should be usable immediately, so refresh the index.
	if kind == improve.KindSkill && s.Skills != nil {
		_, _ = s.Skills.Discover()
	}
	return fmt.Sprintf("distilled %s (%d runs) into %s: %s",
		strings.Join(top.Tools, "→"), top.Runs, kind, path), nil
}

// --- docs (change 0025, ADR-0016) ---

// context7 tool names as they appear in the registry after namespacing.
var (
	context7ResolveTool = defaultContext7Name + ".resolve-library-id"
	context7QueryTool   = defaultContext7Name + ".query-docs"
)

// libraryIDRe extracts the first Context7 library id from a resolve-library-id
// result. The result is the MCP text content — typically a JSON blob whose id
// values look like "/facebook/react". Parsing the id defensively (rather than
// assuming an exact envelope) tolerates Context7 changing its wrapper shape.
var libraryIDRe = regexp.MustCompile(`(?s)"id"\s*:\s*"([^"]+)"`)

// cmdDocs fetches up-to-date library docs from Context7. It is direct,
// read-only user action — not routed through PolicyGate (the autonomous agent
// loop's calls to context7.* tools still are). Returns the docs text.
func (s *Session) cmdDocs(args string) (string, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", fmt.Errorf("runtime: /docs needs a library name; usage: /docs <library> [query]")
	}
	parts := strings.SplitN(args, " ", 2)
	libraryName := parts[0]
	query := "usage and API overview"
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		query = strings.TrimSpace(parts[1])
	}

	resolve, ok := s.Tools.Get(context7ResolveTool)
	if !ok {
		return "", fmt.Errorf("runtime: Context7 docs not connected. " +
			"Run with no `mcp` config to get the default, or add `mcp.context7` " +
			"pointing at https://mcp.context7.com/mcp")
	}

	ctx := context.Background()
	resolveOut, err := resolve.Execute(ctx, jsonString(map[string]string{
		"libraryName": libraryName,
		"query":       query,
	}))
	if err != nil {
		return "", fmt.Errorf("runtime: /docs resolve-library-id: %w", err)
	}
	libraryID := libraryIDRe.FindStringSubmatch(resolveOut)
	if len(libraryID) < 2 {
		return "", fmt.Errorf("runtime: /docs: Context7 returned no library matching %q", libraryName)
	}

	queryTool, ok := s.Tools.Get(context7QueryTool)
	if !ok {
		return "", fmt.Errorf("runtime: Context7 %s tool unavailable", context7QueryTool)
	}
	docs, err := queryTool.Execute(ctx, jsonString(map[string]string{
		"libraryId": libraryID[1],
		"query":     query,
	}))
	if err != nil {
		return "", fmt.Errorf("runtime: /docs query-docs: %w", err)
	}
	return docs, nil
}

// jsonString marshals m for a tool call; the inputs here are simple string maps
// so an encode failure is a programmer error.
func jsonString(m map[string]string) json.RawMessage {
	b, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("runtime: encode tool args: %v", err))
	}
	return b
}
