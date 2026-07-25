package jsworkflow

import (
	"context"
	"strings"
	"testing"

	"github.com/7-solutions/openplus/internal/orchestrate"
)

// T-1416 + T-1417 — LoadFile reads testdata/example.js, compiles it, and runs it
// end to end: hand-off and state.last both survive.
func TestLoadFileExample(t *testing.T) {
	c, err := LoadFile("testdata/example.js")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Name != "example" {
		t.Errorf("Name = %q, want example", c.Name)
	}
	if c.Run.MaxRetries != 1 {
		t.Errorf("MaxRetries = %d, want 1", c.Run.MaxRetries)
	}
	rep, err := c.Run.Run(context.Background(), &orchestrate.State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.OK {
		t.Fatalf("report not OK: %+v", rep)
	}
	if len(rep.Phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(rep.Phases))
	}
	want := "topic=goja last=produced"
	if rep.Phases[1].Output != want {
		t.Errorf("phase[1] output = %q, want %q", rep.Phases[1].Output, want)
	}
}

// T-1416 — a missing file errors naming the path.
func TestLoadFileMissing(t *testing.T) {
	_, err := LoadFile("testdata/does-not-exist.js")
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if !strings.Contains(err.Error(), "does-not-exist.js") {
		t.Errorf("err = %q, want it to name the file", err.Error())
	}
}
