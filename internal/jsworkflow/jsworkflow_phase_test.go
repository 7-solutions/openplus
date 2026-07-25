package jsworkflow

import (
	"context"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/orchestrate"
)

// T-1412 — a JS phase's return value becomes the phase output.
func TestJSPhaseOutput(t *testing.T) {
	c := mustCompile(t, `module.exports = { name:"w", phases:[{name:"p", run:(s)=>"hello"}] };`)
	rep := runSimple(t, c)
	if !rep.OK {
		t.Fatalf("report not OK: %+v", rep)
	}
	if rep.Phases[0].Output != "hello" {
		t.Errorf("output = %q, want hello", rep.Phases[0].Output)
	}
}

// T-1412 — a non-string return is coerced to a string; undefined becomes "".
func TestJSPhaseReturnCoerced(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr string
		want string
	}{
		{"number", `(s) => 42`, "42"},
		{"boolean", `(s) => true`, "true"},
		{"undefined", `(s) => {}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mustCompile(t, `module.exports = { name:"w", phases:[{name:"p", run: `+tc.expr+` }] };`)
			rep := runSimple(t, c)
			if rep.Phases[0].Output != tc.want {
				t.Errorf("output = %q, want %q", rep.Phases[0].Output, tc.want)
			}
		})
	}
}

// T-1412 — state hand-off round-trips: a later phase sees a value an earlier
// phase set, and state.last is the prior phase's output.
func TestJSPhaseStateHandoff(t *testing.T) {
	c := mustCompile(t, `
module.exports = {
  name: "w",
  phases: [
    { name: "produce", run: (s) => { s.set("k", "v1"); return "out1"; } },
    { name: "consume", run: (s) => {
        if (s.get("k") !== "v1") throw new Error("get lost: " + s.get("k"));
        if (s.last !== "out1") throw new Error("last lost: " + s.last);
        return "ok";
      } },
  ],
};`)
	rep := runSimple(t, c)
	if !rep.OK {
		t.Fatalf("report not OK: %+v", rep)
	}
	if rep.Phases[1].Output != "ok" {
		t.Errorf("phase[1] output = %q, want ok", rep.Phases[1].Output)
	}
}

// T-1412 — state.get on a missing key returns undefined (not "" or an error).
func TestJSPhaseStateGetMissing(t *testing.T) {
	c := mustCompile(t, `
module.exports = { name: "w", phases: [{ name: "p", run: (s) => {
  if (s.get("nope") !== undefined) throw new Error("expected undefined");
  return "ok";
} }] };`)
	rep := runSimple(t, c)
	if !rep.OK {
		t.Fatalf("report not OK: %+v", rep)
	}
}

// T-1412 — a thrown value is a phase failure: the workflow errors and the
// message surfaces.
func TestJSPhaseThrowIsError(t *testing.T) {
	c := mustCompile(t, `module.exports = { name:"w", phases:[{name:"p", run:(s)=>{ throw new Error("boom"); }}] };`)
	rep, err := c.Run.Run(context.Background(), &orchestrate.State{})
	if err == nil {
		t.Fatal("expected an error from a thrown phase")
	}
	if rep.OK {
		t.Error("report must not be OK when a phase throws")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %q, want it to mention boom", err.Error())
	}
}
