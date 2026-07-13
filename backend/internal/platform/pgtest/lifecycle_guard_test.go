package pgtest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const pgtestImportPath = "github.com/vgoats/goatos/backend/internal/platform/pgtest"

// TestLifecycleContractGuard enforces the container-teardown contract statically (no Docker): every
// package whose *_test.go calls pgtest.StartPostgres MUST have a TestMain whose body calls
// pgtest.RunMain or pgtest.Shutdown. Without it, that package's shared container is left running
// after its test binary exits.
//
// This parses Go syntax rather than grepping for strings: it resolves each file's pgtest import
// alias and requires a real call inside the actual TestMain function body, so a comment mentioning
// pgtest.Shutdown, an aliased import, or an unrelated helper cannot satisfy the guard, and a
// TestMain that omits teardown (e.g. os.Exit(m.Run())) is correctly flagged.
func TestLifecycleContractGuard(t *testing.T) {
	root := pgtest.RepoRootForTest(t)
	backend := filepath.Join(root, "backend")
	fset := token.NewFileSet()

	type info struct{ caller, teardown bool }
	dirs := map[string]*info{}
	at := func(dir string) *info {
		if dirs[dir] == nil {
			dirs[dir] = &info{}
		}
		return dirs[dir]
	}

	err := filepath.WalkDir(backend, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		alias := pgtestAlias(f)
		if alias == "" {
			return nil // file does not import pgtest; cannot be a qualified caller
		}
		dir := filepath.Dir(path)

		ast.Inspect(f, func(n ast.Node) bool {
			if isPgtestCall(n, alias, "StartPostgres") {
				at(dir).caller = true
			}
			if fd, ok := n.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "TestMain" && fd.Body != nil {
				ast.Inspect(fd.Body, func(m ast.Node) bool {
					if isPgtestCall(m, alias, "RunMain") || isPgtestCall(m, alias, "Shutdown") {
						at(dir).teardown = true
					}
					return true
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk/parse backend: %v", err)
	}

	callers := 0
	var violations []string
	for dir, in := range dirs {
		if !in.caller {
			continue
		}
		callers++
		if !in.teardown {
			rel, _ := filepath.Rel(root, dir)
			violations = append(violations, rel)
		}
	}
	if callers == 0 {
		t.Fatal("guard found no pgtest.StartPostgres callers; the AST scan is likely broken")
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("these packages call pgtest.StartPostgres but no TestMain body calls pgtest.RunMain(m) "+
			"or pgtest.Shutdown() (their shared container would leak):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// pgtestAlias returns the local name the file uses for the pgtest package ("pgtest" by default, an
// alias if renamed, "." for a dot-import), or "" if the file does not import pgtest.
func pgtestAlias(f *ast.File) string {
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) != pgtestImportPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name // includes "." for dot-imports and "_" for blank (blank can't call)
		}
		return "pgtest"
	}
	return ""
}

// isPgtestCall reports whether n is a call to <alias>.<name> (or bare <name> under a dot-import).
func isPgtestCall(n ast.Node, alias, name string) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		x, ok := fun.X.(*ast.Ident)
		return ok && x.Name == alias && fun.Sel.Name == name
	case *ast.Ident:
		return alias == "." && fun.Name == name
	default:
		return false
	}
}
