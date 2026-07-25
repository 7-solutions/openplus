// Package compose implements the spec→ship phase machine (ADR-0002) that
// enforces the house gates: grill → spec → implement → verify → review →
// finish. The machine's job is to make the gates unskippable — a phase cannot
// be left until its gate is satisfied, so "no code before an approved spec" and
// "no finish with an open Advisor finding" are structural rather than advisory.
package compose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sentinel errors. Callers use errors.Is to distinguish a blocked gate (retry
// after satisfying it) from a TDD violation (the caller did something in the
// wrong order).
var (
	// ErrGateBlocked means the current phase's gate is not satisfied yet.
	ErrGateBlocked = errors.New("compose: phase gate blocked")
	// ErrTDDViolation means production code was attempted before a failing test.
	ErrTDDViolation = errors.New("compose: TDD violation")
	// ErrWrongPhase means the operation is not valid in the current phase.
	ErrWrongPhase = errors.New("compose: wrong phase")
)

// Phase is one step of the compose workflow.
type Phase int

const (
	PhaseGrill Phase = iota
	PhaseSpec
	PhaseImplement
	PhaseVerify
	PhaseReview
	PhaseFinish
)

func (p Phase) String() string {
	switch p {
	case PhaseGrill:
		return "grill"
	case PhaseSpec:
		return "spec"
	case PhaseImplement:
		return "implement"
	case PhaseVerify:
		return "verify"
	case PhaseReview:
		return "review"
	case PhaseFinish:
		return "finish"
	default:
		return fmt.Sprintf("phase(%d)", int(p))
	}
}

// Phases returns the ordered phase sequence.
func Phases() []Phase {
	return []Phase{PhaseGrill, PhaseSpec, PhaseImplement, PhaseVerify, PhaseReview, PhaseFinish}
}

// Task is one implementable unit inside the implement phase. The TDD gate
// tracks its red→green progression.
type Task struct {
	ID    string
	Title string
	// FailingTest records that a red test was shown for this task.
	FailingTest bool
	// ProductionCode records that production code was written (only legal after
	// FailingTest).
	ProductionCode bool
	// Green records that the task's tests now pass.
	Green bool
}

// Finding is one Advisor review finding.
type Finding struct {
	ID       string
	Detail   string
	Resolved bool
}

// Session is one feature's walk through the compose phases.
type Session struct {
	// Root is the project root; feature documents live under
	// <Root>/docs/compose/spec/.
	Root string
	// Feature names the feature being composed.
	Feature string

	phase Phase

	grillNotes   string
	specWritten  bool
	specApproved bool

	tasks []Task

	verifyPassed bool

	advisorRan bool
	findings   []Finding
}

// NewSession starts a compose session for feature, rooted at root.
func NewSession(root, feature string) *Session {
	return &Session{Root: root, Feature: feature, phase: PhaseGrill}
}

// Current returns the active phase.
func (s *Session) Current() Phase { return s.phase }

// SpecPath is where this feature's spec document lives.
func (s *Session) SpecPath() string {
	return filepath.Join(s.Root, "docs", "compose", "spec", s.Feature+".md")
}

// CompleteGrill records the clarifying notes from the grill phase and advances
// to spec. Empty notes mean the grill did not actually happen.
func (s *Session) CompleteGrill(notes string) error {
	if s.phase != PhaseGrill {
		return fmt.Errorf("%w: CompleteGrill in %s", ErrWrongPhase, s.phase)
	}
	if strings.TrimSpace(notes) == "" {
		return fmt.Errorf("%w: grill produced no notes", ErrGateBlocked)
	}
	s.grillNotes = notes
	s.phase = PhaseSpec
	return nil
}

// GrillNotes returns the recorded grill notes.
func (s *Session) GrillNotes() string { return s.grillNotes }

// WriteSpec writes the feature spec document. Writing is not approval — the
// spec gate still requires ApproveSpec.
func (s *Session) WriteSpec(body string) error {
	if s.phase != PhaseSpec {
		return fmt.Errorf("%w: WriteSpec in %s", ErrWrongPhase, s.phase)
	}
	path := s.SpecPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("compose: mkdir spec dir: %w", err)
	}
	content := fmt.Sprintf("# %s\n\n%s\n", s.Feature, strings.TrimSpace(body))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("compose: write spec: %w", err)
	}
	s.specWritten = true
	return nil
}

// ApproveSpec marks the written spec approved, opening the implement gate.
func (s *Session) ApproveSpec() error {
	if s.phase != PhaseSpec {
		return fmt.Errorf("%w: ApproveSpec in %s", ErrWrongPhase, s.phase)
	}
	if !s.specWritten {
		return fmt.Errorf("%w: no spec has been written", ErrGateBlocked)
	}
	s.specApproved = true
	return nil
}

// AddTask registers an implementable task in the implement phase.
func (s *Session) AddTask(id, title string) error {
	if s.phase != PhaseImplement {
		return fmt.Errorf("%w: AddTask in %s", ErrWrongPhase, s.phase)
	}
	if id == "" {
		return fmt.Errorf("compose: task needs an id")
	}
	for _, t := range s.tasks {
		if t.ID == id {
			return fmt.Errorf("compose: task %q already exists", id)
		}
	}
	s.tasks = append(s.tasks, Task{ID: id, Title: title})
	return nil
}

