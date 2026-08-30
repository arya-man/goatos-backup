// Command scaleguard is a zero-dependency static guard that blocks the
// million-animal scale anti-patterns catalogued in
// docs/decisions/scale-anti-patterns.md.
//
// It walks Go source under backend/internal (request-path adapters, app
// services, and worker repo methods) and reports:
//
//   - n-plus-one         : a DB call (.Query/.QueryRow/.Exec/.SendBatch) inside a
//     for/range loop body.
//   - n-plus-one-fanout  : a ctx-taking call to an injected I/O dependency
//     (repo/reader/port/client/roster/ownership) inside a for/range loop. The
//     real driver call sits one adapter layer down, so the raw-driver n-plus-one
//     rule cannot see it. This is the ShedSummary owner-enrichment class:
//     "small data, still slow" = one round trip per row.
//   - loop-no-cursor      : an infinite `for {}` that calls a paging repo method
//     (List*/Fetch*/*Page) without any cursor/progress guard
//     in the loop body — the park-consolidation hang class.
//   - offset-pagination  : an OFFSET clause in a SQL string literal (use keyset).
//   - full-mv-refresh     : DELETE FROM <...projection...> WHERE tenant_id with NO
//     projection_version guard = a stop-the-world whole-tenant
//     rebuild. A version-scoped prune (projection_version <>)
//     is fine and is NOT flagged.
//   - non-sargable-like   : lower(col) LIKE '%..%' (unindexable leading wildcard).
//   - non-sargable-cast   : casts an indexed column to text in an ANY predicate
//     (e.g. `id::text = ANY(...)`); bind a typed UUID/text array instead.
//   - god-cte             : a single SQL literal with too many "x AS (" CTEs on a
//     request path (compute-on-read; move to a read model).
//   - read-rollup-truth   : request-path/service-layer rollup that bumps a raw
//     list limit or clears pagination after in-memory aggregation, presenting a
//     partial summary as business truth.
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
	"time"
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
	// A cast on the column side of an equality/ANY predicate prevents the
	// ordinary index on the underlying UUID/text column from being used directly.
	// Cast the bind array instead: `id = ANY($1::uuid[])`.
	nonSargableCastRe = regexp.MustCompile(`(?is)\b(?:[a-z_][a-z0-9_]*\.)?[a-z_][a-z0-9_]*\s*::\s*(?:text|varchar)\s*=\s*ANY\s*\(`)
	delProjRe         = regexp.MustCompile(`(?is)DELETE\s+FROM\s+[a-z_]*projection[a-z_]*\b[^;]*\btenant_id`)
	readRollupFnRe    = regexp.MustCompile(`(?i)\b(aggregate|group|rollup)\w*(List|Events)\b`)
	rawLimitBumpRe    = regexp.MustCompile(`(?i)\.Limit\s*=\s*\w*(aggregate|raw|rollup)\w*Limit`)
	nextCursorNilRe   = regexp.MustCompile(`(?i)\bNextCursor\s*=\s*nil`)
	// A version-scoped prune (build-new / flip / drop-old generations) is the
	// APPROVED pattern, not a whole-tenant wipe. Do not treat range predicates
	// like projection_version > 0 as safe; those can still delete the serving set.
	versionPruneGuardRe = regexp.MustCompile(`(?i)projection_version\s*(<>|!=)`)
	// Paging repo methods whose loops must prove forward progress.
	pageMethodRe = regexp.MustCompile(`^(List|Fetch)|Page$|Chunk$`)
	ignoreRe     = regexp.MustCompile(`scale-guard:ignore:\s*\S`)
	// hot-path-inline-sql: a multi-line SQL literal declared INSIDE a function body in a postgres
	// adapter. Such a statement is unreachable to every guard this repo has -- a query-plan test and
	// this scanner can only address SQL they can NAME -- so it is structurally exempt from review.
	// GET /vaccination/command carried FOURTEEN of them and its plans regressed until the endpoint
	// returned 500 in staging. Hoist it to a package-level const (or SQLC) and give it a plan test.
	inlineSQLStartRe = regexp.MustCompile(`(?is)^\s*(WITH|SELECT|INSERT|UPDATE|DELETE)\b`)
	// Leading SQL comments and blank lines are stripped before the start match. A
	// "-- name: closed-without-dose residual bucket" header is this repo's HOUSE STYLE, so anchoring
	// the match at the literal's first character made the rule a pure false negative for the most
	// idiomatic way to write the very statement it exists to catch -- no evasion intent required.
	inlineSQLLeadingCommentRe = regexp.MustCompile(`(?m)\A(?:\s*(?:--[^\n]*|/\*.*?\*/)?\s*\n)+`)
	inlineSQLBodyRe           = regexp.MustCompile(`(?is)\bFROM\b|\bJOIN\b|\bWHERE\b`)
	godCTELimit               = 8
	// n-plus-one-fanout: the receiver field of an in-loop ctx-taking call must
	// name an injected I/O dependency (repo/reader/port/client/roster/proto/...)
	// for the call to count as a round trip. Descriptive field naming is the
	// codebase norm (s.repo, s.ownership, s.proto); an undescriptively-named
	// dependency is missed here but caught by the raw-driver rule or a review.
	depFieldRe = regexp.MustCompile(`(?i)(repo|repository|store|reader|writer|dao|conn|pool|client|gateway|remote|svc|service|ownership|owner|roster|adapter|port|proto|projector|loader|finder|resolver|lookup|queue|publisher|producer|consumer|sink|cache|source|backend)`)
	// Known ctx-taking-but-not-I/O receivers (logging, tracing, metrics, config,
	// context itself) are excluded so the rule stays high-precision.
	noiseFieldRe = regexp.MustCompile(`(?i)^(log|logger|logs|slog|tracer|trace|span|meter|metric|metrics|obs|clock|ctx|context|cfg|config|opts|options)$`)
)

