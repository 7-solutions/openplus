package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/7solutions/openplus/internal/ports"
)

func fakeLS() ports.LanguageService {
	return ports.FakeLanguageService{
		Diags: []ports.Diagnostic{
			{Path: "main.go", Line: 10, Column: 2, Severity: ports.SeverityError,
				Message: "undefined: foo", Source: "compiler"},
			{Path: "main.go", Line: 12, Column: 5, Severity: ports.SeverityWarning,
				Message: "unused variable x"},
		},
		HoverText: "func Foo() error",
		Locs:      []ports.Location{{Path: "other.go", Line: 3, Column: 5}},
		Syms:      []ports.Symbol{{Name: "Foo", Kind: "func", Path: "main.go", Line: 7}},
	}
}

// TestLSPToolsHaveValidMetadata: every tool must present a usable name,
// description, and JSON-Schema object to the model.
func TestLSPToolsHaveValidMetadata(t *testing.T) {
	tools := LSPTools(fakeLS())
	if len(tools) != 5 {
		t.Fatalf("LSPTools returned %d tools, want 5", len(tools))
	}

	want := map[string]bool{
		"lsp_diagnostics": true, "lsp_hover": true, "lsp_definition": true,
		"lsp_symbols": true, "lsp_references": true,
	}
	for _, tl := range tools {
		if !want[tl.Name()] {
			t.Errorf("unexpected tool %q", tl.Name())
		}
		delete(want, tl.Name())

		if tl.Description() == "" {
			t.Errorf("%s: empty description", tl.Name())
		}
		var schema map[string]any
		if err := json.Unmarshal(tl.Schema(), &schema); err != nil {
			t.Errorf("%s: schema is not valid JSON: %v", tl.Name(), err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("%s: schema type = %v, want object", tl.Name(), schema["type"])
		}
	}
	for n := range want {
		t.Errorf("missing tool %q", n)
	}
}

func toolNamed(t *testing.T, name string) Tool {
	t.Helper()
	for _, tl := range LSPTools(fakeLS()) {
		if tl.Name() == name {
			return tl
		}
	}
	t.Fatalf("no tool named %q", name)
	return nil
}

func TestDiagnosticsToolRendersSeverityAndPosition(t *testing.T) {
	out, err := toolNamed(t, "lsp_diagnostics").
		Execute(context.Background(), json.RawMessage(`{"path":"main.go"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"main.go:10:2", "error", "undefined: foo", "warning", "unused variable x"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestDiagnosticsToolCleanFileIsExplicit: "no diagnostics" must be a statement,
// not an empty string the model has to interpret.
func TestDiagnosticsToolCleanFileIsExplicit(t *testing.T) {
	tl := LSPDiagnostics{LS: ports.FakeLanguageService{}}
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"path":"clean.go"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("a clean file produced empty output; want an explicit statement")
	}
	if !strings.Contains(strings.ToLower(out), "no diagnostics") {
		t.Errorf("output = %q, want it to say there are no diagnostics", out)
	}
}

func TestHoverTool(t *testing.T) {
	out, err := toolNamed(t, "lsp_hover").
		Execute(context.Background(), json.RawMessage(`{"path":"main.go","line":7,"column":1}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "func Foo() error") {
		t.Errorf("output = %q, want the hover text", out)
	}
}

func TestDefinitionAndReferencesRenderLocations(t *testing.T) {
	for _, name := range []string{"lsp_definition", "lsp_references"} {
		t.Run(name, func(t *testing.T) {
			out, err := toolNamed(t, name).
				Execute(context.Background(), json.RawMessage(`{"path":"main.go","line":7,"column":1}`))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !strings.Contains(out, "other.go:3:5") {
				t.Errorf("output = %q, want other.go:3:5", out)
			}
		})
	}
}

func TestSymbolsTool(t *testing.T) {
	out, err := toolNamed(t, "lsp_symbols").
		Execute(context.Background(), json.RawMessage(`{"path":"main.go"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Foo") || !strings.Contains(out, "func") {
		t.Errorf("output = %q, want the symbol name and kind", out)
	}
}

// TestLSPToolsRejectBadInput: a malformed call is a clean error, never a panic.
func TestLSPToolsRejectBadInput(t *testing.T) {
	for _, tl := range LSPTools(fakeLS()) {
		t.Run(tl.Name()+"/malformed", func(t *testing.T) {
			if _, err := tl.Execute(context.Background(), json.RawMessage(`{`)); err == nil {
				t.Error("malformed JSON: want an error")
			}
		})
		t.Run(tl.Name()+"/missing path", func(t *testing.T) {
			if _, err := tl.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
				t.Error("missing path: want an error")
			}
		})
	}
}

// TestPositionToolsRequireAPosition: hover/definition/references are meaningless
// without a line, so a zero line must be rejected rather than silently probing
// line 0 (which does not exist in 1-based coordinates).
func TestPositionToolsRequireAPosition(t *testing.T) {
	for _, name := range []string{"lsp_hover", "lsp_definition", "lsp_references"} {
		t.Run(name, func(t *testing.T) {
			_, err := toolNamed(t, name).
				Execute(context.Background(), json.RawMessage(`{"path":"main.go"}`))
			if err == nil {
				t.Error("missing line: want an error")
			}
		})
	}
}
