package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/improve"
	"github.com/7solutions/openplus/internal/ports"
	portsfake "github.com/7solutions/openplus/internal/ports/providerfake"
)

// run dispatches and fails the test on a dispatch error.
func run(t *testing.T, s *Session, input string) string {
	t.Helper()
	out, handled, err := s.Dispatch(context.Background(), input)
	if !handled {
		t.Fatalf("%q was not handled as a command", input)
	}
	if err != nil {
		t.Fatalf("%q: %v", input, err)
	}
	return out
}

// runErr dispatches expecting an error, and returns it.
func runErr(t *testing.T, s *Session, input string) error {
	t.Helper()
	_, handled, err := s.Dispatch(context.Background(), input)
	if !handled {
		t.Fatalf("%q was not handled", input)
	}
	if err == nil {
		t.Fatalf("%q: expected an error", input)
	}
	return err
}

// --- T-910/T-911: skills ---

// skillSession builds a project with exactly one discoverable skill. HOME is
// redirected so a developer's own ~/.claude/skills cannot influence assertions
// about what is discoverable.
func skillSession(t *testing.T) *Session {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := project(t, "")
	write(t, root, ".claude/skills/deploy/SKILL.md",
		"---\nname: deploy\ndescription: Deploy the service to kubernetes\n---\nRun scripts/ship.sh")
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	return s
}

func TestCmdSkillReturnsBody(t *testing.T) {
	s := skillSession(t)
	out := run(t, s, "/skill deploy")
	if !strings.Contains(out, "scripts/ship.sh") {
		t.Errorf("/skill did not return the body:\n%s", out)
	}
	if !strings.Contains(out, "deploy") {
		t.Errorf("/skill output should name the skill:\n%s", out)
	}
}

// TestCmdSkillMissingListsAvailable is the spec scenario: the error names what
// the user could have run instead.
func TestCmdSkillMissingListsAvailable(t *testing.T) {
	s := skillSession(t)
	err := runErr(t, s, "/skill nonexistent")
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should name the miss: %v", err)
	}
	if !strings.Contains(err.Error(), "deploy") {
		t.Errorf("error should list discoverable skills: %v", err)
	}
}

func TestCmdSkillNeedsAName(t *testing.T) {
	s := skillSession(t)
	if err := runErr(t, s, "/skill"); !strings.Contains(err.Error(), "name") {
		t.Errorf("error should ask for a name: %v", err)
	}
}

func TestCmdSkillsLists(t *testing.T) {
	s := skillSession(t)
	out := run(t, s, "/skills")
	if !strings.Contains(out, "deploy") || !strings.Contains(out, "kubernetes") {
		t.Errorf("/skills should list name and description:\n%s", out)
	}
}

// TestCmdSkillsEmptyIsNotAnError is the spec scenario.
//
// HOME is redirected to an empty dir: skillRoots deliberately includes
// ~/.claude/skills, so a developer's own user-level skills would otherwise leak
// into this assertion. That discovery is correct in production — the isolation
// belongs in the test, not the code.
func TestCmdSkillsEmptyIsNotAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	out := run(t, s, "/skills")
	if !strings.Contains(strings.ToLower(out), "no skills") {
		t.Errorf("expected an honest report, got:\n%s", out)
	}
}

// --- T-920..T-922: compose ---

func TestCmdComposeStartsAtGrill(t *testing.T) {
	s := cmdSession(t)
	out := run(t, s, "/compose widget-api")
	if !strings.Contains(out, "grill") {
		t.Errorf("/compose should report the grill phase:\n%s", out)
	}
	if s.Compose == nil {
		t.Fatal("Session.Compose not set")
	}
}

// TestCmdComposeRefusesSecondSession pins that starting a second compose does
// not silently discard the first.
func TestCmdComposeRefusesSecondSession(t *testing.T) {
	s := cmdSession(t)
	run(t, s, "/compose first-feature")
	err := runErr(t, s, "/compose second-feature")
	if !strings.Contains(err.Error(), "first-feature") {
		t.Errorf("error should name the in-flight feature: %v", err)
	}
}

// TestComposePhaseVerbsNeedASession is the spec scenario: every verb says to
// start a session first.
func TestComposePhaseVerbsNeedASession(t *testing.T) {
	verbs := []string{
		"/grill notes", "/spec body", "/approve-spec", "/task T1 title",
		"/red T1", "/green T1", "/verify", "/advisor",
		"/finding F1 detail", "/resolve F1", "/advance", "/phase",
	}
	for _, v := range verbs {
		s := cmdSession(t) // fresh: no compose session
		err := runErr(t, s, v)
		if !strings.Contains(err.Error(), "/compose") {
			t.Errorf("%s error should point at /compose: %v", v, err)
		}
	}
}

