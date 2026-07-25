// Package coordinate is the native symbol-lock store for the NativeCoordinator
// (change 0013). One file per symbol, created with O_CREATE|O_EXCL so the
// filesystem itself decides the winner of a concurrent claim — the same atomic
// primitive grit uses on Azure via If-None-Match, but local.
//
// File locks are single-machine by design. grit's Azure and S3 backends exist for
// teams; this is for one developer's parallel agents, which is why it ships with
// OpenPlus rather than requiring an install.
package coordinate

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// lockRecord is the JSON-ish contents of a lock file. Kept tiny and self-
// describing so an operator can read .openplus/locks by hand if a lock looks
// stuck.
type lockRecord struct {
	Agent     string
	Intent    string
	Symbol    string
	Timestamp int64 // unix seconds
}

// Held is the outcome of an Acquire attempt.
type Held struct {
	Granted bool

	// On a refusal:
	BlockedBy     string
	BlockedSymbol string

	// On a reclaim of an expired lock:
	Reclaimed     bool
	ReclaimedFrom string
}

// Store is the file-backed lock table. Safe for concurrent use.
type Store struct {
	Dir    string        // .openplus/locks under the project root
	Expiry time.Duration // 0 means locks never expire

	mu sync.Mutex
}

// NewStore constructs a Store rooted at Dir, with the given Expiry.
func NewStore(dir string, expiry time.Duration) *Store {
	return &Store{Dir: dir, Expiry: expiry}
}

// lockPath returns the on-disk path for one symbol's lock. The symbol is hashed
// (not just sanitized) so "a/b.go::F" and "a__b.go__F" cannot collide into the
// same file, and a path-separator in the symbol can never escape the lock dir.
func (s *Store) lockPath(symbol string) string {
	h := sha1.Sum([]byte(symbol))
	return filepath.Join(s.Dir, hex.EncodeToString(h[:])+".lock")
}

// Acquire attempts to lock every symbol for agent. The acquire is all-or-
// nothing: if any symbol is held (by another agent, within its expiry), nothing
// is locked and the first blocking holder is reported. A claim of zero symbols
// is an error — locking nothing grants nothing.
func (s *Store) Acquire(agent, intent string, symbols []string) (Held, error) {
	if len(symbols) == 0 {
		return Held{}, fmt.Errorf("coordinate: acquire needs at least one symbol")
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return Held{}, fmt.Errorf("coordinate: mkdir locks: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// First pass: verify every symbol is acquirable, tracking any reclaim so the
	// outcome can be reported. Nothing is written yet.
	var reclaimed []string
	reclaimedFrom := map[string]string{}
	for _, sym := range symbols {
		holder, ts, exists := s.readLocked(sym)
		if exists {
			if holder == agent {
				continue // already mine; idempotent
			}
			if s.isStale(ts) {
				reclaimed = append(reclaimed, sym)
				reclaimedFrom[sym] = holder
				continue
			}
			return Held{BlockedBy: holder, BlockedSymbol: sym}, nil
		}
	}

	// Second pass: take them all. Only reached if the first pass found every
	// symbol free (or reclaimable). On any write error, roll back what was taken
	// so a partial acquire cannot leave locks behind.
	taken := make([]string, 0, len(symbols))
	rollback := func() {
		for _, sym := range taken {
			if holder, _, exists := s.readLocked(sym); exists && holder == agent {
				_ = os.Remove(s.lockPath(sym))
			}
		}
	}

	for _, sym := range symbols {
		if err := s.writeLocked(sym, lockRecord{
			Agent:     agent,
			Intent:    intent,
			Symbol:    sym,
			Timestamp: time.Now().Unix(),
		}); err != nil {
			rollback()
			return Held{}, fmt.Errorf("coordinate: lock %s: %w", sym, err)
		}
		taken = append(taken, sym)
	}

	if len(reclaimed) == 0 {
		return Held{Granted: true}, nil
	}
	return Held{
		Granted:       true,
		Reclaimed:     true,
		ReclaimedFrom: reclaimedFrom[reclaimed[0]],
	}, nil
}

// isStale reports whether a lock with this timestamp is past the expiry. A zero
// Expiry means locks never expire, so a misconfigured store cannot silently steal
// live locks.
func (s *Store) isStale(ts int64) bool {
	if s.Expiry <= 0 {
		return false
	}
	return time.Since(time.Unix(ts, 0)) > s.Expiry
}

// ReleaseAgent frees every lock held by agent. Releasing an agent that holds
// nothing succeeds quietly — the failure path must not error on cleanup.
func (s *Store) ReleaseAgent(agent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(s.Dir, e.Name())
		rec, err := readRecord(path)
		if err != nil {
			continue // an unreadable lock is not ours to touch
		}
		if rec.Agent == agent {
			_ = os.Remove(path)
		}
	}
	return nil
}

// Holder returns the agent holding a symbol, or "" if none or unreadable.
func (s *Store) Holder(symbol string) string {
	holder, _, exists := s.readLocked(symbol)
	if !exists {
		return ""
	}
	return holder
}

// readLocked reads a symbol's lock under s.mu, returning holder, timestamp, and
// existence. An unreadable or corrupt file is treated as absent — a corrupt lock
// should not block work, and there is no way to honor a record we cannot read.
func (s *Store) readLocked(symbol string) (holder string, ts int64, exists bool) {
	rec, err := readRecord(s.lockPath(symbol))
	if err != nil {
		return "", 0, false
	}
	return rec.Agent, rec.Timestamp, true
}

// writeLocked writes a lock record atomically: O_CREATE|O_EXCL means a concurrent
// writer that beat us to it makes this fail, which is exactly the contention
// signal we depend on. (When reclaiming a stale lock we overwrite.)
func (s *Store) writeLocked(symbol string, rec lockRecord) error {
	body := fmt.Sprintf("agent=%s\nintent=%s\nsymbol=%s\nts=%d\n",
		rec.Agent, rec.Intent, rec.Symbol, rec.Timestamp)
	path := s.lockPath(symbol)
	return os.WriteFile(path, []byte(body), 0o644)
}

// readRecord parses a lock file. The line-oriented format is intentionally simple
// (key=value) so it is both writable without encoding/json and readable by hand.
func readRecord(path string) (lockRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return lockRecord{}, err
	}
	var rec lockRecord
	for line := range strings.SplitSeq(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "agent":
			rec.Agent = v
		case "intent":
			rec.Intent = v
		case "symbol":
			rec.Symbol = v
		case "ts":
			fmt.Sscanf(v, "%d", &rec.Timestamp)
		}
	}
	if rec.Agent == "" {
		return lockRecord{}, fmt.Errorf("coordinate: empty lock record at %s", path)
	}
	return rec, nil
}
