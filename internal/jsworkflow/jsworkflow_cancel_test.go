package jsworkflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7solutions/openplus/internal/orchestrate"
)

// T-1414 — a cancelled context interrupts a running JS phase mid-execution.
// `while(true){}` would hang the engine forever if cancellation only applied
// between phases; vm.Interrupt must abort it.
func TestJSPhaseCancellationInterruptsLoop(t *testing.T) {
	c := mustCompile(t, `
module.exports = { name: "w", phases: [{ name: "loop", run: (s) => {
  while (true) {}
  return "never";
} }] };`)

	// Hard ceiling so a regression fails the test instead of hanging it.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := c.Run.Run(ctx, &orchestrate.State{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v — interrupt did not abort mid-phase", elapsed)
	}
}

// T-1414 — a phase that respects cancellation cooperatively (checks state) also
// stops; the report is not OK and names the phase.
func TestJSPhaseCancellationReportedNotOK(t *testing.T) {
	c := mustCompile(t, `
module.exports = { name: "w", phases: [
  { name: "first", run: (s) => "ok" },
  { name: "second", run: (s) => { while (true) {} } },
] };`)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	rep, err := c.Run.Run(ctx, &orchestrate.State{})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if rep.OK {
		t.Error("cancelled workflow must not report OK")
	}
	if rep.FailedPhase != "second" {
		t.Errorf("FailedPhase = %q, want second", rep.FailedPhase)
	}
}