// Tasks returns the registered tasks.
func (s *Session) Tasks() []Task {
	out := make([]Task, len(s.tasks))
	copy(out, s.tasks)
	return out
}

// RecordFailingTest records that a failing (red) test was shown for a task —
// the precondition for writing production code.
func (s *Session) RecordFailingTest(id string) error {
	t, err := s.task(id)
	if err != nil {
		return err
	}
	t.FailingTest = true
	return nil
}

// RecordProductionCode records production code for a task. It is refused unless
// a failing test was recorded first (TDD gate, T-081).
func (s *Session) RecordProductionCode(id string) error {
	t, err := s.task(id)
	if err != nil {
		return err
	}
	if !t.FailingTest {
		return fmt.Errorf("%w: task %q has no failing test yet (red before green)", ErrTDDViolation, id)
	}
	t.ProductionCode = true
	return nil
}

// MarkTaskGreen records that a task's tests now pass. Requires production code,
// which in turn required a red test.
func (s *Session) MarkTaskGreen(id string) error {
	t, err := s.task(id)
	if err != nil {
		return err
	}
	if !t.ProductionCode {
		return fmt.Errorf("%w: task %q has no production code to be green", ErrTDDViolation, id)
	}
	t.Green = true
	return nil
}

// RecordVerifyPass records the verify phase's evidence (build/tests green).
func (s *Session) RecordVerifyPass() error {
	if s.phase != PhaseVerify {
		return fmt.Errorf("%w: RecordVerifyPass in %s", ErrWrongPhase, s.phase)
	}
	s.verifyPassed = true
	return nil
}

// RecordAdvisorRun records that the Advisor review pass actually ran. Zero
// findings only counts when the Advisor ran (T-081).
func (s *Session) RecordAdvisorRun() error {
	if s.phase != PhaseReview {
		return fmt.Errorf("%w: RecordAdvisorRun in %s", ErrWrongPhase, s.phase)
	}
	s.advisorRan = true
	return nil
}

// AddFinding records an Advisor finding. Open findings block finish.
func (s *Session) AddFinding(id, detail string) {
	s.findings = append(s.findings, Finding{ID: id, Detail: detail})
}

// ResolveFinding marks a finding resolved, reporting whether the id was found.
func (s *Session) ResolveFinding(id string) bool {
	for i := range s.findings {
		if s.findings[i].ID == id {
			s.findings[i].Resolved = true
			return true
		}
	}
	return false
}

// OpenFindings returns the unresolved findings.
func (s *Session) OpenFindings() []Finding {
	var out []Finding
	for _, f := range s.findings {
		if !f.Resolved {
			out = append(out, f)
		}
	}
	return out
}

// Advance moves to the next phase if the current phase's gate is satisfied,
// otherwise returns ErrGateBlocked and stays put.
func (s *Session) Advance() error {
	if err := s.gate(); err != nil {
		return err
	}
	if s.phase == PhaseFinish {
		return fmt.Errorf("compose: already at %s", PhaseFinish)
	}
	s.phase++
	return nil
}

// gate reports whether the current phase may be left.
func (s *Session) gate() error {
	switch s.phase {
	case PhaseGrill:
		if strings.TrimSpace(s.grillNotes) == "" {
			return fmt.Errorf("%w: grill produced no notes", ErrGateBlocked)
		}
	case PhaseSpec:
		// The house rule: no code before an approved spec.
		if !s.specWritten {
			return fmt.Errorf("%w: no spec written for %q", ErrGateBlocked, s.Feature)
		}
		if !s.specApproved {
			return fmt.Errorf("%w: spec for %q is not approved", ErrGateBlocked, s.Feature)
		}
	case PhaseImplement:
		if len(s.tasks) == 0 {
			return fmt.Errorf("%w: implement has no tasks", ErrGateBlocked)
		}
		for _, t := range s.tasks {
			if !t.Green {
				return fmt.Errorf("%w: task %q is not green", ErrGateBlocked, t.ID)
			}
		}
	case PhaseVerify:
		if !s.verifyPassed {
			return fmt.Errorf("%w: verify has no recorded pass", ErrGateBlocked)
		}
	case PhaseReview:
		if !s.advisorRan {
			return fmt.Errorf("%w: the Advisor review has not run", ErrGateBlocked)
		}
		if open := s.OpenFindings(); len(open) > 0 {
			return fmt.Errorf("%w: %d unresolved finding(s), first: %s",
				ErrGateBlocked, len(open), open[0].ID)
		}
	}
	return nil
}

// task finds a task by id, requiring the implement phase.
func (s *Session) task(id string) (*Task, error) {
	if s.phase != PhaseImplement {
		return nil, fmt.Errorf("%w: task operations require %s, in %s", ErrWrongPhase, PhaseImplement, s.phase)
	}
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			return &s.tasks[i], nil
		}
	}
	return nil, fmt.Errorf("compose: unknown task %q", id)
}
