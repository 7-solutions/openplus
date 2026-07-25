package orchestrate

import (
	"context"
	"fmt"
	"strings"
)

// Phase is one step of a workflow (ADR-0006). Run receives the shared State so
// a phase can read prior hand-off values and publish its own.
type Phase interface {
	Name() string
	Run(ctx context.Context, st *State) (string, error)
}

// State is the structured hand-off between phases: a string-keyed bag plus the
// previous phase's output.
type State struct {
	// Last is the output of the most recently completed phase.
	Last string

	values map[string]string
}

// Set publishes a hand-off value for later phases.
func (s *State) Set(key, value string) {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
}

// Get reads a hand-off value.
func (s *State) Get(key string) (string, bool) {
	v, ok := s.values[key]
	return v, ok
}

// PhaseReport records one phase's outcome.
type PhaseReport struct {
	Name     string
	Attempts int
	Output   string
	Err      error
}

// Report is the workflow's structured result. On failure it stops at the phase
// that exhausted its retry budget, so the report shows exactly how far the
// workflow got (ADR-0006: "the workflow fails with a report").
type Report struct {
	OK          bool
	Phases      []PhaseReport
	FailedPhase string
}

// String renders a human-readable summary.
func (r Report) String() string {
	var b strings.Builder
	if r.OK {
		b.WriteString("workflow OK\n")
	} else {
		fmt.Fprintf(&b, "workflow FAILED at phase %q\n", r.FailedPhase)
	}
	for _, p := range r.Phases {
		if p.Err != nil {
			fmt.Fprintf(&b, "  - %s: attempts=%d error=%v\n", p.Name, p.Attempts, p.Err)
			continue
		}
		fmt.Fprintf(&b, "  - %s: attempts=%d output=%s\n", p.Name, p.Attempts, p.Output)
	}
	return b.String()
}

// Workflow is an ordered set of phases with a bounded retry budget per phase
// (ADR-0006). JS (goja) compatibility is deferred behind this same shape.
type Workflow struct {
	Phases []Phase
	// MaxRetries is the number of *additional* attempts per phase after the
	// first. Zero means a phase runs once.
	MaxRetries int
}

// Run executes the phases in order, retrying a failing phase up to MaxRetries
// extra attempts. The first phase to exhaust its budget aborts the workflow;
// later phases do not run. The returned Report is always populated, including on
// failure and cancellation.
func (w Workflow) Run(ctx context.Context, st *State) (Report, error) {
	if st == nil {
		st = &State{}
	}
	rep := Report{OK: true}

	for _, p := range w.Phases {
		// Honor cancellation between phases so a cancelled workflow stops
		// promptly rather than starting more work.
		if err := ctx.Err(); err != nil {
			rep.OK = false
			rep.FailedPhase = p.Name()
			return rep, fmt.Errorf("orchestrate: workflow cancelled before phase %q: %w", p.Name(), err)
		}

		pr, out, err := w.runPhase(ctx, p, st)
		rep.Phases = append(rep.Phases, pr)
		if err != nil {
			rep.OK = false
			rep.FailedPhase = p.Name()
			return rep, fmt.Errorf("orchestrate: phase %q failed after %d attempt(s): %w",
				p.Name(), pr.Attempts, err)
		}
		st.Last = out
	}
	return rep, nil
}

// runPhase runs one phase with its retry budget, returning its report, final
// output, and terminal error (nil when it eventually succeeded).
func (w Workflow) runPhase(ctx context.Context, p Phase, st *State) (PhaseReport, string, error) {
	pr := PhaseReport{Name: p.Name()}
	var lastErr error

	for attempt := 0; attempt <= w.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			pr.Err = err
			return pr, "", err
		}
		pr.Attempts++

		out, err := p.Run(ctx, st)
		if err == nil {
			pr.Output = out
			return pr, out, nil
		}
		lastErr = err
	}

	pr.Err = lastErr
	return pr, "", lastErr
}
