package main

// projection-review: membership=SQL statements changed in `git diff <merge-base>` under backend/internal; group_key=(file, statement) one finding per changed statement without proof; join_cardinality=each changed statement is matched against the changed at-scale plan tests of the same diff, no data join; pagination=none, bounded by the diff; scope=repository paths only, no tenant or runtime data

// plan-proof (PP-22, 2026-09-26): "validated only at STG size" is not a proof.
//
// A PR that changes serving SQL over a large table (largeTables) must, in the SAME diff, add
// or change an at-scale plan proof:
//   - a Go test named Test*AtScale (the TestXxxQueryPlanUsesIndexesAtScale pattern: bulk-load
//     ~500k rows, ANALYZE, EXPLAIN without enable_seqscan=off, assert no Seq Scan) in the same
//     package directory, or any changed Test*AtScale file that names the changed statement;
//   - or an added explain_* entry in backend/tests/integration/validate-sqlc-query-plans.sh.
// A change that cannot move a plan (comment text, a renamed column alias) carries
//   scale-guard:plan-proof-exempt: <reason>
// on an added line in the same file.
//
// Run: go run . -root <repo> -plan-proof [-base origin/main]
//
// BLIND SPOTS: "changed" is textual (git diff), per assembled package-level statement or
// literal. SQL built at runtime is not attributed. Whether the plan test actually EXPLAINs
// the changed statement is checked only by name or package, not by execution.

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	diffFileRe      = regexp.MustCompile(`^\+\+\+ b/(.+)$`)
	diffHunkRe      = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
	sqlLineRe       = regexp.MustCompile(`\b(?:SELECT|FROM|JOIN|WHERE|AND|OR|ON|IN|EXISTS|ANY|ARRAY|UNION|GROUP BY|ORDER BY|LIMIT|WITH|LATERAL|HAVING|CASE|WHEN|NOT|DISTINCT|PARTITION BY|OVER|INTERSECT|EXCEPT|USING)\b`)
	atScaleTestRe   = regexp.MustCompile(`func (Test\w*AtScale)\(`)
	explainEntryRe  = regexp.MustCompile(`^\s*explain_\w+\s+"(\w+)"`)
	planExemptRe    = regexp.MustCompile(`scale-guard:plan-proof-exempt:\s*\S`)
	sqlcPlanScript  = "backend/tests/integration/validate-sqlc-query-plans.sh"
	selectKeywordRe = regexp.MustCompile(`(?i)\bSELECT\b`)
)

type fileDiff struct {
	added   map[int]string // new-file line -> text
	touched []int          // new-file lines at which something was added or removed
}

func parseUnifiedDiff(out string) map[string]*fileDiff {
	res := map[string]*fileDiff{}
	var cur *fileDiff
	newLine := 0
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for sc.Scan() {
		l := sc.Text()
		if m := diffFileRe.FindStringSubmatch(l); m != nil {
			cur = &fileDiff{added: map[int]string{}}
			res[m[1]] = cur
			continue
		}
		if strings.HasPrefix(l, "+++ ") || strings.HasPrefix(l, "--- ") || cur == nil {
			if strings.HasPrefix(l, "diff --git") {
				cur = nil
			}
			continue
		}
		if m := diffHunkRe.FindStringSubmatch(l); m != nil {
			newLine, _ = strconv.Atoi(m[1])
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			cur.added[newLine] = l[1:]
			cur.touched = append(cur.touched, newLine)
			newLine++
		case strings.HasPrefix(l, "-"):
			cur.touched = append(cur.touched, newLine)
		case strings.HasPrefix(l, " "):
			newLine++
		}
	}
	return res
}

// isSQLChangeLine: a changed line that carries SQL, not a SQL/Go comment.
func isSQLChangeLine(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "--") || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") {
		return false
	}
	return sqlLineRe.MatchString(t)
}

type changedStatement struct {
	rel, name string
	line      int
	tables    []string
}

