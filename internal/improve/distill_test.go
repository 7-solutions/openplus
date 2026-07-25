package improve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runs(seqs ...[]string) []Run {
	out := make([]Run, len(seqs))
	for i, s := range seqs {
		out[i] = Run{Tools: s}
	}
	return out
}

func TestMinePatternsFindsRepeatedSequence(t *testing.T) {
	// the same read->edit->bash sequence appears in three runs
	got := MinePatterns(runs(
		[]string{"read", "edit", "bash"},
		[]string{"glob", "read", "edit", "bash"},
		[]string{"read", "edit", "bash", "grep"},
	), MineOptions{MinLength: 3, MinRuns: 3})

	if len(got) == 0 {
		t.Fatal("expected at least one mined pattern")
	}
	top := got[0]
	if strings.Join(top.Tools, ",") != "read,edit,bash" {
		t.Fatalf("top pattern = %v, want read,edit,bash", top.Tools)
	}
	if top.Runs != 3 {
		t.Errorf("Runs = %d, want 3", top.Runs)
	}
}

func TestMinePatternsRespectsMinRuns(t *testing.T) {
	// the sequence appears twice; MinRuns=3 must reject it
	got := MinePatterns(runs(
		[]string{"read", "edit", "bash"},
		[]string{"read", "edit", "bash"},
	), MineOptions{MinLength: 3, MinRuns: 3})
	if len(got) != 0 {
		t.Fatalf("patterns = %+v, want none below MinRuns", got)
	}
}

func TestMinePatternsRespectsMinLength(t *testing.T) {
	got := MinePatterns(runs(
		[]string{"read", "edit"},
		[]string{"read", "edit"},
		[]string{"read", "edit"},
	), MineOptions{MinLength: 3, MinRuns: 2})
	if len(got) != 0 {
		t.Fatalf("patterns = %+v, want none below MinLength", got)
	}
}

// TestMinePatternsCountsRunsNotOccurrences guards against double-counting a
// sequence that repeats inside a single run.
func TestMinePatternsCountsRunsNotOccurrences(t *testing.T) {
	got := MinePatterns(runs(
		[]string{"read", "edit", "bash", "read", "edit", "bash"},
	), MineOptions{MinLength: 3, MinRuns: 2})
	if len(got) != 0 {
		t.Fatalf("a sequence repeated within one run is not a cross-run pattern: %+v", got)
	}
}

func TestMinePatternsPrefersLongerThenMoreFrequent(t *testing.T) {
	got := MinePatterns(runs(
		[]string{"read", "edit", "bash", "grep"},
		[]string{"read", "edit", "bash", "grep"},
		[]string{"read", "edit", "bash", "grep"},
	), MineOptions{MinLength: 2, MinRuns: 2})
	if len(got) == 0 {
		t.Fatal("expected patterns")
	}
	// the full 4-tool sequence should outrank its shorter sub-sequences
	if len(got[0].Tools) != 4 {
		t.Fatalf("top pattern = %v, want the longest sequence", got[0].Tools)
	}
}

func TestMinePatternsEmptyInput(t *testing.T) {
	if got := MinePatterns(nil, MineOptions{}); len(got) != 0 {
		t.Fatalf("patterns = %+v, want none", got)
	}
}

func TestMineOptionsDefaults(t *testing.T) {
	// zero options must still behave sanely rather than mining 1-tool "patterns"
	got := MinePatterns(runs(
		[]string{"read", "edit", "bash"},
		[]string{"read", "edit", "bash"},
	), MineOptions{})
	for _, p := range got {
		if len(p.Tools) < 2 {
			t.Fatalf("mined a degenerate pattern with defaults: %+v", p)
		}
	}
}

// --- scaffold generation ---

func TestScaffoldSkillWritesFile(t *testing.T) {
	root := t.TempDir()
	p := Pattern{Tools: []string{"read", "edit", "bash"}, Runs: 4}
	path, err := ScaffoldSkill(root, "fix-and-verify", p)
	if err != nil {
		t.Fatalf("ScaffoldSkill: %v", err)
	}
	want := filepath.Join(root, ".claude", "skills", "fix-and-verify", "SKILL.md")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read scaffold: %v", err)
	}
	s := string(body)
	// frontmatter the SkillIndex can parse (T-050)
	if !strings.HasPrefix(s, "---\n") {
		t.Errorf("scaffold must start with frontmatter:\n%s", s)
	}
	if !strings.Contains(s, "name: fix-and-verify") {
		t.Errorf("missing name in frontmatter:\n%s", s)
	}
	if !strings.Contains(s, "description:") {
		t.Errorf("missing description in frontmatter:\n%s", s)
	}
	// the mined sequence is documented in the body
	for _, tool := range p.Tools {
		if !strings.Contains(s, tool) {
			t.Errorf("scaffold body missing %q:\n%s", tool, s)
		}
	}
}

