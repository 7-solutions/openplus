package policy

import (
	"context"
	"testing"

	"github.com/7-solutions/openplus/internal/ports"
)

func TestSkipExplicitDenyWinsOverBase(t *testing.T) {
	s, err := NewSkip(map[string]string{"bash": "deny"}, nil)
	if err != nil {
		t.Fatalf("NewSkip: %v", err)
	}
	got, _ := s.Permit(context.Background(), ports.ToolCall{Name: "bash", Input: []byte(`{}`)})
	if got != Deny {
		t.Fatalf("explicit deny lost to base: got %v, want Deny", got)
	}
}

func TestSkipBaseAllowsUnmatched(t *testing.T) {
	s, _ := NewSkip(map[string]string{"bash": "deny"}, nil)
	got, _ := s.Permit(context.Background(), ports.ToolCall{Name: "read", Input: []byte(`{}`)})
	if got != Allow {
		t.Fatalf("unmatched call: got %v, want Allow (base)", got)
	}
}

func TestSkipAskBecomesAllow(t *testing.T) {
	// --dangerously-skip-permissions skips prompts: an explicit ask rule must
	// not block (becomes Allow), while an explicit deny still denies.
	s, _ := NewSkip(map[string]string{"bash": "ask", "rm": "deny"}, nil)
	got, _ := s.Permit(context.Background(), ports.ToolCall{Name: "bash", Input: []byte(`{}`)})
	if got != Allow {
		t.Fatalf("ask should become allow under skip: got %v", got)
	}
	got, _ = s.Permit(context.Background(), ports.ToolCall{Name: "rm", Input: []byte(`{}`)})
	if got != Deny {
		t.Fatalf("deny must still deny under skip: got %v", got)
	}
}

func TestSkipPathRulesWin(t *testing.T) {
	// explicit path deny still wins even with an allow-all base.
	s, _ := NewSkip(nil, map[string]string{"/etc/**": "deny"})
	got, _ := s.Permit(context.Background(), ports.ToolCall{
		Name: "write", Input: []byte(`{"file_path":"/etc/passwd"}`),
	})
	if got != Deny {
		t.Fatalf("path deny lost: got %v", got)
	}
	got, _ = s.Permit(context.Background(), ports.ToolCall{
		Name: "write", Input: []byte(`{"file_path":"/tmp/x"}`),
	})
	if got != Allow {
		t.Fatalf("non-/etc write: got %v, want Allow", got)
	}
}

func TestSkipRejectsBadDecision(t *testing.T) {
	if _, err := NewSkip(map[string]string{"bash": "bogus"}, nil); err == nil {
		t.Fatal("NewSkip should reject unknown decision")
	}
}

func TestSkipImplementsGate(t *testing.T) {
	var _ Gate = (*Skip)(nil)
}
