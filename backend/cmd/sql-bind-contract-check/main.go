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
	imports := collectImports(f)
	constants := collectConstants(f)
	bound := collectBoundQueries(f, imports)
	named := collectStrictNamedArgs(f, imports)
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
		if !shouldInspectCall(sel, imports) {
			return true
		}
		if len(call.Args) <= sqlIndex {
			return true
		}
		pos := fs.Position(call.Pos())
		add := func(kind, msg string) { out = append(out, finding{filepath.ToSlash(path), pos.Line, kind, msg}) }
		if isBoundUse(call, sqlIndex, name, bound, imports) {
			return true
		}
		sql, ok := constantString(call.Args[sqlIndex], constants, map[string]bool{})
		strictNamed, namedKeys, strictErr := strictNamedArgs(call.Args[sqlIndex+1:], name, imports, named)
		if strictErr != "" {
			kind := "invalid-strict-named-args"
			if strings.HasPrefix(strictErr, "unverified:") {
				kind = "unverified-dynamic-args"
				strictErr = strings.TrimPrefix(strictErr, "unverified:")
			}
			add(kind, strictErr)
			return true
		}
		if strictNamed {
			if !ok {
				add("unverified-dynamic-bind", "dynamic StrictNamedArgs SQL must use an approved validated builder")
				return true
			}
			if err := sqlbind.ValidateNamed(sql, namedKeys); err != nil {
				add("invalid-strict-named-args", err.Error())
				return true
			}
			ordinals, err := sqlbind.PlaceholderOrdinals(sql)
			if err != nil || len(ordinals) != 0 {
				add("invalid-strict-named-args", "StrictNamedArgs SQL must not mix positional placeholders")
			}
			return true
		}
		if !ok {
			add("unverified-dynamic-bind", "dynamic SQL must use sqlbind.BoundQuery or pgx.StrictNamedArgs")
			return true
		}
		dataCount, provable := staticArgumentCount(call.Args[sqlIndex+1:], call.Ellipsis.IsValid(), name, imports)
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

func collectImports(f *ast.File) map[string]string {
	imports := map[string]string{}
	for _, spec := range f.Imports {
		pathValue, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := filepath.Base(pathValue)
		if pathValue == "github.com/jackc/pgx/v5" {
			name = "pgx"
		}
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports[name] = pathValue
	}
	return imports
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

func collectBoundQueries(f *ast.File, imports map[string]string) map[*ast.Object]bool {
	assignments := map[*ast.Object]int{}
	valid := map[*ast.Object]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range as.Rhs {
			if i >= len(as.Lhs) {
				continue
			}
			if id, ok := as.Lhs[i].(*ast.Ident); ok {
				if id.Obj == nil {
					continue
				}
				assignments[id.Obj]++
				valid[id.Obj] = isSQLBindConstructor(rhs, imports)
			}
		}
		return true
	})
	out := map[*ast.Object]bool{}
	for obj, count := range assignments {
		out[obj] = count == 1 && valid[obj]
	}
	for obj := range collectCheckedBindQueries(f, imports) {
		if assignments[obj] == 1 {
			out[obj] = true
		}
	}
	return out
}

// collectCheckedBindQueries recognizes only the fail-closed form:
//
//	q, err := sqlbind.Bind(...)
//	if err != nil { return ... }
//
// The check must be the immediately following statement and its body must end
// in return. This intentionally rejects ignored, delayed, overwritten, or
// merely logged errors without needing a general control-flow engine.
func collectCheckedBindQueries(f *ast.File, imports map[string]string) map[*ast.Object]bool {
	out := map[*ast.Object]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i := 0; i+1 < len(block.List); i++ {
			as, ok := block.List[i].(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 2 || len(as.Rhs) != 1 {
				continue
			}
			q, okQ := as.Lhs[0].(*ast.Ident)
			errID, okErr := as.Lhs[1].(*ast.Ident)
			if !okQ || !okErr || q.Name == "_" || errID.Name == "_" || q.Obj == nil || errID.Obj == nil {
				continue
			}
			if !isSQLBindCall(as.Rhs[0], imports, "Bind") {
				continue
			}
			guard, ok := block.List[i+1].(*ast.IfStmt)
			if !ok || guard.Init != nil || !isErrNotNil(guard.Cond, errID.Obj) || !endsInReturn(guard.Body) {
				continue
			}
			out[q.Obj] = true
		}
		return true
	})
	return out
}

