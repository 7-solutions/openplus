package coordinate

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// symbolFile is the on-disk path for a symbol's lock: a sanitized filename, one
// file per symbol. Tested here because the claim atomicity rests on it.
func TestSymbolFilePathIsSanitized(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	got := s.lockPath("pkg/auth.go::Config.Validate")
	// no path separators (would escape the lock dir) and no colons
	if strings.ContainsAny(filepath.Base(got), `/\:`) {
		t.Errorf("lock path contains a separator or colon: %q", got)
	}
	if !strings.HasSuffix(got, ".lock") {
		t.Errorf("lock path should end in .lock: %q", got)
	}
}

// --- T-1310: atomic acquire ---

// TestAcquireExclusive is the spec scenario under contention: one symbol, N
// concurrent claims, exactly one winner. Run under -race in CI.
func TestAcquireExclusive(t *testing.T) {
	s := NewStore(t.TempDir(), 0)

	const N = 20
	var winners atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		i := i
		wg.Go(func() {
			held, err := s.Acquire(agentName(i), "intent", []string{"f.go::A"})
			if err != nil {
				return
			}
			if held.Granted {
				winners.Add(1)
			}
		})
	}
	wg.Wait()

	if w := winners.Load(); w != 1 {
		t.Fatalf("winners = %d, want exactly 1", w)
	}
}

func TestAcquireRecordsHolder(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	held, err := s.Acquire("agent-1", "fix login", []string{"f.go::A"})
	if err != nil || !held.Granted {
		t.Fatalf("Acquire: %+v %v", held, err)
	}
	if h := s.Holder("f.go::A"); h != "agent-1" {
		t.Errorf("Holder = %q, want agent-1", h)
	}
}

// TestAcquireRefusedReportsHolder is the spec scenario: a refused claim names the
// holder and the blocking symbol.
func TestAcquireRefusedReportsHolder(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	if _, err := s.Acquire("agent-1", "first", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	held, err := s.Acquire("agent-2", "second", []string{"f.go::A"})
	if err != nil {
		t.Fatalf("a refused claim is not an error: %v", err)
	}
	if held.Granted {
		t.Fatal("second claim should be refused")
	}
	if held.BlockedBy != "agent-1" {
		t.Errorf("BlockedBy = %q", held.BlockedBy)
	}
	if held.BlockedSymbol != "f.go::A" {
		t.Errorf("BlockedSymbol = %q", held.BlockedSymbol)
	}
}

// TestAcquireAllOrNothing is the spec scenario: a partially-blocked claim leaves
// nothing locked.
func TestAcquireAllOrNothing(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	if _, err := s.Acquire("agent-1", "hold", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	held, err := s.Acquire("agent-2", "mixed", []string{"f.go::A", "f.go::B"})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if held.Granted {
		t.Fatal("a claim touching a held symbol must be refused")
	}
	// f.go::B was free, but the claim was refused, so it must NOT be locked.
	if h := s.Holder("f.go::B"); h != "" {
		t.Errorf("B was locked despite the claim being refused: held by %q", h)
	}
}

func TestAcquireDifferentSymbolsSucceed(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	if _, err := s.Acquire("agent-1", "a", []string{"f.go::Login"}); err != nil {
		t.Fatal(err)
	}
	held, err := s.Acquire("agent-2", "b", []string{"f.go::Logout"})
	if err != nil || !held.Granted {
		t.Fatalf("different symbol should be granted: %+v %v", held, err)
	}
}

func TestAcquireSameAgentReclaimsItsOwn(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	if _, err := s.Acquire("agent-1", "a", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	// reclaiming your own symbol is fine (idempotent across claims)
	held, err := s.Acquire("agent-1", "a", []string{"f.go::A"})
	if err != nil || !held.Granted {
		t.Fatalf("same agent reclaiming its own symbol: %+v %v", held, err)
	}
}

func TestAcquireNoSymbols(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	if _, err := s.Acquire("a", "i", nil); err == nil {
		t.Fatal("expected an error acquiring no symbols")
	}
}

// --- T-1312: expiry and reclaim ---

// TestExpiredLockReclaimable is the spec scenario: a crashed agent's stale lock
// must not block work forever.
func TestExpiredLockReclaimable(t *testing.T) {
	// A tiny expiry so the lock is stale immediately.
	s := NewStore(t.TempDir(), 1*time.Millisecond)
	if _, err := s.Acquire("agent-1", "stale", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)

	held, err := s.Acquire("agent-2", "takeover", []string{"f.go::A"})
	if err != nil || !held.Granted {
		t.Fatalf("stale lock should be reclaimable: %+v %v", held, err)
	}
	if !held.Reclaimed {
		t.Error("reclaim should be reported")
	}
	if held.ReclaimedFrom != "agent-1" {
		t.Errorf("ReclaimedFrom = %q, want agent-1", held.ReclaimedFrom)
	}
}

func TestLiveLockNotStolen(t *testing.T) {
	s := NewStore(t.TempDir(), time.Hour) // long expiry: nothing is stale
	if _, err := s.Acquire("agent-1", "live", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	held, err := s.Acquire("agent-2", "attempt", []string{"f.go::A"})
	if err != nil {
		t.Fatal(err)
	}
	if held.Granted {
		t.Fatal("a live lock must not be stolen")
	}
}

func TestZeroExpiryNeverExpires(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	if _, err := s.Acquire("agent-1", "forever", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	held, err := s.Acquire("agent-2", "never", []string{"f.go::A"})
	if err != nil || held.Granted {
		t.Fatalf("zero expiry means never expire: %+v %v", held, err)
	}
}

// --- T-1313: release ---

func TestReleaseAgentFreesLocks(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	if _, err := s.Acquire("agent-1", "a", []string{"f.go::A", "f.go::B"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseAgent("agent-1"); err != nil {
		t.Fatalf("ReleaseAgent: %v", err)
	}
	for _, sym := range []string{"f.go::A", "f.go::B"} {
		if h := s.Holder(sym); h != "" {
			t.Errorf("%s still held by %q after release", sym, h)
		}
	}
}

func TestReleaseUnheldAgentIsHarmless(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	if err := s.ReleaseAgent("nobody"); err != nil {
		t.Fatalf("releasing an agent holding nothing should not error: %v", err)
	}
}

// TestAcquireAfterRelease is the round-trip: release, then claim again succeeds.
func TestAcquireAfterRelease(t *testing.T) {
	s := NewStore(t.TempDir(), 0)
	if _, err := s.Acquire("agent-1", "a", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseAgent("agent-1"); err != nil {
		t.Fatal(err)
	}
	held, err := s.Acquire("agent-2", "again", []string{"f.go::A"})
	if err != nil || !held.Granted {
		t.Fatalf("re-claim after release failed: %+v %v", held, err)
	}
}

func TestAcquirePersistenceAcrossStoreInstances(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir, 0)
	if _, err := s1.Acquire("agent-1", "a", []string{"f.go::A"}); err != nil {
		t.Fatal(err)
	}
	// a new Store over the same directory must see the held lock
	s2 := NewStore(dir, 0)
	if h := s2.Holder("f.go::A"); h != "agent-1" {
		t.Errorf("lock not persisted: holder = %q", h)
	}
	held, _ := s2.Acquire("agent-2", "b", []string{"f.go::A"})
	if held.Granted {
		t.Error("second store instance should see the lock held")
	}
}

// agentName maps an index to a stable agent id.
func agentName(i int) string { return "agent-" + itoaCoord(i) }

func itoaCoord(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
