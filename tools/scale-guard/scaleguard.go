// Command scaleguard is a zero-dependency static guard that blocks the
// million-animal scale anti-patterns catalogued in
// docs/decisions/scale-anti-patterns.md.
//
// It walks Go source under backend/ (request-path adapters, app services, and
// workers) and reports:
//
//   - n-plus-one         : a DB call (.Query/.QueryRow/.Exec/.SendBatch) inside a
//                          for/range loop body.
//   - offset-pagination  : an OFFSET clause in a SQL string literal (use keyset).
//   - full-mv-refresh     : DELETE FROM <...projection...> WHERE tenant_id (a
//                          stop-the-world projection rebuild; use incremental).
//   - non-sargable-like   : lower(col) LIKE '%..%' (unindexable leading wildcard).
//   - god-cte             : a single SQL literal with too many "x AS (" CTEs on a
//                          request path (compute-on-read; move to a read model).
//
// Two escape hatches keep it usable:
//
//   - Inline: append `// scale-guard:ignore: <reason>` to the offending line (or
//     the line above it). The reason is mandatory.
//   - Baseline: tools/scale-guard/baseline.txt lists pre-existing offenders as
//     `<rule> <relpath>:<line>` so the guard blocks only NEW violations while the
//     known debt is tracked and burned down.
//
// Usage:
//
//	go run ./tools/scale-guard            # from backend/ module root context
//	go run . -root <repo> -baseline <f>   # explicit
//
// Exit code 1 on any un-baselined, un-ignored violation.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type finding struct {
	rule string
	rel  string
	line int
	msg  string
}

func (f finding) key() string { return fmt.Sprintf("%s %s:%d", f.rule, f.rel, f.line) }

var (
	dbCallSel   = map[string]bool{"Query": true, "QueryRow": true, "Exec": true, "SendBatch": true}
	cteRe       = regexp.MustCompile(`(?i)\b[a-z_][a-z0-9_]* AS \(`)
	// OFFSET only when it is a real SQL clause (followed by a bind/number), not an
	// "offset must be a non-negative integer" error string or a param name.
	offsetRe    = regexp.MustCompile(`(?i)\bOFFSET\s+[\$:@%\d(]`)
	// Gate SQL-shaped rules on the literal actually looking like a query.
	sqlishRe    = regexp.MustCompile(`(?i)\b(SELECT|LIMIT|INSERT|UPDATE)\b`)
	sargableRe  = regexp.MustCompile(`(?is)lower\s*\([^)]*\)\s+LIKE\s+'%`)
	delProjRe   = regexp.MustCompile(`(?is)DELETE\s+FROM\s+[a-z_]*projection[a-z_]*\s+.*WHERE[^;]*tenant_id`)
	ignoreRe    = regexp.MustCompile(`scale-guard:ignore:\s*\S`)
	godCTELimit = 8
)

