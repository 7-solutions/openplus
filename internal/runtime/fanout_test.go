package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/7-solutions/openplus/internal/policy"
	"github.com/7-solutions/openplus/internal/ports"
)

// --- T-1100: fan-out ---

func TestFanoutRunsEveryPrompt(t *testing.T) {
	s := cmdSession(t)
	// ports.Fake scripts a fixed number of turns; a fan-out makes one call
	// per subagent, so use a provider that answers every call.
	s.Provider = &alwaysProvider{reply: "subagent done"}

	got, err := s.Fanout(context.Background(), []string{"one", "two", "three"})
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("results = %d, want 3", len(got))
	}
	for i, r := range got {
		if r.Err != nil {
			t.Errorf("results[%d] failed: %v", i, r.Err)
		}
		if r.Output == "" {
			t.Errorf("results[%d] produced no output", i)
		}
	}
}

// TestFanoutResultsFollowInputOrder is the spec scenario: completion order must
// not decide reporting order.
func TestFanoutResultsFollowInputOrder(t *testing.T) {
	s := cmdSession(t)
	// A provider where the first prompt is slowest, so completion order inverts.
	s.Provider = &delayedProvider{}

	got, err := s.Fanout(context.Background(), []string{"SLOW", "FAST"})
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("results = %d", len(got))
	}
	if !strings.Contains(got[0].ID, "1") || !strings.Contains(got[1].ID, "2") {
		t.Errorf("result ids not in input order: %q, %q", got[0].ID, got[1].ID)
	}
}

