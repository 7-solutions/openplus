// Leak guard (T-1808). No file outside internal/provider/ and internal/ports/
// may import github.com/7-solutions/openplus/internal/provider — the adapter
// package is adapter-only after change 0018; core depends on internal/ports.
// This file is a Go test rather than a build tag, so a violation fails the
// go test ./... gate instead of producing a less-localized build error.
package ports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoCoreImportsProviderPackage(t *testing.T) {
	root := findModuleRoot(t)
	violations := []string{}

	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip the adapter pkg itself; it is allowed to re-export.
			if strings.HasPrefix(path, filepath.Join(root, "internal/provider")) {
				return filepath.SkipDir
			}
			// The ports package itself may grow package-internal helpers.
			if strings.HasPrefix(path, filepath.Join(root, "internal/ports")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// _test.go is allowed to import portsfake (test seam at the port)
		// but not the adapter package.
		b, _ := os.ReadFile(path)
		for line := range strings.SplitSeq(string(b), "\n") {
			if importsAdapterPackage(line) {
				violations = append(violations, path)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("core packages must not import internal/provider (use internal/ports instead):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// cmd/ may still wire concrete adapters (it is the wiring layer), so we walk it
// separately. The only allowed form is the prefix-select package
// (internal/provider/select) which is the adapter registry; it must NOT depend
// on Anthropic or OpenAI-compat directly.
func TestCmdUsesOnlySelectAdapter(t *testing.T) {
	root := findModuleRoot(t)
	allowed := map[string]bool{
		"internal/provider/select": true,
		// core test wiring helper (assemble.go / assemble_test.go) does not
		// exist in cmd today, but if a future wiring helper does, route it
		// through Select, not direct adapters.
	}

	err := filepath.Walk(filepath.Join(root, "cmd"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, _ := os.ReadFile(path)
		for line := range strings.SplitSeq(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "\"github.com/7-solutions/openplus/internal/provider") {
				continue
			}
			if trimmed == "\"github.com/7-solutions/openplus/internal/provider\"" {
				t.Fatalf("cmd must not import the adapter package directly: %s", path)
			}
			// internal/provider/<sub>/ — extract sub
			rest := strings.TrimPrefix(trimmed, "\"github.com/7-solutions/openplus/")
			end := strings.Index(rest, "\"")
			if end < 0 {
				continue
			}
			sub := rest[:end]
			if !allowed[sub] {
				t.Errorf("cmd/%s imports %s, which is not in the allowed adapter registry: %s",
					filepath.Base(filepath.Dir(path)), sub, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// adapterImportPath is the package core may not import.
const adapterImportPath = `"github.com/7-solutions/openplus/internal/provider"`

// importsAdapterPackage reports whether one source line imports the adapter
// package in any of Go's import forms:
//
//	"…/internal/provider"       plain
//	_ "…/internal/provider"     blank, for side effects
//	p "…/internal/provider"     aliased
//	. "…/internal/provider"     dot
//
// Matching only the plain form left a hole: a blank import pulls the adapter
// and its transitive dependencies into a core package just as surely, and it is
// exactly what someone reaches for to register a driver. A comment mentioning
// the path is not an import, so the quoted path must end the line.
func importsAdapterPackage(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "//") {
		return false
	}
	if !strings.HasSuffix(trimmed, adapterImportPath) {
		return false
	}
	// Whatever precedes the quoted path must be an import prefix, not code.
	prefix := strings.TrimSpace(strings.TrimSuffix(trimmed, adapterImportPath))
	switch prefix {
	case "", "_", ".":
		return true
	}
	// An alias: a single identifier.
	for i, r := range prefix {
		isLetter := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		if !isLetter && !(i > 0 && isDigit) {
			return false
		}
	}
	return true
}

// findModuleRoot returns the directory containing go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod")
		}
		dir = parent
	}
}
