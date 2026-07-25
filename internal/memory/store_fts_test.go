package memory

import (
	"context"
	"testing"
)

// TestOpenWithFTS verifies the WithFTS() option opens the shadow index
// alongside the primary Turso store. The shadow is the modernc.org/sqlite
// FTS5 derived index (change 0021).
func TestOpenWithFTS(t *testing.T) {
	s, err := Open(":memory:", WithFTS())
	if err != nil {
		t.Fatalf("Open WithFTS: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.fts == nil {
		t.Fatal("WithFTS() did not open the shadow index (s.fts == nil)")
	}
	// The shadow's chunks_fts virtual table must exist.
	var name string
	err = s.fts.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='chunks_fts'`,
	).Scan(&name)
	if err != nil || name != "chunks_fts" {
		t.Fatalf("shadow chunks_fts missing: name=%q err=%v", name, err)
	}
}

// TestVectorOnlyDefault verifies Open(path) WITHOUT WithFTS keeps the
// change 0020 behavior: vector-only, no shadow. This is the backward-
// compatibility guarantee — no existing caller changes.
func TestVectorOnlyDefault(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.fts != nil {
		t.Fatal("Open without WithFTS opened a shadow (s.fts != nil); breaks backward compat")
	}
}

// TestWriteIndexesShadow verifies Write populates the shadow index: after
// a write, an FTS MATCH for the written text resolves to the chunk's id.
func TestWriteIndexesShadow(t *testing.T) {
	s, err := Open(":memory:", WithFTS())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.Embedder = fakeEmbed{dim: 4}

	id, err := s.Write(context.Background(), "the rust ownership model", "doc")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	hits, err := s.fts.search(context.Background(), "rust", 5)
	if err != nil {
		t.Fatalf("shadow search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("shadow returned %d hits for 'rust', want 1: %v", len(hits), hits)
	}
	if _, ok := hits[id]; !ok {
		t.Errorf("shadow did not return written id %d: %v", id, hits)
	}
}

// TestPruneDeletesShadow verifies pruneToMaxEntries removes pruned rowids
// from the shadow too — otherwise Search returns phantom lexical hits for
// chunks that no longer exist in the primary table.
func TestPruneDeletesShadow(t *testing.T) {
	s, err := Open(":memory:", WithFTS())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.Embedder = fakeEmbed{dim: 4}
	s.SetMaxEntries(2)

	// Write 4 chunks; the cap of 2 prunes the oldest two (ids 1,2).
	for _, tx := range []string{"alpha one", "beta two", "gamma three", "delta four"} {
		if _, err := s.Write(context.Background(), tx, "doc"); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	// Primary and shadow must agree on row count (both pruned to 2).
	var primCount, shadowCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&primCount)
	s.fts.db.QueryRow(`SELECT COUNT(*) FROM chunks_fts`).Scan(&shadowCount)
	if primCount != 2 {
		t.Errorf("primary count = %d, want 2", primCount)
	}
	if shadowCount != 2 {
		t.Errorf("shadow count = %d, want 2 (prune must delete from shadow too)", shadowCount)
	}
	// The pruned terms must no longer match.
	if hits, _ := s.fts.search(context.Background(), "alpha", 5); len(hits) != 0 {
		t.Errorf("pruned term 'alpha' still in shadow: %v", hits)
	}
}

// TestHybridSearchBoostsLexicalMatch is the golden hybrid test: for the
// same data, enabling FTS raises the RRF score of a chunk that lexically
// matches the query. This directly proves the lexical half contributes —
// the keyword chunk's score is strictly higher under hybrid retrieval
// than under vector-only retrieval.
func TestHybridSearchBoostsLexicalMatch(t *testing.T) {
	ctx := context.Background()
	// Two stores with identical data: one vector-only, one hybrid.
	vo, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vo.Close() })
	hy, err := Open(":memory:", WithFTS())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hy.Close() })
	for _, s := range []*Store{vo, hy} {
		s.Embedder = fakeEmbed{dim: 4}
	}

	chunks := []string{
		"an introduction to relational databases",
		"the rust ownership model and borrow checker",
		"cooking recipes for healthy weeknight meals",
		"travel guide to european mountain ranges",
	}
	var rustID int64
	for _, tx := range chunks {
		id, err := vo.Write(ctx, tx, "doc")
		if err != nil {
			t.Fatalf("vo Write: %v", err)
		}
		if _, err := hy.Write(ctx, tx, "doc"); err != nil {
			t.Fatalf("hy Write: %v", err)
		}
		if tx == chunks[1] {
			rustID = id
		}
	}

	voRes, err := vo.Search(ctx, "rust", 4)
	if err != nil {
		t.Fatalf("vo Search: %v", err)
	}
	hyRes, err := hy.Search(ctx, "rust", 4)
	if err != nil {
		t.Fatalf("hy Search: %v", err)
	}
	voScore := scoreOf(voRes, rustID)
	hyScore := scoreOf(hyRes, rustID)
	if voScore <= 0 {
		t.Fatalf("vector-only did not return the rust chunk (score=%v); test setup issue", voScore)
	}
	// The hybrid score must strictly exceed the vector-only score: the
	// FTS half adds a positive RRF contribution for the lexical match.
	if hyScore <= voScore {
		t.Errorf("hybrid did not boost the lexical match: vector-only=%.6f hybrid=%.6f (hybrid must be greater)", voScore, hyScore)
	}
	t.Logf("rust chunk: vector-only score=%.6f, hybrid score=%.6f (boost=%.6f)", voScore, hyScore, hyScore-voScore)
}

// scoreOf finds the Result with the given id and returns its Score, or 0
// if absent. Small helper for the boost test's readable assertions.
func scoreOf(rs []Result, id int64) float64 {
	for _, r := range rs {
		if r.ID == id {
			return r.Score
		}
	}
	return 0
}
