package compose

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newSession(t *testing.T, feature string) *Session {
	t.Helper()
	return NewSession(t.TempDir(), feature)
}

func TestPhaseSequenceOrder(t *testing.T) {
	want := []Phase{PhaseGrill, PhaseSpec, PhaseImplement, PhaseVerify, PhaseReview, PhaseFinish}
	got := Phases()
	if len(got) != len(want) {
		t.Fatalf("Phases() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Phases() = %v, want %v", got, want)
		}
	}
}

func TestSessionStartsAtGrill(t *testing.T) {
	s := newSession(t, "my-feature")
	if s.Current() != PhaseGrill {
		t.Fatalf("Current() = %v, want grill", s.Current())
	}
}

func TestGrillAdvancesToSpec(t *testing.T) {
	s := newSession(t, "f")
	if err := s.CompleteGrill("clarified the requirements"); err != nil {
		t.Fatalf("CompleteGrill: %v", err)
	}
	if s.Current() != PhaseSpec {
		t.Fatalf("Current() = %v, want spec", s.Current())
	}
}

// TestSpecGateBlocksImplement is the spec scenario: with no approved spec the
// implement phase cannot start.
func TestSpecGateBlocksImplement(t *testing.T) {
	s := newSession(t, "f")
	if err := s.CompleteGrill("ok"); err != nil {
		t.Fatal(err)
	}
	// writing a spec is not enough — it must be approved
	if err := s.WriteSpec("## Spec\nthe design"); err != nil {
		t.Fatalf("WriteSpec: %v", err)
	}
	err := s.Advance()
	if err == nil {
		t.Fatal("Advance must fail while the spec is unapproved")
	}
	if !errors.Is(err, ErrGateBlocked) {
		t.Errorf("err = %v, want ErrGateBlocked", err)
	}
	if s.Current() != PhaseSpec {
		t.Fatalf("phase advanced despite the blocked gate: %v", s.Current())
	}

	// approving it opens the gate
	if err := s.ApproveSpec(); err != nil {
		t.Fatalf("ApproveSpec: %v", err)
	}
	if err := s.Advance(); err != nil {
		t.Fatalf("Advance after approval: %v", err)
	}
	if s.Current() != PhaseImplement {
		t.Fatalf("Current() = %v, want implement", s.Current())
	}
}

