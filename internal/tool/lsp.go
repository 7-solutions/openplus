package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/7-solutions/openplus/internal/ports"
)

// The LSP tools expose the LanguageService port to the model (change 0026,
// ADR-0017). They are thin: parse, delegate to the port, render text. All
// intelligence lives behind the port; all wire knowledge lives in internal/lsp.
//
// Output is plain text rather than JSON. The model reads these results the way
// a developer reads compiler output, and `file:line:col message` is the format
// it has seen a million times in training data.
//
// Every surface is read-only. They still pass the PolicyGate on the autonomous
// path like any other tool.

// LSPTools returns every language-service tool bound to ls. Registration is the
// caller's decision — the runtime only registers these when LSP is configured.
func LSPTools(ls ports.LanguageService) []Tool {
	return []Tool{
		LSPDiagnostics{LS: ls},
		LSPHover{LS: ls},
		LSPDefinition{LS: ls},
		LSPSymbols{LS: ls},
		LSPReferences{LS: ls},
	}
}

// --- shared input handling ---

type pathInput struct {
	Path string `json:"path"`
}

type positionInput struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

func parsePath(tool string, input json.RawMessage) (string, error) {
	var in pathInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("%s: bad input: %w", tool, err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("%s: path is required", tool)
	}
	return in.Path, nil
}

// parsePosition requires a line. Column defaults to 1: a caller who knows the
// line but not the exact column is asking a reasonable question, but a caller
// with no line at all is asking about nothing — positions are 1-based, so a
// zero line is absent rather than "the first line".
func parsePosition(tool string, input json.RawMessage) (positionInput, error) {
	var in positionInput
	if err := json.Unmarshal(input, &in); err != nil {
		return in, fmt.Errorf("%s: bad input: %w", tool, err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return in, fmt.Errorf("%s: path is required", tool)
	}
	if in.Line <= 0 {
		return in, fmt.Errorf("%s: line is required and is 1-based", tool)
	}
	if in.Column <= 0 {
		in.Column = 1
	}
	return in, nil
}

const positionSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "file path, relative to the project root"},
    "line": {"type": "integer", "description": "1-based line number"},
    "column": {"type": "integer", "description": "1-based column number; defaults to 1"}
  },
  "required": ["path", "line"]
}`

const pathSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "file path, relative to the project root"}
  },
  "required": ["path"]
}`

func renderLocations(locs []ports.Location, empty string) string {
	if len(locs) == 0 {
		return empty
	}
	var b strings.Builder
	for _, l := range locs {
		fmt.Fprintf(&b, "%s:%d:%d\n", l.Path, l.Line, l.Column)
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- diagnostics ---

// LSPDiagnostics reports the problems a language server sees in a file.
type LSPDiagnostics struct{ LS ports.LanguageService }

func (t LSPDiagnostics) Name() string { return "lsp_diagnostics" }

func (t LSPDiagnostics) Description() string {
	return "Report compiler and linter problems in a file, as seen by its language " +
		"server. Use after editing to check your work compiles."
}

func (t LSPDiagnostics) Schema() json.RawMessage { return json.RawMessage(pathSchema) }

func (t LSPDiagnostics) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	path, err := parsePath(t.Name(), input)
	if err != nil {
		return "", err
	}
	diags, err := t.LS.Diagnostics(ctx, path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.Name(), err)
	}
	// An empty result must be a statement, not an empty string: "" reads to the
	// model as a failed call, while "no diagnostics" is a useful answer.
	if len(diags) == 0 {
		return fmt.Sprintf("no diagnostics for %s", path), nil
	}

	var b strings.Builder
	for _, d := range diags {
		fmt.Fprintf(&b, "%s:%d:%d: %s: %s", d.Path, d.Line, d.Column, d.Severity, d.Message)
		if d.Source != "" {
			fmt.Fprintf(&b, " (%s)", d.Source)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// --- hover ---

// LSPHover describes the symbol at a position.
type LSPHover struct{ LS ports.LanguageService }

func (t LSPHover) Name() string { return "lsp_hover" }

func (t LSPHover) Description() string {
	return "Describe the symbol at a position: its signature and documentation. " +
		"Use to learn what a function or type is without reading its definition."
}

func (t LSPHover) Schema() json.RawMessage { return json.RawMessage(positionSchema) }

func (t LSPHover) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := parsePosition(t.Name(), input)
	if err != nil {
		return "", err
	}
	text, err := t.LS.Hover(ctx, in.Path, in.Line, in.Column)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.Name(), err)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Sprintf("no hover information at %s:%d:%d", in.Path, in.Line, in.Column), nil
	}
	return text, nil
}

// --- definition ---

// LSPDefinition locates where a symbol is defined.
type LSPDefinition struct{ LS ports.LanguageService }

func (t LSPDefinition) Name() string { return "lsp_definition" }

func (t LSPDefinition) Description() string {
	return "Locate where the symbol at a position is defined. Returns file:line:column."
}

func (t LSPDefinition) Schema() json.RawMessage { return json.RawMessage(positionSchema) }

func (t LSPDefinition) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := parsePosition(t.Name(), input)
	if err != nil {
		return "", err
	}
	locs, err := t.LS.Definition(ctx, in.Path, in.Line, in.Column)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.Name(), err)
	}
	return renderLocations(locs,
		fmt.Sprintf("no definition found at %s:%d:%d", in.Path, in.Line, in.Column)), nil
}

// --- references ---

// LSPReferences finds the uses of a symbol.
type LSPReferences struct{ LS ports.LanguageService }

func (t LSPReferences) Name() string { return "lsp_references" }

func (t LSPReferences) Description() string {
	return "Find the uses of the symbol at a position. Returns one file:line:column " +
		"per reference. Use before changing a signature."
}

func (t LSPReferences) Schema() json.RawMessage { return json.RawMessage(positionSchema) }

func (t LSPReferences) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := parsePosition(t.Name(), input)
	if err != nil {
		return "", err
	}
	locs, err := t.LS.References(ctx, in.Path, in.Line, in.Column)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.Name(), err)
	}
	return renderLocations(locs,
		fmt.Sprintf("no references found to the symbol at %s:%d:%d", in.Path, in.Line, in.Column)), nil
}

// --- symbols ---

// LSPSymbols lists the declarations in a file.
type LSPSymbols struct{ LS ports.LanguageService }

func (t LSPSymbols) Name() string { return "lsp_symbols" }

func (t LSPSymbols) Description() string {
	return "List the symbols declared in a file (functions, types, variables) with " +
		"their line numbers. Use to map a file without reading all of it."
}

func (t LSPSymbols) Schema() json.RawMessage { return json.RawMessage(pathSchema) }

func (t LSPSymbols) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	path, err := parsePath(t.Name(), input)
	if err != nil {
		return "", err
	}
	syms, err := t.LS.DocumentSymbols(ctx, path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.Name(), err)
	}
	if len(syms) == 0 {
		return fmt.Sprintf("no symbols found in %s", path), nil
	}

	var b strings.Builder
	for _, s := range syms {
		fmt.Fprintf(&b, "%s:%d: %s %s\n", s.Path, s.Line, s.Kind, s.Name)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