func main() {
	root := flag.String("root", ".", "repo root to scan")
	baseline := flag.String("baseline", "", "baseline file of accepted offenders")
	flag.Parse()

	repo, err := filepath.Abs(*root)
	must(err)
	// Scan product + worker repo logic only. backend/internal holds every request
	// path, app service, and worker repo method — i.e. every hot path that must
	// hold at 1-5M animals. One-time tooling (backend/cmd/seed-*, migrate) is
	// intentionally out of scope: a seed doing N+1 or delete+reinsert runs once
	// and never serves traffic. Fall back sensibly when run from other roots.
	scanRoot := filepath.Join(repo, "backend", "internal")
	for _, cand := range []string{filepath.Join(repo, "backend", "internal"), filepath.Join(repo, "internal"), filepath.Join(repo, "backend"), repo} {
		if _, err := os.Stat(cand); err == nil {
			scanRoot = cand
			break
		}
	}
	if *baseline == "" {
		*baseline = filepath.Join(repo, "tools", "scale-guard", "baseline.txt")
	}

	accepted := loadBaseline(*baseline)

	var findings []finding
	err = filepath.Walk(scanRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == "testdata" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		findings = append(findings, scanFile(repo, path)...)
		return nil
	})
	must(err)

	// Split into blocking (not baselined/ignored) vs accepted.
	var blocking []finding
	usedBaseline := map[string]bool{}
	for _, f := range findings {
		if accepted[f.key()] {
			usedBaseline[f.key()] = true
			continue
		}
		blocking = append(blocking, f)
	}

	sort.Slice(blocking, func(i, j int) bool {
		if blocking[i].rel != blocking[j].rel {
			return blocking[i].rel < blocking[j].rel
		}
		return blocking[i].line < blocking[j].line
	})

	if len(blocking) == 0 {
		fmt.Printf("scale-guard: OK (%d known offenders baselined)\n", len(accepted))
		// Warn about stale baseline entries so the debt list stays honest.
		for k := range accepted {
			if !usedBaseline[k] {
				fmt.Printf("scale-guard: note: stale baseline entry (no longer found, please remove): %s\n", k)
			}
		}
		return
	}

	fmt.Fprintf(os.Stderr, "scale-guard: %d NEW scale anti-pattern violation(s):\n\n", len(blocking))
	for _, f := range blocking {
		fmt.Fprintf(os.Stderr, "  %s\n    %s:%d  %s\n", f.rule, f.rel, f.line, f.msg)
	}
	fmt.Fprintf(os.Stderr, "\nEach must be fixed, annotated `// scale-guard:ignore: <reason>`,\n"+
		"or (if pre-existing) added to tools/scale-guard/baseline.txt.\n"+
		"See docs/decisions/scale-anti-patterns.md.\n")
	os.Exit(1)
}

func scanFile(repo, path string) []finding {
	rel, _ := filepath.Rel(repo, path)
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil // unparseable file: skip rather than crash CI
	}
	ignored := ignoredLines(src)
	var out []finding
	add := func(rule string, pos token.Pos, msg string) {
		ln := fset.Position(pos).Line
		if ignored[ln] || ignored[ln-1] {
			return
		}
		out = append(out, finding{rule: rule, rel: rel, line: ln, msg: msg})
	}

	// AST pass: N+1 (DB call inside a loop body).
	ast.Inspect(file, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch s := n.(type) {
		case *ast.ForStmt:
			body = s.Body
		case *ast.RangeStmt:
			body = s.Body
		default:
			return true
		}
		ast.Inspect(body, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !dbCallSel[sel.Sel.Name] {
				return true
			}
			add("n-plus-one", call.Pos(),
				fmt.Sprintf("DB call .%s(...) inside a loop; batch it (UNNEST / multi-row) or hoist out of the loop", sel.Sel.Name))
			return true
		})
		return true
	})

	// String-literal pass: SQL patterns.
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v := lit.Value
		if len(v) < 12 {
			return true
		}
		if offsetRe.MatchString(v) && sqlishRe.MatchString(v) {
			add("offset-pagination", lit.Pos(),
				"OFFSET in SQL; deep offsets scan-and-discard. Use keyset/cursor pagination")
		}
		if delProjRe.MatchString(v) {
			add("full-mv-refresh", lit.Pos(),
				"whole-projection DELETE by tenant = stop-the-world rebuild. Use incremental/version-swap upsert")
		}
		if sargableRe.MatchString(v) {
			add("non-sargable-like", lit.Pos(),
				"lower(col) LIKE '%..%' is unindexable. Add a normalized column / expression index or trigram")
		}
		if n := len(cteRe.FindAllString(v, -1)); n > godCTELimit {
			add("god-cte", lit.Pos(),
				fmt.Sprintf("%d CTEs in one request-path query = compute-on-read. Serve from a materialized read model", n))
		}
		return true
	})
	return out
}

// ignoredLines returns the set of line numbers carrying a scale-guard:ignore.
func ignoredLines(src []byte) map[int]bool {
	out := map[int]bool{}
	sc := bufio.NewScanner(strings.NewReader(string(src)))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	ln := 0
	for sc.Scan() {
		ln++
		if ignoreRe.MatchString(sc.Text()) {
			out[ln] = true
		}
	}
	return out
}

func loadBaseline(path string) map[string]bool {
	out := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: "<rule> <relpath>:<line>"  (trailing comment after '#' allowed)
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		out[line] = true
	}
	return out
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "scale-guard: fatal:", err)
		os.Exit(2)
	}
}
