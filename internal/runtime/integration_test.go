package runtime

// End-to-end integration tests for the runtime (Change 0004 / sub-scope D).
// These tests cross subsystem boundaries — they drive Assemble → Run against
// a real memory store, a scripted provider, and the policy gate. They are
// the runtime-level counterpart of the per-package unit tests in
// assemble_test.go and turn_test.go.
//
// Per the spec scenario at
// openspec/changes/0004-config-integration/specs/embedder-memory-config/spec.md#requirement-end-to-end-integration-tests
// there are four scenarios; each is one T-43x task.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/7solutions/openplus/internal/orchestrate"
	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/provider"
	"github.com/7solutions/openplus/internal/tool"
)

// --- T-430: Memory round-trip across two Sessions ---

// TestIntegrationMemoryRoundTripAcrossSessions: session A writes a chunk via
// Run, session B (assembled against the same memory path) retrieves it via
// AssembleContext. This is the spec's "memory reaches a later session"
// scenario.
//
// Both sessions use Options{Fake: true} so no real provider endpoint is
// required; the Fake provider streams a single empty turn and Run returns.
// The chunk is persisted by Session.Run's write-through to memory.
//
// HOME is redirected to a temp dir so skillRoots() doesn't pick up the
// developer's ~/.claude/skills, which would otherwise auto-load into the
// budget and squeeze the retrieved chunk out of the assembled context.
func TestIntegrationMemoryRoundTripAcrossSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := project(t, `{
  "model": "local/qwen2.5-coder",
  "provider": {"local": {"options": {"baseURL": "http://localhost:11434/v1"}}},
  "embedder": {"model": "nomic-embed-text", "baseURL": "http://localhost:11434/v1"},
  "memory": {"autoOpen": true}
}`)

	// Session A: writes via Run. Use Fake so the run is offline; swap in a
	// deterministic embedder because the real one would need a live endpoint.
	sA, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble A: %v", err)
	}
	sA.Embedder = memOnlyEmbedder{dim: 8}
	sA.Memory.Embedder = sA.Embedder

	prompt := "the deploy script lives in scripts/ship.sh"
	if _, err := sA.Run(context.Background(), prompt, nil); err != nil {
		t.Fatalf("Run A: %v", err)
	}

	// Diagnostic: confirm session A actually wrote the chunk before closing.
	// If we don't see it here, the rest of the test is meaningless.
	var n int
	if err := sA.Memory.DB().QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&n); err != nil {
		t.Fatalf("count chunks A: %v", err)
	}
	if n == 0 {
		t.Fatal("session A persisted 0 chunks; round-trip cannot work")
	}

	// Close A so its WAL is merged into the main db before session B opens.
	if err := sA.Close(); err != nil {
		t.Fatalf("Close A: %v", err)
	}

	// Session B: fresh Session, same memory path. Same embedder swap so
	// retrieval is offline.
	sB, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble B: %v", err)
	}
	sB.Embedder = memOnlyEmbedder{dim: 8}
	sB.Memory.Embedder = sB.Embedder
	t.Cleanup(func() { _ = sB.Close() })

	// Confirm session B sees the chunk in the underlying table — proves
	// the data round-tripped across the two Sessions (separate Store
	// instances, same on-disk file).
	var nb int
	if err := sB.Memory.DB().QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&nb); err != nil {
		t.Fatalf("count chunks B: %v", err)
	}
	if nb == 0 {
		t.Fatal("session B's chunks table is empty — session A's writes are not visible")
	}

	// Pin the embedding dimension by writing one observation through
	// session B's Memory. The store's dim is fixed at the first Embed
	// call (see store.go); without a write here, Search returns nil
	// because the schema isn't migrated yet. This is the same dim
	// session A pinned (memOnlyEmbedder{dim:8}), so vec0 KNN matches.
	if _, err := sB.Memory.Write(context.Background(), "warmup", "session-b-bootstrap"); err != nil {
		t.Fatalf("Write B warmup: %v", err)
	}

	// Spec wording: "Session B's AssembleContext surfaces the original
	// exchange via retrieval." We test the retrieval boundary two ways:
	//   (1) Memory.Search returns the chunk — proves the data + index
	//       round-tripped across the two Sessions.
	//   (2) AssembleContext's System prompt contains it — proves the
	//       retrieval reaches the model.
	// Both must hold; budget-side squeezing is a separate concern tested
	// elsewhere.
	res, err := sB.Memory.Search(context.Background(), "where is the deploy script", 5)
	if err != nil {
		t.Fatalf("Search B: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("session B's Search returned 0 hits; round-trip is broken")
	}
	found := false
	for _, r := range res {
		if strings.Contains(r.Text, "scripts/ship.sh") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("retrieved chunks (%d) don't contain the original prompt text", len(res))
	}

	got, err := sB.AssembleContext(context.Background(), "where is the deploy script", nil)
	if err != nil {
		t.Fatalf("AssembleContext B: %v", err)
	}
	if !strings.Contains(got.System, "scripts/ship.sh") {
		t.Errorf("retrieval surfaced via Search but AssembleContext dropped it (budget?):\n%s", got.System)
	}
}