func isErrNotNil(e ast.Expr, errObj *ast.Object) bool {
	b, ok := e.(*ast.BinaryExpr)
	if !ok || b.Op != token.NEQ {
		return false
	}
	check := func(a, b ast.Expr) bool {
		id, ok := a.(*ast.Ident)
		if !ok || id.Obj != errObj {
			return false
		}
		nilID, ok := b.(*ast.Ident)
		return ok && nilID.Name == "nil"
	}
	return check(b.X, b.Y) || check(b.Y, b.X)
}
func endsInReturn(block *ast.BlockStmt) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}
	_, ok := block.List[len(block.List)-1].(*ast.ReturnStmt)
	return ok
}

func isSQLBindCall(e ast.Expr, imports map[string]string, method string) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	s, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != method {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && importedPackagePath(id, imports) == "github.com/vgoats/goatos/backend/internal/platform/sqlbind"
}
func isSQLBindConstructor(e ast.Expr, imports map[string]string) bool {
	return isSQLBindCall(e, imports, "MustBind")
}
func isBoundUse(call *ast.CallExpr, sqlIndex int, method string, bound map[*ast.Object]bool, imports map[string]string) bool {
	sqlCall, ok := call.Args[sqlIndex].(*ast.CallExpr)
	if !ok || len(sqlCall.Args) != 0 {
		return false
	}
	s, ok := sqlCall.Fun.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != "SQL" {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	if !ok || id.Obj == nil || !bound[id.Obj] {
		return false
	}
	if len(call.Args) <= sqlIndex+1 {
		return false
	}
	last, ok := call.Args[len(call.Args)-1].(*ast.CallExpr)
	if !ok || len(last.Args) != 0 {
		return false
	}
	a, ok := last.Fun.(*ast.SelectorExpr)
	if !ok || !call.Ellipsis.IsValid() || a.Sel.Name != "Args" {
		return false
	}
	argID, ok := a.X.(*ast.Ident)
	if !ok || argID.Obj != id.Obj {
		return false
	}
	for _, option := range call.Args[sqlIndex+1 : len(call.Args)-1] {
		if !isPGXOption(option, method, imports) {
			return false
		}
	}
	return true
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

type namedInfo struct {
	keys  []string
	valid bool
}

func collectStrictNamedArgs(f *ast.File, imports map[string]string) map[*ast.Object]namedInfo {
	infos := map[*ast.Object]namedInfo{}
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok && isStrictNamedType(vs.Type, imports) {
			for _, id := range vs.Names {
				if id.Obj != nil {
					infos[id.Obj] = namedInfo{valid: false}
				}
			}
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range as.Rhs {
			if i >= len(as.Lhs) {
				continue
			}
			id, ok := as.Lhs[i].(*ast.Ident)
			if !ok || id.Obj == nil {
				continue
			}
			keys, ok := strictNamedLiteralKeys(rhs, imports)
			if ok {
				infos[id.Obj] = namedInfo{keys: keys, valid: true}
			}
		}
		return true
	})
	counts := map[*ast.Object]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Obj != nil {
				if _, tracked := infos[id.Obj]; tracked {
					counts[id.Obj]++
				}
			}
		}
		return true
	})
	// Only the declaration and direct database-call argument uses preserve
	// static map-key evidence. Index writes, delete/clear, aliases, pointers,
	// and passing the map to helpers can mutate its keys; fail closed on all
	// other uses rather than attempting interprocedural alias analysis.
	safeUses := map[*ast.Ident]bool{}
	for obj := range infos {
		if as, ok := obj.Decl.(*ast.AssignStmt); ok {
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					safeUses[id] = true
				}
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !shouldInspectCall(sel, imports) {
			return true
		}
		start := 2
		switch sel.Sel.Name {
		case "Query", "QueryRow", "Exec":
		case "Queue":
			start = 1
		default:
			return true
		}
		for i := start; i < len(call.Args); i++ {
			if id, ok := call.Args[i].(*ast.Ident); ok {
				safeUses[id] = true
			}
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok && id.Obj != nil && !safeUses[id] {
			if info, tracked := infos[id.Obj]; tracked {
				info.valid = false
				infos[id.Obj] = info
			}
		}
		return true
	})
	for obj, info := range infos {
		info.valid = info.valid && counts[obj] == 1
		infos[obj] = info
	}
	return infos
}

