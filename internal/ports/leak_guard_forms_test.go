package ports

import "testing"

// TestImportsAdapterPackageCatchesEveryForm locks the leak guard's matcher.
//
// The original matcher only recognized a plain import, so
//
//	_ "github.com/7-solutions/openplus/internal/provider"
//
// in a core package passed the build. A blank import pulls in the adapter and
// everything behind it exactly like a plain one, and it is the form someone
// reaches for to register a driver — the most likely way the rule would have
// been broken in practice.
func TestImportsAdapterPackageCatchesEveryForm(t *testing.T) {
	const p = `"github.com/7-solutions/openplus/internal/provider"`

	violations := []struct {
		name string
		line string
	}{
		{"plain", "\t" + p},
		{"blank", "\t_ " + p},
		{"aliased", "\tprov " + p},
		{"dot", "\t. " + p},
		{"no indent", p},
		{"trailing space", "\t_ " + p + "  "},
	}
	for _, tc := range violations {
		t.Run(tc.name, func(t *testing.T) {
			if !importsAdapterPackage(tc.line) {
				t.Errorf("missed a violation: %q", tc.line)
			}
		})
	}

	allowed := []struct {
		name string
		line string
	}{
		{"subpackage is fine", "\t\"github.com/7-solutions/openplus/internal/provider/anthropic\""},
		{"ports import", "\t\"github.com/7-solutions/openplus/internal/ports\""},
		{"commented out", "\t// " + p},
		{"prose mentioning the path", "// no core package may import " + p + " — see ADR-0005"},
		{"unrelated", "\t\"strings\""},
		{"empty", ""},
	}
	for _, tc := range allowed {
		t.Run(tc.name, func(t *testing.T) {
			if importsAdapterPackage(tc.line) {
				t.Errorf("false positive on: %q", tc.line)
			}
		})
	}
}
