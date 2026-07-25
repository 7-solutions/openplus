package memory

import (
	"testing"
)

// TestDefaultRRF pins the default fusion config: the change-0021
// equal-weight behavior (K=60, both weights 1.0). Any change to these
// defaults is a behavior change that must update the hybrid-search golden
// tests and the MEMORY.md ADR-0014 note.
func TestDefaultRRF(t *testing.T) {
	got := DefaultRRF()
	want := RRFConfig{K: 60.0, VectorWeight: 1.0, LexicalWeight: 1.0}
	if got != want {
		t.Errorf("DefaultRRF() = %+v, want %+v", got, want)
	}
	// Sanity: the default is internally consistent — non-zero K and weights.
	if got.K <= 0 || got.VectorWeight < 0 || got.LexicalWeight < 0 {
		t.Errorf("DefaultRRF() has non-physical values: %+v", got)
	}
}

// TestOpenAppliesRRFDefault verifies Open initializes the store to the
// default RRF config even when no WithRRF option is passed. This is the
// backward-compatibility guarantee: Open(path) and Open(path, WithFTS())
// behave identically to pre-0024 because the fusion runs on {60,1,1}.
func TestOpenAppliesRRFDefault(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.rrf != DefaultRRF() {
		t.Errorf("Open without WithRRF left rrf = %+v, want DefaultRRF() = %+v", s.rrf, DefaultRRF())
	}
}

// TestWithRRFOverrides verifies the WithRRF option replaces the default
// config wholesale (no per-field zero-value magic: the caller's config is
// used as-is, including zero fields if they explicitly chose them).
func TestWithRRFOverrides(t *testing.T) {
	cfg := RRFConfig{K: 30.0, VectorWeight: 2.5, LexicalWeight: 0.5}
	s, err := Open(":memory:", WithRRF(cfg))
	if err != nil {
		t.Fatalf("Open WithRRF: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.rrf != cfg {
		t.Errorf("WithRRF did not store the config: got %+v, want %+v", s.rrf, cfg)
	}
}

// TestWithRRFZeroIsExplicit documents the no-magic contract: a caller who
// passes WithRRF(RRFConfig{}) gets {0,0,0} — that is their explicit choice
// (it zeroes out the fusion). The default comes from DefaultRRF(), set by
// Open before options; WithRRF does not fill zeroes. This keeps weight
// semantics unambiguous (0 means "disable this half", not "use default").
func TestWithRRFZeroIsExplicit(t *testing.T) {
	s, err := Open(":memory:", WithRRF(RRFConfig{}))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.rrf != (RRFConfig{}) {
		t.Errorf("WithRRF(RRFConfig{}) should store the zero value as-is; got %+v", s.rrf)
	}
}
