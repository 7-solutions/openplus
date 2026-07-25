package orchestrate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// --- T-1200: the port ---

func TestFakeCoordinatorGrantsAndRecords(t *testing.T) {
	c := NewFakeCoordinator()

	got, err := c.Claim(context.Background(), "agent-1", "add validation",
		[]string{"src/auth.go::Validate"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !got.Granted {
		t.Fatalf("claim refused: %+v", got)
	}
	if got.Dir == "" {
		t.Error("a granted claim should carry a worktree dir")
	}
	if held := c.Holder("src/auth.go::Validate"); held != "agent-1" {
		t.Errorf("holder = %q, want agent-1", held)
	}
}

// TestFakeCoordinatorBlocksHeldSymbol is the spec scenario: a second agent
// asking for a held symbol is refused, and told who holds it.
func TestFakeCoordinatorBlocksHeldSymbol(t *testing.T) {
	c := NewFakeCoordinator()
	if _, err := c.Claim(context.Background(), "agent-1", "first", []string{"f.go::Login"}); err != nil {
		t.Fatal(err)
	}

	got, err := c.Claim(context.Background(), "agent-2", "second", []string{"f.go::Login"})
	if err != nil {
		t.Fatalf("a blocked claim is not an error: %v", err)
	}
	if got.Granted {
		t.Fatal("second claim on a held symbol must be refused")
	}
	if got.BlockedBy != "agent-1" {
		t.Errorf("BlockedBy = %q, want agent-1", got.BlockedBy)
	}
	if got.BlockedSymbol != "f.go::Login" {
		t.Errorf("BlockedSymbol = %q", got.BlockedSymbol)
	}
}

// TestFakeCoordinatorDifferentSymbolsSameFile is the spec scenario and the whole
// point of symbol-level locking.
func TestFakeCoordinatorDifferentSymbolsSameFile(t *testing.T) {
	c := NewFakeCoordinator()
	first, err := c.Claim(context.Background(), "agent-1", "a", []string{"auth.go::Login"})
	if err != nil || !first.Granted {
		t.Fatalf("first claim: %+v %v", first, err)
	}
	second, err := c.Claim(context.Background(), "agent-2", "b", []string{"auth.go::Logout"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Granted {
		t.Fatalf("different symbols in one file must both be granted: %+v", second)
	}
	if first.Dir == second.Dir {
		t.Error("each agent needs its own worktree")
	}
}

func TestFakeCoordinatorDoneReleases(t *testing.T) {
	c := NewFakeCoordinator()
	if _, err := c.Claim(context.Background(), "agent-1", "x", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Done(context.Background(), "agent-1"); err != nil {
		t.Fatalf("Done: %v", err)
	}
	if held := c.Holder("f.go::A"); held != "" {
		t.Errorf("symbol still held after Done: %q", held)
	}
	if !c.Merged("agent-1") {
		t.Error("Done should record a merge")
	}
}

// TestFakeCoordinatorReleaseDoesNotMerge pins the distinction: Release frees the
// locks without claiming the work was merged.
func TestFakeCoordinatorReleaseDoesNotMerge(t *testing.T) {
	c := NewFakeCoordinator()
	if _, err := c.Claim(context.Background(), "agent-1", "x", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Release(context.Background(), "agent-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if held := c.Holder("f.go::A"); held != "" {
		t.Errorf("symbol still held after Release: %q", held)
	}
	if c.Merged("agent-1") {
		t.Error("Release must not record a merge")
	}
}

func TestFakeCoordinatorClaimNeedsSymbols(t *testing.T) {
	c := NewFakeCoordinator()
	if _, err := c.Claim(context.Background(), "agent-1", "x", nil); err == nil {
		t.Fatal("expected an error claiming no symbols")
	}
}

func TestFakeCoordinatorAvailable(t *testing.T) {
	c := NewFakeCoordinator()
	if !c.Available() {
		t.Fatal("the fake coordinator should report available")
	}
	c.Unavailable = true
	if c.Available() {
		t.Fatal("Unavailable was not honored")
	}
}

// TestFakeCoordinatorClaimError lets a test drive the hard-error path, which is
// distinct from a blocked claim.
func TestFakeCoordinatorClaimError(t *testing.T) {
	c := NewFakeCoordinator()
	boom := errors.New("coordination backend down")
	c.ClaimErr = boom

	if _, err := c.Claim(context.Background(), "a", "i", []string{"f.go::A"}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

// --- T-1201: NoCoordinator ---

// TestNoCoordinatorIsUnavailable pins the uncoordinated default: a real object
// rather than a nil check at every call site.
func TestNoCoordinatorIsUnavailable(t *testing.T) {
	var c Coordinator = NoCoordinator{}
	if c.Available() {
		t.Fatal("NoCoordinator must report unavailable")
	}
}

func TestNoCoordinatorClaimExplainsWhy(t *testing.T) {
	var c Coordinator = NoCoordinator{}
	_, err := c.Claim(context.Background(), "a", "i", []string{"f.go::A"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no coordinator") {
		t.Errorf("error should say coordination is unconfigured: %v", err)
	}
}

func TestNoCoordinatorDoneAndReleaseAreNoops(t *testing.T) {
	var c Coordinator = NoCoordinator{}
	if err := c.Done(context.Background(), "a"); err != nil {
		t.Errorf("Done: %v", err)
	}
	if err := c.Release(context.Background(), "a"); err != nil {
		t.Errorf("Release: %v", err)
	}
}

// compile-time: both implementations satisfy the port.
var (
	_ Coordinator = (*FakeCoordinator)(nil)
	_ Coordinator = NoCoordinator{}
)
