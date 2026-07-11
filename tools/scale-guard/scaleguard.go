// Command scaleguard is a zero-dependency static guard that blocks the
// million-animal scale anti-patterns catalogued in
// docs/decisions/scale-anti-patterns.md.
//
// It walks Go source under backend/internal (request-path adapters, app
// services, and worker repo methods) and reports:
//
//   - n-plus-one         : a DB call (.Query/.QueryRow/.Exec/.SendBatch) inside a
//     for/range loop body.
//   - loop-no-cursor      : an infinite `for {}` that calls a paging repo method
//     (List*/Fetch*/*Page) without any cursor/progress guard
//     in the loop body — the park-consolidation hang class.
//   - offset-pagination  : an OFFSET clause in a SQL string literal (use keyset).
//   - full-mv-refresh     : DELETE FROM <...projection...> WHERE tenant_id with NO
//     projection_version guard = a stop-the-world whole-tenant
//     rebuild. A version-scoped prune (projection_version <>)
//     is fine and is NOT flagged.
//   - non-sargable-like   : lower(col) LIKE '%..%' (unindexable leading wildcard).
//   - god-cte             : a single SQL literal with too many "x AS (" CTEs on a
//     request path (compute-on-read; move to a read model).
//
// Two escape hatches keep it usable:
//
//   - Inline: append `// scale-guard:ignore: <reason>` to the offending line (or
//     the line above it). The reason is mandatory.
//   - Baseline: tools/scale-guard/baseline.txt accepts pre-existing debt as
//     per-(rule, file) COUNTS: `<rule> <relpath> <count>`. Counts are immune to
//     line drift when files are edited. The guard blocks only findings BEYOND the
//     baselined count for a (rule, file). Reduce a count when you fix code.
//
// Usage:
//
//	go run . -root <repo> [-baseline <f>]
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
	"strconv"
	"strings"
)

type finding struct {
	rule string
	rel  string
	line int
	msg  string
}

func (f finding) group() string { return f.rule + "\t" + f.rel }

var (
	dbCallSel = map[string]bool{"Query": true, "QueryRow": true, "Exec": true, "SendBatch": true}
	cteRe     = regexp.MustCompile(`(?i)\b[a-z_][a-z0-9_]* AS \(`)
	// OFFSET only when it is a real SQL clause (followed by a bind/number), not an
	// "offset must be a non-negative integer" error string or a param name.
	offsetRe = regexp.MustCompile(`(?i)\bOFFSET\s+[\$:@%\d(]`)
	// Gate SQL-shaped rules on the literal actually looking like a query.
	sqlishRe   = regexp.MustCompile(`(?i)\b(SELECT|LIMIT|INSERT|UPDATE)\b`)
	sargableRe = regexp.MustCompile(`(?is)lower\s*\([^)]*\)\s+LIKE\s+'%`)
	delProjRe  = regexp.MustCompile(`(?is)DELETE\s+FROM\s+[a-z_]*projection[a-z_]*\b[^;]*\btenant_id`)
	// A version-scoped prune (build-new / flip / drop-old generations) is the
	// APPROVED pattern, not a whole-tenant wipe. Do not treat range predicates
	// like projection_version > 0 as safe; those can still delete the serving set.
	versionPruneGuardRe = regexp.MustCompile(`(?i)projection_version\s*(<>|!=)`)
	// Paging repo methods whose loops must prove forward progress.
	pageMethodRe = regexp.MustCompile(`^(List|Fetch)|Page$|Chunk$`)
	// Identifiers whose presence in a loop body signals a cursor/progress guard.
	cursorHintRe = regexp.MustCompile(`(?i)cursor|after|keyset|seen|advance|nextpage|next_page|pagetoken|page_token`)
	ignoreRe     = regexp.MustCompile(`scale-guard:ignore:\s*\S`)
	godCTELimit  = 8
)