func main() {
	root := flag.String("root", ".", "repo root to scan")
	baseline := flag.String("baseline", "", "baseline file of accepted per-(rule,file) counts")
	flag.Parse()

	repo, err := filepath.Abs(*root)
	must(err)
	// Scan product + worker repo logic only. backend/internal holds every request
	// path, app service, and worker repo method — i.e. every hot path that must
	// hold at 1-5M animals. backend/cmd/obligation-sweeper and other durable workers
	// have production hot paths too (must hold under concurrent load). One-time tooling
	// (backend/cmd/seed-*, migrate) is intentionally out of scope: a seed doing N+1 or
	// delete+reinsert runs once and never serves traffic. Scan backend/ for both.
	scanRoot := repo
	for _, cand := range []string{filepath.Join(repo, "backend"), filepath.Join(repo, "internal"), repo} {
		if _, err := os.Stat(cand); err == nil {
			scanRoot = cand
			break
		}
	}
	if *baseline == "" {
		*baseline = filepath.Join(repo, "tools", "scale-guard", "baseline.txt")
	}
	allowed, baselineErrors := loadBaseline(*baseline, time.Now().UTC())
	if len(baselineErrors) > 0 {
		for _, baselineErr := range baselineErrors {
			fmt.Fprintln(os.Stderr, "scale-guard: baseline:", baselineErr)
		}
		os.Exit(1)
	}

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
		if isExplicitOneTimeCommand(repo, path) {
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
		if total == 0 {
			fmt.Println("scale-guard: CERTIFIED (zero known or new offenders)")
		} else {
			fmt.Printf("scale-guard: RATCHET PASS — NOT SCALE CERTIFIED (%d time-bounded known offenders across %d rule/file groups; zero new)\n", total, len(allowed))
		}
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

var explicitOneTimeCommands = map[string]bool{
	"backend/cmd/migrate/main.go":                     true,
	"backend/cmd/seed-dev-email-grants/main.go":       true,
	"backend/cmd/seed-position-duties/main.go":        true,
	"backend/cmd/seed-roster-real/main.go":            true,
	"backend/cmd/seed-shed-positions/main.go":         true,
	"backend/cmd/seed-vaccination-real/main.go":       true,
	"backend/cmd/seed-vaccination-trigger/main.go":    true,
	"backend/cmd/counts-projection-recompute/main.go": true,
	"backend/cmd/counts-source-import/main.go":        true,
	"backend/cmd/legacy-god-sheet-sync/main.go":       true,
}

func isExplicitOneTimeCommand(repo, path string) bool {
	rel, err := filepath.Rel(repo, path)
	if err != nil {
		return false
	}
	return explicitOneTimeCommands[filepath.ToSlash(rel)]
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
	addLine := func(rule string, line int, msg string) {
		if ignored[line] || ignored[line-1] {
			return
		}
		out = append(out, finding{rule: rule, rel: rel, line: line, msg: msg})
	}

	if line, msg := detectReadRollupTruth(src); line > 0 {
		addLine("read-rollup-truth", line, msg)
	}

	// hot-path-inline-sql runs only in postgres adapters, where a multi-line SQL literal inside a
	// function is by definition a hot-path statement no guard can reach.
	if isPostgresAdapter(rel) {
		for _, f := range detectInlineHotPathSQL(file) {
			add("hot-path-inline-sql", f.pos, f.msg)
		}
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

		// n-plus-one (raw driver) and n-plus-one-fanout (cross-boundary) in the loop.
		ast.Inspect(body, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if dbCallSel[sel.Sel.Name] {
				add("n-plus-one", call.Pos(),
					fmt.Sprintf("DB call .%s(...) inside a loop; batch it (UNNEST / multi-row) or hoist out of the loop", sel.Sel.Name))
				return true
			}
			// A ctx-first call to a named I/O dependency = one round trip per
			// iteration even though the driver call is an adapter layer down.
			if isFanoutCall(call, sel) {
				add("n-plus-one-fanout", call.Pos(),
					fmt.Sprintf("cross-boundary call .%s(ctx, ...) on dependency %q inside a loop; one round trip per row. Batch it (a single *ByIDs / = ANY($1) read) or hoist out of the loop", sel.Sel.Name, recvName(sel)))
			}
			return true
		})

		// loop-no-cursor: an infinite for{} that pages a repo method with no
		// cursor/progress guard anywhere in the body (the park-consolidation hang).
		if infinite {
			// A paging loop is guarded when EITHER an argument it passes to the
			// paging call is reassigned in the body (a keyset cursor advances) OR
			// the body has a zero-progress break (`if counter == 0 { break }`).
			// Both are checked precisely rather than by loose identifier substring,
			// so an incidental name like `...DateAfter` does not mask a real hang,
			// and the ubiquitous `if len(rows) == 0` empty-page exit is not mistaken
			// for a progress guard.
			var pageCall *ast.SelectorExpr
			argIdents := map[string]bool{}
			assigned := map[string]bool{}
			zeroBreak := false
			ast.Inspect(body, func(m ast.Node) bool {
				switch e := m.(type) {
				case *ast.CallExpr:
					if sel, ok := e.Fun.(*ast.SelectorExpr); ok &&
						pageMethodRe.MatchString(sel.Sel.Name) && !dbCallSel[sel.Sel.Name] {
						pageCall = sel
						for _, a := range e.Args {
							if id, ok := a.(*ast.Ident); ok {
								argIdents[id.Name] = true
							}
						}
					}
				case *ast.AssignStmt:
					for _, lhs := range e.Lhs {
						if id, ok := lhs.(*ast.Ident); ok {
							assigned[id.Name] = true
						}
					}
				case *ast.IfStmt:
					if isProgressBreak(e) {
						zeroBreak = true
					}
				}
				return true
			})
			cursorAdvances := false
			for name := range argIdents {
				if assigned[name] {
					cursorAdvances = true
					break
				}
			}
			if pageCall != nil && !zeroBreak && !cursorAdvances {
				add("loop-no-cursor", pageCall.Pos(),
					fmt.Sprintf("infinite for{} pages %s(...) with no cursor advance or zero-progress break; can loop forever if returned rows are never mutated out", pageCall.Sel.Name))
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
		if nonSargableCastRe.MatchString(v) {
			add("non-sargable-cast", lit.Pos(),
				"casting an indexed column to text in an ANY predicate can disable its index; cast the bind array instead (column = ANY($1::uuid[]))")
		}
		if c := len(cteRe.FindAllString(v, -1)); c > godCTELimit {
			add("god-cte", lit.Pos(),
				fmt.Sprintf("%d CTEs in one request-path query = compute-on-read. Serve from a materialized read model", c))
		}
		return true
	})
	return out
}

// isPostgresAdapter reports whether rel is a repository adapter file. The rule is scoped to these
// because that is where serving SQL lives; a SQL literal in a migration tool or a one-off command
// is not a hot path and naming it buys nothing.
func isPostgresAdapter(rel string) bool {
	rel = filepath.ToSlash(rel)
	return strings.Contains(rel, "/adapters/postgres/") && strings.HasSuffix(rel, ".go")
}

type inlineSQLFinding struct {
	pos token.Pos
	msg string
}

// detectInlineHotPathSQL finds multi-line SQL string literals declared inside a function body.
//
// A package-level const or var passes: it has a name, so commandboard_query_plan_test.go (or any
// plan test) can EXPLAIN it, this scanner can find it, and a reviewer can diff it. A literal inside
// a function has none of those properties. That is not a style preference -- it is the reason the
// command board's fourteen statements could regress into a 15s timeout with every test green.
//
// MULTI-LINE is the threshold, deliberately. A one-line "SELECT 1" or a short single-table lookup is
// not the shape that hides a plan regression, and flagging it would train people to reach for
// scale-guard:ignore, which is how a guard stops meaning anything.
func detectInlineHotPathSQL(file *ast.File) []inlineSQLFinding {
	var out []inlineSQLFinding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// BOTH literal kinds. Restricting this to raw literals left a one-keystroke evasion
			// (backtick -> quote plus \n escapes) that gofmt will not undo, and an interpreted
			// literal carrying a whole statement is the same unreachable hot-path SQL.
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if strings.Count(text, "\n") < 3 {
				return true
			}
			// Strip leading comment/blank lines before deciding whether this is SQL.
			body := inlineSQLLeadingCommentRe.ReplaceAllString(text, "")
			if !inlineSQLStartRe.MatchString(body) || !inlineSQLBodyRe.MatchString(body) {
				return true
			}
			out = append(out, inlineSQLFinding{
				pos: lit.Pos(),
				msg: "multi-line SQL declared inside " + fn.Name.Name + "(): hoist it to a package-level named const (or SQLC) so a query-plan test and this guard can reach it",
			})
			return true
		})
	}
	return out
}

