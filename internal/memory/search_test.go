package memory

import (
	"context"
	"strings"
	"testing"
)

func writeAll(t *testing.T, s *Store, texts ...string) {
	t.Helper()
	for _, tx := range texts {
		if _, err := s.Write(context.Background(), tx, "doc"); err != nil {
			t.Fatalf("Write %q: %v", tx, err)
		}
	}
}

func TestSearchEmptyBeforeWrite(t *testing.T) {
	s := newTestStore(t, 4)
	res, err := s.Search(context.Background(), "anything", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("expected no results before any write, got %v", res)
	}
}

func TestSearchGoldenRanking(t *testing.T) {
	s := newTestStore(t, 4)
	writeAll(t, s,
		"the rust ownership model moves values",
		"python is a scripting language",
		"rust borrowing prevents data races",
		"gardening tips for tomatoes",
	)
	res, err := s.Search(context.Background(), "rust", 4)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("no results")
	}
	// Vector-only baseline (change 0020 default): the two rust chunks
	// outrank the non-rust chunks because fakeEmbed's per-first-letter
	// buckets make the 'r' of "rust" a vector signal. This is the
	// backward-compat proof — Search still works without the shadow.
	// The hybrid variant below (TestSearchHybridGoldenRanking) makes the
	// same assertion backed by lexical bm25 signal instead.
	tops := []string{res[0].Text, res[1].Text}
	for _, top := range tops {
		if !strings.Contains(top, "rust") {
			t.Errorf("non-rust chunk ranked top-2: %q; full=%v", top, texts(res))
		}
	}
	// non-rust chunks (no lexical signal) rank below the rust chunks.
	for _, below := range res[2:] {
		if strings.Contains(below.Text, "rust") {
			t.Errorf("rust chunk ranked below non-rust: %q; full=%v", below.Text, texts(res))
		}
	}
}

// newTestStoreFTS is the hybrid counterpart of newTestStore: an in-memory
// store with the FTS5 lexical shadow index enabled (change 0021).
func newTestStoreFTS(t *testing.T, dim int) *Store {
	t.Helper()
	s, err := Open(":memory:", WithFTS())
	if err != nil {
		t.Fatalf("Open WithFTS: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.Embedder = fakeEmbed{dim: dim}
	return s
}

// TestSearchHybridGoldenRanking is the change 0021 hybrid golden test.
// With the FTS shadow enabled, the two rust chunks outrank the non-rust
// chunks on the "rust" query — now backed by lexical bm25 signal, not
// just fakeEmbed's vector coincidence. This is the restoration of the
// pre-0020 hybrid contract.
func TestSearchHybridGoldenRanking(t *testing.T) {
	s := newTestStoreFTS(t, 4)
	writeAll(t, s,
		"the rust ownership model moves values",
		"python is a scripting language",
		"rust borrowing prevents data races",
		"gardening tips for tomatoes",
	)
	res, err := s.Search(context.Background(), "rust", 4)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) < 2 {
		t.Fatalf("got %d results, want >= 2", len(res))
	}
	tops := []string{res[0].Text, res[1].Text}
	for _, top := range tops {
		if !strings.Contains(top, "rust") {
			t.Errorf("non-rust chunk ranked top-2 under hybrid: %q; full=%v", top, texts(res))
		}
	}
	// A non-rust chunk must NOT outrank a rust chunk: the lexical boost
	// gives both rust chunks an FTS rank contribution the non-rust chunks
	// never receive.
	for _, below := range res[2:] {
		if strings.Contains(below.Text, "rust") {
			t.Errorf("rust chunk ranked below non-rust under hybrid: %q; full=%v", below.Text, texts(res))
		}
	}
}

// TestSearchHybridLexicalOnlyMatch proves FTS surfaces a chunk that the
// vector half ranks outside its top-k. With k small, a keyword match the
// vector misses can still appear because RRF unions the two ranked lists.
func TestSearchHybridLexicalOnlyMatch(t *testing.T) {
	s := newTestStoreFTS(t, 4)
	// Chunk "tax accounting" shares no first-letter bucket overlap with
	// a "gardening" query, so the vector half ranks it low — but it is
	// the ONLY chunk containing "tax", so FTS ranks it #1.
	writeAll(t, s,
		"gardening tips for tomatoes",
		"tax accounting fundamentals",
		"python scripting basics",
		"gardening tools and soil",
	)
	res, err := s.Search(context.Background(), "tax", 4)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("no results; the lexical match should surface the tax chunk")
	}
	if !strings.Contains(res[0].Text, "tax") {
		t.Errorf("top result is %q, want the 'tax' chunk (FTS must surface the lone lexical match): %v", res[0].Text, texts(res))
	}
}

func TestSearchRespectsTopK(t *testing.T) {
	s := newTestStore(t, 4)
	writeAll(t, s, "a a", "b b", "c c", "d d", "e e")
	res, err := s.Search(context.Background(), "a", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) > 3 {
		t.Errorf("got %d results, want <= 3", len(res))
	}
}

func TestSearchResultShape(t *testing.T) {
	s := newTestStore(t, 4)
	writeAll(t, s, "alpha beta gamma")
	res, err := s.Search(context.Background(), "alpha", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("results = %v", res)
	}
	r := res[0]
	if r.ID <= 0 || r.Text != "alpha beta gamma" || r.Source != "doc" {
		t.Fatalf("result shape wrong: %+v", r)
	}
	if r.Score <= 0 {
		t.Errorf("score = %v, want > 0", r.Score)
	}
}

// texts helper for assertion messages.
func texts(r []Result) []string {
	out := make([]string, len(r))
	for i, x := range r {
		out[i] = x.Text
	}
	return out
}
