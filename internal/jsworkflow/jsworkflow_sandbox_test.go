package jsworkflow

import (
	"context"
	"strings"
	"testing"

	"github.com/7-solutions/openplus/internal/orchestrate"
)

// T-1415 — the sandbox binds only module/exports/state/console.log. require,
// process, and fetch are undefined and raise a ReferenceError rather than
// reaching the host.
func TestSandboxNoHostGlobals(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr string
	}{
		{"require", `(s) => { require("fs"); return "x"; }`},
		{"process", `(s) => { process.exit(0); return "x"; }`},
		{"fetch", `(s) => { fetch("http://x"); return "x"; }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mustCompile(t, `module.exports = { name:"w", phases:[{name:"p", run: `+tc.expr+` }] };`)
			_, err := c.Run.Run(context.Background(), &orchestrate.State{})
			if err == nil {
				t.Fatalf("expected a ReferenceError for %s, got nil", tc.name)
			}
			// goja: "ReferenceError: <name> is not defined".
			if !strings.Contains(err.Error(), tc.name) {
				t.Errorf("err = %q, want it to name %s", err.Error(), tc.name)
			}
			if !strings.Contains(err.Error(), "ReferenceError") &&
				!strings.Contains(err.Error(), "not defined") {
				t.Errorf("err = %q, want a ReferenceError/not-defined", err.Error())
			}
		})
	}
}

// T-1415 — console.log routes through the WithLogger sink.
func TestConsoleLogCaptured(t *testing.T) {
	var got []string
	c, err := Compile(
		`module.exports = { name:"w", phases:[{name:"p", run:(s)=>{ console.log("hello", "world"); return "ok"; }}] };`,
		WithLogger(func(s string, _ ...any) { got = append(got, s) }),
	)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	rep, err := c.Run.Run(context.Background(), &orchestrate.State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Phases[0].Output != "ok" {
		t.Errorf("output = %q, want ok", rep.Phases[0].Output)
	}
	if len(got) != 1 {
		t.Fatalf("log calls = %d, want 1", len(got))
	}
	for _, want := range []string{"hello", "world"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("log %q missing %q", got[0], want)
		}
	}
}