// memOnlyEmbedder returns deterministic dim-d vectors regardless of input,
// so retrieval is testable without a live embedder endpoint. Mirrors the
// fakeEmbedder in turn_test.go but kept inline here so integration_test.go
// is self-contained.
type memOnlyEmbedder struct{ dim int }

func (f memOnlyEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, tx := range texts {
		v := make([]float32, f.dim)
		// small per-text offset so lexical+FTS5 has signal to rank on
		for j := range v {
			v[j] = float32(len(tx)%7) + float32(j)/10
		}
		out[i] = v
	}
	return out, nil
}
func (f memOnlyEmbedder) Dim() int { return f.dim }

// --- T-431: Permission deny stops tool execution ---

// countingTool records every Execute call. The integration test asserts the
// counter is 0 after a denied call — proving the gate short-circuited the
// tool and the tool body never ran.
type countingTool struct {
	name  string
	calls atomic.Int32
}

func (c *countingTool) Name() string  { return c.name }
func (c *countingTool) Description() string {
	return "counts Execute calls so the test can prove the gate denied before the tool ran"
}
func (c *countingTool) Schema() json.RawMessage {
	return []byte(`{"type":"object","properties":{}}`)
}
func (c *countingTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	c.calls.Add(1)
	return "tool body ran", nil
}

