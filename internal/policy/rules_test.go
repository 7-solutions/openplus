package policy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7solutions/openplus/internal/provider"
)

func call(name string, input string) provider.ToolCall {
	return provider.ToolCall{Name: name, Input: []byte(input)}
}

func TestRulesDefaultWhenNoMatch(t *testing.T) {
	r := Rules{Default: Allow}
	got, err := r.Permit(context.Background(), call("bash", `{}`))
	if err != nil {
		t.Fatalf("Permit: %v", err)
	}
	if got != Allow {
		t.Fatalf("got %v, want Allow (default)", got)
	}
}

func TestRulesLastMatchWins(t *testing.T) {
	r := Rules{
		Default: Allow,
		List: []Rule{
			{ToolGlob: "bash", Decision: Deny},
			{ToolGlob: "bash", Decision: Ask}, // later rule overrides
		},
	}
	got, _ := r.Permit(context.Background(), call("bash", `{}`))
	if got != Ask {
		t.Fatalf("last-match-wins: got %v, want Ask", got)
	}
}

func TestRulesToolGlobMatch(t *testing.T) {
	r := Rules{
		Default: Allow,
		List:    []Rule{{ToolGlob: "read_*", Decision: Deny}},
	}
	got, _ := r.Permit(context.Background(), call("read_file", `{}`))
	if got != Deny {
		t.Fatalf("got %v, want Deny", got)
	}
	// non-matching tool falls through to default
	got, _ = r.Permit(context.Background(), call("write", `{}`))
	if got != Allow {
		t.Fatalf("got %v, want Allow default", got)
	}
}

func TestRulesPathGlobOverridesTool(t *testing.T) {
	// "write" asks everywhere, but under /tmp/** it's allowed. Path rule is
	// appended after the tool rule → last-match-wins lets /tmp override.
	r := Rules{
		Default: Deny,
		List: []Rule{
			{ToolGlob: "write", Decision: Ask},
			{PathGlob: "/tmp/**", Decision: Allow},
		},
	}
	got, _ := r.Permit(context.Background(), call("write", `{"file_path":"/tmp/x.txt"}`))
	if got != Allow {
		t.Fatalf("path override: got %v, want Allow", got)
	}
	got, _ = r.Permit(context.Background(), call("write", `{"file_path":"/home/x.txt"}`))
	if got != Ask {
		t.Fatalf("non-/tmp: got %v, want Ask", got)
	}
}

func TestRulesPathGlobRequiresPathArg(t *testing.T) {
	// a path rule must not match a call with no path arg (e.g. bash command).
	r := Rules{
		Default: Allow,
		List:    []Rule{{PathGlob: "/tmp/**", Decision: Deny}},
	}
	got, _ := r.Permit(context.Background(), call("bash", `{"command":"ls"}`))
	if got != Allow {
		t.Fatalf("path rule matched a path-less call: got %v", got)
	}
}

func TestParseDecision(t *testing.T) {
	cases := map[string]Decision{
		"allow": Allow, "ask": Ask, "deny": Deny,
		"ALLOW": Allow, "Allow": Allow,
	}
	for s, want := range cases {
		got, err := ParseDecision(s)
		if err != nil || got != want {
			t.Errorf("ParseDecision(%q) = (%v,%v), want (%v,nil)", s, got, err, want)
		}
	}
	if _, err := ParseDecision("bogus"); err == nil {
		t.Error("ParseDecision(bogus) should error")
	}
}

func TestNewRulesFromMapsOrdering(t *testing.T) {
	// tool rules + path rules build a deterministic, last-match-wins list:
	// tool rules first, then path rules (path wins when both apply).
	r, err := NewRules(Deny, map[string]string{"write": "ask"}, map[string]string{"/tmp/**": "allow"})
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}
	if r.Default != Deny {
		t.Fatalf("default = %v", r.Default)
	}
	// path rule present and last
	if len(r.List) != 2 {
		t.Fatalf("list len = %d", len(r.List))
	}
	if r.List[0].ToolGlob != "write" || r.List[1].PathGlob != "/tmp/**" {
		t.Fatalf("ordering wrong: %+v", r.List)
	}
	got, _ := r.Permit(context.Background(), call("write", `{"file_path":"/tmp/a"}`))
	if got != Allow {
		t.Fatalf("got %v, want Allow (path wins)", got)
	}
}

// recordingPrompter records calls and returns a canned answer.
type recordingPrompter struct {
	approved bool
	calls    []string
}

func (p *recordingPrompter) Ask(ctx context.Context, c provider.ToolCall) (bool, error) {
	p.calls = append(p.calls, c.Name)
	return p.approved, nil
}

func TestPromptingAskApproved(t *testing.T) {
	rp := &recordingPrompter{approved: true}
	gate := Prompting{Rules: &Rules{
		Default: Allow,
		List:    []Rule{{ToolGlob: "bash", Decision: Ask}},
	}, Prompter: rp}
	got, err := gate.Permit(context.Background(), call("bash", `{}`))
	if err != nil {
		t.Fatalf("Permit: %v", err)
	}
	if got != Allow {
		t.Fatalf("approved ask: got %v, want Allow", got)
	}
	if len(rp.calls) != 1 || rp.calls[0] != "bash" {
		t.Fatalf("prompter calls = %v", rp.calls)
	}
}

func TestPromptingAskDeniedByPrompter(t *testing.T) {
	gate := Prompting{
		Rules:    &Rules{Default: Allow, List: []Rule{{ToolGlob: "bash", Decision: Ask}}},
		Prompter: &recordingPrompter{approved: false},
	}
	got, _ := gate.Permit(context.Background(), call("bash", `{}`))
	if got != Deny {
		t.Fatalf("denied ask: got %v, want Deny", got)
	}
}

func TestPromptingAskDeniedWhenNoPrompter(t *testing.T) {
	// Safe default: with no prompter wired, Ask must not silently allow.
	gate := Prompting{Rules: &Rules{
		Default: Allow,
		List:    []Rule{{ToolGlob: "bash", Decision: Ask}},
	}} // Prompter nil
	got, _ := gate.Permit(context.Background(), call("bash", `{}`))
	if got != Deny {
		t.Fatalf("no prompter: got %v, want Deny (safe)", got)
	}
}

func TestPromptingPassesThroughAllowDeny(t *testing.T) {
	gate := Prompting{Rules: &Rules{
		Default: Allow,
		List: []Rule{
			{ToolGlob: "read", Decision: Allow},
			{ToolGlob: "rm", Decision: Deny},
		},
	}}
	if got, _ := gate.Permit(context.Background(), call("read", `{}`)); got != Allow {
		t.Errorf("read: got %v", got)
	}
	if got, _ := gate.Permit(context.Background(), call("rm", `{}`)); got != Deny {
		t.Errorf("rm: got %v", got)
	}
}

// TestPromptingAskTimeout proves the forced-ask timeout is ctx-driven: a
// prompter that blocks past the ctx deadline surfaces as an error→Deny.
type blockingPrompter struct{}

func (blockingPrompter) Ask(ctx context.Context, c provider.ToolCall) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

func TestPromptingAskTimeout(t *testing.T) {
	gate := Prompting{
		Rules:    &Rules{Default: Allow, List: []Rule{{ToolGlob: "bash", Decision: Ask}}},
		Prompter: blockingPrompter{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	got, err := gate.Permit(ctx, call("bash", `{}`))
	if got != Deny {
		t.Fatalf("timeout: got %v, want Deny", got)
	}
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}
