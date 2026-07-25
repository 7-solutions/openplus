package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/7solutions/openplus/internal/orchestrate"
	"github.com/7solutions/openplus/internal/provider"
)

// maxSession builds a session whose provider plays n generations followed by the
// judge's answer. Sampling order is scheduling-dependent, but every generation
// script precedes the judge's, so the judge always draws the last one.
func maxSession(t *testing.T, gens []string, judge string) *Session {
	t.Helper()
	s, err := Assemble(project(t, `{"model":"fake/fake"}`), Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	scripts := make([][]provider.Event, 0, len(gens)+1)
	for _, g := range gens {
		scripts = append(scripts, []provider.Event{
			{Kind: provider.EventTextDelta, Text: g},
			{Kind: provider.EventTurnEnd},
		})
	}
	scripts = append(scripts, []provider.Event{
		{Kind: provider.EventTextDelta, Text: judge},
		{Kind: provider.EventTurnEnd},
	})
	s.Provider = &provider.Fake{Scripts: scripts}
	return s
}

// T-1627: /max runs best-of-N and returns the winning answer.
func TestCmdMaxReturnsWinner(t *testing.T) {
	s := maxSession(t, []string{"answer A", "answer B", "answer C"}, "BEST: 1\nB is clearest")
	out := run(t, s, "/max 3 how do I ship this")

	if !strings.Contains(out, "answer") {
		t.Fatalf("/max output has no winning answer:\n%s", out)
	}
	if !strings.Contains(out, "B is clearest") {
		t.Errorf("/max should report the judge's rationale:\n%s", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("/max should report how many samples it ran:\n%s", out)
	}
}

// T-1627: N defaults to orchestrate.DefaultSamples when none is given, and the
// leading number is only consumed when it really is the count.
func TestCmdMaxDefaultN(t *testing.T) {
	s := maxSession(t, []string{"a", "b", "c"}, "BEST: 0\nfine")
	out := run(t, s, "/max how do I ship this")
	if !strings.Contains(out, "3") {
		t.Errorf("default N should be %d and be reported:\n%s", orchestrate.DefaultSamples, out)
	}

	// A prompt that starts with a number keeps it as prompt text when it is the
	// whole argument — /max 3 alone has no prompt and must be refused.
	if err := runErr(t, s, "/max 3"); !strings.Contains(err.Error(), "prompt") {
		t.Errorf("/max with no prompt error = %v", err)
	}
	if err := runErr(t, s, "/max"); !strings.Contains(err.Error(), "prompt") {
		t.Errorf("/max with no arguments error = %v", err)
	}
}

// T-1625/T-1627: an over-cap N is clamped and the clamp is reported.
func TestCmdMaxClampsN(t *testing.T) {
	s := maxSession(t, nil, "BEST: 0\nok")
	out := run(t, s, "/max 99 pick something")
	if !strings.Contains(out, "clamped") {
		t.Errorf("over-cap N should report the clamp:\n%s", out)
	}
	if strings.Contains(out, "99") && !strings.Contains(out, "clamped to") {
		t.Errorf("output implies 99 samples ran:\n%s", out)
	}
}

// T-1627: config max.samples sets the default N, and max.model the judge's model.
func TestCmdMaxUsesConfig(t *testing.T) {
	s, err := Assemble(project(t, `{"model":"fake/fake","max":{"samples":2,"model":"fake/judge"}}`),
		Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if s.MaxSamples != 2 {
		t.Fatalf("MaxSamples = %d, want 2 from config", s.MaxSamples)
	}
	if s.MaxModel != "fake/judge" {
		t.Fatalf("MaxModel = %q, want fake/judge from config", s.MaxModel)
	}

	rec := &recordingFake{}
	s.Provider = rec
	out := run(t, s, "/max weigh the options")
	if !strings.Contains(out, "2") {
		t.Errorf("configured sample count not reported:\n%s", out)
	}
	// Two generations on the session model, then the judge on its own.
	if got := rec.models(); len(got) != 3 {
		t.Fatalf("provider calls = %v, want 3", got)
	}
	if last := rec.models()[2]; last != "fake/judge" {
		t.Errorf("judge ran on model %q, want fake/judge", last)
	}
}

// T-1627: a normal turn is unaffected — Max Mode is opt-in only.
func TestMaxModeIsOptIn(t *testing.T) {
	s := maxSession(t, []string{"a"}, "BEST: 0")
	if _, handled, _ := s.Dispatch(t.Context(), "just a normal message"); handled {
		t.Error("a normal message must not be handled as /max")
	}
}

// recordingFake records the model each call ran on and always answers "BEST: 0".
// Sampling calls it from several goroutines, so the record is mutex-guarded.
type recordingFake struct {
	mu   sync.Mutex
	seen []string
}

func (r *recordingFake) models() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

func (r *recordingFake) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	r.mu.Lock()
	r.seen = append(r.seen, req.Model)
	r.mu.Unlock()
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Kind: provider.EventTextDelta, Text: "BEST: 0\nchosen"}
	ch <- provider.Event{Kind: provider.EventTurnEnd}
	close(ch)
	return ch, nil
}
