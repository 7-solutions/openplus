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
type fakeEmbed struct{ dim int }

func (f fakeEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, tx := range texts {
		v := make([]float32, f.dim)
		for j := range v {
			v[j] = float32(len(tx))*0.1 + float32(j)*0.01 + float32(i)
		}
		out[i] = v
	}
	return out, nil
}
func (f fakeEmbed) Dim() int { return f.dim }

// compile-time: fakeEmbed satisfies embed.Embedder.
var _ embed.Embedder = fakeEmbed{}

func TestStoreWriteCreatesSchema(t *testing.T) {
	s := newTestStore(t, 4)
	if _, err := s.Write(context.Background(), "hello world", "test"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for _, tbl := range []string{"chunks", "chunks_fts", "chunks_vec"} {
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

func TestStoreWriteIndexesFTSAndVec(t *testing.T) {
	s := newTestStore(t, 4)
	if _, err := s.Write(context.Background(), "alpha", "d"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// FTS index has the row
	var n int
	if err := s.DB().QueryRow(`SELECT count(*) FROM chunks_fts`).Scan(&n); err != nil {
		t.Fatalf("fts count: %v", err)
	}
	if n != 1 {
		t.Errorf("chunks_fts count = %d, want 1", n)
	}
	// vec0 has the row
	if err := s.DB().QueryRow(`SELECT count(*) FROM chunks_vec`).Scan(&n); err != nil {
		t.Fatalf("vec count: %v", err)
	}
	if n != 1 {
		t.Errorf("chunks_vec count = %d, want 1", n)
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
