package ports

import (
	"context"
	"testing"
	"time"
)

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
