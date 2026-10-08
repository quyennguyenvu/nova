// Package gosrc edits Go source files structurally: merging scaffolded
// declarations into an existing file while keeping its imports and
// formatting intact.
package gosrc

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// MergeDecls splices the declarations rendered into scaffoldSrc onto the end
// of the existing Go file at absPath: every top-level declaration after the
// scaffold's import block is appended, and any import the scaffold needs but
// the file lacks is injected into the file's import group (a fresh group is
// created when the file has none). The merged source is gofmt'd before writing.
// Both inputs must already parse.
func MergeDecls(absPath, scaffoldSrc string) error {
	target, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", absPath, err)
	}

	declBlock, scaffoldImports, err := splitGoScaffold(scaffoldSrc)
	if err != nil {
		return err
	}
	if declBlock == "" {
		return nil // nothing to merge
	}

	merged, err := injectImports(string(target), absPath, scaffoldImports)
	if err != nil {
		return err
	}
	merged = strings.TrimRight(merged, "\n") + "\n\n" + declBlock
	if !strings.HasSuffix(merged, "\n") {
		merged += "\n"
	}
	formatted, err := format.Source([]byte(merged))
	if err != nil {
		return fmt.Errorf("format merged %s: %w", absPath, err)
	}
	//nolint:gosec // absPath = baseDir + fixed di file name; not user-tainted.
	if wErr := os.WriteFile(absPath, formatted, 0o600); wErr != nil {
		return fmt.Errorf("write %s: %w", absPath, wErr)
	}
	return nil
}

// splitGoScaffold parses rendered Go source and returns (1) the text of every
// top-level declaration that is not the import block — from the first such
// declaration (including its doc comment) to EOF — and (2) the imports those
// declarations bring along.
func splitGoScaffold(src string) (string, []*ast.ImportSpec, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "scaffold.go", src, parser.ParseComments)
	if err != nil {
		return "", nil, fmt.Errorf("parse scaffold: %w", err)
	}
	start := -1
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}
		pos := d.Pos()
		if doc := declDoc(d); doc != nil {
			pos = doc.Pos()
		}
		if off := fset.Position(pos).Offset; start == -1 || off < start {
			start = off
		}
	}
	if start == -1 {
		return "", nil, nil
	}
	return src[start:], f.Imports, nil
}

// declDoc returns the doc comment group attached to a top-level declaration, or
// nil when it has none.
func declDoc(d ast.Decl) *ast.CommentGroup {
	switch decl := d.(type) {
	case *ast.GenDecl:
		return decl.Doc
	case *ast.FuncDecl:
		return decl.Doc
	default:
		return nil
	}
}

// injectImports adds every import in want that src does not already declare,
// returning the updated source. Missing imports go into the file's existing
// parenthesised import group; if the file has none, a new group is inserted
// after the package clause. gofmt (run by the caller) re-sorts the result.
func injectImports(src, name string, want []*ast.ImportSpec) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", name, err)
	}

	have := make(map[string]bool, len(f.Imports))
	for _, imp := range f.Imports {
		have[importPath(imp)] = true
	}
	var lines []string
	for _, imp := range want {
		if have[importPath(imp)] {
			continue
		}
		spec := imp.Path.Value
		if imp.Name != nil {
			spec = imp.Name.Name + " " + spec
		}
		lines = append(lines, "\t"+spec)
		have[importPath(imp)] = true
	}
	if len(lines) == 0 {
		return src, nil
	}
	block := strings.Join(lines, "\n")

	if grp := importGroup(f); grp != nil && grp.Lparen.IsValid() {
		at := fset.Position(grp.Rparen).Offset
		return src[:at] + block + "\n" + src[at:], nil
	}
	at := fset.Position(f.Name.End()).Offset
	return src[:at] + "\n\nimport (\n" + block + "\n)" + src[at:], nil
}

// importGroup returns the file's import declaration, or nil when it has none.
func importGroup(f *ast.File) *ast.GenDecl {
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			return gd
		}
	}
	return nil
}

// importPath returns an import spec's unquoted path, the key used to dedupe.
func importPath(imp *ast.ImportSpec) string {
	return strings.Trim(imp.Path.Value, `"`)
}
