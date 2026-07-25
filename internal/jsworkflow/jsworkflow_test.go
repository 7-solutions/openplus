package jsworkflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/7solutions/openplus/internal/orchestrate"
)

// T-1411 — a valid CommonJS source compiles to a Workflow whose phase names
// and count match the source.
func TestCompileValidSource(t *testing.T) {
	src := `
module.exports = {
  name: "deep-research",
  phases: [
    { name: "query",      run: (s) => "q" },
    { name: "synthesize", run: (s) => "s" },
  ],
};`
	c, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c.Name != "deep-research" {
		t.Errorf("Name = %q, want deep-research", c.Name)
	}
	if len(c.Run.Phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(c.Run.Phases))
	}
	if got := c.Run.Phases[0].Name(); got != "query" {
		t.Errorf("phase[0] = %q, want query", got)
	}
	if got := c.Run.Phases[1].Name(); got != "synthesize" {
		t.Errorf("phase[1] = %q, want synthesize", got)
	}
}

// T-1411 — maxRetries is read from the source.
func TestCompileReadsMaxRetries(t *testing.T) {
	c, err := Compile(`module.exports = { name: "w", maxRetries: 3, phases: [{name:"p", run:(s)=>"x"}] };`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c.Run.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", c.Run.MaxRetries)
	}
}

// T-1411 — a malformed source errors naming the defect, never an empty workflow.
func TestCompileErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string // substring of the error
	}{
		{"missing exports", `1 + 1;`, "module.exports"},
		{"missing name", `module.exports = { phases: [{name:"p", run:(s)=>"x"}] };`, "name"},
		{"empty name", `module.exports = { name: "", phases: [{name:"p", run:(s)=>"x"}] };`, "name"},
		{"missing phases", `module.exports = { name: "w" };`, "phases"},
		{"empty phases", `module.exports = { name: "w", phases: [] };`, "phases"},
		{"run not a function", `module.exports = { name: "w", phases: [{name:"p", run: 42}] };`, "run"},
		{"phase missing name", `module.exports = { name: "w", phases: [{run:(s)=>"x"}] };`, "name"},
		{"load-time throw", `throw new Error("boom");`, "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// Compile returns a Compiled ready to run; this helper keeps later tests short.
func mustCompile(t *testing.T, src string) Compiled {
	t.Helper()
	c, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return c
}

// runSimple runs a one-phase workflow and returns its first phase output.
func runSimple(t *testing.T, c Compiled) orchestrate.Report {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rep, err := c.Run.Run(ctx, &orchestrate.State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return rep
}
