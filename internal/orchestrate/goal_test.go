package orchestrate

import (
	"context"
	"strings"
	"testing"

	"github.com/7-solutions/openplus/internal/ports"
	portsfake "github.com/7-solutions/openplus/internal/ports/providerfake"
)

// judgeSays builds a Fake provider that streams one verdict as text.
func judgeSays(verdict string) *portsfake.Fake {
	return &portsfake.Fake{Scripts: [][]ports.Event{{
		{Kind: ports.EventTextDelta, Text: verdict},
		{Kind: ports.EventTurnEnd},
	}}}
}

func history(texts ...string) []ports.Message {
	msgs := make([]ports.Message, 0, len(texts))
	for _, tx := range texts {
		msgs = append(msgs, ports.Message{
			Role:   ports.RoleAssistant,
			Blocks: []ports.Block{{Kind: ports.BlockText, Text: tx}},
		})
	}
	return msgs
}

func TestJudgeMetAllowsStop(t *testing.T) {
	j := Judge{Provider: judgeSays("MET: the tests pass and the feature works")}
	v, err := j.Evaluate(context.Background(), "make the tests pass", history("I ran the tests, all green"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !v.Met {
		t.Fatalf("verdict = %+v, want Met", v)
	}
}

// TestJudgeUnmetRejectsStop is the spec scenario: the agent tries to stop, the
// judge finds the goal unmet, and the agent must continue with feedback.
func TestJudgeUnmetRejectsStop(t *testing.T) {
	j := Judge{Provider: judgeSays("UNMET: you never ran the test suite")}
	v, err := j.Evaluate(context.Background(), "make the tests pass", history("I think it's fine"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if v.Met {
		t.Fatal("judge should reject a premature stop")
	}
	if !strings.Contains(v.Feedback, "never ran the test suite") {
		t.Errorf("feedback = %q, want the judge's reason", v.Feedback)
	}
}

func TestJudgeVerdictParsingIsCaseInsensitive(t *testing.T) {
	for _, verdict := range []string{"met", "Met: fine", "MET"} {
		j := Judge{Provider: judgeSays(verdict)}
		v, err := j.Evaluate(context.Background(), "goal", history("work"))
		if err != nil {
			t.Fatalf("Evaluate(%q): %v", verdict, err)
		}
		if !v.Met {
			t.Errorf("verdict %q parsed as unmet", verdict)
		}
	}
	for _, verdict := range []string{"unmet", "UNMET: nope", "Unmet"} {
		j := Judge{Provider: judgeSays(verdict)}
		v, err := j.Evaluate(context.Background(), "goal", history("work"))
		if err != nil {
			t.Fatalf("Evaluate(%q): %v", verdict, err)
		}
		if v.Met {
			t.Errorf("verdict %q parsed as met", verdict)
		}
	}
}

// TestJudgeAmbiguousVerdictIsUnmet is the safe default: if the judge's answer
// cannot be read as approval, the agent keeps working rather than stopping.
func TestJudgeAmbiguousVerdictIsUnmet(t *testing.T) {
	j := Judge{Provider: judgeSays("hmm, hard to say either way")}
	v, err := j.Evaluate(context.Background(), "goal", history("work"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if v.Met {
		t.Fatal("an ambiguous verdict must not authorize stopping")
	}
	if v.Feedback == "" {
		t.Error("ambiguous verdict should still carry the judge's text as feedback")
	}
}

func TestJudgeEmptyGoalAlwaysMet(t *testing.T) {
	// No goal configured means nothing gates stopping; the judge must not be
	// consulted at all.
	j := Judge{Provider: &portsfake.Fake{Scripts: [][]ports.Event{{
		{Kind: ports.EventTextDelta, Text: "UNMET: should never be asked"},
		{Kind: ports.EventTurnEnd},
	}}}}
	v, err := j.Evaluate(context.Background(), "", history("anything"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !v.Met {
		t.Fatal("with no goal set, stopping is always allowed")
	}
}

func TestJudgeUsesJudgeModel(t *testing.T) {
	// The judge must be independent: it sends its own model, not the agent's.
	rec := &recordingProvider{reply: "MET: ok"}
	j := Judge{Provider: rec, Model: "anthropic/judge-model"}
	if _, err := j.Evaluate(context.Background(), "goal", history("work")); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if rec.gotModel != "anthropic/judge-model" {
		t.Errorf("model = %q, want the judge model", rec.gotModel)
	}
	// the judge prompt must carry both the goal and the transcript
	if !strings.Contains(rec.gotSystem+rec.gotUser, "goal") {
		t.Errorf("judge prompt missing the goal: system=%q user=%q", rec.gotSystem, rec.gotUser)
	}
	if !strings.Contains(rec.gotUser, "work") {
		t.Errorf("judge prompt missing the transcript: %q", rec.gotUser)
	}
	// the judge must not be handed the agent's tools — it only renders a verdict
	if rec.gotTools != 0 {
		t.Errorf("judge was given %d tools, want 0", rec.gotTools)
	}
}

func TestJudgeProviderErrorPropagates(t *testing.T) {
	j := Judge{Provider: &portsfake.Fake{Scripts: [][]ports.Event{{
		{Kind: ports.EventError, Err: errJudgeBoom},
	}}}}
	if _, err := j.Evaluate(context.Background(), "goal", history("work")); err == nil {
		t.Fatal("expected the provider error to propagate")
	}
}

func TestJudgeNilProviderErrors(t *testing.T) {
	j := Judge{}
	if _, err := j.Evaluate(context.Background(), "goal", history("work")); err == nil {
		t.Fatal("expected an error with no judge provider configured")
	}
}

// recordingProvider captures the request the judge sends.
type recordingProvider struct {
	reply     string
	gotModel  string
	gotSystem string
	gotUser   string
	gotTools  int
}

func (r *recordingProvider) Stream(ctx context.Context, req ports.Request) (<-chan ports.Event, error) {
	r.gotModel = req.Model
	r.gotSystem = req.System
	r.gotTools = len(req.Tools)
	var b strings.Builder
	for _, m := range req.Messages {
		for _, blk := range m.Blocks {
			b.WriteString(blk.Text)
			b.WriteString(blk.ToolResultText)
			b.WriteByte(' ')
		}
	}
	r.gotUser = b.String()

	ch := make(chan ports.Event, 2)
	ch <- ports.Event{Kind: ports.EventTextDelta, Text: r.reply}
	ch <- ports.Event{Kind: ports.EventTurnEnd}
	close(ch)
	return ch, nil
}