// TestComposeSpecGateEnforcedThroughCommands is the spec scenario: the surface
// does not weaken the gate.
func TestComposeSpecGateEnforcedThroughCommands(t *testing.T) {
	s := cmdSession(t)
	run(t, s, "/compose gated")
	run(t, s, "/grill we clarified the requirements")
	run(t, s, "/spec the design body")

	// unapproved spec: advance must refuse and stay put
	err := runErr(t, s, "/advance")
	if !strings.Contains(err.Error(), "spec") {
		t.Errorf("blocked gate should mention the spec: %v", err)
	}
	if got := s.Compose.Current().String(); got != "spec" {
		t.Fatalf("phase advanced past a blocked gate: %s", got)
	}

	// approving opens it
	run(t, s, "/approve-spec")
	out := run(t, s, "/advance")
	if !strings.Contains(out, "implement") {
		t.Errorf("/advance should reach implement:\n%s", out)
	}
}

// TestComposeTDDGateEnforcedThroughCommands pins that /green without /red is
// refused through the command surface.
func TestComposeTDDGateEnforcedThroughCommands(t *testing.T) {
	s := cmdSession(t)
	run(t, s, "/compose tdd")
	run(t, s, "/grill notes")
	run(t, s, "/spec body")
	run(t, s, "/approve-spec")
	run(t, s, "/advance") // -> implement
	run(t, s, "/task T1 the task")

	if err := runErr(t, s, "/green T1"); !strings.Contains(strings.ToLower(err.Error()), "failing test") {
		t.Errorf("/green before /red should cite the missing failing test: %v", err)
	}
	run(t, s, "/red T1")
	run(t, s, "/green T1")
}

func TestComposeReviewGateNeedsAdvisor(t *testing.T) {
	s := cmdSession(t)
	for _, c := range []string{
		"/compose review", "/grill n", "/spec b", "/approve-spec", "/advance",
		"/task T1 t", "/red T1", "/green T1", "/advance", "/verify", "/advance",
	} {
		run(t, s, c)
	}
	if got := s.Compose.Current().String(); got != "review" {
		t.Fatalf("expected review phase, got %s", got)
	}
	// no Advisor run yet
	if err := runErr(t, s, "/advance"); !strings.Contains(strings.ToLower(err.Error()), "advisor") {
		t.Errorf("review gate should cite the Advisor: %v", err)
	}
	run(t, s, "/advisor")
	run(t, s, "/finding F1 unchecked error")
	if err := runErr(t, s, "/advance"); !strings.Contains(strings.ToLower(err.Error()), "unresolved") {
		t.Errorf("open finding should block finish: %v", err)
	}
	run(t, s, "/resolve F1")
	out := run(t, s, "/advance")
	if !strings.Contains(out, "finish") {
		t.Errorf("/advance should reach finish:\n%s", out)
	}
}

func TestCmdPhaseReportsState(t *testing.T) {
	s := cmdSession(t)
	run(t, s, "/compose reported")
	out := run(t, s, "/phase")
	if !strings.Contains(out, "reported") || !strings.Contains(out, "grill") {
		t.Errorf("/phase should report feature and phase:\n%s", out)
	}
}

// --- T-930..T-932: dream ---

// dreamSession wires a provider that returns two bullet facts.
func dreamSession(t *testing.T) *Session {
	t.Helper()
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	s.Provider = &portsfake.Fake{Scripts: [][]ports.Event{{
		{Kind: ports.EventTextDelta, Text: "- the build is cgo-free by policy\n- tests run offline\n"},
		{Kind: ports.EventTurnEnd},
	}}}
	s.History = []ports.Message{{
		Role:   ports.RoleUser,
		Blocks: []ports.Block{{Kind: ports.BlockText, Text: "we fixed the build"}},
	}}
	return s
}

// TestCmdDreamAppendsFacts is the spec scenario.
func TestCmdDreamAppendsFacts(t *testing.T) {
	s := dreamSession(t)
	out := run(t, s, "/dream")
	if !strings.Contains(out, "2") {
		t.Errorf("/dream should report the count: %s", out)
	}

	body, err := os.ReadFile(filepath.Join(s.Root, "MEMORY.md"))
	if err != nil {
		t.Fatalf("MEMORY.md not written: %v", err)
	}
	for _, want := range []string{"cgo-free", "offline"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("MEMORY.md missing %q:\n%s", want, body)
		}
	}
}

