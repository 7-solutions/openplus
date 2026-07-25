package symbols

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleGo = `package auth

type Config struct {
	Field int
}

func Login(user string) error {
	return nil
}

func (c *Config) Validate() error {
	return nil
}

var X = 1
`

func writeGoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// --- T-1300: IndexFile ---

func TestIndexFileFindsFuncsMethodsTypes(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "auth.go", sampleGo)

	got, err := IndexFile(filepath.Join(root, "auth.go"))
	if err != nil {
		t.Fatalf("IndexFile: %v", err)
	}
	byName := map[string]Symbol{}
	for _, s := range got {
		byName[s.Name] = s
	}
	for _, want := range []string{"Login", "Config.Validate", "Config"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing %q in %+v", want, got)
		}
	}
}

func TestIndexFileMethodIsDistinguishedFromFunc(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "f.go", `package p
func F() {}
func (T) F() {}
`)
	got, err := IndexFile(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range got {
		names[s.Name] = true
	}
	if !names["F"] || !names["T.F"] {
		t.Errorf("expected both F and T.F, got %+v", got)
	}
}

func TestIndexFileCarriesLineRange(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "f.go", "package p\n\nfunc A() {\n\t// body\n}\n")
	got, err := IndexFile(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	s := got[0]
	if s.StartLine != 3 || s.EndLine != 5 {
		t.Errorf("A line range = %d-%d, want 3-5", s.StartLine, s.EndLine)
	}
}

func TestIndexFileUnparseableReportsFile(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "bad.go")
	writeGoFile(t, root, "bad.go", "this is not go at all {")
	_, err := IndexFile(p)
	if err == nil {
		t.Fatal("expected an error for unparseable source")
	}
	if !strings.Contains(err.Error(), "bad.go") {
		t.Errorf("error should name the file: %v", err)
	}
}

func TestIndexFileLineRangeForType(t *testing.T) {
	root := t.TempDir()
	src := "package p\n\ntype T struct {\n\tx int\n}\n"
	writeGoFile(t, root, "t.go", src)
	got, _ := IndexFile(filepath.Join(root, "t.go"))
	if len(got) != 1 || got[0].Kind != KindType || got[0].StartLine != 3 || got[0].EndLine != 5 {
		t.Fatalf("type line range wrong: %+v", got)
	}
}

// --- T-1301: IndexDir, Ref, Parse ---

func TestIndexDirBuildsMap(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "a.go", "package p\nfunc A() {}\n")
	writeGoFile(t, root, "sub/b.go", "package p\nfunc B() {}\n")

	idx, err := IndexDir(root)
	if err != nil {
		t.Fatalf("IndexDir: %v", err)
	}
	// keyed by the path relative to root
	hasA := false
	hasB := false
	for _, syms := range idx {
		for _, s := range syms {
			if s.Name == "A" {
				hasA = true
			}
			if s.Name == "B" {
				hasB = true
			}
		}
	}
	if !hasA || !hasB {
		t.Errorf("IndexDir missed a symbol: %+v", idx)
	}
}

func TestIndexDirSkipsVendorTestdataGit(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "real.go", "package p\nfunc Real() {}\n")
	writeGoFile(t, root, "vendor/v.go", "package p\nfunc Vendored() {}\n")
	writeGoFile(t, root, "testdata/t.go", "package p\nfunc Tested() {}\n")
	writeGoFile(t, root, ".git/g.go", "package p\nfunc Hidden() {}\n")

	idx, _ := IndexDir(root)
	for _, syms := range idx {
		for _, s := range syms {
			if strings.HasPrefix(s.Name, "Vendored") || strings.HasPrefix(s.Name, "Tested") || strings.HasPrefix(s.Name, "Hidden") {
				t.Errorf("should have skipped this symbol: %+v", s)
			}
		}
	}
}

func TestRefRendersAndParses(t *testing.T) {
	ref := Ref("path/to/auth.go", "Config.Validate")
	if ref != "path/to/auth.go::Config.Validate" {
		t.Fatalf("Ref = %q", ref)
	}
	file, name, ok := Parse(ref)
	if !ok || file != "path/to/auth.go" || name != "Config.Validate" {
		t.Errorf("Parse(%q) = (%q,%q,%v)", ref, file, name, ok)
	}
}

func TestParseMalformedReturnsFalse(t *testing.T) {
	for _, bad := range []string{"noseparator", "::", "f.go::", "::Name"} {
		if _, _, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

// --- T-1302: Exists ---

func TestExistsTrue(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "auth.go", sampleGo)
	if ok, err := Exists(root, "auth.go::Login"); err != nil || !ok {
		t.Fatalf("Login should exist: ok=%v err=%v", ok, err)
	}
}

func TestExistsFalse(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "auth.go", sampleGo)
	if ok, _ := Exists(root, "auth.go::NoSuchThing"); ok {
		t.Fatal("a nonexistent symbol reported as existing")
	}
}

// TestExistsNonGoErrorsPointsAtGrit is the spec scenario: a Go-only parser must
// not silently grant a lock on a file it cannot read.
func TestExistsNonGoErrorsPointsAtGrit(t *testing.T) {
	root := t.TempDir()
	ok, err := Exists(root, "app.ts::render")
	if ok {
		t.Fatal("a non-Go ref must not exist")
	}
	if err == nil {
		t.Fatal("expected an error for a non-Go ref")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "go") {
		t.Errorf("error should mention Go: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "grit") {
		t.Errorf("error should point at grit for other languages: %v", err)
	}
}

func TestExistsMalformedRef(t *testing.T) {
	ok, err := Exists(t.TempDir(), "noseparator")
	if ok || err == nil {
		t.Fatalf("ok=%v err=%v, want false + error", ok, err)
	}
}
