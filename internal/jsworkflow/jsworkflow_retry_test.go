package jsworkflow

import (
	"context"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/orchestrate"
)

// T-1413 — a JS maxRetries flows through to the engine: a failing phase retries
// the declared number of extra attempts, then succeeds (the ADR-0006 scenario,
// from JS).
func TestJSPhaseRetriesThenSucceeds(t *testing.T) {
	c := mustCompile(t, `
var attempts = 0;
module.exports = {
  name: "w", maxRetries: 3,
  phases: [{ name: "flaky", run: (s) => {
    attempts++;
    if (attempts < 3) throw new Error("transient");
    return "eventually-ok";
  } }],
};`)
	rep := runSimple(t, c)
	if !rep.OK {
		t.Fatalf("expected success after retries: %+v", rep)
	}
	if rep.Phases[0].Attempts != 3 {
		t.Errorf("attempts = %d, want 3", rep.Phases[0].Attempts)
	}
	if rep.Phases[0].Output != "eventually-ok" {
		t.Errorf("output = %q, want eventually-ok", rep.Phases[0].Output)
	}
}

// T-1413 — when the budget runs out, the workflow fails at the JS phase with the
// right attempt count and failed-phase name.
func TestJSPhaseRetryBudgetExhausted(t *testing.T) {
	c := mustCompile(t, `
module.exports = {
  name: "w", maxRetries: 1,
  phases: [{ name: "doomed", run: (s) => { throw new Error("always"); } }],
};`)
	rep, err := c.Run.Run(context.Background(), &orchestrate.State{})
	if err == nil {
		t.Fatal("expected failure when the retry budget is exhausted")
	}
	if rep.OK {
		t.Error("report must not be OK")
	}
	// maxRetries=1 → 1 initial + 1 retry.
	if rep.Phases[0].Attempts != 2 {
		t.Errorf("attempts = %d, want 2", rep.Phases[0].Attempts)
	}
	if rep.FailedPhase != "doomed" {
		t.Errorf("FailedPhase = %q, want doomed", rep.FailedPhase)
	}
	if !strings.Contains(err.Error(), "always") {
		t.Errorf("err = %q, want it to mention always", err.Error())
	}
}