func strictNamedArgs(args []ast.Expr, method string, imports map[string]string, named map[*ast.Object]namedInfo) (bool, []string, string) {
	for len(args) > 0 && isPGXOption(args[0], method, imports) {
		args = args[1:]
	}
	if len(args) == 0 || !isPGXStrictNamedArgs(args[0], imports) {
		if len(args) > 0 {
			if id, ok := args[0].(*ast.Ident); ok && id.Obj != nil {
				if info, found := named[id.Obj]; found {
					if !info.valid {
						return false, nil, "unverified:StrictNamedArgs variable is reassigned, mutated, escapes, or is not a static literal"
					}
					if len(args) != 1 {
						return false, nil, "StrictNamedArgs must be the sole data argument after valid leading pgx options"
					}
					return true, info.keys, ""
				}
			}
		}
		return false, nil, ""
	}
	if len(args) != 1 {
		return false, nil, "StrictNamedArgs must be the sole data argument after valid leading pgx options"
	}
	keys, ok := strictNamedLiteralKeys(args[0], imports)
	if !ok {
		return false, nil, "StrictNamedArgs keys must be a static string-keyed literal"
	}
	return true, keys, ""
}
func isStrictNamedType(e ast.Expr, imports map[string]string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != "StrictNamedArgs" {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && importedPackagePath(id, imports) == "github.com/jackc/pgx/v5"
}
func strictNamedLiteralKeys(e ast.Expr, imports map[string]string) ([]string, bool) {
	cl, ok := e.(*ast.CompositeLit)
	if !ok || !isPGXStrictNamedArgs(cl, imports) {
		return nil, false
	}
	keys := make([]string, 0, len(cl.Elts))
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, false
		}
		lit, ok := kv.Key.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return nil, false
		}
		key, err := strconv.Unquote(lit.Value)
		if err != nil {
			// exception:exempt malformed source literals are classified as unverified scanner evidence
			return nil, false
		}
		keys = append(keys, key)
	}
	return keys, true
}
func isPGXStrictNamedArgs(e ast.Expr, imports map[string]string) bool {
	var target ast.Expr
	switch x := e.(type) {
	case *ast.CompositeLit:
		target = x.Type
	case *ast.CallExpr:
		target = x.Fun
	default:
		return false
	}
	s, ok := target.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != "StrictNamedArgs" {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && importedPackagePath(id, imports) == "github.com/jackc/pgx/v5"
}
func shouldInspectCall(sel *ast.SelectorExpr, imports map[string]string) bool {
	if id, ok := sel.X.(*ast.Ident); ok {
		if importedPackagePath(id, imports) != "" {
			return false
		}
	}
	return true
}
func selectorName(e ast.Expr) string {
	if s, ok := e.(*ast.SelectorExpr); ok {
		return s.Sel.Name
	}
	return ""
}
func staticArgumentCount(args []ast.Expr, variadic bool, method string, imports map[string]string) (int, bool) {
	if variadic {
		return 0, false
	}
	for len(args) > 0 && isPGXOption(args[0], method, imports) {
		args = args[1:]
	}
	return len(args), true
}
func isPGXOption(e ast.Expr, method string, imports map[string]string) bool {
	if method == "Queue" {
		return false
	}
	var name string
	var pkg *ast.Ident
	switch x := e.(type) {
	case *ast.SelectorExpr:
		name = x.Sel.Name
		if id, ok := x.X.(*ast.Ident); ok {
			pkg = id
		}
	case *ast.CompositeLit:
		if s, ok := x.Type.(*ast.SelectorExpr); ok {
			name = s.Sel.Name
			if id, ok := s.X.(*ast.Ident); ok {
				pkg = id
			}
		}
	}
	if importedPackagePath(pkg, imports) != "github.com/jackc/pgx/v5" {
		return false
	}
	if strings.HasPrefix(name, "QueryExecMode") {
		return true
	}
	return method != "Exec" && (name == "QueryResultFormats" || name == "QueryResultFormatsByOID")
}

// Imported package identifiers are unresolved by go/parser (Obj == nil).
// A same-named local has a non-nil Obj and must never inherit package trust.
func importedPackagePath(id *ast.Ident, imports map[string]string) string {
	if id == nil || id.Obj != nil {
		return ""
	}
	return imports[id.Name]
}