// changedLargeTableStatements returns the serving statements in a Go source whose text a
// diff touched on a SQL-bearing line and which read a large table.
func changedLargeTableStatements(rel string, src []byte, fd *fileDiff, removedSQL bool) []changedStatement {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil
	}
	lineOf := func(p token.Pos) int { return fset.Position(p).Line }
	touchedSQL := func(from, to int) bool {
		for ln, text := range fd.added {
			if ln >= from && ln <= to && isSQLChangeLine(text) {
				return true
			}
		}
		if removedSQL {
			for _, ln := range fd.touched {
				if _, add := fd.added[ln]; !add && ln >= from && ln <= to+1 {
					return true
				}
			}
		}
		return false
	}
	var out []changedStatement
	seen := map[string]bool{}
	for _, st := range sqlConstText(file) {
		if !selectKeywordRe.MatchString(st.text) {
			continue
		}
		tables := referencedLargeTables(maskSQLNoise(st.text))
		if len(tables) == 0 {
			continue
		}
		// A statement changes when any literal it is assembled from changed; walk its value.
		changed := false
		var walk func(e ast.Expr, depth int)
		decls := packageLevelValues(file)
		walk = func(e ast.Expr, depth int) {
			if changed || depth > 16 {
				return
			}
			switch v := e.(type) {
			case *ast.BasicLit:
				if touchedSQL(lineOf(v.Pos()), lineOf(v.End())) {
					changed = true
				}
			case *ast.BinaryExpr:
				walk(v.X, depth+1)
				walk(v.Y, depth+1)
			case *ast.ParenExpr:
				walk(v.X, depth+1)
			case *ast.Ident:
				if inner, ok := decls[v.Name]; ok {
					walk(inner, depth+1)
				}
			}
		}
		if inner, ok := decls[st.name]; ok {
			walk(inner, 0)
		}
		if changed && !seen[st.name] {
			seen[st.name] = true
			out = append(out, changedStatement{rel: rel, name: st.name, line: lineOf(st.pos), tables: tables})
		}
	}
	// SQL literals inside functions (not package-level) are attributed by literal.
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		ast.Inspect(fn.Body, func(m ast.Node) bool {
			lit, ok := m.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil || !sqlishRe.MatchString(text) || !selectKeywordRe.MatchString(text) {
				return true
			}
			tables := referencedLargeTables(maskSQLNoise(text))
			if len(tables) > 0 && touchedSQL(lineOf(lit.Pos()), lineOf(lit.End())) {
				out = append(out, changedStatement{rel: rel, name: fn.Name.Name + " (inline SQL)", line: lineOf(lit.Pos()), tables: tables})
			}
			return true
		})
		return false
	})
	return out
}

