package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestProtocolAdherenceReadsPageAndSummaryInOneStatement pins the fix for the /protocol-adherence
// 500s (Blocked tab: "iterate projection rows: timeout: context deadline exceeded"): the adherence
// list path must not run the canonical CTE twice (page + summary on two connections). It must call
// fetchCanonicalRowsAndSummary, never issue processIntegrityCanonicalAdherenceSummarySQL itself, and
// the combined statement must carry the same summary aggregate and keyset page as the reads it
// replaces. Runs in the default suite (no database).
func TestProtocolAdherenceReadsPageAndSummaryInOneStatement(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "repository.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var body *ast.BlockStmt
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "listRowsCanonical" {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatal("listRowsCanonical not found")
	}
	uses := map[string]int{}
	ast.Inspect(body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			uses[id.Name]++
		}
		if _, ok := n.(*ast.GoStmt); ok {
			t.Errorf("listRowsCanonical spawns a goroutine again: parallel canonical reads doubled the DB work and overran the timeout")
		}
		return true
	})
	if uses["fetchCanonicalRowsAndSummary"] == 0 {
		t.Error("listRowsCanonical must serve IncludeAdherenceSummary through fetchCanonicalRowsAndSummary")
	}
	if uses["processIntegrityCanonicalAdherenceSummarySQL"] > 0 {
		t.Error("listRowsCanonical issues the separate adherence summary read again")
	}

	sql := processIntegrityCanonicalRowsAndSummarySQL
	if got := maxPlaceholderStatic(sql); got != rowsQueryArgCount {
		t.Fatalf("rows+summary placeholders = %d, want %d", got, rowsQueryArgCount)
	}
	for _, frag := range []string{
		"visible AS MATERIALIZED (",
		"WHERE ($14::boolean OR work_state <> 'completed' OR due_at >= $11::timestamptz)",
		"COALESCE(SUM(expected_count), 0)::integer",
		"COALESCE(SUM(completed_count), 0)::integer",
		"COUNT(*) FILTER (WHERE NOT process_intact)::integer",
		"GREATEST(deferred_count, 1)",
		"COUNT(*) FILTER (WHERE process_intact)::integer",
		"LIMIT $20",
		"LEFT JOIN page ON true",
		"page.row_id IS NULL AS page_empty",
	} {
		if !strings.Contains(sql, frag) {
			t.Errorf("rows+summary statement lost %q", frag)
		}
	}
	for _, w := range ConnWarmups() {
		if w.SQL == processIntegrityCanonicalAdherenceSummarySQL {
			t.Error("warm the statement the request path runs (rows+summary), not the retired summary read")
		}
	}
}

func maxPlaceholderStatic(sql string) int {
	max := 0
	for i := 0; i < len(sql); i++ {
		if sql[i] != '$' {
			continue
		}
		n, j := 0, i+1
		for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
			n = n*10 + int(sql[j]-'0')
			j++
		}
		if n > max {
			max = n
		}
	}
	return max
}