func main() {
	root := flag.String("root", ".", "repo root to scan")
	baseline := flag.String("baseline", "", "baseline file of accepted per-(rule,file) counts")
	flag.Parse()

	repo, err := filepath.Abs(*root)
	must(err)
	// Scan product + worker repo logic only. backend/internal holds every request
	// path, app service, and worker repo method — i.e. every hot path that must
	// hold at 1-5M animals. One-time tooling (backend/cmd/seed-*, migrate) is
	// intentionally out of scope: a seed doing N+1 or delete+reinsert runs once
	// and never serves traffic.
	scanRoot := repo
	for _, cand := range []string{filepath.Join(repo, "backend", "internal"), filepath.Join(repo, "internal"), filepath.Join(repo, "backend"), repo} {
		if _, err := os.Stat(cand); err == nil {
			scanRoot = cand
			break
		}
	}
	if *baseline == "" {
		*baseline = filepath.Join(repo, "tools", "scale-guard", "baseline.txt")
	}
	allowed := loadBaseline(*baseline)

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

	// Count actual findings per (rule, file); block anything beyond the baseline.
	actual := map[string]int{}
	sample := map[string]finding{} // one example per over-limit group, for the message
	for _, f := range findings {
		g := f.group()
		actual[g]++
		if _, ok := sample[g]; !ok {
			sample[g] = f
		}
	}

	type block struct {
		group      string
		have, base int
		ex         finding
	}
	var blocks []block
	for g, have := range actual {
		if base := allowed[g]; have > base {
			blocks = append(blocks, block{g, have, base, sample[g]})
		}
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].group < blocks[j].group })

	if len(blocks) == 0 {
		total := 0
		for _, n := range allowed {
			total += n
		}
		fmt.Printf("scale-guard: OK (%d known offenders baselined across %d rule/file groups)\n", total, len(allowed))
		for g, base := range allowed {
			if actual[g] < base {
				parts := strings.SplitN(g, "\t", 2)
				fmt.Printf("scale-guard: note: baseline over-counts %s in %s (%d baselined, %d found) — lower it\n",
					parts[0], parts[1], base, actual[g])
			}
		}
		return
	}

	fmt.Fprintf(os.Stderr, "scale-guard: %d rule/file group(s) exceed baseline:\n\n", len(blocks))
	for _, b := range blocks {
		parts := strings.SplitN(b.group, "\t", 2)
		fmt.Fprintf(os.Stderr, "  %s  %s\n    %d found, %d baselined (+%d new)  e.g. line %d: %s\n",
			parts[0], parts[1], b.have, b.base, b.have-b.base, b.ex.line, b.ex.msg)
	}
	fmt.Fprintf(os.Stderr, "\nFix the new offender, annotate it `// scale-guard:ignore: <reason>`,\n"+
		"or (if genuinely pre-existing) raise its count in tools/scale-guard/baseline.txt.\n"+
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

	// AST pass: loop-scoped rules.
	ast.Inspect(file, func(n ast.Node) bool {
		var body *ast.BlockStmt
		infinite := false
		switch s := n.(type) {
		case *ast.ForStmt:
			body = s.Body
			infinite = s.Cond == nil && s.Init == nil && s.Post == nil
		case *ast.RangeStmt:
			body = s.Body
		default:
			return true
		}

		// n-plus-one: a DB driver call inside the loop.
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

		// loop-no-cursor: an infinite for{} that pages a repo method with no
		// cursor/progress guard anywhere in the body (the park-consolidation hang).
		if infinite {
			var pageCall *ast.SelectorExpr
			guarded := false
			ast.Inspect(body, func(m ast.Node) bool {
				switch e := m.(type) {
				case *ast.CallExpr:
					if sel, ok := e.Fun.(*ast.SelectorExpr); ok &&
						pageMethodRe.MatchString(sel.Sel.Name) && !dbCallSel[sel.Sel.Name] {
						pageCall = sel
					}
				case *ast.Ident:
					if cursorHintRe.MatchString(e.Name) {
						guarded = true
					}
				}
				return true
			})
			if pageCall != nil && !guarded {
				add("loop-no-cursor", pageCall.Pos(),
					fmt.Sprintf("infinite for{} pages %s(...) with no cursor/progress guard; keyset-advance or it can loop forever", pageCall.Sel.Name))
			}
		}
		return true
	})

	// String-literal pass: SQL patterns.
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || len(lit.Value) < 12 {
			return true
		}
		v := lit.Value
		if offsetRe.MatchString(v) && sqlishRe.MatchString(v) {
			add("offset-pagination", lit.Pos(),
				"OFFSET in SQL; deep offsets scan-and-discard. Use keyset/cursor pagination")
		}
		if delProjRe.MatchString(v) && !versionPruneGuardRe.MatchString(v) {
			add("full-mv-refresh", lit.Pos(),
				"whole-tenant projection DELETE with no projection_version guard = stop-the-world rebuild. Use version-swap (build new, flip, prune old)")
		}
		if sargableRe.MatchString(v) {
			add("non-sargable-like", lit.Pos(),
				"lower(col) LIKE '%..%' is unindexable. Add a normalized column / expression index or trigram")
		}
		if c := len(cteRe.FindAllString(v, -1)); c > godCTELimit {
			add("god-cte", lit.Pos(),
				fmt.Sprintf("%d CTEs in one request-path query = compute-on-read. Serve from a materialized read model", c))
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

// loadBaseline reads "<rule> <relpath> <count>" lines into a per-group count map.
// A trailing "# comment" is ignored; a missing count defaults to 1.
func loadBaseline(path string) map[string]int {
	out := map[string]int{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		count := 1
		if len(fields) >= 3 {
			if n, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
				count = n
			}
		}
		out[fields[0]+"\t"+fields[1]] += count
	}
	return out
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "scale-guard: fatal:", err)
		os.Exit(2)
	}
}
