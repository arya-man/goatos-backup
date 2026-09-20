// Command sql-bind-contract-check statically checks pgx query call sites whose
// SQL and argument shape can be proven at build time. Unprovable dynamic sites
// are reported for the repository ratchet unless they use sqlbind.BoundQuery or
// pgx.StrictNamedArgs.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

type finding struct {
	File          string
	Line          int
	Kind, Message string
}

func main() {
	root := flag.String("root", ".", "backend source root")
	flag.Parse()
	findings, err := scanTree(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(findings) == 0 {
		fmt.Println("postgres bind-contract guard: ok")
		return
	}
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "%s:%d: %s: %s\n", f.File, f.Line, f.Kind, f.Message)
	}
	os.Exit(1)
}

func scanTree(root string) ([]finding, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (d.Name() == "vendor" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") && !strings.Contains(filepath.ToSlash(path), "/generated/") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []finding
	for _, path := range files {
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		out = append(out, scanFile(path, fs, f)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

func scanFile(path string, fs *token.FileSet, f *ast.File) []finding {
	constants := collectConstants(f)
	bound := collectBoundQueries(f)
	var out []finding
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		sqlIndex := -1
		switch name {
		case "Query", "QueryRow", "Exec":
			sqlIndex = 1
		case "Queue":
			sqlIndex = 0
		default:
			return true
		}
		if len(call.Args) <= sqlIndex {
			return true
		}
		pos := fs.Position(call.Pos())
		add := func(kind, msg string) { out = append(out, finding{filepath.ToSlash(path), pos.Line, kind, msg}) }
		if isBoundUse(call, sqlIndex, bound) {
			return true
		}
		if hasStrictNamedArgs(call.Args[sqlIndex+1:]) {
			return true
		}
		sql, ok := constantString(call.Args[sqlIndex], constants, map[string]bool{})
		if !ok {
			add("unverified-dynamic-bind", "dynamic SQL must use sqlbind.BoundQuery or pgx.StrictNamedArgs")
			return true
		}
		dataCount, provable := staticArgumentCount(call.Args[sqlIndex+1:], call.Ellipsis.IsValid())
		if !provable {
			add("unverified-dynamic-args", "variadic or computed arguments must use sqlbind.BoundQuery")
			return true
		}
		args := make([]any, dataCount)
		if err := sqlbind.ValidatePositional(sql, args); err != nil {
			add("positional-bind-mismatch", err.Error())
		}
		return true
	})
	return out
}

func collectConstants(f *ast.File) map[string]ast.Expr {
	m := map[string]ast.Expr{}
	ast.Inspect(f, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if i < len(vs.Values) {
					m[n.Name] = vs.Values[i]
				}
			}
		}
		return false
	})
	return m
}

func collectBoundQueries(f *ast.File) map[string]bool {
	m := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range as.Rhs {
			if i >= len(as.Lhs) || !isSQLBindConstructor(rhs) {
				continue
			}
			if id, ok := as.Lhs[i].(*ast.Ident); ok {
				m[id.Name] = true
			}
		}
		return true
	})
	return m
}
func isSQLBindConstructor(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	s, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || (s.Sel.Name != "MustBind" && s.Sel.Name != "Bind") {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == "sqlbind"
}
func isBoundUse(call *ast.CallExpr, sqlIndex int, bound map[string]bool) bool {
	s, ok := call.Args[sqlIndex].(*ast.SelectorExpr)
	if !ok || s.Sel.Name != "SQL" {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	if !ok || !bound[id.Name] {
		return false
	}
	if len(call.Args) <= sqlIndex+1 {
		return false
	}
	last := call.Args[len(call.Args)-1]
	a, ok := last.(*ast.SelectorExpr)
	return ok && call.Ellipsis.IsValid() && a.Sel.Name == "Args" && exprIdent(a.X) == id.Name
}
func exprIdent(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func constantString(e ast.Expr, constants map[string]ast.Expr, visiting map[string]bool) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(x.Value)
		return v, err == nil
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		a, ok := constantString(x.X, constants, visiting)
		if !ok {
			return "", false
		}
		b, ok := constantString(x.Y, constants, visiting)
		return a + b, ok
	case *ast.ParenExpr:
		return constantString(x.X, constants, visiting)
	case *ast.Ident:
		if visiting[x.Name] {
			return "", false
		}
		if x.Obj != nil && x.Obj.Kind != ast.Con {
			return "", false
		}
		var v ast.Expr
		var ok bool
		if x.Obj != nil && x.Obj.Kind == ast.Con {
			if spec, isSpec := x.Obj.Decl.(*ast.ValueSpec); isSpec {
				for i, name := range spec.Names {
					if name.Name == x.Name && i < len(spec.Values) {
						v, ok = spec.Values[i], true
						break
					}
				}
			}
		}
		if !ok {
			v, ok = constants[x.Name]
		}
		if !ok {
			return "", false
		}
		visiting[x.Name] = true
		s, ok := constantString(v, constants, visiting)
		delete(visiting, x.Name)
		return s, ok
	}
	return "", false
}
func hasStrictNamedArgs(args []ast.Expr) bool {
	for _, e := range args {
		switch x := e.(type) {
		case *ast.CompositeLit:
			if selectorName(x.Type) == "StrictNamedArgs" {
				return true
			}
		case *ast.CallExpr:
			if selectorName(x.Fun) == "StrictNamedArgs" {
				return true
			}
		}
	}
	return false
}
func selectorName(e ast.Expr) string {
	if s, ok := e.(*ast.SelectorExpr); ok {
		return s.Sel.Name
	}
	return ""
}
func staticArgumentCount(args []ast.Expr, variadic bool) (int, bool) {
	if variadic {
		return 0, false
	}
	n := 0
	for _, e := range args {
		if isPGXOption(e) {
			continue
		}
		n++
	}
	return n, true
}
func isPGXOption(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return strings.HasPrefix(x.Sel.Name, "QueryExecMode") || x.Sel.Name == "QueryResultFormats" || x.Sel.Name == "QueryResultFormatsByOID"
	case *ast.CompositeLit:
		n := selectorName(x.Type)
		return n == "QueryResultFormats" || n == "QueryResultFormatsByOID"
	}
	return false
}
