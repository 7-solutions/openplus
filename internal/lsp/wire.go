package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/7solutions/openplus/internal/ports"
	"go.lsp.dev/jsonrpc2"
)

// This file is the neutrality boundary (ADR-0017 hard rule). Every LSP wire
// shape is declared here and converted to a ports type before it leaves the
// package. Nothing above this line in the dependency graph knows what a
// DiagnosticSeverity is.
//
// The shapes are declared locally rather than taken from go.lsp.dev/protocol
// because we consume a deliberately small slice of the protocol, and a local
// struct documents exactly which fields the adapter depends on.

type wirePosition struct {
	Line      int `json:"line"`      // 0-based
	Character int `json:"character"` // 0-based
}

type wireRange struct {
	Start wirePosition `json:"start"`
	End   wirePosition `json:"end"`
}

type wireLocation struct {
	URI   string    `json:"uri"`
	Range wireRange `json:"range"`
}

type wireDiagnostic struct {
	Range    wireRange `json:"range"`
	Severity int       `json:"severity"`
	Message  string    `json:"message"`
	Source   string    `json:"source"`
}

type publishDiagnosticsParams struct {
	URI         string           `json:"uri"`
	Diagnostics []wireDiagnostic `json:"diagnostics"`
}

// wireSymbol covers both documentSymbol shapes: the hierarchical DocumentSymbol
// (Range set) and the flat SymbolInformation (Location set). We ask for the
// flat form in initialize, but servers are inconsistent, so both are accepted.
type wireSymbol struct {
	Name     string       `json:"name"`
	Kind     int          `json:"kind"`
	Range    wireRange    `json:"range"`
	Location wireLocation `json:"location"`
}

type hoverResult struct {
	Contents hoverContents `json:"contents"`
}

// hoverContents absorbs the three shapes LSP has used for hover contents over
// the years: a bare string, a {language,value} pair, and a {kind,value}
// MarkupContent. A server picking any of them must not produce an empty hover.
type hoverContents struct {
	Value string
}

func (h *hoverContents) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		h.Value = s
		return nil
	}

	var obj struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err == nil && obj.Value != "" {
		h.Value = obj.Value
		return nil
	}

	// MarkedString[] — join the values.
	var arr []struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &arr); err == nil {
		var parts []string
		for _, a := range arr {
			if a.Value != "" {
				parts = append(parts, a.Value)
			}
		}
		h.Value = strings.Join(parts, "\n")
		return nil
	}

	// Unknown shape: an empty hover is better than a failed call.
	return nil
}

// decode unmarshals a raw JSON-RPC payload. Params are borrowed from the
// transport frame and valid only during the handler, so this must copy
// everything it keeps — which json.Unmarshal into a value type does.
func decode(raw jsonrpc2.RawMessage, v any) error {
	return json.Unmarshal([]byte(raw), v)
}

// toSeverity maps LSP's 1..4 severities to the neutral enum. An absent or
// unknown severity becomes SeverityError: a problem the agent cannot rank is
// better surfaced loudly than silently demoted to a hint.
func toSeverity(v int) ports.Severity {
	switch v {
	case 2:
		return ports.SeverityWarning
	case 3:
		return ports.SeverityInformation
	case 4:
		return ports.SeverityHint
	default:
		return ports.SeverityError
	}
}

// symbolKindNames maps LSP SymbolKind numbers to short names. Anything outside
// the table renders as "symbol" rather than a bare number, which would be
// meaningless in the model's context.
var symbolKindNames = map[int]string{
	1: "file", 2: "module", 3: "namespace", 4: "package", 5: "class",
	6: "method", 7: "property", 8: "field", 9: "constructor", 10: "enum",
	11: "interface", 12: "func", 13: "var", 14: "const", 15: "string",
	16: "number", 17: "bool", 18: "array", 19: "object", 20: "key",
	21: "null", 22: "enum-member", 23: "struct", 24: "event", 25: "operator",
	26: "type-param",
}

func symbolKind(v int) string {
	if name, ok := symbolKindNames[v]; ok {
		return name
	}
	return "symbol"
}

// pathToURI converts a filesystem path to a file:// URI.
func pathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

// uriToPath converts a file:// URI back to a filesystem path. A value that is
// not a URI is returned unchanged, so a server that answers with a bare path
// still works.
func uriToPath(raw string) string {
	if !strings.HasPrefix(raw, "file://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return filepath.FromSlash(u.Path)
}

// languageID maps a file extension to the LSP language identifier. Servers use
// it to pick a parser; an unknown extension falls back to "plaintext".
var languageIDs = map[string]string{
	".go": "go", ".ts": "typescript", ".tsx": "typescriptreact",
	".js": "javascript", ".jsx": "javascriptreact", ".py": "python",
	".rs": "rust", ".java": "java", ".rb": "ruby", ".c": "c",
	".cc": "cpp", ".cpp": "cpp", ".h": "c", ".hpp": "cpp",
	".cs": "csharp", ".php": "php", ".sh": "shellscript", ".json": "json",
	".yaml": "yaml", ".yml": "yaml", ".md": "markdown", ".sql": "sql",
	".zig": "zig", ".lua": "lua", ".swift": "swift", ".kt": "kotlin",
}

func languageID(path string) string {
	if id, ok := languageIDs[strings.ToLower(filepath.Ext(path))]; ok {
		return id
	}
	return "plaintext"
}
