package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-1418 — /workflow load <path> compiles a JS file and registers it; a later
// /workflow <name> runs it through the unchanged path, with hand-off intact.
func TestCmdWorkflowLoadAndRun(t *testing.T) {
	s := cmdSession(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.js")
	src := `module.exports = { name: "jsdemo", phases: [
		{ name: "a", run: (st) => { st.set("v", "42"); return "A"; } },
		{ name: "b", run: (st) => "B=" + st.get("v") + " last=" + st.last },
	] };`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := s.cmdWorkflow("load " + path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(out, "jsdemo") {
		t.Errorf("load output = %q, want the registered name", out)
	}
	if _, ok := s.Workflows["jsdemo"]; !ok {
		t.Error("workflow was not registered under its declared name")
	}

	rep, err := s.cmdWorkflow("jsdemo")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(rep, "workflow OK") {
		t.Errorf("run report = %q, want OK", rep)
	}
	if !strings.Contains(rep, "B=42 last=A") {
		t.Errorf("run report = %q, hand-off lost", rep)
	}
}

// T-1418 — a missing file errors naming the path.
func TestCmdWorkflowLoadMissingFile(t *testing.T) {
	s := cmdSession(t)
	if _, err := s.cmdWorkflow("load /no/such.js"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

// T-1418 — load refuses to shadow an existing name (built-in or otherwise); no
// silent replacement.
func TestCmdWorkflowLoadRefusesClobber(t *testing.T) {
	s := cmdSession(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "w.js")
	if err := os.WriteFile(path, []byte(`module.exports={name:"review", phases:[{name:"x", run:(s)=>"y"}]};`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.cmdWorkflow("load " + path); err == nil {
		t.Fatal("expected a refusal to shadow the built-in 'review' workflow")
	}
}

// T-1418 — bare `load` with no path errors rather than doing nothing.
func TestCmdWorkflowLoadNeedsPath(t *testing.T) {
	s := cmdSession(t)
	if _, err := s.cmdWorkflow("load"); err == nil {
		t.Fatal("expected an error for bare load")
	}
	if _, err := s.cmdWorkflow("load   "); err == nil {
		t.Fatal("expected an error for whitespace-only load")
	}
}