// TestIntegrationPermissionDenyStopsExecution: a scripted provider issues a
// bash call; Permission.Tools["bash"] = "deny" makes the gate refuse; the
// tool body never runs; the rejection surfaces as a ToolResultError block
// in the returned history.
func TestIntegrationPermissionDenyStopsExecution(t *testing.T) {
	// Use Fake mode so Assemble doesn't require a model in the config file.
	// We immediately replace the provider below with our own Fake that
	// scripts the bash call.
	root := project(t, "")

	bash := &countingTool{name: "bash"}
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Replace the session's Provider with a Fake that issues a bash call.
	s.Provider = &provider.Fake{Scripts: [][]provider.Event{
		{
			{Kind: provider.EventToolCallStart, Call: &provider.ToolCall{
				ID: "call_1", Name: "bash", Input: []byte(`{}`),
			}},
			{Kind: provider.EventTurnEnd},
		},
		{{Kind: provider.EventTurnEnd}},
	}}

	// Wire a permission rule that denies bash. We re-build the gate via the
	// session's Rules and replace the session's Gate to use it. This is
	// hacky but lets the test drive a deny without changing the opencode.json
	// loader. The alternative — writing the rule into opencode.json — would
	// couple the test to the file parser more tightly.
	denyRules, err := policy.NewRules(policy.Allow,
		map[string]string{"bash": "deny"}, nil)
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}
	s.Rules = denyRules
	s.Gate = &policy.Prompting{Rules: denyRules}

	// Register the counting bash tool.
	s.Tools = tool.NewRegistry(bash)
	s.ToolSchemas = []provider.ToolSchema{{
		Name:        bash.Name(),
		Description: bash.Description(),
		InputSchema: bash.Schema(),
	}}

	// Run one turn.
	hist, err := s.Run(context.Background(), "do the thing", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The bash tool body must NOT have run.
	if got := bash.calls.Load(); got != 0 {
		t.Errorf("bash tool executed %d times; want 0 (gate should have denied)", got)
	}

	// The history must contain a user-side tool result for call_1,
	// flagged as an error, with text mentioning denial. The agent loop
	// runs an extra empty turn after the rejection so the model can react;
	// scan the whole history for the rejection block rather than indexing
	// the last-but-one message.
	var found bool
	for _, m := range hist {
		if m.Role != provider.RoleUser {
			continue
		}
		for _, b := range m.Blocks {
			if b.Kind != provider.BlockToolResult || b.ToolResultForID != "call_1" {
				continue
			}
			if !b.ToolResultError {
				t.Errorf("tool-result.ToolResultError = false; want true (gate denied)")
			}
			if !strings.Contains(b.ToolResultText, "denied") {
				t.Errorf("tool-result text = %q; want something mentioning denial", b.ToolResultText)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no tool-result block for call_1 in history: %+v", hist)
	}
}

// --- T-432: Credential-missing wraps to exit 2 ---
//
// This pins the runtime half of the spec contract: Assemble returns an
// error wrapping ErrMissingCredential. The exit-code half (mapping to 2)
// is pinned at the cmd surface by TestMainExitCodeMissingCredential in
// cmd/openplus/main_test.go; we can't import main.exitCode from here, so
// we trust the cmd-side test for that mapping.

func TestIntegrationCredentialMissingWraps(t *testing.T) {
	// Remote provider with no apiKey and no baseURL → ErrMissingCredential.
	root := project(t, `{
  "model": "anthropic/claude-sonnet-5",
  "provider": {"anthropic": {"options": {}}}
}`)
	_, err := Assemble(root, Options{})
	if err == nil {
		t.Fatal("expected an error for a remote provider with no apiKey")
	}
	if !errors.Is(err, ErrMissingCredential) {
		t.Errorf("err = %v, want errors.Is(_, ErrMissingCredential)", err)
	}
}

// --- T-433: --fake end-to-end smoke ---

// TestIntegrationFakeSmokeEndToEnd: the full path
//   Assemble(tempRoot, Options{Fake:true}) → Run("hi") → Close
// returns a history with at least one user and one assistant message,
// and no error. This is the lowest-cost end-to-end assertion that proves
// the runtime wires Assemble + Session.Run + Close without regression.
func TestIntegrationFakeSmokeEndToEnd(t *testing.T) {
	root := project(t, "") // empty opencode.json is fine with --fake

	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	hist, err := s.Run(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(hist) < 2 {
		t.Fatalf("history = %d messages, want >= 2 (user + assistant)", len(hist))
	}
	if hist[0].Role != provider.RoleUser {
		t.Errorf("history[0] = %+v, want RoleUser", hist[0])
	}
	foundAssistant := false
	for _, m := range hist {
		if m.Role == provider.RoleAssistant {
			foundAssistant = true
			break
		}
	}
	if !foundAssistant {
		t.Errorf("no assistant message in history: %+v", hist)
	}
}

// writeFile is no longer used; the package's own `write` helper in
// assemble_test.go handles the same thing for that file.

// --- Change 0007 / T-440..T-444: Goal/Judge on Session ---
//
// These tests wire the existing orchestrate.Judge (0001/M7, ADR-0006)
// into Session.Run as the loop's termination condition. The judge is
// consulted after the agent loop produces a tool-less reply; MET
// stops, UNMET loops with feedback appended. RED until the fields
// and the wiring exist.

// judgeSays returns a scripted provider whose Stream calls return
// the given verdict replies in order (one per Evaluate call).
func judgeSays(replies ...string) *provider.Fake {
	scripts := make([][]provider.Event, len(replies))
	for i, reply := range replies {
		scripts[i] = []provider.Event{
			{Kind: provider.EventTextDelta, Text: reply},
			{Kind: provider.EventTurnEnd},
		}
	}
	return &provider.Fake{Scripts: scripts}
}

// TestGoalAbsentSkipsJudge (T-440): with Goal empty and Judge nil,
// Run behaves exactly as today — no judge consult, no extra turn.
func TestGoalAbsentSkipsJudge(t *testing.T) {
	root := project(t, "")
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	hist, err := s.Run(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Today: 2 messages (user + assistant).
	if len(hist) != 2 {
		t.Fatalf("history = %d messages, want 2 (user + assistant; no judge consult expected)", len(hist))
	}
}

// TestGoalEmptyStopsImmediately (T-441): Goal is empty but Judge is
// non-nil. Judge.Evaluate short-circuits to Met=true on empty goal;
// Run inherits that and returns without consulting the judge.
//
// Regression guard for the explicit Goal-empty short-circuit (see
// orchestrate/goal.go:62).
func TestGoalEmptyStopsImmediately(t *testing.T) {
	root := project(t, "")
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Set a Judge with a provider that records calls. The judge must
	// not be called because Goal is empty.
	judgeCalls := &atomic.Int32{}
	jp := &judgeRecordingProvider{inner: judgeSays("MET: anything"), calls: judgeCalls}
	s.Judge = &orchestrate.Judge{Provider: jp, Model: "fake/judge"}

	hist, err := s.Run(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := judgeCalls.Load(); got != 0 {
		t.Errorf("judge called %d times; want 0 (empty goal must short-circuit)", got)
	}
	if len(hist) != 2 {
		t.Errorf("history = %d messages; want 2", len(hist))
	}
}

// TestGoalJudgeStopsLoopWhenMet (T-442): agent wants to stop
// (tool-less reply), judge says MET → Run returns after one judge
// consult; judge called exactly once.
func TestGoalJudgeStopsLoopWhenMet(t *testing.T) {
	root := project(t, "")
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	s.Goal = "the user said hi"
	judgeCalls := &atomic.Int32{}
	jp := &judgeRecordingProvider{inner: judgeSays("MET: greeting acknowledged"), calls: judgeCalls}
	s.Judge = &orchestrate.Judge{Provider: jp, Model: "fake/judge"}

	if _, err := s.Run(context.Background(), "hi", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := judgeCalls.Load(); got != 1 {
		t.Errorf("judge calls = %d; want 1 (MET on first consult)", got)
	}
}

// TestGoalJudgeKeepsLoopingWhenUnmet (T-443): first judge says
// UNMET, second says MET. Run loops the agent twice; the second
// iteration's history includes the first feedback as a user
// message.
func TestGoalJudgeKeepsLoopingWhenUnmet(t *testing.T) {
	root := project(t, "")
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	s.Goal = "agent must call a tool"
	judgeCalls := &atomic.Int32{}
	jp := &judgeRecordingProvider{
		inner: judgeSays("UNMET: no tool called yet", "MET: tool called"),
		calls: judgeCalls,
	}
	s.Judge = &orchestrate.Judge{Provider: jp, Model: "fake/judge"}

	if _, err := s.Run(context.Background(), "hi", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := judgeCalls.Load(); got != 2 {
		t.Errorf("judge calls = %d; want 2 (UNMET then MET)", got)
	}
}

// TestGoalJudgeRespectsMaxIterations (T-444): judge always says
// UNMET. Run returns after MaxJudgeIterations (default 3) with an
// error wrapping the last verdict's feedback. No infinite loop.
func TestGoalJudgeRespectsMaxIterations(t *testing.T) {
	root := project(t, "")
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	s.Goal = "an unsatisfiable goal"
	s.MaxJudgeIterations = 3
	judgeCalls := &atomic.Int32{}
	jp := &judgeRecordingProvider{
		inner: judgeSays("UNMET: nope", "UNMET: still nope", "UNMET: try again"),
		calls: judgeCalls,
	}
	s.Judge = &orchestrate.Judge{Provider: jp, Model: "fake/judge"}

	start := time.Now()
	_, err = s.Run(context.Background(), "hi", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error after MaxJudgeIterations rounds of UNMET")
	}
	if !strings.Contains(err.Error(), "judge") && !strings.Contains(err.Error(), "goal") {
		t.Errorf("error = %v; want one that mentions the judge or the goal", err)
	}
	if got := judgeCalls.Load(); got != 3 {
		t.Errorf("judge calls = %d; want 3 (MaxJudgeIterations)", got)
	}
	if elapsed > 30*time.Second {
		t.Errorf("Run took %v; want < 30s (must not infinite-loop)", elapsed)
	}
}

// judgeRecordingProvider wraps a scripted Fake with an atomic counter.
// The counter increments every time Stream is called.
type judgeRecordingProvider struct {
	inner *provider.Fake
	calls *atomic.Int32
}

func (j *judgeRecordingProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	j.calls.Add(1)
	return j.inner.Stream(ctx, req)
}