func TestApproveSpecRequiresWrittenSpec(t *testing.T) {
	s := newSession(t, "f")
	if err := s.CompleteGrill("ok"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveSpec(); err == nil {
		t.Fatal("cannot approve a spec that was never written")
	}
}

func TestWriteSpecCreatesFeatureDocument(t *testing.T) {
	root := t.TempDir()
	s := NewSession(root, "widget-api")
	if err := s.CompleteGrill("ok"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteSpec("## Spec\nbuild the widget api"); err != nil {
		t.Fatalf("WriteSpec: %v", err)
	}
	path := filepath.Join(root, "docs", "compose", "spec", "widget-api.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the spec document at %s: %v", path, err)
	}
	if !strings.Contains(string(body), "build the widget api") {
		t.Errorf("spec document content = %q", body)
	}
}

func TestSpecPathIsFeatureScoped(t *testing.T) {
	s := NewSession("/root", "my-feature")
	want := filepath.Join("/root", "docs", "compose", "spec", "my-feature.md")
	if got := s.SpecPath(); got != want {
		t.Fatalf("SpecPath() = %q, want %q", got, want)
	}
}

// --- T-081: TDD per task ---

func atImplement(t *testing.T) *Session {
	t.Helper()
	s := newSession(t, "f")
	if err := s.CompleteGrill("ok"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteSpec("spec body"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveSpec(); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestTDDGateRedBeforeGreen is the spec scenario: production code is refused
// until a failing test has been recorded for that task.
func TestTDDGateRedBeforeGreen(t *testing.T) {
	s := atImplement(t)
	if err := s.AddTask("T1", "add the endpoint"); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	// production code before a red test is refused
	err := s.RecordProductionCode("T1")
	if err == nil {
		t.Fatal("production code must be blocked before a failing test")
	}
	if !errors.Is(err, ErrTDDViolation) {
		t.Errorf("err = %v, want ErrTDDViolation", err)
	}

	// record the red test, then production code is allowed
	if err := s.RecordFailingTest("T1"); err != nil {
		t.Fatalf("RecordFailingTest: %v", err)
	}
	if err := s.RecordProductionCode("T1"); err != nil {
		t.Fatalf("RecordProductionCode after red: %v", err)
	}
}

func TestTDDGateUnknownTask(t *testing.T) {
	s := atImplement(t)
	if err := s.RecordFailingTest("nope"); err == nil {
		t.Fatal("expected an error for an unknown task")
	}
}

func TestImplementBlocksAdvanceUntilAllTasksGreen(t *testing.T) {
	s := atImplement(t)
	if err := s.AddTask("T1", "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTask("T2", "second"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFailingTest("T1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordProductionCode("T1"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkTaskGreen("T1"); err != nil {
		t.Fatal(err)
	}

	// T2 is untouched — implement is not done
	if err := s.Advance(); !errors.Is(err, ErrGateBlocked) {
		t.Fatalf("Advance err = %v, want ErrGateBlocked while T2 is incomplete", err)
	}

	if err := s.RecordFailingTest("T2"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordProductionCode("T2"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkTaskGreen("T2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(); err != nil {
		t.Fatalf("Advance with all tasks green: %v", err)
	}
	if s.Current() != PhaseVerify {
		t.Fatalf("Current() = %v, want verify", s.Current())
	}
}

func TestMarkTaskGreenRequiresProductionCode(t *testing.T) {
	s := atImplement(t)
	if err := s.AddTask("T1", "x"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFailingTest("T1"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkTaskGreen("T1"); err == nil {
		t.Fatal("cannot go green without production code")
	}
}

// --- T-081: review / Advisor gate ---

func atReview(t *testing.T) *Session {
	t.Helper()
	s := atImplement(t)
	if err := s.AddTask("T1", "only task"); err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { return s.RecordFailingTest("T1") },
		func() error { return s.RecordProductionCode("T1") },
		func() error { return s.MarkTaskGreen("T1") },
		s.Advance,          // -> verify
		s.RecordVerifyPass, // verify evidence
		s.Advance,          // -> review
	} {
		if err := step(); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return s
}

func TestVerifyGateRequiresEvidence(t *testing.T) {
	s := atImplement(t)
	if err := s.AddTask("T1", "x"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFailingTest("T1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordProductionCode("T1"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkTaskGreen("T1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(); err != nil {
		t.Fatal(err)
	}
	// in verify with no recorded pass
	if err := s.Advance(); !errors.Is(err, ErrGateBlocked) {
		t.Fatalf("Advance err = %v, want ErrGateBlocked without verify evidence", err)
	}
}

// TestUnresolvedFindingBlocksFinish is the spec scenario for the review gate.
func TestUnresolvedFindingBlocksFinish(t *testing.T) {
	s := atReview(t)
	if err := s.RecordAdvisorRun(); err != nil {
		t.Fatalf("RecordAdvisorRun: %v", err)
	}
	s.AddFinding("F1", "unchecked error return")

	if err := s.Advance(); !errors.Is(err, ErrGateBlocked) {
		t.Fatalf("Advance err = %v, want ErrGateBlocked with an open finding", err)
	}
	if s.Current() != PhaseReview {
		t.Fatalf("phase advanced past review with an open finding: %v", s.Current())
	}

	if !s.ResolveFinding("F1") {
		t.Fatal("ResolveFinding should report success")
	}
	if err := s.Advance(); err != nil {
		t.Fatalf("Advance with all findings resolved: %v", err)
	}
	if s.Current() != PhaseFinish {
		t.Fatalf("Current() = %v, want finish", s.Current())
	}
}

// TestReviewGateRequiresAdvisorRun proves the Advisor pass itself is mandatory —
// zero findings only counts if the Advisor actually ran.
func TestReviewGateRequiresAdvisorRun(t *testing.T) {
	s := atReview(t)
	// no findings, but the Advisor never ran
	if err := s.Advance(); !errors.Is(err, ErrGateBlocked) {
		t.Fatalf("Advance err = %v, want ErrGateBlocked without an Advisor run", err)
	}
	if err := s.RecordAdvisorRun(); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(); err != nil {
		t.Fatalf("Advance after a clean Advisor run: %v", err)
	}
}

func TestResolveUnknownFinding(t *testing.T) {
	s := atReview(t)
	if s.ResolveFinding("absent") {
		t.Fatal("ResolveFinding on a missing id should report false")
	}
}

func TestOpenFindingsReportsOnlyUnresolved(t *testing.T) {
	s := atReview(t)
	s.AddFinding("F1", "one")
	s.AddFinding("F2", "two")
	s.ResolveFinding("F1")
	open := s.OpenFindings()
	if len(open) != 1 || open[0].ID != "F2" {
		t.Fatalf("OpenFindings() = %+v, want only F2", open)
	}
}

func TestAdvanceFromFinishIsTerminal(t *testing.T) {
	s := atReview(t)
	if err := s.RecordAdvisorRun(); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(); err == nil {
		t.Fatal("advancing past finish should error")
	}
	if s.Current() != PhaseFinish {
		t.Fatalf("Current() = %v, want finish", s.Current())
	}
}

func TestPhaseString(t *testing.T) {
	cases := map[Phase]string{
		PhaseGrill: "grill", PhaseSpec: "spec", PhaseImplement: "implement",
		PhaseVerify: "verify", PhaseReview: "review", PhaseFinish: "finish",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("Phase(%d).String() = %q, want %q", p, got, want)
		}
	}
}

func TestCompleteGrillRequiresNotes(t *testing.T) {
	s := newSession(t, "f")
	if err := s.CompleteGrill("   "); err == nil {
		t.Fatal("grill cannot complete with empty notes")
	}
	if s.Current() != PhaseGrill {
		t.Fatalf("phase advanced on a failed grill: %v", s.Current())
	}
}

func TestWriteSpecOutsideSpecPhaseFails(t *testing.T) {
	s := newSession(t, "f") // still in grill
	if err := s.WriteSpec("body"); err == nil {
		t.Fatal("WriteSpec should require the spec phase")
	}
}

func TestAddTaskOutsideImplementFails(t *testing.T) {
	s := newSession(t, "f")
	if err := s.AddTask("T1", "x"); err == nil {
		t.Fatal("AddTask should require the implement phase")
	}
}
