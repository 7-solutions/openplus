// Package symbols indexes claimable Go symbols with their source locations, so a
// coordinator can lock at the symbol (function/type) level rather than the file
// level (change 0013).
//
// go/parser is stdlib and cgo-free, which is why the native coordinator is
// Go-only: grit's tree-sitter parses thirteen languages, but tree-sitter is C
// and therefore off-limits under ADR-0001. A non-Go file is rejected by name and
// points an operator at grit.
package symbols

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Kind is a symbol's category.
type Kind int

const (
	KindFunc Kind = iota
	KindMethod
	KindType
)

// Symbol is one claimable code symbol with its location.
type Symbol struct {
	File      string // path relative to the indexed root
	Name      string // bare name, or "Type.Method" for methods
	Kind      Kind
	StartLine int
	EndLine   int
}

// IndexFile parses one Go file and returns the claimable symbols it declares:
// functions, methods (receiver-qualified as "Type.Method"), and types. An
// unparseable file is an error naming the path, since silently indexing nothing
// would let a claim on a real-but-broken file look like it succeeded.
func IndexFile(path string) ([]Symbol, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("symbols: read %s: %w", path, err)
	}
	return parseSource(path, src)
}

// parseSource is IndexFile without the filesystem, so tests can feed strings.
func parseSource(path string, src []byte) ([]Symbol, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("symbols: parse %s: %w", path, err)
	}

	var out []Symbol
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			kind := KindFunc
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = KindMethod
				name = recvTypeName(d.Recv.List[0].Type) + "." + name
			}
			out = append(out, Symbol{
				File:      path,
				Name:      name,
				Kind:      kind,
				StartLine: fset.Position(d.Pos()).Line,
				EndLine:   fset.Position(d.End()).Line,
			})
		case *ast.GenDecl:
			// Only type declarations are claimable; consts and vars are not.
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				out = append(out, Symbol{
					File:      path,
					Name:      ts.Name.Name,
					Kind:      KindType,
					StartLine: fset.Position(ts.Pos()).Line,
					EndLine:   fset.Position(ts.End()).Line,
				})
			}
		}
	}
	return out, nil
}

// recvTypeName extracts the type name from a method receiver, stripping pointer
// and generic qualifiers, so `(c *Config)` becomes "Config".
func recvTypeName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		return recvTypeName(star.X)
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	if idx, ok := expr.(*ast.IndexExpr); ok {
		// Generic instantiation T[Arg].
		return recvTypeName(idx.X)
	}
	return ""
}

// IndexDir walks root and indexes every *.go file, returning a map keyed by the
// file path relative to root. vendor/, testdata/, and .git/ are skipped: a claim
// on those would lock code that is not the project's own, and a vendored symbol
// colliding with a local one would be a false block.
func IndexDir(root string) (map[string][]Symbol, error) {
	out := map[string][]Symbol{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		syms, err := IndexFile(path)
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		out[rel] = syms
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("symbols: walk %s: %w", root, walkErr)
	}
	return out, nil
}

// Ref renders the "file.go::Name" reference form a claim uses.
func Ref(file, name string) string {
	return file + "::" + name
}

// Parse splits a reference back into (file, name). Returns ok=false for anything
// malformed, so a caller can refuse a junk claim without distinguishing its
// specific defect.
func Parse(ref string) (file, name string, ok bool) {
	idx := strings.Index(ref, "::")
	if idx <= 0 || idx == len(ref)-2 {
		return "", "", false
	}
	return ref[:idx], ref[idx+2:], true
}

// Exists reports whether ref names a real symbol under root. It is the check a
// claim depends on: granting a lock on a symbol that does not exist would let
// two agents both believe they had exclusive access to nothing.
//
// A non-Go file is an error, not a quiet miss: the native coordinator parses Go
// only, and silently returning "doesn't exist" for a TypeScript file would make
// a cross-language claim look like a typo. The error points at grit for those.
func Exists(root, ref string) (bool, error) {
	file, name, ok := Parse(ref)
	if !ok {
		return false, fmt.Errorf("symbols: malformed reference %q (want file.go::Name)", ref)
	}
	if !strings.HasSuffix(file, ".go") {
		return false, fmt.Errorf("symbols: native coordinator indexes Go only; %q is not Go "+
			"(use grit for other languages)", file)
	}
	idx, err := IndexDir(root)
	if err != nil {
		return false, err
	}
	syms, found := idx[file]
	if !found {
		return false, nil
	}
	for _, s := range syms {
		if s.Name == name {
			return true, nil
		}
	}
	return false, nil
}
