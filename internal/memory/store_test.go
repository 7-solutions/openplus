package memory

import (
	"context"
	"testing"
)

// TestOpenAndVecVersion is the T-040 acceptance test: the store opens on the
// cgo-free ncruces driver with sqlite-vec embedded, and vec_version() resolves
// (proving the vector extension is loaded into the same DB).
func TestOpenAndVecVersion(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	v, err := s.VecVersion()
	if err != nil {
		t.Fatalf("vec_version: %v", err)
	}
	if v == "" {
		t.Fatal("vec_version() returned empty")
	}
	t.Logf("vec_version=%s", v)
}

func TestOpenFilePersists(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir + "/mem.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// reopening the same file must succeed (valid SQLite file written).
	s2, err := Open(dir + "/mem.db")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = s2.Close()
}

// --- Change 0004 / T-415: Memory.MaxEntries cap with oldest-first pruning ---

// localFakeEmbedder returns a fixed-dim vector regardless of input. Keeps
// the cap test independent of any real embedding endpoint.
type localFakeEmbedder struct{ dim int }

func (f localFakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		v := make([]float32, f.dim)
		out[i] = v
	}
	return out, nil
}
func (f localFakeEmbedder) Dim() int { return f.dim }

// TestStoreMaxEntriesZeroIsUnbounded pins the zero-cost path: with no cap
// set, every write sticks. (Regression guard — already true today.)
func TestStoreMaxEntriesZeroIsUnbounded(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.Embedder = localFakeEmbedder{dim: 4}

	for i := 0; i < 5; i++ {
		if _, err := s.Write(context.Background(), "chunk", "test"); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 5 {
		t.Fatalf("count = %d, want 5 (no cap set)", n)
	}
}

// TestStoreMaxEntriesCapEvictsOldest writes 5 chunks with cap=2 and asserts
// exactly 2 remain, the 2 most recent. RED before T-416 (cap is not
// enforced today — all 5 stay).
func TestStoreMaxEntriesCapEvictsOldest(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.Embedder = localFakeEmbedder{dim: 4}
	s.SetMaxEntries(2)

	for i := 0; i < 5; i++ {
		if _, err := s.Write(context.Background(), "chunk", "test"); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2 (cap=2)", n)
	}
	// the 2 retained must be the most recent (highest ids).
	rows, err := s.DB().Query(`SELECT id FROM chunks ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v", ids)
	}
	// ids 4 and 5 should remain (ids 1, 2, 3 evicted). Asserting the
	// monotonic sequence is enough.
	if ids[0] != 4 || ids[1] != 5 {
		t.Errorf("retained ids = %v, want [4 5]", ids)
	}
}

// TestStoreMaxEntriesPrunesIndexes: prune must also delete from chunks_fts
// and chunks_vec, otherwise Search returns phantom rowids (ids that no
// longer exist in chunks but still score in the index). Without this,
// the cap silently leaks index bloat and pollutes retrieval.
func TestStoreMaxEntriesPrunesIndexes(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.Embedder = localFakeEmbedder{dim: 4}
	s.SetMaxEntries(2)

	for i := 0; i < 5; i++ {
		if _, err := s.Write(context.Background(), "chunk", "test"); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	for _, tbl := range []string{"chunks", "chunks_fts", "chunks_vec"} {
		var n int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 2 {
			t.Errorf("%s count = %d, want 2 (cap must prune all three tables)", tbl, n)
		}
	}
}
