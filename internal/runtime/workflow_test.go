package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/orchestrate"
	"github.com/7solutions/openplus/internal/ports"
)

// --- T-1110: promptPhase ---

// TestPromptPhaseIsAPhase pins that promptPhase is the first production
// implementation of orchestrate.Phase.
func TestPromptPhaseIsAPhase(t *testing.T) {
	var _ orchestrate.Phase = promptPhase{}
}

func TestPromptPhaseRunsATurn(t *testing.T) {
	s := cmdSession(t)
	s.Provider = &alwaysProvider{reply: "the phase answer"}

	p := promptPhase{session: s, name: "draft", prompt: "write something"}
	if p.Name() != "draft" {
		t.Errorf("Name() = %q", p.Name())
	}
	out, err := p.Run(context.Background(), &orchestrate.State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "the phase answer" {
		t.Errorf("Run output = %q", out)
	}
}

func TestPromptPhaseSurfacesProviderError(t *testing.T) {
	s := cmdSession(t)
	s.Provider = &failOnProvider{failWhenContains: "doomed"}

	p := promptPhase{session: s, name: "bad", prompt: "doomed prompt"}
	if _, err := p.Run(context.Background(), &orchestrate.State{}); err == nil {
		t.Fatal("expected the provider failure to reach the workflow")
	}
}

// --- T-1111: hand-off ---

// TestPromptPhaseHandsOffOutput is the spec scenario: phase two can read phase
// one's output.
func TestPromptPhaseHandsOffOutput(t *testing.T) {
	s := cmdSession(t)
	rec := &promptRecorder{reply: "PHASE-OUTPUT"}
	s.Provider = rec

	wf := orchestrate.Workflow{Phases: []orchestrate.Phase{
		promptPhase{session: s, name: "first", prompt: "do the first thing"},
		// {{previous}} is substituted with the prior phase's output.
		promptPhase{session: s, name: "second", prompt: "now build on: {{previous}}"},
	}}

	rep, err := wf.Run(context.Background(), &orchestrate.State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.OK {
		t.Fatalf("report not OK: %s", rep)
	}
	if len(rec.prompts) != 2 {
		t.Fatalf("expected two prompts, got %v", rec.prompts)
	}
	if !strings.Contains(rec.prompts[1], "PHASE-OUTPUT") {
		t.Errorf("phase two did not receive phase one's output: %q", rec.prompts[1])
	}
	if strings.Contains(rec.prompts[1], "{{previous}}") {
		t.Errorf("placeholder left unsubstituted: %q", rec.prompts[1])
	}
}

func TestPromptPhaseNoPlaceholderIsVerbatim(t *testing.T) {
	s := cmdSession(t)
	rec := &promptRecorder{reply: "x"}
	s.Provider = rec

	p := promptPhase{session: s, name: "plain", prompt: "just this"}
	st := &orchestrate.State{Last: "earlier output"}
	if _, err := p.Run(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if rec.prompts[0] != "just this" {
		t.Errorf("prompt altered without a placeholder: %q", rec.prompts[0])
	}
}

// --- T-1120/T-1121: registration and invocation ---

func TestWorkflowsRegisteredAtAssembly(t *testing.T) {
	s := cmdSession(t)
	if len(s.Workflows) == 0 {
		t.Fatal("no built-in workflow registered; the engine would be unexercised")
	}
}

func TestCmdWorkflowsLists(t *testing.T) {
	s := cmdSession(t)
	out := run(t, s, "/workflows")
	if strings.TrimSpace(out) == "" {
		t.Fatal("/workflows returned nothing")
	}
	// every registered name must be listed
	for name := range s.Workflows {
		if !strings.Contains(out, name) {
			t.Errorf("/workflows omits %q:\n%s", name, out)
		}
	}
}

// TestCmdWorkflowsEmptyIsHonest is the spec scenario.
func TestCmdWorkflowsEmptyIsHonest(t *testing.T) {
	s := cmdSession(t)
	s.Workflows = nil
	out := run(t, s, "/workflows")
	if !strings.Contains(strings.ToLower(out), "no workflows") {
		t.Errorf("expected an honest empty report: %s", out)
	}
}

// TestCmdWorkflowRunsBuiltin is the spec scenario.
func TestCmdWorkflowRunsBuiltin(t *testing.T) {
	s := cmdSession(t)
	s.Provider = &alwaysProvider{reply: "phase output"}

	// pick whichever built-in is registered
	var name string
	for n := range s.Workflows {
		name = n
		break
	}
	out := run(t, s, "/workflow "+name)
	if !strings.Contains(strings.ToLower(out), "ok") &&
		!strings.Contains(out, "phase output") {
		t.Errorf("/workflow %s produced no recognizable report:\n%s", name, out)
	}
}

// TestCmdWorkflowUnknownListsKnown is the spec scenario.
func TestCmdWorkflowUnknownListsKnown(t *testing.T) {
	s := cmdSession(t)
	err := runErr(t, s, "/workflow definitely-not-registered")
	if !strings.Contains(err.Error(), "definitely-not-registered") {
		t.Errorf("error should name the miss: %v", err)
	}
	for name := range s.Workflows {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error should list %q: %v", name, err)
		}
	}
}

func TestCmdWorkflowNeedsAName(t *testing.T) {
	s := cmdSession(t)
	if err := runErr(t, s, "/workflow"); !strings.Contains(err.Error(), "name") {
		t.Errorf("error should ask for a name: %v", err)
	}
}

// --- T-1122: retry budget ---

// TestWorkflowRetryBudgetReported is the spec scenario: a phase that keeps
// failing exhausts its budget and the report names it.
func TestWorkflowRetryBudgetReported(t *testing.T) {
	s := cmdSession(t)
	s.Provider = &failOnProvider{failWhenContains: "always"}

	wf := orchestrate.Workflow{
		MaxRetries: 2,
		Phases: []orchestrate.Phase{
			promptPhase{session: s, name: "doomed", prompt: "always fails"},
		},
	}
	rep, err := wf.Run(context.Background(), &orchestrate.State{})
	if err == nil {
		t.Fatal("expected the workflow to fail")
	}
	if rep.OK {
		t.Error("report should not be OK")
	}
	if rep.FailedPhase != "doomed" {
		t.Errorf("FailedPhase = %q, want doomed", rep.FailedPhase)
	}
	// 1 initial attempt + 2 retries
	if len(rep.Phases) != 1 || rep.Phases[0].Attempts != 3 {
		t.Errorf("attempts = %+v, want 3", rep.Phases)
	}
	if !strings.Contains(rep.String(), "doomed") {
		t.Errorf("report text should name the phase:\n%s", rep.String())
	}
}

// promptRecorder records the user prompt of each request.
type promptRecorder struct {
	reply   string
	prompts []string
}

func (p *promptRecorder) Stream(_ context.Context, req ports.Request) (<-chan ports.Event, error) {
	for _, m := range req.Messages {
		for _, b := range m.Blocks {
			if b.Text != "" {
				p.prompts = append(p.prompts, b.Text)
			}
		}
	}
	ch := make(chan ports.Event, 2)
	ch <- ports.Event{Kind: ports.EventTextDelta, Text: p.reply}
	ch <- ports.Event{Kind: ports.EventTurnEnd}
	close(ch)
	return ch, nil
}
