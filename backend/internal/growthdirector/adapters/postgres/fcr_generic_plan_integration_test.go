package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// THE GENERIC-PLAN TRAP (measured 2026-09-24 on the OCI clone). pgx caches the FCR statements as
// prepared statements, so from the sixth execution Postgres may switch to a GENERIC plan. The
// generic plan cannot see the size of the same-animal map or the park set, estimates every CTE at
// a handful of rows, and joined scan_rounds to ITSELF (cur x prev) as a nested loop of two CTE
// scans: 2,131 x 2,531 row comparisons, 720 ms of a 730 ms statement, while the first five
// (custom-planned) calls took ~40 ms. The cure is structural, not a plan hint: the segments
// statement must never join a materialized CTE to itself, so no plan shape can turn the pairing
// into a quadratic loop.
func TestFCRSegmentsGenericPlanNeverSelfJoinsScanRounds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)

	for name, stmt := range map[string]string{"segments": fcrSegmentsSQL, "pens": fcrPensSQL} {
		args := []any{gdTenant, []string{gdPark}, nil, nil, "", []string{}, []string{}, "2026-07-01", "2026-08-01"}
		if name == "pens" { // the pens statement also takes the General-ADG kid filter ($10/$11)
			args = append(args, false, []string{})
		}
		bound, err := sqlbind.Bind(stmt, args...)
		if err != nil {
			t.Fatalf("%s bind: %v", name, err)
		}
		// Simple protocol, no binds: EXPLAIN (GENERIC_PLAN) plans the $n statement as a prepared one.
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		res, err := conn.Conn().PgConn().Exec(ctx, "EXPLAIN (GENERIC_PLAN, FORMAT JSON) "+bound.SQL()).ReadAll()
		conn.Release()
		if err != nil || len(res) != 1 || len(res[0].Rows) != 1 {
			t.Fatalf("%s explain generic: %v", name, err)
		}
		raw := res[0].Rows[0][0]
		var plans []struct {
			Plan map[string]any `json:"Plan"`
		}
		if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
			t.Fatalf("%s decode plan: %v", name, err)
		}
		if node, cte := fcrPlanCTESelfJoin(plans[0].Plan); node != "" {
			t.Fatalf("%s: generic plan joins CTE %q to itself under a %s node (quadratic under a misestimate)", name, cte, node)
		}
	}
}

// fcrPlanCTESelfJoin returns the first Nested Loop whose INNER input is a bare CTE Scan (re-read
// in full for every outer row) of a CTE the outer input also reads: a self-join of a materialized
// CTE, quadratic in its size.
func fcrPlanCTESelfJoin(n map[string]any) (string, string) {
	children, _ := n["Plans"].([]any)
	nodeType, _ := n["Node Type"].(string)
	if nodeType == "Nested Loop" && len(children) == 2 {
		outer, _ := children[0].(map[string]any)
		inner, _ := children[1].(map[string]any)
		if it, _ := inner["Node Type"].(string); it == "CTE Scan" {
			cte, _ := inner["CTE Name"].(string)
			if fcrPlanCTEScans(outer)[cte] {
				return nodeType, cte
			}
		}
	}
	for _, c := range children {
		if cm, ok := c.(map[string]any); ok {
			if node, cte := fcrPlanCTESelfJoin(cm); node != "" {
				return node, cte
			}
		}
	}
	return "", ""
}

func fcrPlanCTEScans(n map[string]any) map[string]bool {
	out := map[string]bool{}
	if t, _ := n["Node Type"].(string); t == "CTE Scan" {
		if name, _ := n["CTE Name"].(string); name != "" {
			out[name] = true
		}
	}
	if children, ok := n["Plans"].([]any); ok {
		for _, c := range children {
			if cm, ok := c.(map[string]any); ok {
				for k := range fcrPlanCTEScans(cm) {
					out[k] = true
				}
			}
		}
	}
	return out
}
