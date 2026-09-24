package readcache

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// WRITER GUARD. The analytics read cache is only correct if EVERY writer of a table its reads
// depend on publishes an eviction (readcache.NotifyTx inside the write transaction, or
// readcache.CommitAndEvict). This test finds every function in backend/internal and backend/cmd
// whose SQL (inline literal, package-level const/var, or a sqlc-generated query it calls) writes
// one of those tables, and fails when that function does not invalidate -- directly, through a
// same-package helper that does, or (for a helper that takes the caller's pgx.Tx) through every
// caller doing so. The dependency table list is the traced SQL of the cached reads
// (docs/perf/2026-09-24-stg-latency/audit/cache-correctness.md section 2).

var cachedReadDependencyTables = []string{
	"goats", "goat_shed_partitions", "goat_identifiers", "procurement_load_goats",
	"feed_purchases", "feed_direction_issues", "feed_direction_issue_rows",
	"locations", "shed_partitions", "animal_stage_lookup",
	"growth_assumptions", "growth_sale_price_assumptions",
	"weighing_campaigns", "weighing_campaign_sheds", "weighing_observations",
	"weighing_shed_observations", "weighing_shed_load_tags",
}

// writerAllowlist names writers that deliberately do not invalidate, with the reason. Keep it
// short: every entry is a place a stale page can come from.
var writerAllowlist = map[string]string{
	"internal/procurement/adapters/postgres/feed_purchase_repository.go:Repository.RecordFeedPurchasePayment":    "writes payment columns only (payment_released/payment_status); no cached read selects them",
	"internal/procurement/adapters/postgres/feed_purchase_repository.go:Repository.SetFeedPurchasePaymentStatus": "writes payment_status only; no cached read selects it",
	"internal/weighing/adapters/postgres/repository.go:Repository.completeIndividualScopeIfDone":                 "unused helper (no caller); any future caller commits through commitAndEvict",
	"cmd/seed-roster-real/main.go:importRoster":                                                                  "dev/staging seed CLI; run on an empty or maintenance database, API restarted after",
	"cmd/seed-vaccination-per-goat-qa/main.go:run":                                                               "QA seed CLI; not run against a live API",
	"cmd/seed-vaccination-real/main.go:purgeSyntheticFixtures":                                                   "seed CLI; not run against a live API",
	"cmd/seed-vaccination-real/main.go:retireActiveNonSourceLocations":                                           "seed CLI; not run against a live API",
	"cmd/seed-vaccination-real/main.go:seed":                                                                     "seed CLI; not run against a live API",
	"cmd/seed-vaccination-real/main.go:seedAnimalStageLookup":                                                    "seed CLI; not run against a live API",
	"cmd/seed-vaccination-real/main.go:upsertSeedGoatIdentifiers":                                                "seed CLI; not run against a live API",
	"cmd/seed-vaccination-real/main.go:upsertSeedGoats":                                                          "seed CLI; not run against a live API",
	"cmd/seed-vaccination-trigger/main.go:run":                                                                   "seed CLI; not run against a live API",
	"cmd/seed-weighing-fixture/main.go:importFixture":                                                            "dev fixture seed CLI; not run against a live API",
}

// Row-lock clauses ("FOR UPDATE OF goats", "FOR NO KEY UPDATE") are reads, not writes.
var forUpdate = regexp.MustCompile(`(?is)\bfor\s+(?:no\s+key\s+)?update\b`)

var writeSQL = func() *regexp.Regexp {
	return regexp.MustCompile(`(?is)\b(?:insert\s+into|update|delete\s+from)\s+(?:public\.)?(` + strings.Join(cachedReadDependencyTables, "|") + `)\b`)
}()

type fnInfo struct {
	pkgDir     string
	file       string
	name       string // Recv.Name or Name
	bare       string // Name
	text       string // source of the body
	writes     []string
	takesTx    bool
	callsNames map[string]bool
	callsSQLC  []string
}

func backendRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd()
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