func detectReadRollupTruth(src []byte) (int, string) {
	lines := strings.Split(string(src), "\n")
	aggregateLine := firstMatchingLine(lines, readRollupFnRe)
	if aggregateLine == 0 {
		return 0, ""
	}
	if limitLine := firstMatchingLine(lines, rawLimitBumpRe); limitLine > 0 {
		return limitLine, "request-path rollup bumps a raw list limit before in-memory aggregation; move drive grouping into a projector/read model and keep page truth bounded"
	}
	if cursorLine := firstMatchingLine(lines, nextCursorNilRe); cursorLine > 0 {
		return cursorLine, "request-path rollup clears NextCursor after in-memory aggregation; do not hide truncation or pagination when summarizing business truth"
	}
	return 0, ""
}

func firstMatchingLine(lines []string, re *regexp.Regexp) int {
	for i, line := range lines {
		if re.MatchString(line) {
			return i + 1
		}
	}
	return 0
}

// isFanoutCall reports whether an in-loop method call is a cross-boundary round
// trip: its first argument is a context and its receiver names an injected I/O
// dependency. Raw driver calls (Query/Exec/...) are handled by n-plus-one and
// must not reach here.
func isFanoutCall(call *ast.CallExpr, sel *ast.SelectorExpr) bool {
	if dbCallSel[sel.Sel.Name] {
		return false
	}
	if len(call.Args) == 0 || !isCtxArg(call.Args[0]) {
		return false
	}
	name := recvName(sel)
	if name == "" || noiseFieldRe.MatchString(name) {
		return false
	}
	return depFieldRe.MatchString(name)
}

