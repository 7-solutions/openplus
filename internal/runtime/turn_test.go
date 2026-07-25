package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/contextmgr"
	"github.com/7solutions/openplus/internal/ports"
)

// memProject builds a project whose memory store is backed by a deterministic
// in-process embedder, so retrieval is testable without a network endpoint.
func memProject(t *testing.T) *Session {
	t.Helper()
	root := project(t, `{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}},
  "embedder": {"model": "nomic-embed-text", "baseURL": "http://localhost:11434/v1"},
  "memory": {"autoOpen": true}
}`)
	s, err := Assemble(root, Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// swap in an offline embedder: the real one would need a live endpoint
	s.Embedder = fakeEmbedder{dim: 8}
	s.Memory.Embedder = s.Embedder
	return s
}

type fakeEmbedder struct{ dim int }

func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, tx := range texts {
		v := make([]float32, f.dim)
		for j := range v {
			v[j] = float32(len(tx)%7) + float32(j)/10
		}
		out[i] = v
	}
	return out, nil
}
func (f fakeEmbedder) Dim() int { return f.dim }

func TestAssembleContextIncludesSystemPrompt(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true, BaseSystemPrompt: "BASE PROMPT"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.AssembleContext(context.Background(), "do the thing", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if !strings.Contains(got.System, "BASE PROMPT") {
		t.Errorf("assembled system prompt missing the base: %q", got.System)
	}
	if !strings.Contains(got.System, "cgo-free") {
		t.Errorf("assembled system prompt missing AGENTS.md: %q", got.System)
	}
}

// TestAssembleContextInjectsRetrievedMemory is the spec scenario for memory.
func TestAssembleContextInjectsRetrievedMemory(t *testing.T) {
	s := memProject(t)
	if _, err := s.Memory.Write(context.Background(), "the deploy script lives in scripts/ship.sh", "note"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := s.AssembleContext(context.Background(), "where is the deploy script", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if !strings.Contains(got.System, "scripts/ship.sh") {
		t.Errorf("retrieved memory not injected:\n%s", got.System)
	}
}

func TestAssembleContextWithoutMemoryStillWorks(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.AssembleContext(context.Background(), "anything", nil)
	if err != nil {
		t.Fatalf("AssembleContext without memory: %v", err)
	}
	if got.System == "" {
		t.Error("expected a system prompt even with no memory")
	}
}

// TestAssembleContextAutoLoadsSkill is the spec scenario for skills.
func TestAssembleContextAutoLoadsSkill(t *testing.T) {
	root := project(t, "")
	write(t, root, ".claude/skills/deploy/SKILL.md",
		"---\nname: deploy\ndescription: Deploy the service to kubernetes production\n---\n"+
			"Run scripts/ship.sh then verify rollout status.")
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.AssembleContext(context.Background(), "deploy the service to kubernetes", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if !strings.Contains(got.System, "verify rollout status") {
		t.Errorf("relevant skill did not auto-load:\n%s", got.System)
	}
}

func TestAssembleContextSkipsIrrelevantSkill(t *testing.T) {
	root := project(t, "")
	write(t, root, ".claude/skills/deploy/SKILL.md",
		"---\nname: deploy\ndescription: Deploy the service to kubernetes\n---\nship it")
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.AssembleContext(context.Background(), "what is the capital of France", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if strings.Contains(got.System, "ship it") {
		t.Errorf("irrelevant skill was auto-loaded:\n%s", got.System)
	}
}

// TestAssembleContextRespectsBudget is the spec scenario for budgeting.
func TestAssembleContextRespectsBudget(t *testing.T) {
	s := memProject(t)
	// a large memory corpus that cannot possibly fit a tiny budget
	for _, chunk := range []string{
		strings.Repeat("alpha detail about the deploy script. ", 200),
		strings.Repeat("beta detail about the deploy script. ", 200),
	} {
		if _, err := s.Memory.Write(context.Background(), chunk, "note"); err != nil {
			t.Fatal(err)
		}
	}
	// shrink the budget to just past the system prompt
	tk := contextmgr.Heuristic{}
	s.Budgeter = contextmgr.Budgeter{Tokenizer: tk, Budget: tk.Count(s.SystemPrompt) + 10}

	got, err := s.AssembleContext(context.Background(), "deploy script", nil)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	// the system prompt survives; the oversized memory does not
	if !strings.Contains(got.System, "cgo-free") {
		t.Errorf("system prompt was dropped under budget pressure:\n%s", got.System)
	}
	if strings.Contains(got.System, "alpha detail") {
		t.Errorf("oversized memory was injected despite the budget:\n%s", got.System)
	}
}

func TestAssembleContextCarriesHistory(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	prior := []ports.Message{{
		Role:   ports.RoleUser,
		Blocks: []ports.Block{{Kind: ports.BlockText, Text: "earlier question"}},
	}}
	got, err := s.AssembleContext(context.Background(), "follow-up", prior)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}
	if len(got.History) == 0 {
		t.Fatal("prior history dropped")
	}
	last := got.History[len(got.History)-1]
	if last.Blocks[0].Text != "follow-up" {
		t.Errorf("the new user message should be last, got %q", last.Blocks[0].Text)
	}
}

// --- T-111: Run ---

func TestRunDrivesTheLoop(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	hist, err := s.Run(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(hist) < 2 {
		t.Fatalf("history = %d messages, want the user turn plus a reply", len(hist))
	}
	if hist[0].Blocks[0].Text != "hello" {
		t.Errorf("first message = %q, want the user input", hist[0].Blocks[0].Text)
	}
	// the fake provider's reply must be in the history
	var replied bool
	for _, m := range hist {
		for _, b := range m.Blocks {
			if strings.Contains(b.Text, "runtime is wired") {
				replied = true
			}
		}
	}
	if !replied {
		t.Errorf("assistant reply missing from history: %+v", hist)
	}
}

func TestRunPersistsTurnToMemory(t *testing.T) {
	s := memProject(t)
	s.Provider = fakeProvider()

	if _, err := s.Run(context.Background(), "remember this exchange", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := s.Memory.Search(context.Background(), "remember this exchange", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("the turn was not persisted to memory")
	}
}

func TestRunWithoutMemoryDoesNotError(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(context.Background(), "no memory configured", nil); err != nil {
		t.Fatalf("Run without memory: %v", err)
	}
}

func TestRunEmptyInputErrors(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(context.Background(), "   ", nil); err == nil {
		t.Fatal("expected an error for empty input")
	}
}

func TestRunCancelledContext(t *testing.T) {
	s, err := Assemble(project(t, ""), Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Run(ctx, "anything", nil); err == nil {
		t.Fatal("expected a cancellation error")
	}
}
