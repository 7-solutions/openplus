package memory

import (
	"context"
	"testing"

	"github.com/7solutions/openplus/internal/embed"
)

func newTestStore(t *testing.T, dim int) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.Embedder = fakeEmbed{dim: dim}
	return s
}

// fakeEmbed is a deterministic embed.Embedder for store tests (no HTTP).
// post-0020 (no FTS5): it produces vectors where the dominant signal is
// the per-word "first-character bucket" — the dim-i entry of a text's
// vector is the number of words starting with the (i mod 26)-th letter
// of the alphabet, scaled. This makes the "rust" query (single word
// starting with 'r' = bucket 17) rank rust-chunks higher than
// non-rust chunks deterministically without a lexical index.
type fakeEmbed struct{ dim int }

func (f fakeEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, tx := range texts {
		v := make([]float32, f.dim)
		for _, w := range words(tx) {
			if w == "" {
				continue
			}
			bucket := int(w[0]) % f.dim
			v[bucket] += 1.0
		}
		out[i] = v
	}
	return out, nil
}

// words splits on whitespace, lowercased, stripped of trivial punctuation
// for the embedding's word-hash.
func words(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n':
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		case r >= 'A' && r <= 'Z':
			cur += string(r + ('a' - 'A'))
		default:
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
func (f fakeEmbed) Dim() int { return f.dim }

// compile-time: fakeEmbed satisfies embed.Embedder.
var _ embed.Embedder = fakeEmbed{}

func TestStoreWriteCreatesSchema(t *testing.T) {
	s := newTestStore(t, 4)
	if _, err := s.Write(context.Background(), "hello world", "test"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// post-0020 schema: only the `chunks` table. The vector column lives
	// on it (no separate chunks_vec virtual table). FTS5 is deferred
	// (Turso v0.2.2 doesn't ship the fts5 module).
	for _, tbl := range []string{"chunks"} {
		var name string
		err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type IN ('table','view') AND name=?`, tbl).Scan(&name)
		if err != nil || name != tbl {
			t.Errorf("table %q not created: %v", tbl, err)
		}
	}
}

func TestStoreWritePersistsChunk(t *testing.T) {
	s := newTestStore(t, 4)
	id, err := s.Write(context.Background(), "the quick brown fox", "doc1")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id <= 0 {
		t.Fatalf("id = %d", id)
	}
	var text, source string
	if err := s.DB().QueryRow(`SELECT text, source FROM chunks WHERE id=?`, id).Scan(&text, &source); err != nil {
		t.Fatalf("query: %v", err)
	}
	if text != "the quick brown fox" || source != "doc1" {
		t.Fatalf("row = (%q,%q)", text, source)
	}
}

func TestStoreWriteStoresEmbedding(t *testing.T) {
	s := newTestStore(t, 4)
	if _, err := s.Write(context.Background(), "alpha", "d"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// post-0020: the embedding is a BLOB column on the chunks table.
	// Verify the row count is 1 (single write) and the embedding column
	// is non-null.
	var n int
	if err := s.DB().QueryRow(`SELECT count(*) FROM chunks`).Scan(&n); err != nil {
		t.Fatalf("chunks count: %v", err)
	}
	if n != 1 {
		t.Errorf("chunks count = %d, want 1", n)
	}
	var emb []byte
	if err := s.DB().QueryRow(`SELECT embedding FROM chunks LIMIT 1`).Scan(&emb); err != nil {
		t.Fatalf("embedding read: %v", err)
	}
	if len(emb) == 0 {
		t.Error("embedding column empty")
	}
}

func TestStoreWriteErrorsWithoutEmbedder(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	if _, err := s.Write(context.Background(), "x", "y"); err == nil {
		t.Fatal("want error when Embedder is nil")
	}
}

func TestStoreWriteMultipleBuilds(t *testing.T) {
	s := newTestStore(t, 4)
	for _, tx := range []string{"one", "two", "three"} {
		if _, err := s.Write(context.Background(), tx, "d"); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	var n int
	s.DB().QueryRow(`SELECT count(*) FROM chunks`).Scan(&n)
	if n != 3 {
		t.Fatalf("chunks count = %d, want 3", n)
	}
}