// recvName returns the receiver field/identifier name of a selector call:
// "ownership" for s.ownership.ShedOwnership(...), "repo" for repo.Get(...).
func recvName(sel *ast.SelectorExpr) string {
	switch x := sel.X.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

// isCtxArg reports whether an expression is a context argument (ctx / reqCtx /
// groupCtx / c). The first-arg-is-context convention is the I/O boundary signal.
func isCtxArg(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	n := strings.ToLower(id.Name)
	return n == "ctx" || n == "c" || strings.Contains(n, "ctx")
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

// loadBaseline accepts only owned, expiring exceptions. A ratchet entry without
// owner/issue/expiry/reason is itself a CI failure; permanent anonymous debt is
// the false-green condition this guard is meant to prevent.
func loadBaseline(path string, now time.Time) (map[string]int, []string) {
	out := map[string]int{}
	var problems []string
	f, err := os.Open(path)
	if err != nil {
		return out, []string{err.Error()}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := strings.TrimSpace(sc.Text())
		line, metadata := raw, ""
		if i := strings.Index(raw, "#"); i >= 0 {
			line = strings.TrimSpace(raw[:i])
			metadata = strings.TrimSpace(raw[i+1:])
		}
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			problems = append(problems, fmt.Sprintf("line %d malformed", lineNo))
			continue
		}
		count := 1
		if len(fields) >= 3 {
			if n, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
				count = n
			}
		}
		owner := metadataValue(metadata, "owner")
		issue := metadataValue(metadata, "issue")
		expires := metadataValue(metadata, "expires")
		reason := metadataValue(metadata, "reason")
		if owner == "" || issue == "" || expires == "" || reason == "" {
			problems = append(problems, fmt.Sprintf("line %d requires owner= issue= expires= reason= metadata", lineNo))
			continue
		}
		expiry, err := time.Parse("2006-01-02", expires)
		if err != nil {
			problems = append(problems, fmt.Sprintf("line %d has invalid expires=%q", lineNo, expires))
			continue
		}
		if !expiry.After(now.Truncate(24 * time.Hour)) {
			problems = append(problems, fmt.Sprintf("line %d exception expired on %s", lineNo, expires))
			continue
		}
		out[fields[0]+"\t"+fields[1]] += count
	}
	return out, problems
}

