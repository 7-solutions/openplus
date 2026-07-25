package memory

import (
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