// TestCmdDreamNeverRewritesExisting is the spec scenario and the risk named in
// the proposal: MEMORY.md is a file the user owns.
func TestCmdDreamNeverRewritesExisting(t *testing.T) {
	s := dreamSession(t)
	const handWritten = "- HAND-WRITTEN-LINE the user typed this themselves"
	if err := os.WriteFile(filepath.Join(s.Root, "MEMORY.md"),
		[]byte("# My memory\n"+handWritten+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	run(t, s, "/dream")

	body, err := os.ReadFile(filepath.Join(s.Root, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), handWritten) {
		t.Fatalf("/dream destroyed hand-written content:\n%s", body)
	}
	if !strings.Contains(string(body), "cgo-free") {
		t.Errorf("new facts not appended:\n%s", body)
	}
	// order matters: existing content comes first
	if strings.Index(string(body), handWritten) > strings.Index(string(body), "cgo-free") {
		t.Errorf("appended facts should follow existing content:\n%s", body)
	}
}

// TestCmdDreamEmptyExtractionIsHonest is the spec scenario.
func TestCmdDreamEmptyExtractionIsHonest(t *testing.T) {
	s := dreamSession(t)
	s.Provider = &portsfake.Fake{Scripts: [][]ports.Event{{
		{Kind: ports.EventTextDelta, Text: "I found nothing worth keeping."},
		{Kind: ports.EventTurnEnd},
	}}}

	out := run(t, s, "/dream")
	if !strings.Contains(strings.ToLower(out), "nothing") {
		t.Errorf("expected an honest report: %s", out)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "MEMORY.md")); !os.IsNotExist(err) {
		t.Error("MEMORY.md should be untouched when nothing was extracted")
	}
}

// TestCmdDreamNeedsHistory is the spec scenario.
func TestCmdDreamNeedsHistory(t *testing.T) {
	s := cmdSession(t) // no History
	if err := runErr(t, s, "/dream"); !strings.Contains(strings.ToLower(err.Error()), "history") {
		t.Errorf("error should say there is no transcript: %v", err)
	}
}

// --- T-940/T-941: distill ---

func TestCmdDistillNeedsRuns(t *testing.T) {
	s := cmdSession(t)
	if err := runErr(t, s, "/distill"); !strings.Contains(strings.ToLower(err.Error()), "run") {
		t.Errorf("error should mention missing runs: %v", err)
	}
}

// TestCmdDistillNoPatternIsHonest is the spec scenario.
func TestCmdDistillNoPatternIsHonest(t *testing.T) {
	s := cmdSession(t)
	// a single run: nothing recurs across runs
	s.Runs = []improve.Run{{Tools: []string{"read", "edit", "bash"}}}
	out := run(t, s, "/distill")
	if !strings.Contains(strings.ToLower(out), "nothing") &&
		!strings.Contains(strings.ToLower(out), "not") {
		t.Errorf("expected an honest no-pattern report: %s", out)
	}
}

// TestCmdDistillWritesDiscoverableScaffold is the spec scenario: the scaffold is
// found by the skill index afterwards.
func TestCmdDistillWritesDiscoverableScaffold(t *testing.T) {
	s := cmdSession(t)
	seq := []string{"read", "edit", "bash"}
	s.Runs = []improve.Run{{Tools: seq}, {Tools: seq}, {Tools: seq}}

	out := run(t, s, "/distill fix-and-verify")
	if !strings.Contains(out, "fix-and-verify") {
		t.Errorf("/distill should report the name: %s", out)
	}

	// a mutating sequence routes to a skill, which the index must then find
	found, err := s.Skills.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var names []string
	for _, sk := range found {
		names = append(names, sk.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "fix-and-verify") {
		t.Errorf("distilled skill not discoverable: %v", names)
	}
}

// TestCmdDistillRefusesToClobber is the spec scenario.
func TestCmdDistillRefusesToClobber(t *testing.T) {
	s := cmdSession(t)
	seq := []string{"read", "edit", "bash"}
	s.Runs = []improve.Run{{Tools: seq}, {Tools: seq}}

	run(t, s, "/distill dup")
	if err := runErr(t, s, "/distill dup"); !strings.Contains(strings.ToLower(err.Error()), "exists") {
		t.Errorf("second /distill should refuse: %v", err)
	}
}