func metadataValue(metadata, key string) string {
	needle := key + "="
	start := strings.Index(metadata, needle)
	if start < 0 {
		return ""
	}
	value := metadata[start+len(needle):]
	if key == "reason" {
		return strings.TrimSpace(value)
	}
	if end := strings.IndexByte(value, ' '); end >= 0 {
		value = value[:end]
	}
	return strings.TrimSpace(value)
}

// isProgressBreak reports whether an if-stmt is a zero-progress guard: a
// condition comparing a plain counter variable to 0 (== / <= / <) whose body
// breaks or returns. `len(x) == 0` (the empty-page exit) is deliberately NOT a
// match — its compared operand is a call, not a bare counter — so only a real
// "no work happened this page -> stop" guard counts.
func isProgressBreak(ifs *ast.IfStmt) bool {
	counterZero := false
	ast.Inspect(ifs.Cond, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if be.Op != token.EQL && be.Op != token.LEQ && be.Op != token.LSS {
			return true
		}
		if (isZeroLit(be.Y) && isPlainIdent(be.X)) || (isZeroLit(be.X) && isPlainIdent(be.Y)) {
			counterZero = true
		}
		return true
	})
	if !counterZero {
		return false
	}
	stops := false
	ast.Inspect(ifs.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.BranchStmt:
			if s.Tok == token.BREAK {
				stops = true
			}
		case *ast.ReturnStmt:
			stops = true
		}
		return true
	})
	return stops
}

func isZeroLit(e ast.Expr) bool {
	bl, ok := e.(*ast.BasicLit)
	return ok && bl.Kind == token.INT && bl.Value == "0"
}

func isPlainIdent(e ast.Expr) bool {
	_, ok := e.(*ast.Ident)
	return ok
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "scale-guard: fatal:", err)
		os.Exit(2)
	}
}