func packageLevelValues(file *ast.File) map[string]ast.Expr {
	decls := map[string]ast.Expr{}
	for _, d := range file.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
			continue
		}
		for _, spec := range gen.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok {
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						decls[name.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	return decls
}

// isServingSQLFile: product adapters that hold serving SQL (not tests, not one-time tools).
func isServingSQLFile(rel string) bool {
	rel = filepath.ToSlash(rel)
	return strings.HasPrefix(rel, "backend/internal/") && strings.Contains(rel, "/adapters/") &&
		strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
}

type planProofs struct {
	testDirs     map[string][]string // package dir -> changed Test*AtScale names
	testText     map[string]string   // changed at-scale test file -> content (for name matching)
	sqlcExplains []string
}

func collectPlanProofs(repo string, diffs map[string]*fileDiff) planProofs {
	pp := planProofs{testDirs: map[string][]string{}, testText: map[string]string{}}
	for rel, fd := range diffs {
		if len(fd.touched) == 0 {
			continue
		}
		if strings.HasSuffix(rel, "_test.go") {
			src, err := os.ReadFile(filepath.Join(repo, rel))
			if err != nil {
				continue
			}
			ms := atScaleTestRe.FindAllStringSubmatch(string(src), -1)
			if len(ms) == 0 {
				continue
			}
			dir := filepath.ToSlash(filepath.Dir(rel))
			for _, m := range ms {
				pp.testDirs[dir] = append(pp.testDirs[dir], m[1])
			}
			pp.testText[rel] = string(src)
		}
		if rel == sqlcPlanScript {
			for _, text := range fd.added {
				if m := explainEntryRe.FindStringSubmatch(text); m != nil {
					pp.sqlcExplains = append(pp.sqlcExplains, m[1])
				}
			}
		}
	}
	return pp
}

func (pp planProofs) proves(st changedStatement) bool {
	dir := filepath.ToSlash(filepath.Dir(st.rel))
	if len(pp.testDirs[dir]) > 0 {
		return true
	}
	name := strings.Fields(st.name)[0]
	for _, text := range pp.testText {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(text) {
			return true
		}
	}
	// sqlc: generated query consts are named after the query (e.g. getGoatByID) and the
	// validate-sqlc-plans entry after the query name.
	if strings.Contains(st.rel, "/sqlc/") && len(pp.sqlcExplains) > 0 {
		return true
	}
	return false
}

// planProofFindings is the pure core: diffs + a reader for new-file contents.
func planProofFindings(repo string, diffs map[string]*fileDiff) []changedStatement {
	proofs := collectPlanProofs(repo, diffs)
	var missing []changedStatement
	rels := make([]string, 0, len(diffs))
	for rel := range diffs {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		fd := diffs[rel]
		if !isServingSQLFile(rel) || len(fd.touched) == 0 {
			continue
		}
		exempt := false
		for _, text := range fd.added {
			if planExemptRe.MatchString(text) {
				exempt = true
				break
			}
		}
		if exempt {
			continue
		}
		src, err := os.ReadFile(filepath.Join(repo, rel))
		if err != nil {
			continue // deleted file: nothing left to prove
		}
		for _, st := range changedLargeTableStatements(rel, src, fd, true) {
			if !proofs.proves(st) {
				missing = append(missing, st)
			}
		}
	}
	return missing
}

func planProofMessage(st changedStatement) string {
	dir := filepath.ToSlash(filepath.Dir(st.rel))
	return fmt.Sprintf("  %s:%d  %s changes SQL over %s with no at-scale plan proof in this diff.\n"+
		"    Add or extend a Test<Name>QueryPlanUsesIndexesAtScale in %s (bulk-load ~500k rows, ANALYZE,\n"+
		"    EXPLAIN (ANALYZE) WITHOUT enable_seqscan=off, assert no Seq Scan on %s), or an explain_* entry in\n"+
		"    %s for sqlc queries. If the change cannot move a plan, add\n"+
		"    `scale-guard:plan-proof-exempt: <reason>` on a changed line of %s.",
		st.rel, st.line, st.name, strings.Join(st.tables, ", "), dir, st.tables[0], sqlcPlanScript, filepath.Base(st.rel))
}

// runPlanProof diffs HEAD+worktree against merge-base(base, HEAD) and fails on unproven changes.
func runPlanProof(repo, base string) int {
	if base == "none" {
		fmt.Println("scale-guard plan-proof: skipped (base=none)")
		return 0
	}
	mb, err := exec.Command("git", "-C", repo, "merge-base", base, "HEAD").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "scale-guard plan-proof: cannot resolve merge-base with %s (git fetch origin, or pass -base <ref>): %v\n", base, err)
		return 1
	}
	out, err := exec.Command("git", "-C", repo, "diff", "--no-color", "--unified=0", "--no-renames", strings.TrimSpace(string(mb)), "--", "backend").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "scale-guard plan-proof: git diff failed: %v\n", err)
		return 1
	}
	diffs := parseUnifiedDiff(string(out))
	// Untracked files are part of the change being validated locally (git diff omits them).
	if untracked, err := exec.Command("git", "-C", repo, "ls-files", "--others", "--exclude-standard", "--", "backend").Output(); err == nil {
		for _, rel := range strings.Fields(string(untracked)) {
			src, err := os.ReadFile(filepath.Join(repo, rel))
			if err != nil {
				continue
			}
			fd := &fileDiff{added: map[int]string{}}
			for i, l := range strings.Split(string(src), "\n") {
				fd.added[i+1] = l
				fd.touched = append(fd.touched, i+1)
			}
			diffs[filepath.ToSlash(rel)] = fd
		}
	}
	missing := planProofFindings(repo, diffs)
	if len(missing) == 0 {
		fmt.Printf("scale-guard plan-proof: ok (changed large-table SQL vs %s carries at-scale plan proof)\n", base)
		return 0
	}
	fmt.Fprintf(os.Stderr, "scale-guard plan-proof: %d changed statement(s) over large tables lack an at-scale plan proof (PP-22: a plan validated at STG size is not a proof):\n\n", len(missing))
	for _, st := range missing {
		fmt.Fprintln(os.Stderr, planProofMessage(st))
	}
	return 1
}
