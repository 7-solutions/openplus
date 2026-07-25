package ports

import "context"

// LanguageService is the code-intelligence seam (ADR-0017, change 0026): the
// eleventh port. It answers read-only questions about source code — what is
// broken, what a symbol means, where it is defined, and who uses it.
//
// Every surface is read-only. Mutating surfaces (code actions, rename,
// apply-edit) are deliberately absent: they need a gating story of their own.
//
// Neutrality rule (hard rule, ADR-0017): no LSP wire type may appear in this
// interface or in any type it returns. The concrete adapter in internal/lsp/
// converts protocol values to the neutral types below at its boundary, exactly
// as internal/provider converts provider wire types to the neutral model. The
// regression guard internal/ports/lsp_leak_guard_test.go fails the build if a
// go.lsp.dev type reaches this package.
//
// Positions are 1-based line and column numbers — what a human reads in an
// editor and what a compiler prints. The adapter converts from LSP's 0-based
// UTF-16 positions.
type LanguageService interface {
	// Diagnostics reports the current problems in one file. It returns the
	// latest known set; an implementation backed by a push-notification
	// protocol serves what the server last published.
	Diagnostics(ctx context.Context, path string) ([]Diagnostic, error)

	// Hover describes the symbol at a position — typically its signature and
	// doc comment, rendered as text.
	Hover(ctx context.Context, path string, line, col int) (string, error)

	// Definition locates where the symbol at a position is defined.
	Definition(ctx context.Context, path string, line, col int) ([]Location, error)

	// DocumentSymbols lists the symbols declared in one file.
	DocumentSymbols(ctx context.Context, path string) ([]Symbol, error)

	// References finds the uses of the symbol at a position.
	References(ctx context.Context, path string, line, col int) ([]Location, error)

	// Shutdown stops every language server this service started. It is
	// idempotent so a deferred call after an error path is safe.
	Shutdown(ctx context.Context) error
}

// Severity ranks a diagnostic. The zero value is SeverityError, so a
// diagnostic that loses its severity in translation is surfaced rather than
// silently demoted to a hint.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
	SeverityInformation
	SeverityHint
)

func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	case SeverityInformation:
		return "info"
	case SeverityHint:
		return "hint"
	default:
		return "error"
	}
}

// Diagnostic is one problem reported in a file: a compiler error, a vet
// warning, a linter hint.
type Diagnostic struct {
	Path     string // file the problem is in, relative to the project root
	Line     int    // 1-based
	Column   int    // 1-based
	Severity Severity
	Message  string
	Source   string // which tool produced it ("compiler", "vet", …); may be empty
}

// Location points at a span of source — where something is defined, or one
// place it is used.
type Location struct {
	Path   string // relative to the project root
	Line   int    // 1-based
	Column int    // 1-based
}

// Symbol is one declaration in a file.
type Symbol struct {
	Name string
	Kind string // "func", "type", "var", … (the adapter maps LSP's numeric kinds)
	Path string
	Line int // 1-based
}

// FakeLanguageService returns canned answers. It lets tests exercise
// LSP-dependent code without spawning a language server.
type FakeLanguageService struct {
	Diags     []Diagnostic
	HoverText string
	Locs      []Location
	Syms      []Symbol
}

func (f FakeLanguageService) Diagnostics(context.Context, string) ([]Diagnostic, error) {
	return f.Diags, nil
}

func (f FakeLanguageService) Hover(context.Context, string, int, int) (string, error) {
	return f.HoverText, nil
}

func (f FakeLanguageService) Definition(context.Context, string, int, int) ([]Location, error) {
	return f.Locs, nil
}

func (f FakeLanguageService) DocumentSymbols(context.Context, string) ([]Symbol, error) {
	return f.Syms, nil
}

func (f FakeLanguageService) References(context.Context, string, int, int) ([]Location, error) {
	return f.Locs, nil
}

func (f FakeLanguageService) Shutdown(context.Context) error { return nil }
