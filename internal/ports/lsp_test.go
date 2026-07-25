package ports

import (
	"context"
	"slices"
	"testing"
)

// TestLanguageServiceIsDeclared: the eleventh port is in the canonical list.
func TestLanguageServiceIsDeclared(t *testing.T) {
	names := PortNames()
	if !slices.Contains(names, "LanguageService") {
		t.Fatalf("PortNames() = %v, missing LanguageService", names)
	}
}

// TestFakeLanguageServiceReturnsCannedDiagnostics: the fake is usable as a test
// seam without any subprocess.
func TestFakeLanguageServiceReturnsCannedDiagnostics(t *testing.T) {
	want := []Diagnostic{{
		Path:     "main.go",
		Line:     10,
		Column:   2,
		Severity: SeverityError,
		Message:  "undefined: foo",
		Source:   "compiler",
	}}
	var ls LanguageService = FakeLanguageService{Diags: want}

	got, err := ls.Diagnostics(context.Background(), "main.go")
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1", len(got))
	}
	if got[0].Message != "undefined: foo" || got[0].Severity != SeverityError {
		t.Errorf("diagnostic = %+v, want the canned error", got[0])
	}
}

// TestFakeLanguageServiceOtherSurfaces: hover/definition/symbols/references are
// wired and return the fake's canned values.
func TestFakeLanguageServiceOtherSurfaces(t *testing.T) {
	loc := Location{Path: "other.go", Line: 3, Column: 5}
	sym := Symbol{Name: "Foo", Kind: "func", Path: "main.go", Line: 7}
	var ls LanguageService = FakeLanguageService{
		HoverText: "func Foo()",
		Locs:      []Location{loc},
		Syms:      []Symbol{sym},
	}
	ctx := context.Background()

	if got, err := ls.Hover(ctx, "main.go", 7, 1); err != nil || got != "func Foo()" {
		t.Errorf("Hover = %q, %v; want the canned text", got, err)
	}
	if got, err := ls.Definition(ctx, "main.go", 7, 1); err != nil || len(got) != 1 || got[0] != loc {
		t.Errorf("Definition = %v, %v; want the canned location", got, err)
	}
	if got, err := ls.References(ctx, "main.go", 7, 1); err != nil || len(got) != 1 {
		t.Errorf("References = %v, %v; want the canned location", got, err)
	}
	if got, err := ls.DocumentSymbols(ctx, "main.go"); err != nil || len(got) != 1 || got[0].Name != "Foo" {
		t.Errorf("DocumentSymbols = %v, %v; want the canned symbol", got, err)
	}
	if err := ls.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
