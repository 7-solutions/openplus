package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/7solutions/openplus/internal/compose"
	"github.com/7solutions/openplus/internal/improve"
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
