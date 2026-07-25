package memory

import (
	"context"
	"testing"
)

// newTestFTS opens an in-memory shadow index and registers cleanup.
func newTestFTS(t *testing.T) *ftsIndex {
	t.Helper()
	f, err := openFTS(":memory:")
	if err != nil {
		t.Fatalf("openFTS: %v", err)
	}
	t.Cleanup(func() { _ = f.close() })
	return f
}

// TestFTSOpenAndSchema is the canary: openFTS creates the chunks_fts
// virtual table with the fts5 module (provided by modernc.org/sqlite,
// the cgo-free shadow-index engine added in change 0021).
func TestFTSOpenAndSchema(t *testing.T) {
	f := newTestFTS(t)
	var name string
	err := f.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='chunks_fts'`,
	).Scan(&name)
	if err != nil || name != "chunks_fts" {
		t.Fatalf("chunks_fts virtual table not created: name=%q err=%v", name, err)
	}
}

// TestFTSIndexAndSearch proves the MATCH+bm25 path works: index two
// documents, search for a term present in only one, expect exactly
// that rowid back with a positive rank score.
func TestFTSIndexAndSearch(t *testing.T) {
	f := newTestFTS(t)
	ctx := context.Background()

	if err := f.index(ctx, 1, "the rust programming language"); err != nil {
		t.Fatalf("index 1: %v", err)
	}
	if err := f.index(ctx, 2, "a recipe for apple pie"); err != nil {
		t.Fatalf("index 2: %v", err)
	}

	hits, err := f.search(ctx, "rust", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("search returned %d hits, want 1 (only the rust doc matches): %v", len(hits), hits)
	}
	if _, ok := hits[1]; !ok {
		t.Fatalf("search returned ids %v, want [1]", idsOf(hits))
	}
	if hits[1] <= 0 {
		t.Errorf("bm25-derived score for a match should be positive, got %f", hits[1])
	}
}

// TestFTSSearchMiss verifies a query with no matching documents returns
// an empty (non-nil) map and no error.
func TestFTSSearchMiss(t *testing.T) {
	f := newTestFTS(t)
	ctx := context.Background()
	if err := f.index(ctx, 1, "hello world"); err != nil {
		t.Fatal(err)
	}
	hits, err := f.search(ctx, "quantum", 5)
	if err != nil {
		t.Fatalf("search miss: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("search miss returned %d hits, want 0", len(hits))
	}
}

// TestFTSDelete verifies delete removes rows from the shadow index so a
// subsequent search no longer returns them.
func TestFTSDelete(t *testing.T) {
	f := newTestFTS(t)
	ctx := context.Background()
	for i, tx := range []string{"alpha beta", "gamma delta", "epsilon zeta"} {
		if err := f.index(ctx, int64(i+1), tx); err != nil {
			t.Fatalf("index %d: %v", i+1, err)
		}
	}
	if err := f.delete(ctx, []int64{2}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hits, err := f.search(ctx, "gamma", 5)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("deleted rowid 2 still returned by search: %v", hits)
	}
	// The other rows must survive.
	hits, _ = f.search(ctx, "alpha", 5)
	if len(hits) != 1 {
		t.Errorf("alpha search after deleting gamma = %d hits, want 1", len(hits))
	}
}

// TestFTSSearchKLimit verifies the k argument caps the result count.
func TestFTSSearchKLimit(t *testing.T) {
	f := newTestFTS(t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		if err := f.index(ctx, int64(i), "shared keyword doc"); err != nil {
			t.Fatalf("index %d: %v", i, err)
		}
	}
	hits, err := f.search(ctx, "shared", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) > 2 {
		t.Errorf("search k=2 returned %d hits, want <= 2", len(hits))
	}
}

// idsOf is a small test helper that extracts the id keys from a
// search-result map for readable failure messages.
func idsOf(m map[int64]float64) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
