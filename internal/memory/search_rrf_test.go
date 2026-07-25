package memory

import (
	"context"
	"math"
	"testing"
)

// rrfChunkSetup writes a deterministic 3-chunk corpus where the vector and
// lexical halves DISAGREE on the top match for the query "database":
//   - Y "deep dive delta"        — vector rank 0 (three d-words → strong d-bucket
//                                  signal matching the query), NO lexical match.
//   - X "database internals guide" — lexical match (token "database"), vector rank 1.
//   - Z "cooking recipes"         — filler; vector rank 2 (no d-bucket overlap), no lexical.
//
// fakeEmbed (per-first-letter buckets, dim 4) produces these distinct ranks
// with no ties, so the weight tests are deterministic. Returns the chunk ids
// in (Y, X, Z) order.
func rrfChunkSetup(t *testing.T, s *Store) (yID, xID, zID int64) {
	t.Helper()
	ctx := context.Background()
	var err error
	if yID, err = s.Write(ctx, "deep dive delta", "doc"); err != nil {
		t.Fatalf("write Y: %v", err)
	}
	if xID, err = s.Write(ctx, "database internals guide", "doc"); err != nil {
		t.Fatalf("write X: %v", err)
	}
	if zID, err = s.Write(ctx, "cooking recipes", "doc"); err != nil {
		t.Fatalf("write Z: %v", err)
	}
	return yID, xID, zID
}

// TestRRFDefaultMatchesNoOption is the backward-compat proof: a store opened
// with WithRRF(DefaultRRF()) produces identical chunk scores to one opened
// with no RRF option (which Open defaults to DefaultRRF()). Scores are
// compared, not ranking, so a tie-break can't make the test flaky.
func TestRRFDefaultMatchesNoOption(t *testing.T) {
	ctx := context.Background()
	noOpt, err := Open(":memory:", WithFTS())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = noOpt.Close() })
	def, err := Open(":memory:", WithFTS(), WithRRF(DefaultRRF()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = def.Close() })
	for _, s := range []*Store{noOpt, def} {
		s.Embedder = fakeEmbed{dim: 4}
	}
	yNo, xNo, _ := rrfChunkSetup(t, noOpt)
	yDef, xDef, _ := rrfChunkSetup(t, def)

	noRes, err := noOpt.Search(ctx, "database", 2)
	if err != nil {
		t.Fatalf("noOpt Search: %v", err)
	}
	defRes, err := def.Search(ctx, "database", 2)
	if err != nil {
		t.Fatalf("def Search: %v", err)
	}
	for _, id := range []int64{yNo, xNo} {
		want := scoreOf(noRes, id)
		got := scoreOf(defRes, mapID(id, yNo, xNo, yDef, xDef))
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("score for chunk %d: noOpt=%.6f def=%.6f (must be equal under DefaultRRF)", id, want, got)
		}
	}
}

// mapID translates a chunk id from the noOpt store's id-space to the def
// store's id-space by position. Both stores are fresh :memory: dbs written
// in the same order, so position 0/1 maps across. (Keeps the score-equality
// test readable without exposing internal ids.)
func mapID(id, yNo, xNo, yDef, xDef int64) int64 {
	switch id {
	case yNo:
		return yDef
	case xNo:
		return xDef
	}
	return id
}

// TestRRFVectorWeightFavorsVector proves a high VectorWeight makes the
// vector-only match (Y) outrank the lexical-only match (X). With k=1 the
// vector half returns only Y (rank 0) and the lexical half returns only X
// (rank 0), so the weights alone decide the winner.
func TestRRFVectorWeightFavorsVector(t *testing.T) {
	s, err := Open(":memory:", WithFTS(), WithRRF(RRFConfig{K: 60, VectorWeight: 10, LexicalWeight: 1}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.Embedder = fakeEmbed{dim: 4}
	yID, _, _ := rrfChunkSetup(t, s)

	res, err := s.Search(context.Background(), "database", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1: %v", len(res), res)
	}
	if res[0].ID != yID {
		t.Errorf("VectorWeight=10 picked %d (X, lexical-only), want %d (Y, vector-only); weights not tilting toward vector: %v", res[0].ID, yID, res)
	}
}

// TestRRFLexicalWeightFavorsLexical is the mirror: a high LexicalWeight makes
// the lexical-only match (X) outrank the vector-only match (Y).
func TestRRFLexicalWeightFavorsLexical(t *testing.T) {
	s, err := Open(":memory:", WithFTS(), WithRRF(RRFConfig{K: 60, VectorWeight: 1, LexicalWeight: 10}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.Embedder = fakeEmbed{dim: 4}
	_, xID, _ := rrfChunkSetup(t, s)

	res, err := s.Search(context.Background(), "database", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1: %v", len(res), res)
	}
	if res[0].ID != xID {
		t.Errorf("LexicalWeight=10 picked %d (Y, vector-only), want %d (X, lexical-only); weights not tilting toward lexical: %v", res[0].ID, xID, res)
	}
}

// TestRRFKControlsSteepness proves K controls rank-damping steepness: with a
// lexical-only fusion (VectorWeight=0) over two lexical-matching chunks, a
// smaller K makes the rank-0 chunk's score a LARGER multiple of rank-1's.
// Standard RRF: rank0/rank1 ratio = (K+1)/K, which grows as K shrinks.
func TestRRFKControlsSteepness(t *testing.T) {
	ctx := context.Background()
	// Two chunks both containing "alpha"; bm25 ranks the shorter doc rank 0.
	mk := func(k float64) *Store {
		t.Helper()
		s, err := Open(":memory:", WithFTS(), WithRRF(RRFConfig{K: k, VectorWeight: 0, LexicalWeight: 1}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		s.Embedder = fakeEmbed{dim: 4}
		if _, err := s.Write(ctx, "alpha", "doc"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Write(ctx, "alpha beta gamma delta epsilon", "doc"); err != nil {
			t.Fatal(err)
		}
		return s
	}
	resHi := mustSearch(t, mk(60), "alpha", 2)   // K=60: gentle damping
	resLo := mustSearch(t, mk(1), "alpha", 2)    // K=1:  steep damping
	if len(resHi) < 2 || len(resLo) < 2 {
		t.Fatalf("need 2 results to compare ranks; hi=%d lo=%d", len(resHi), len(resLo))
	}
	// rank0 is res[0], rank1 is res[1] in both (bm25 orders identically).
	ratioHi := resHi[0].Score / resHi[1].Score
	ratioLo := resLo[0].Score / resLo[1].Score
	if !(ratioLo > ratioHi) {
		t.Errorf("lower K should steepen (rank0 dominates more): ratio(K=1)=%.4f should exceed ratio(K=60)=%.4f", ratioLo, ratioHi)
	}
	// Spot-check the formula: ratio at K is exactly (K+1)/K = 1 + 1/K.
	if math.Abs(ratioHi-(61.0/60.0)) > 1e-9 {
		t.Errorf("K=60 ratio = %.6f, want exactly 61/60 = %.6f", ratioHi, 61.0/60.0)
	}
	if math.Abs(ratioLo-2.0) > 1e-9 {
		t.Errorf("K=1 ratio = %.6f, want exactly 2.0", ratioLo)
	}
}

// mustSearch is a small helper for the K-steepness test.
func mustSearch(t *testing.T, s *Store, query string, k int) []Result {
	t.Helper()
	res, err := s.Search(context.Background(), query, k)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	return res
}