// TestFanoutOneFailureKeepsSiblings is the spec scenario.
func TestFanoutOneFailureKeepsSiblings(t *testing.T) {
	s := cmdSession(t)
	s.Provider = &failOnProvider{failWhenContains: "BOOM"}

	got, err := s.Fanout(context.Background(), []string{"fine", "BOOM", "also fine"})
	if err != nil {
		t.Fatalf("Fanout itself must not fail: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("results = %d, want 3", len(got))
	}
	if got[1].Err == nil {
		t.Error("the failing subagent should report its error")
	}
	for _, i := range []int{0, 2} {
		if got[i].Err != nil {
			t.Errorf("sibling %d lost to its neighbour's failure: %v", i, got[i].Err)
		}
	}
}

// TestFanoutRespectsParallelCap is the spec scenario.
func TestFanoutRespectsParallelCap(t *testing.T) {
	s := cmdSession(t)
	s.MaxSubagentParallel = 2

	tracker := &concurrencyProvider{}
	s.Provider = tracker

	if _, err := s.Fanout(context.Background(), []string{"a", "b", "c", "d", "e", "f"}); err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if tracker.peak() > 2 {
		t.Fatalf("peak concurrency %d exceeded the cap of 2", tracker.peak())
	}
}

// TestFanoutRefusesTooManyTasks is the spec scenario.
func TestFanoutRefusesTooManyTasks(t *testing.T) {
	s := cmdSession(t)
	s.MaxSubagents = 2

	_, err := s.Fanout(context.Background(), []string{"a", "b", "c"})
	if err == nil {
		t.Fatal("expected a refusal past the task limit")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("error should name the limit: %v", err)
	}
}

func TestFanoutNeedsPrompts(t *testing.T) {
	s := cmdSession(t)
	if _, err := s.Fanout(context.Background(), nil); err == nil {
		t.Fatal("expected an error with no prompts")
	}
}

// --- T-1101: the subagent gate never asks ---

// TestSubagentGateResolvesAskWithoutBlocking is the spec scenario: an Ask rule
// must not hang a subagent nobody is watching.
func TestSubagentGateResolvesAskWithoutBlocking(t *testing.T) {
	rules, err := policy.NewRules(policy.Allow, map[string]string{"bash": "ask"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gate := subagentGate(rules)

	done := make(chan policy.Decision, 1)
	go func() {
		d, _ := gate.Permit(context.Background(), ports.ToolCall{Name: "bash", Input: []byte(`{}`)})
		done <- d
	}()
	select {
	case d := <-done:
		// Any decision is acceptable; blocking is not.
		if d == policy.Ask {
			t.Error("a subagent gate must resolve Ask, not return it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subagent gate blocked on an Ask rule")
	}
}

// TestSubagentGateStillDenies is the spec scenario.
func TestSubagentGateStillDenies(t *testing.T) {
	rules, err := policy.NewRules(policy.Allow, map[string]string{"rm": "deny"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gate := subagentGate(rules)

	got, err := gate.Permit(context.Background(), ports.ToolCall{Name: "rm", Input: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Permit: %v", err)
	}
	if got != policy.Deny {
		t.Fatalf("explicit deny = %v, want Deny", got)
	}
}

func TestSubagentGateNilRulesIsSafe(t *testing.T) {
	gate := subagentGate(nil)
	if gate == nil {
		t.Fatal("subagentGate(nil) should still return a usable gate")
	}
	if _, err := gate.Permit(context.Background(), ports.ToolCall{Name: "read"}); err != nil {
		t.Fatalf("Permit: %v", err)
	}
}

// --- T-1102: worktree isolation ---

// TestFanoutIsolatesInGitRepo pins that a git project fans out into worktrees and
// leaves none behind.
func TestFanoutIsolatesInGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := gitProject(t)
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	var dirs []string
	var mu sync.Mutex
	s.OnSubagentDir = func(dir string) {
		mu.Lock()
		dirs = append(dirs, dir)
		mu.Unlock()
	}

	if _, err := s.Fanout(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("Fanout: %v", err)
	}

	if len(dirs) != 2 {
		t.Fatalf("expected two isolated dirs, got %v", dirs)
	}
	if dirs[0] == dirs[1] {
		t.Fatalf("subagents shared a directory: %v", dirs)
	}
	// the spec scenario: nothing left behind
	for _, d := range dirs {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("worktree %s survived the fan-out", d)
		}
	}
	out := gitOut(t, root, "worktree", "list")
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Errorf("git still tracks extra worktrees:\n%s", out)
	}
}

// TestFanoutReleasesWorktreesOnFailure pins cleanup on the error path.
func TestFanoutReleasesWorktreesOnFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := gitProject(t)
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Provider = &failOnProvider{failWhenContains: "BOOM"}

	if _, err := s.Fanout(context.Background(), []string{"BOOM", "BOOM"}); err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	out := gitOut(t, root, "worktree", "list")
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Errorf("worktrees leaked after failing subagents:\n%s", out)
	}
}

// TestFanoutNonRepoRunsInPlace pins that a project without git still works.
func TestFanoutNonRepoRunsInPlace(t *testing.T) {
	s := cmdSession(t) // plain temp dir, not a repo

	var dirs []string
	var mu sync.Mutex
	s.OnSubagentDir = func(dir string) {
		mu.Lock()
		dirs = append(dirs, dir)
		mu.Unlock()
	}
	if _, err := s.Fanout(context.Background(), []string{"a"}); err != nil {
		t.Fatalf("Fanout in a non-repo must still work: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != "" {
		t.Errorf("expected in-place execution (empty dir), got %v", dirs)
	}
}

// --- T-1103: the command ---

func TestCmdSubagentsSplitsOnPipe(t *testing.T) {
	s := cmdSession(t)
	out := run(t, s, "/subagents write the docs | review the docs")
	if !strings.Contains(out, "2") {
		t.Errorf("output should report the count: %s", out)
	}
	// both prompts should appear in the merged report
	for _, want := range []string{"write the docs", "review the docs"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func TestCmdSubagentsNoPromptsErrors(t *testing.T) {
	s := cmdSession(t)
	if err := runErr(t, s, "/subagents"); !strings.Contains(err.Error(), "prompt") {
		t.Errorf("error should explain usage: %v", err)
	}
}

func TestCmdSubagentsRefusesTooMany(t *testing.T) {
	s := cmdSession(t)
	s.MaxSubagents = 1
	if err := runErr(t, s, "/subagents a | b | c"); !strings.Contains(err.Error(), "1") {
		t.Errorf("error should name the limit: %v", err)
	}
}

func TestCmdSubagentsIgnoresEmptySegments(t *testing.T) {
	s := cmdSession(t)
	out := run(t, s, "/subagents  real prompt |   |  another  ")
	if !strings.Contains(out, "2") {
		t.Errorf("blank segments should be dropped, got: %s", out)
	}
}

// --- helpers ---

// gitProject creates a git repo with one commit, plus AGENTS.md.
func gitProject(t *testing.T) string {
	t.Helper()
	root := project(t, "")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-q", "-m", "seed")
	return root
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// alwaysProvider answers every request with the same reply, however many calls
// arrive. ports.Fake plays a fixed script and defaults to an empty turn once
// it runs out, which a fan-out exhausts immediately.
type alwaysProvider struct{ reply string }

func (a *alwaysProvider) Stream(_ context.Context, _ ports.Request) (<-chan ports.Event, error) {
	ch := make(chan ports.Event, 2)
	ch <- ports.Event{Kind: ports.EventTextDelta, Text: a.reply}
	ch <- ports.Event{Kind: ports.EventTurnEnd}
	close(ch)
	return ch, nil
}

// delayedProvider makes the first request slow so completion order inverts.
type delayedProvider struct {
	mu sync.Mutex
	n  int
}

func (d *delayedProvider) Stream(_ context.Context, req ports.Request) (<-chan ports.Event, error) {
	d.mu.Lock()
	d.n++
	first := d.n == 1
	d.mu.Unlock()

	ch := make(chan ports.Event, 2)
	go func() {
		if first {
			time.Sleep(80 * time.Millisecond)
		}
		ch <- ports.Event{Kind: ports.EventTextDelta, Text: "done"}
		ch <- ports.Event{Kind: ports.EventTurnEnd}
		close(ch)
	}()
	return ch, nil
}

// failOnProvider errors when the request mentions a marker.
type failOnProvider struct{ failWhenContains string }

func (f *failOnProvider) Stream(_ context.Context, req ports.Request) (<-chan ports.Event, error) {
	hit := false
	for _, m := range req.Messages {
		for _, b := range m.Blocks {
			if strings.Contains(b.Text, f.failWhenContains) {
				hit = true
			}
		}
	}
	ch := make(chan ports.Event, 2)
	if hit {
		ch <- ports.Event{Kind: ports.EventError, Err: os.ErrInvalid}
		close(ch)
		return ch, nil
	}
	ch <- ports.Event{Kind: ports.EventTextDelta, Text: "ok"}
	ch <- ports.Event{Kind: ports.EventTurnEnd}
	close(ch)
	return ch, nil
}

// concurrencyProvider records peak simultaneous requests.
type concurrencyProvider struct {
	mu       sync.Mutex
	inFlight int
	max      int
}

func (c *concurrencyProvider) Stream(_ context.Context, _ ports.Request) (<-chan ports.Event, error) {
	c.mu.Lock()
	c.inFlight++
	if c.inFlight > c.max {
		c.max = c.inFlight
	}
	c.mu.Unlock()

	ch := make(chan ports.Event, 2)
	go func() {
		time.Sleep(30 * time.Millisecond)
		c.mu.Lock()
		c.inFlight--
		c.mu.Unlock()
		ch <- ports.Event{Kind: ports.EventTextDelta, Text: "ok"}
		ch <- ports.Event{Kind: ports.EventTurnEnd}
		close(ch)
	}()
	return ch, nil
}

func (c *concurrencyProvider) peak() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.max
}

// keep filepath referenced for helpers that build paths in future tasks.
var _ = filepath.Join
