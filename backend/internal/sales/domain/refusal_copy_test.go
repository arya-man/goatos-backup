package domain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSalesRefusalCopyHasNoDoubleDash reads every string literal in the sales module's Go source
// and refuses " -- ". These strings are the sentences a refused sale shows on the phone and the
// web ("Sale date cannot be in the future for a closed sale -- record it ..." reached the phone
// verbatim, 2026-09-26); a double hyphen is source-comment punctuation, never farm copy.
// Comments are not string literals, so the long-form reasoning in them is untouched.
func TestSalesRefusalCopyHasNoDoubleDash(t *testing.T) {
	dirs := []string{".", "../app", "../adapters/http"}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(parsed, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(lit.Value)
				if err == nil && strings.Contains(value, " -- ") {
					t.Errorf("%s: user-facing string carries \" -- \": %q", fset.Position(lit.Pos()), value)
				}
				return true
			})
		}
	}
}