// TestScaffoldSkillIsDiscoverable closes the loop: a distilled skill must be
// discoverable by the SkillIndex it was generated for.
func TestScaffoldSkillIsDiscoverable(t *testing.T) {
	root := t.TempDir()
	p := Pattern{Tools: []string{"glob", "grep", "read"}, Runs: 3}
	if _, err := ScaffoldSkill(root, "locate-code", p); err != nil {
		t.Fatalf("ScaffoldSkill: %v", err)
	}

	idx := newSkillIndex(filepath.Join(root, ".claude", "skills"))
	found, err := idx.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("discovered %d skills, want 1: %+v", len(found), found)
	}
	if found[0].Name != "locate-code" {
		t.Errorf("discovered name = %q", found[0].Name)
	}
	if found[0].Description == "" {
		t.Error("discovered skill has no description — BM25 ranking would be blind")
	}
}

func TestScaffoldSkillRejectsBadName(t *testing.T) {
	root := t.TempDir()
	p := Pattern{Tools: []string{"a", "b"}, Runs: 2}
	for _, name := range []string{"", "../escape", "has/slash"} {
		if _, err := ScaffoldSkill(root, name, p); err == nil {
			t.Errorf("ScaffoldSkill(%q) should be rejected", name)
		}
	}
}

func TestScaffoldSkillDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	p := Pattern{Tools: []string{"read", "edit"}, Runs: 2}
	if _, err := ScaffoldSkill(root, "dup", p); err != nil {
		t.Fatal(err)
	}
	if _, err := ScaffoldSkill(root, "dup", p); err == nil {
		t.Fatal("a second scaffold must not clobber the first")
	}
}

func TestScaffoldCommandWritesFile(t *testing.T) {
	root := t.TempDir()
	p := Pattern{Tools: []string{"bash", "read"}, Runs: 5}
	path, err := ScaffoldCommand(root, "check", p)
	if err != nil {
		t.Fatalf("ScaffoldCommand: %v", err)
	}
	want := filepath.Join(root, ".opencode", "command", "check.md")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "bash") {
		t.Errorf("command scaffold missing the mined tools:\n%s", body)
	}
}

func TestScaffoldSubagentWritesFile(t *testing.T) {
	root := t.TempDir()
	p := Pattern{Tools: []string{"glob", "grep"}, Runs: 3}
	path, err := ScaffoldSubagent(root, "locator", p)
	if err != nil {
		t.Fatalf("ScaffoldSubagent: %v", err)
	}
	want := filepath.Join(root, ".opencode", "agent", "locator.md")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	body, _ := os.ReadFile(path)
	s := string(body)
	if !strings.Contains(s, "grep") {
		t.Errorf("subagent scaffold missing the mined tools:\n%s", s)
	}
	// a subagent scaffold should declare the tools it needs
	if !strings.Contains(s, "tools:") {
		t.Errorf("subagent scaffold should declare its tools:\n%s", s)
	}
}

func TestSuggestKindByPatternShape(t *testing.T) {
	// read-only sequences suit a subagent; mutating ones suit a skill.
	readOnly := Pattern{Tools: []string{"glob", "grep", "read"}, Runs: 3}
	if got := SuggestKind(readOnly); got != KindSubagent {
		t.Errorf("SuggestKind(read-only) = %v, want subagent", got)
	}
	mutating := Pattern{Tools: []string{"read", "edit", "bash"}, Runs: 3}
	if got := SuggestKind(mutating); got != KindSkill {
		t.Errorf("SuggestKind(mutating) = %v, want skill", got)
	}
	short := Pattern{Tools: []string{"bash", "bash"}, Runs: 9}
	if got := SuggestKind(short); got != KindCommand {
		t.Errorf("SuggestKind(short+frequent) = %v, want command", got)
	}
}
