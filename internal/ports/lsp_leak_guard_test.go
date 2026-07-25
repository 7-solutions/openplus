// LSP leak guard (T-2622, ADR-0017). The LSP wire protocol is confined to
// internal/lsp/. No go.lsp.dev type may reach internal/ports/, and no package
// outside internal/lsp/ may import one.
//
// This is the provider-neutrality hard rule (ADR-0005) applied to a second wire
// protocol: the core must not learn LSP any more than it learns the Anthropic
// wire. Like internal/ports/leak_guard_test.go, it is a Go test rather than a
// build tag, so a violation fails `go test ./...` with a localized message
// naming the offending file.
package ports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lspWirePrefix is the module path of the LSP protocol libraries.
const lspWirePrefix = "go.lsp.dev/"

// lspAdapterDir is the only package allowed to speak the LSP wire protocol.
const lspAdapterDir = "internal/lsp"

// TestPortsDeclareNoLSPWireType fails if the ports package references an LSP
// wire type. The port surface must be expressed entirely in neutral types
// (ports.Diagnostic, ports.Location, ports.Symbol).
func TestPortsDeclareNoLSPWireType(t *testing.T) {
	root := findModuleRoot(t)
	dir := filepath.Join(root, "internal/ports")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		// This guard names the forbidden import in its own source; skip it so
		// the guard does not flag itself.
		if e.Name() == "lsp_leak_guard_test.go" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), lspWirePrefix) {
			t.Errorf("internal/ports/%s references %s — the port surface must use "+
				"neutral types only (ports.Diagnostic, ports.Location, ports.Symbol); "+
				"convert at the boundary in internal/lsp/ (ADR-0017)",
				e.Name(), lspWirePrefix)
		}
	}
}

// TestOnlyLSPAdapterImportsLSPWire fails if any package outside internal/lsp/
// imports an LSP protocol library. The adapter is the containment boundary.
func TestOnlyLSPAdapterImportsLSPWire(t *testing.T) {
	root := findModuleRoot(t)
	var violations []string

	for _, top := range []string{"internal", "cmd"} {
		base := filepath.Join(root, top)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if strings.HasPrefix(path, filepath.Join(root, lspAdapterDir)) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "lsp_leak_guard_test.go") {
				return nil
			}

			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for line := range strings.SplitSeq(string(b), "\n") {
				trimmed := strings.TrimSpace(line)
				// Match an import line, not a mention in a comment.
				if !strings.HasPrefix(trimmed, `"`+lspWirePrefix) &&
					!strings.Contains(trimmed, `"`+lspWirePrefix) {
					continue
				}
				if strings.HasPrefix(trimmed, "//") {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				violations = append(violations, rel)
				return nil
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(violations) > 0 {
		t.Fatalf("only %s/ may import %s — these packages must depend on the "+
			"LanguageService port instead (ADR-0017):\n  %s",
			lspAdapterDir, lspWirePrefix, strings.Join(violations, "\n  "))
	}
}