func TestEveryWriterOfACachedReadDependencyInvalidates(t *testing.T) {
	missing := uninvalidatedWriters(t, backendRoot(t), writerAllowlist)
	if len(missing) > 0 {
		t.Fatalf("%d writer(s) of cached-read dependency tables publish no read-cache eviction "+
			"(call readcache.CommitAndEvict / readcache.NotifyTx in the write transaction, or allowlist with a reason):\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// The guard must catch the shapes it exists for: an inline write, a write through a package
// const, a tx helper whose caller commits without publishing -- and accept the published ones.
func TestWriterGuardCatchesUnpublishedWriters(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/bad/repo.go", `package bad
const moveSQL = "UPDATE goats SET shed_id = $1"
type Repo struct{}
func (r *Repo) Inline(ctx any, tx Tx) { tx.Exec(ctx, "DELETE FROM goat_identifiers WHERE x"); tx.Commit(ctx) }
func (r *Repo) ViaConst(ctx any) { exec(ctx, moveSQL); commit() }
func helper(ctx any, tx pgx.Tx) { tx.Exec(ctx, "INSERT INTO procurement_load_goats VALUES (1)") }
func (r *Repo) Caller(ctx any, tx pgx.Tx) { helper(ctx, tx); tx.Commit(ctx) }
func (r *Repo) Locks(ctx any, tx pgx.Tx) { tx.Exec(ctx, "SELECT 1 FROM goats FOR UPDATE") }
`)
	write("internal/good/repo.go", `package good
type Repo struct{}
func helper(ctx any, tx pgx.Tx) { tx.Exec(ctx, "UPDATE goats SET sex = 'f'") }
func (r *Repo) Published(ctx any, tx pgx.Tx) { helper(ctx, tx); readcache.CommitAndEvict(ctx, tx, nil, "t") }
func (r *Repo) Notified(ctx any, tx pgx.Tx) { tx.Exec(ctx, "UPDATE feed_purchases SET x=1"); readcache.NotifyTx(ctx, tx, "t") }
`)
	got := strings.Join(uninvalidatedWriters(t, root, nil), "\n")
	for _, want := range []string{"Repo.Inline writes goat_identifiers", "Repo.ViaConst writes goats", "helper writes procurement_load_goats"} {
		if !strings.Contains(got, want) {
			t.Fatalf("guard missed %q; found:\n%s", want, got)
		}
	}
	for _, clean := range []string{"good/", "Repo.Locks"} {
		if strings.Contains(got, clean) {
			t.Fatalf("guard flagged a published writer or a row lock (%q):\n%s", clean, got)
		}
	}
}

func uninvalidatedWriters(t *testing.T, root string, allowlist map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	type pkgConsts map[string]string
	consts := map[string]pkgConsts{} // pkgDir -> name -> sql text
	var fns []*fnInfo
	sqlcWriters := map[string]bool{} // generated method names that write a dependency table
	var files []string
	for _, dir := range []string{"internal", "cmd"} {
		filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			files = append(files, p)
			return nil
		})
	}
	sources := map[string][]byte{}
	parsed := map[string]*ast.File{}
	for _, p := range files {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			continue
		}
		sources[p], parsed[p] = src, f
		dir := filepath.Dir(p)
		if consts[dir] == nil {
			consts[dir] = pkgConsts{}
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						consts[dir][name.Name] = litText(vs.Values[i])
					}
				}
			}
		}
	}
	for _, p := range files {
		f := parsed[p]
		if f == nil {
			continue
		}
		src := sources[p]
		dir := filepath.Dir(p)
		generated := strings.Contains(string(src[:min(len(src), 400)]), "Code generated by sqlc")
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			info := &fnInfo{pkgDir: dir, file: p, bare: fd.Name.Name, callsNames: map[string]bool{}}
			info.name = fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				info.name = recvName(fd.Recv.List[0].Type) + "." + fd.Name.Name
			}
			info.text = string(src[fset.Position(fd.Body.Pos()).Offset:fset.Position(fd.Body.End()).Offset])
			for _, prm := range fd.Type.Params.List {
				typ := prm.Type
				if st, ok := typ.(*ast.StarExpr); ok {
					typ = st.X
				}
				if se, ok := typ.(*ast.SelectorExpr); ok && (se.Sel.Name == "Tx" || se.Sel.Name == "Queries") {
					info.takesTx = true
				}
			}
			var sqlText strings.Builder
			importsSQLC := false
			for _, imp := range f.Imports {
				if strings.Contains(imp.Path.Value, "/sqlc") || strings.HasSuffix(strings.Trim(imp.Path.Value, `"`), "db") {
					importsSQLC = true
				}
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						sqlText.WriteString(unquote(x.Value))
						sqlText.WriteString("\n")
					}
				case *ast.Ident:
					if s, ok := consts[dir][x.Name]; ok {
						sqlText.WriteString(s)
						sqlText.WriteString("\n")
					}
				case *ast.CallExpr:
					switch fn := x.Fun.(type) {
					case *ast.SelectorExpr:
						info.callsNames[fn.Sel.Name] = true
						if importsSQLC {
							info.callsSQLC = append(info.callsSQLC, fn.Sel.Name)
						}
					case *ast.Ident:
						info.callsNames[fn.Name] = true
					}
				}
				return true
			})
			seen := map[string]bool{}
			for _, m := range writeSQL.FindAllStringSubmatch(forUpdate.ReplaceAllString(sqlText.String(), " "), -1) {
				tbl := strings.ToLower(m[1])
				if !seen[tbl] {
					seen[tbl] = true
					info.writes = append(info.writes, tbl)
				}
			}
			if generated {
				if len(info.writes) > 0 {
					sqlcWriters[fd.Name.Name] = true
				}
				continue
			}
			fns = append(fns, info)
		}
	}
	// A call to a sqlc-generated writer makes the caller a writer.
	for _, fn := range fns {
		for _, name := range fn.callsSQLC {
			if sqlcWriters[name] && len(fn.writes) == 0 {
				fn.writes = append(fn.writes, "(sqlc "+name+")")
			}
		}
	}
	// Invalidating functions: call readcache's publishers, or (transitively, same package) a
	// function that does.
	invalidating := map[*fnInfo]bool{}
	byPkgName := map[string][]*fnInfo{}
	for _, fn := range fns {
		byPkgName[fn.pkgDir+"#"+fn.bare] = append(byPkgName[fn.pkgDir+"#"+fn.bare], fn)
		if strings.Contains(fn.text, "readcache.NotifyTx(") || strings.Contains(fn.text, "readcache.CommitAndEvict(") ||
			strings.Contains(fn.text, "readcache.QueueNotify(") ||
			(fn.pkgDir == filepath.Join(root, "internal", "platform", "readcache")) {
			invalidating[fn] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, fn := range fns {
			if invalidating[fn] {
				continue
			}
			for name := range fn.callsNames {
				for _, callee := range byPkgName[fn.pkgDir+"#"+name] {
					if invalidating[callee] && calleeCommitsOrNotifies(callee) {
						invalidating[fn] = true
						changed = true
					}
				}
			}
		}
	}
	callersOf := map[string][]*fnInfo{}
	for _, fn := range fns {
		for name := range fn.callsNames {
			callersOf[name] = append(callersOf[name], fn)
		}
	}
	// An unexported helper can only be called from its own package; an exported one from anywhere.
	callersFor := func(fn *fnInfo) []*fnInfo {
		all := callersOf[fn.bare]
		if ast.IsExported(fn.bare) {
			return all
		}
		var same []*fnInfo
		for _, c := range all {
			if c.pkgDir == fn.pkgDir {
				same = append(same, c)
			}
		}
		return same
	}
	var covered func(fn *fnInfo, depth int, seen map[*fnInfo]bool) bool
	covered = func(fn *fnInfo, depth int, seen map[*fnInfo]bool) bool {
		if invalidating[fn] {
			return true
		}
		if !fn.takesTx || depth > 5 || seen[fn] {
			return false
		}
		seen[fn] = true
		callers := callersFor(fn)
		if len(callers) == 0 {
			return false
		}
		for _, c := range callers {
			if c == fn {
				continue
			}
			if !covered(c, depth+1, seen) {
				return false
			}
		}
		return true
	}
	var missing []string
	for _, fn := range fns {
		if len(fn.writes) == 0 {
			continue
		}
		rel, _ := filepath.Rel(root, fn.file)
		key := rel + ":" + fn.name
		if _, ok := allowlist[key]; ok {
			continue
		}
		if !covered(fn, 0, map[*fnInfo]bool{}) {
			line := key + " writes " + strings.Join(fn.writes, ",")
			if fn.takesTx {
				var via []string
				for _, c := range callersFor(fn) {
					if c != fn && !covered(c, 1, map[*fnInfo]bool{}) {
						crel, _ := filepath.Rel(root, c.file)
						via = append(via, crel+":"+c.name)
					}
				}
				sort.Strings(via)
				if len(via) == 0 {
					via = []string{"(no caller found)"}
				}
				line += " -- tx helper; uncovered caller(s): " + strings.Join(via, "; ")
			}
			missing = append(missing, line)
		}
	}
	sort.Strings(missing)
	return missing
}

// calleeCommitsOrNotifies: calling a same-package publisher helper covers the caller.
func calleeCommitsOrNotifies(*fnInfo) bool { return true }

func recvName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return recvName(x.X)
	}
	return "?"
}

func litText(e ast.Expr) string {
	var b strings.Builder
	ast.Inspect(e, func(n ast.Node) bool {
		if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			b.WriteString(unquote(bl.Value))
		}
		return true
	})
	return b.String()
}

func unquote(s string) string {
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}
