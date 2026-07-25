// Package ports — the architectural seam between core and adapters.
//
// In addition to the compile-time port assertions, this package owns a
// regression guard that bans the prior SQL-stack dependencies from being
// re-introduced as direct deps. Change 0019 + 0020 established that
// Turso v0.2.2 is the only supported driver, and that the
// sqlite-vec + ncruces pairing is broken (ABI break at v0.21+). This
// test fails the build if either is brought back as a direct dep.
//
// Pattern after the internal/ports/leak_guard_test.go guard (T-1808).
package ports

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bannedDirectDeps is the list of SQL-stack packages that change 0020
// explicitly removed. Banning them as direct deps is the durable form
// of the "core depends on ports" rule, applied to the driver story.
var bannedDirectDeps = []string{
	"github.com/asg017/sqlite-vec-go-bindings",
	"github.com/ncruces/go-sqlite3",
}

// TestNoBannedDirectDeps walks the repo's go.mod and fails if any
// banned package is a direct (non-indirect) dependency. Run from the
// repo root via `go test ./internal/ports/...`.
func TestNoBannedDirectDeps(t *testing.T) {
	root := findRepoRoot(t)
	goMod := readFile(t, filepath.Join(root, "go.mod"))
	requireDirect := false // the banned list is direct-only
	for _, dep := range bannedDirectDeps {
		// A direct line looks like:  github.com/... vX.Y.Z
		// An indirect line is commented: // github.com/... vX.Y.Z
		for _, line := range strings.Split(goMod, "\n") {
			if !strings.Contains(line, dep) {
				continue
			}
			trimmed := strings.TrimSpace(line)
			// Indirect lines are commented out.
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// Must be inside a require block (post-Go 1.17 modules).
			if !requireDirect && !isInsideRequireBlock(goMod, line) {
				continue
			}
			t.Errorf("banned direct dependency %q is in go.mod (change 0020 removed it). "+
				"Use github.com/tursodatabase/turso-go (the canonical driver) "+
				"instead — see openspec/changes/0020-turso-migration/tasks.md.",
				dep)
		}
	}
}

// findRepoRoot walks up from the test process's CWD until it finds
// a directory containing go.mod. Catches the common case of running
// `go test ./...` from any subdirectory of the repo.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod walking up from " + dir)
		}
		dir = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// isInsideRequireBlock returns true if the line is inside a
// non-indirect `require` block. The format is two require blocks
// in this repo's go.mod (a "direct" block followed by an `// indirect`
// block); we treat anything in the first block as direct, anything in
// the second as indirect. Implementation: the first `require` block
// has no leading `//` for its members; the second is preceded by a
// `// indirect` comment header.
func isInsideRequireBlock(goMod, line string) bool {
	// Locate the first two `require` keywords.
	firstReq := strings.Index(goMod, "require (")
	if firstReq < 0 {
		return true // no require blocks; conservative true.
	}
	secondReq := strings.Index(goMod[firstReq+1:], "require (")
	if secondReq < 0 {
		// Only one require block: everything inside is direct.
		end := strings.Index(goMod[firstReq:], ")")
		if end < 0 {
			return true
		}
		body := goMod[firstReq : firstReq+end]
		return strings.Contains(body, line)
	}
	secondReq += firstReq + 1
	// If the line is in the first block, it's direct.
	firstEnd := strings.Index(goMod[firstReq:], ")")
	if firstEnd < 0 {
		return true
	}
	firstEnd += firstReq
	if lineAtOffset(goMod, line) < firstEnd {
		return true
	}
	// Otherwise it's in the second (or later) block — check if any
	// intervening `// indirect` comment is present.
	if strings.Contains(goMod[firstEnd:secondReq], "// indirect") {
		return false
	}
	// No // indirect marker — second block is still a direct block?
	// Conservative: treat as direct.
	return true
}

func lineAtOffset(goMod, needle string) int {
	return strings.Index(goMod, needle)
}

// --- existing port tests (compile-time assertions live in ports.go) ---

// TestAllTenPortsAreDeclared is the T-004 acceptance test: every port named in
// the design has a compile-time assertion below, so removing or breaking one
// fails the build rather than drifting silently.
func TestAllTenPortsAreDeclared(t *testing.T) {
	// The assertions live at package scope (see the var block in go's
	// companion fakes); reaching this point means they all compiled.
	names := PortNames()
	if len(names) != 10 {
		t.Fatalf("PortNames() = %v, want 10 ports", names)
	}
	want := map[string]bool{
		"Provider": true, "Embedder": true, "MemoryStore": true, "Tool": true,
		"SkillIndex": true, "Tokenizer": true, "Budgeter": true,
		"Checkpointer": true, "PolicyGate": true, "Workflow": true,
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected port %q", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("missing port %q", n)
	}
}

func TestFakeProviderStreams(t *testing.T) {
	var p Provider = FakeProvider{}
	events, err := p.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var kinds []EventKind
	for ev := range events {
		kinds = append(kinds, ev.Kind)
	}
	if len(kinds) != 1 || kinds[0] != EventTurnEnd {
		t.Fatalf("fake provider events = %v, want a single TurnEnd", kinds)
	}
}

