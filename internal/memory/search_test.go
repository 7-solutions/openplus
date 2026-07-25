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
	// The two rust chunks must outrank the non-rust chunks (FTS dominates RRF).
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
