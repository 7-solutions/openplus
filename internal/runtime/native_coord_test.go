package runtime

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestAssembleDefaultsToNativeCoordinator is the spec scenario: native ships with
// OpenPlus, so coordinated fan-out works with no external binary.
func TestAssembleDefaultsToNativeCoordinator(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := gitProject(t)
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if s.Coordinator == nil {
		t.Fatal("no coordinator wired")
	}
	if !s.Coordinator.Available() {
		t.Fatal("the default coordinator should be available in a git repo")
	}
}

// TestAssembleNativeCoordinatorWorksEndToEnd is the real proof: the native
// coordinator, wired by Assemble, claims and merges without grit.
func TestAssembleNativeCoordinatorWorksEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := gitProject(t)
	write(t, root, "main.go", "package main\n\nfunc Login() string { return \"login\" }\n")
	// re-commit so the Go file is in the repo
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = root
	_ = cmd.Run()
	cmd = exec.Command("git", "commit", "-q", "-m", "add main.go")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com")
	_ = cmd.Run()

	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	s.Provider = &alwaysProvider{reply: "done"}

	results, err := s.FanoutCoordinated(context.Background(), []SubagentTask{
		{Prompt: "edit Login", Symbols: []string{"main.go::Login"}},
	})
	if err != nil {
		t.Fatalf("FanoutCoordinated: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	if results[0].Blocked {
		t.Fatalf("unexpected block: %+v", results[0])
	}
}

// TestCmdSubagentsCoordinatedReportsBackend is the spec scenario: a user can tell
// native from grit at a glance.
func TestCmdSubagentsCoordinatedReportsBackend(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := gitProject(t)
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	s.Provider = &alwaysProvider{reply: "done"}
	write(t, root, "main.go", "package main\nfunc F() {}\n")
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = root
	_ = cmd.Run()
	cmd = exec.Command("git", "commit", "-q", "-m", "x")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com")
	_ = cmd.Run()

	out := run(t, s, "/subagents --coordinated edit F#main.go::F")
	if !strings.Contains(strings.ToLower(out), "native") {
		t.Errorf("report should name the native backend:\n%s", out)
	}
}

// TestAssembleGritBackendWhenConfigured pins that opencode.json can still select
// grit, and that it reports unavailable when the binary is absent.
func TestAssembleGritBackendWhenConfigured(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := gitProject(t)
	write(t, root, "opencode.json", `{
  "model": "local/q",
  "provider": {"local": {"options": {"baseURL": "http://x/v1"}}},
  "coordination": {"backend": "grit"}
}`)
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	// grit is not installed here, so Available must be false — and the report
	// should explain that, not crash.
	if s.Coordinator.Available() {
		t.Log("grit appears to be installed; Available=true is correct")
	} else {
		_, err := s.FanoutCoordinated(context.Background(), []SubagentTask{
			{Prompt: "x", Symbols: []string{"main.go::F"}},
		})
		if err == nil {
			t.Fatal("expected an error explaining grit is unavailable")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "grit") {
			t.Errorf("error should name grit: %v", err)
		}
	}
}

// TestAssembleNoneBackendRestores0011Behavior pins the opt-out: "none" means no
// coordinator, so --coordinated explains its absence.
func TestAssembleNoneBackendRestores0011Behavior(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := gitProject(t)
	write(t, root, "opencode.json", `{
  "model": "local/q",
  "provider": {"local": {"options": {"baseURL": "http://x/v1"}}},
  "coordination": {"backend": "none"}
}`)
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if s.Coordinator.Available() {
		t.Error("\"none\" should make coordination unavailable")
	}
}
