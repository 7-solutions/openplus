package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// funcPhase adapts a func to the Phase interface for tests.
type funcPhase struct {
	name string
	run  func(ctx context.Context, st *State) (string, error)
}

func (p funcPhase) Name() string { return p.name }
func (p funcPhase) Run(ctx context.Context, st *State) (string, error) {
	return p.run(ctx, st)
}

func phase(name string, run func(ctx context.Context, st *State) (string, error)) Phase {
	return funcPhase{name: name, run: run}
}

func TestWorkflowRunsPhasesInOrder(t *testing.T) {
	var order []string
	wf := Workflow{Phases: []Phase{
		phase("first", func(ctx context.Context, st *State) (string, error) {
			order = append(order, "first")
			return "out1", nil
		}),
		phase("second", func(ctx context.Context, st *State) (string, error) {
			order = append(order, "second")
			return "out2", nil
		}),
	}}

	rep, err := wf.Run(context.Background(), &State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Join(order, ",") != "first,second" {
		t.Fatalf("order = %v", order)
	}
	if !rep.OK {
		t.Errorf("report should be OK: %+v", rep)
	}
	if len(rep.Phases) != 2 {
		t.Fatalf("report phases = %d, want 2", len(rep.Phases))
	}
	if rep.Phases[0].Output != "out1" || rep.Phases[1].Output != "out2" {
		t.Errorf("outputs = %+v", rep.Phases)
	}
}

// TestWorkflowHandsOffState proves structured hand-off: a phase sees the prior
// phase's output and can pass values forward.
func TestWorkflowHandsOffState(t *testing.T) {
	wf := Workflow{Phases: []Phase{
		phase("produce", func(ctx context.Context, st *State) (string, error) {
			st.Set("key", "value-from-first")
			return "produced", nil
		}),
		phase("consume", func(ctx context.Context, st *State) (string, error) {
			if got, ok := st.Get("key"); !ok || got != "value-from-first" {
				return "", fmt.Errorf("hand-off lost: %q ok=%v", got, ok)
			}
			if st.Last != "produced" {
				return "", fmt.Errorf("Last = %q, want produced", st.Last)
			}
			return "consumed", nil
		}),
	}}
	rep, err := wf.Run(context.Background(), &State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.OK {
		t.Fatalf("report: %+v", rep)
	}
}

// TestWorkflowBoundedRetrySucceeds is the ADR-0006 scenario: a phase failing
// under its retry budget retries and eventually passes.
func TestWorkflowBoundedRetrySucceeds(t *testing.T) {
	attempts := 0
	wf := Workflow{
		MaxRetries: 3,
		Phases: []Phase{
			phase("flaky", func(ctx context.Context, st *State) (string, error) {
				attempts++
				if attempts < 3 {
					return "", errors.New("transient")
				}
				return "eventually-ok", nil
			}),
		},
	}
	rep, err := wf.Run(context.Background(), &State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.OK {
		t.Fatalf("expected success after retries: %+v", rep)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if rep.Phases[0].Attempts != 3 {
		t.Errorf("report attempts = %d, want 3", rep.Phases[0].Attempts)
	}
}

// TestWorkflowRetryBudgetExhausted is the other half of the scenario: when the
// budget runs out the workflow fails with a report.
func TestWorkflowRetryBudgetExhausted(t *testing.T) {
	attempts := 0
	boom := errors.New("always fails")
	wf := Workflow{
		MaxRetries: 2,
		Phases: []Phase{
			phase("doomed", func(ctx context.Context, st *State) (string, error) {
				attempts++
				return "", boom
			}),
			phase("never-runs", func(ctx context.Context, st *State) (string, error) {
				t.Error("later phase must not run after a fatal failure")
				return "", nil
			}),
		},
	}
	rep, err := wf.Run(context.Background(), &State{})
	if err == nil {
		t.Fatal("expected an error when the retry budget is exhausted")
	}
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want to wrap boom", err)
	}
	// MaxRetries=2 means 1 initial attempt + 2 retries.
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3 (initial + 2 retries)", attempts)
	}
	if rep.OK {
		t.Error("report must not be OK")
	}
	if rep.FailedPhase != "doomed" {
		t.Errorf("FailedPhase = %q, want doomed", rep.FailedPhase)
	}
	if len(rep.Phases) != 1 {
		t.Errorf("report should stop at the failing phase, got %+v", rep.Phases)
	}
}

func TestWorkflowNoRetriesByDefault(t *testing.T) {
	attempts := 0
	wf := Workflow{Phases: []Phase{
		phase("once", func(ctx context.Context, st *State) (string, error) {
			attempts++
			return "", errors.New("nope")
		}),
	}}
	if _, err := wf.Run(context.Background(), &State{}); err == nil {
		t.Fatal("expected failure")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (MaxRetries unset means no retry)", attempts)
	}
}

func TestWorkflowCancellationStopsBetweenPhases(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ran := 0
	wf := Workflow{Phases: []Phase{
		phase("first", func(ctx context.Context, st *State) (string, error) {
			ran++
			cancel() // cancel mid-workflow
			return "ok", nil
		}),
		phase("second", func(ctx context.Context, st *State) (string, error) {
			ran++
			return "should-not-run", nil
		}),
	}}
	rep, err := wf.Run(ctx, &State{})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if ran != 1 {
		t.Errorf("ran %d phases, want 1", ran)
	}
	if rep.OK {
		t.Error("cancelled workflow must not report OK")
	}
}

func TestWorkflowEmptyIsOK(t *testing.T) {
	rep, err := Workflow{}.Run(context.Background(), &State{})
	if err != nil {
		t.Fatalf("empty workflow: %v", err)
	}
	if !rep.OK {
		t.Error("empty workflow should be trivially OK")
	}
}

func TestWorkflowNilStateIsCreated(t *testing.T) {
	wf := Workflow{Phases: []Phase{
		phase("uses-state", func(ctx context.Context, st *State) (string, error) {
			if st == nil {
				return "", errors.New("nil state")
			}
			st.Set("k", "v")
			return "ok", nil
		}),
	}}
	if _, err := wf.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run with nil state: %v", err)
	}
}

func TestReportStringSummarizes(t *testing.T) {
	rep := Report{
		OK: false,
		Phases: []PhaseReport{
			{Name: "a", Attempts: 1, Output: "did a"},
			{Name: "b", Attempts: 3, Err: errors.New("bad")},
		},
		FailedPhase: "b",
	}
	s := rep.String()
	for _, want := range []string{"a", "b", "bad", "FAILED"} {
		if !strings.Contains(s, want) {
			t.Errorf("report string missing %q:\n%s", want, s)
		}
	}
}

func TestStateGetMissing(t *testing.T) {
	var st State
	if got, ok := st.Get("absent"); ok || got != "" {
		t.Fatalf("Get(absent) = (%q,%v), want (\"\",false)", got, ok)
	}
}