func TestFakeEmbedderReturnsPinnedDim(t *testing.T) {
	var e Embedder = FakeEmbedder{Dimension: 4}
	vecs, err := e.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("vectors = %d, want 2", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 4 {
			t.Errorf("vecs[%d] dim = %d, want 4", i, len(v))
		}
	}
	if e.Dim() != 4 {
		t.Errorf("Dim() = %d, want 4", e.Dim())
	}
}

func TestFakeMemoryStoreRoundTrips(t *testing.T) {
	var m MemoryStore = &FakeMemoryStore{}
	id, err := m.Write(context.Background(), "a remembered fact", "test")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id <= 0 {
		t.Fatalf("id = %d, want a positive row id", id)
	}
	got, err := m.Search(context.Background(), "remembered", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0] != "a remembered fact" {
		t.Fatalf("Search = %v, want the written fact", got)
	}
}

func TestFakeMemoryStoreSearchMiss(t *testing.T) {
	var m MemoryStore = &FakeMemoryStore{}
	if _, err := m.Write(context.Background(), "apples", "t"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Search(context.Background(), "oranges", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Search = %v, want no matches", got)
	}
}

func TestFakeToolExecutes(t *testing.T) {
	var tl Tool = FakeTool{ToolName: "noop", Result: "done"}
	if tl.Name() != "noop" {
		t.Errorf("Name() = %q", tl.Name())
	}
	if len(tl.Schema()) == 0 {
		t.Error("Schema() must return a JSON schema")
	}
	out, err := tl.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "done" {
		t.Errorf("Execute = %q, want done", out)
	}
}

func TestFakeSkillIndexFinds(t *testing.T) {
	var s SkillIndex = FakeSkillIndex{Names: []string{"deploy", "migrate"}}
	if got := s.Rank("deploy the thing", 1); len(got) != 1 || got[0] != "deploy" {
		t.Fatalf("Rank = %v, want [deploy]", got)
	}
	if _, ok := s.Find("migrate"); !ok {
		t.Error("Find(migrate) should succeed")
	}
	if _, ok := s.Find("absent"); ok {
		t.Error("Find(absent) should fail")
	}
}

func TestFakeTokenizerCounts(t *testing.T) {
	var tk Tokenizer = FakeTokenizer{}
	if got := tk.Count(""); got != 0 {
		t.Errorf("Count(\"\") = %d, want 0", got)
	}
	if got := tk.Count("four"); got <= 0 {
		t.Errorf("Count(non-empty) = %d, want > 0", got)
	}
}

func TestFakeBudgeterPassesThrough(t *testing.T) {
	var b Budgeter = FakeBudgeter{}
	msgs := []Message{{Role: RoleUser}}
	got := b.Fit(1, msgs)
	if len(got) != len(msgs) {
		t.Fatalf("Fit dropped messages: %d -> %d", len(msgs), len(got))
	}
}

func TestFakeCheckpointerRoundTrips(t *testing.T) {
	var c Checkpointer = &FakeCheckpointer{}
	if c.ShouldCheckpoint(1) {
		t.Error("the no-op checkpointer should never ask to checkpoint")
	}
	if err := c.Save("state snapshot"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := c.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "state snapshot" {
		t.Fatalf("Load = %q", got)
	}
}

func TestFakePolicyGateAllows(t *testing.T) {
	var g PolicyGate = FakePolicyGate{}
	ok, err := g.Permit(context.Background(), ToolCall{Name: "bash"})
	if err != nil {
		t.Fatalf("Permit: %v", err)
	}
	if !ok {
		t.Error("the allow-all fake should permit")
	}
}

func TestFakePolicyGateDenyMode(t *testing.T) {
	var g PolicyGate = FakePolicyGate{DenyAll: true}
	ok, _ := g.Permit(context.Background(), ToolCall{Name: "bash"})
	if ok {
		t.Error("DenyAll fake should refuse")
	}
}

func TestFakeWorkflowRuns(t *testing.T) {
	var w Workflow = FakeWorkflow{PhaseNames: []string{"a", "b"}}
	if got := w.Phases(); len(got) != 2 {
		t.Fatalf("Phases() = %v", got)
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestFakeWorkflowRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var w Workflow = FakeWorkflow{PhaseNames: []string{"a"}}
	if err := w.Run(ctx); err == nil {
		t.Fatal("a cancelled context should stop the fake workflow")
	}
}

// TestFakesAreCheapEnoughForTests guards the point of the no-op fakes: they must
// not touch the network, the disk, or the clock.
func TestFakesAreCheapEnoughForTests(t *testing.T) {
	start := time.Now()
	for range 1000 {
		_, _ = FakeEmbedder{Dimension: 8}.Embed(context.Background(), []string{"x"})
		_ = FakeTokenizer{}.Count("some text to count")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("fakes are too slow to be useful in tests: %v", elapsed)
	}
}
