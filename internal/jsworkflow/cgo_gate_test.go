// Package jsworkflow is the goja-backed adapter that compiles a `.js` workflow
// into an orchestrate.Workflow (ADR-0009). It is the only package that knows JS
// exists; the engine, the Workflow/Phase port, and the report shape live in
// internal/orchestrate and are unchanged.
package jsworkflow

import (
	"os/exec"
	"strings"
	"testing"
)

// TestGojaDepsAreCgoFree is the T-1410 gate. goja was chosen over a cgo JS
// runtime precisely because it is pure Go (ADR-0001: cgo-free single binary). If
// a future goja version grows a cgo dependency, this fails in `go test` here and
// now, not silently in CI. `go list -deps` enumerates the transitive import tree;
// a cgo pull surfaces as runtime/cgo.
func TestGojaDepsAreCgoFree(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/dop251/goja").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps github.com/dop251/goja: %v: %s", err, out)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if pkg == "runtime/cgo" || strings.HasSuffix(pkg, "/cgo") {
			t.Fatalf("goja transitively imports %s — breaks the cgo-free build (ADR-0001)", pkg)
		}
	}
}
