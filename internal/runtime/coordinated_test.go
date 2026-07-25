package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/7-solutions/openplus/internal/orchestrate"
)

func coordSession(t *testing.T) (*Session, *orchestrate.FakeCoordinator) {
	t.Helper()
	s := cmdSession(t)
	s.Provider = &alwaysProvider{reply: "subagent finished"}
	fc := orchestrate.NewFakeCoordinator()
	s.Coordinator = fc
	return s, fc
}

// --- T-1220/T-1221: coordinated fan-out ---

func TestFanoutCoordinatedGrantedRunsAndMerges(t *testing.T) {
	s, fc := coordSession(t)

	got, err := s.FanoutCoordinated(context.Background(), []SubagentTask{
		{Prompt: "fix login", Symbols: []string{"auth.go::Login"}},
	})
	if err != nil {
		t.Fatalf("FanoutCoordinated: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("results = %d, want 1", len(got))
	}
	if got[0].Err != nil {
		t.Fatalf("subagent failed: %v", got[0].Err)
	}
	if !got[0].Merged {
		t.Error("a successful coordinated subagent should be merged")
	}
	if !fc.Merged("sub-1") {
		t.Error("Done was not called for the granted subagent")
	}
	// locks must not linger
	if h := fc.Holder("auth.go::Login"); h != "" {
		t.Errorf("symbol still held after Done: %q", h)
	}
}

// TestFanoutCoordinatedBlockedDoesNotRun is the spec scenario: a blocked claim
// must not run the subagent, since the whole point is that it should not have
// edited that symbol.
func TestFanoutCoordinatedBlockedDoesNotRun(t *testing.T) {
	s, fc := coordSession(t)
	// pre-hold the symbol as somebody else
	if _, err := fc.Claim(context.Background(), "other-agent", "holding", []string{"auth.go::Login"}); err != nil {
		t.Fatal(err)
	}

	counted := &countingProvider{}
	s.Provider = counted

	got, err := s.FanoutCoordinated(context.Background(), []SubagentTask{
		{Prompt: "fix login", Symbols: []string{"auth.go::Login"}},
	})
	if err != nil {
		t.Fatalf("FanoutCoordinated: %v", err)
	}
	if !got[0].Blocked {
		t.Fatal("the result should be marked blocked")
	}
	if got[0].BlockedBy != "other-agent" {
		t.Errorf("BlockedBy = %q, want other-agent", got[0].BlockedBy)
	}
	if counted.calls != 0 {
		t.Errorf("a blocked subagent ran anyway (%d provider calls)", counted.calls)
	}
}

// TestFanoutCoordinatedDifferentSymbolsBothRun is the reason for using grit at
// all: same file, different functions, both proceed.
func TestFanoutCoordinatedDifferentSymbolsBothRun(t *testing.T) {
	s, fc := coordSession(t)

	got, err := s.FanoutCoordinated(context.Background(), []SubagentTask{
		{Prompt: "fix login", Symbols: []string{"auth.go::Login"}},
		{Prompt: "fix logout", Symbols: []string{"auth.go::Logout"}},
	})
	if err != nil {
		t.Fatalf("FanoutCoordinated: %v", err)
	}
	for i, r := range got {
		if r.Blocked {
			t.Errorf("results[%d] blocked; different symbols in one file must both proceed", i)
		}
		if !r.Merged {
			t.Errorf("results[%d] not merged", i)
		}
	}
	if !fc.Merged("sub-1") || !fc.Merged("sub-2") {
		t.Error("both subagents should have merged")
	}
}

// TestFanoutCoordinatedReleasesOnFailure is the spec scenario: a failure must not
// leave locks held forever.
func TestFanoutCoordinatedReleasesOnFailure(t *testing.T) {
	s, fc := coordSession(t)
	s.Provider = &failOnProvider{failWhenContains: "BOOM"}

	got, err := s.FanoutCoordinated(context.Background(), []SubagentTask{
		{Prompt: "BOOM", Symbols: []string{"auth.go::Login"}},
	})
	if err != nil {
		t.Fatalf("FanoutCoordinated: %v", err)
	}
	if got[0].Err == nil {
		t.Fatal("the failure should be reported")
	}
	if got[0].Merged {
		t.Error("a failed subagent must not be reported as merged")
	}
	if fc.Merged("sub-1") {
		t.Error("Done must not be called for a failed subagent")
	}
	if h := fc.Holder("auth.go::Login"); h != "" {
		t.Errorf("locks leaked after failure: held by %q", h)
	}
}

func TestFanoutCoordinatedNeedsTasks(t *testing.T) {
	s, _ := coordSession(t)
	if _, err := s.FanoutCoordinated(context.Background(), nil); err == nil {
		t.Fatal("expected an error with no tasks")
	}
}

func TestFanoutCoordinatedNeedsSymbols(t *testing.T) {
	s, _ := coordSession(t)
	_, err := s.FanoutCoordinated(context.Background(), []SubagentTask{{Prompt: "no symbols"}})
	if err == nil {
		t.Fatal("expected an error: symbols must be stated, never inferred")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "symbol") {
		t.Errorf("error should explain the missing symbols: %v", err)
	}
}

// TestFanoutCoordinatedUnavailableReportsWhy is the spec scenario: an absent
// coordinator degrades with an explanation.
func TestFanoutCoordinatedUnavailableReportsWhy(t *testing.T) {
	s := cmdSession(t) // default: NoCoordinator
	_, err := s.FanoutCoordinated(context.Background(), []SubagentTask{
		{Prompt: "x", Symbols: []string{"f.go::A"}},
	})
	if err == nil {
		t.Fatal("expected an error with no coordinator available")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "coordinat") {
		t.Errorf("error should explain coordination is unavailable: %v", err)
	}
}

func TestSessionDefaultsToNoCoordinator(t *testing.T) {
	s := cmdSession(t)
	if s.Coordinator == nil {
		t.Fatal("Coordinator should default to a real object, not nil")
	}
	if s.Coordinator.Available() {
		t.Error("the default coordinator should be unavailable")
	}
}

// --- T-1222: the report ---

func TestCoordinatedReportStatesItCommits(t *testing.T) {
	results := []CoordinatedResult{{ID: "sub-1", Prompt: "p", Merged: true, Output: "done"}}
	out := CoordinatedReport("native", results)
	// grit commits and merges; a user must be told before reading the outcome
	if !strings.Contains(strings.ToLower(out), "commit") {
		t.Errorf("report should state that coordinated mode commits:\n%s", out)
	}
}

func TestCoordinatedReportSeparatesOutcomes(t *testing.T) {
	results := []CoordinatedResult{
		{ID: "sub-1", Prompt: "merged one", Merged: true, Output: "ok"},
		{ID: "sub-2", Prompt: "blocked one", Blocked: true, BlockedBy: "other", BlockedSymbol: "f.go::A"},
		{ID: "sub-3", Prompt: "failed one", Err: context.Canceled},
	}
	out := CoordinatedReport("native", results)
	for _, want := range []string{"merged one", "blocked one", "failed one", "other", "f.go::A"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

// --- T-1223: the command ---

func TestCmdSubagentsCoordinatedParsesSymbols(t *testing.T) {
	s, fc := coordSession(t)

	out := run(t, s, "/subagents --coordinated fix login#auth.go::Login | fix logout#auth.go::Logout")
	if !strings.Contains(out, "fix login") || !strings.Contains(out, "fix logout") {
		t.Errorf("report missing prompts:\n%s", out)
	}
	if !fc.Merged("sub-1") || !fc.Merged("sub-2") {
		t.Error("both coordinated subagents should have merged")
	}
}

func TestCmdSubagentsCoordinatedNeedsSymbols(t *testing.T) {
	s, _ := coordSession(t)
	err := runErr(t, s, "/subagents --coordinated no symbols here")
	if !strings.Contains(strings.ToLower(err.Error()), "symbol") {
		t.Errorf("error should require symbols after #: %v", err)
	}
}

// TestCmdSubagentsUncoordinatedUnchanged is the spec scenario: without the flag,
// behavior matches change 0011.
func TestCmdSubagentsUncoordinatedUnchanged(t *testing.T) {
	s, fc := coordSession(t)
	out := run(t, s, "/subagents plain one | plain two")
	if !strings.Contains(out, "2 subagent(s)") {
		t.Errorf("uncoordinated report changed shape:\n%s", out)
	}
	// nothing claimed, nothing merged
	if fc.Merged("sub-1") {
		t.Error("uncoordinated fan-out must not merge anything")
	}
}

func TestCmdSubagentsCoordinatedUnavailableExplains(t *testing.T) {
	s := cmdSession(t) // NoCoordinator
	err := runErr(t, s, "/subagents --coordinated x#f.go::A")
	if !strings.Contains(strings.ToLower(err.Error()), "coordinat") {
		t.Errorf("error should explain unavailability: %v", err)
	}
}
