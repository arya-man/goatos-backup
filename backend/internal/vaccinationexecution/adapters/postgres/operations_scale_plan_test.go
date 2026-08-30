package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestVaccinationOperationsAggregateQueryPlanUsesIndexesAtScale is the 500k-ENVELOPE GATE for the
// vaccinationexecution operations aggregate (vaccinationOperationsSQL) at the ADR upper bound (~500k
// obligation rows, operational-kernel-5k-50k-scale-envelope.md step 4). The existing plan test
// (canonical_read_plan_test.go / TestVaccinationOperationsProductionQueryPlanUsesIndexes) EXPLAINs a tiny
// fixture with enable_seqscan=off, which only proves an index EXISTS — not that the planner CHOOSES it at
// scale or that cost stays bounded. Here we bulk-load ~500k canonical obligations, ANALYZE so the planner
// uses real statistics, then EXPLAIN (ANALYZE, BUFFERS) the operations query over a bounded due window
// WITHOUT forcing enable_seqscan off. The executed plan must (a) reach obligation_instances through an index
// path, never a Seq Scan, (b) touch only the bounded due window at that scan (actual rows << 500k), and
// (c) keep estimated total cost far below a full-table-scan aggregate — proving the operations read stays
// index-bound at scale instead of degrading to a compute-on-read full sequential scan.
func TestVaccinationOperationsAggregateQueryPlanUsesIndexesAtScale(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	seedVaccinationExecutionLargeObligationFixture(t, ctx, pool, 500_000)

	// Refresh planner statistics after the bulk load so the plan reflects the ~500k-row reality, not the
	// stale near-empty estimate. Mirrors the mandatory post-seed ANALYZE contract in AGENTS.md.
	execProjectionSQL(t, ctx, pool, "analyze canonical tables at scale",
		`ANALYZE obligation_instances, obligation_batches, vaccination_completions, obligation_status_events,
		   goats, locations, protocol_versions, protocol_definitions, protocol_rules`)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	// A real operator window is a bounded date range near the low end of the fixture. The operations SQL
	// bounds obligations with `due_at <= dueBefore` (no lower bound), so anchoring dueBefore just above the
	// earliest bulk due_at leaves only ~1 day (~1.4k rows) qualifying, and the tenant+due_at index must
	// prune the remaining ~500k rows. asOf sits inside that window.
	asOf := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	dueBefore := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

	// $1 tenant, $2 asOf, $3 dueBefore, $4 park, $5 shed, $6 cursorPark, $7 cursorShed, $8 cursorStage,
	// $9 limit, $10 cursorParkName, $11 cursorShedName. No SET enable_seqscan = off: at ~500k rows a
	// genuinely index-bound aggregate must be the planner's own choice, not a forced one.
	res := explainAnalyzeJSON(t, ctx, tx, vaccinationOperationsSQL,
		testTenant, asOf, dueBefore, "", "", "", "", "", 21, "", "")

	// obligationRowCeiling: the driving obligation_instances scan must touch only the bounded due window
	// (~1.4k rows here), NEVER the whole ~500k table; 50k is a huge margin over the window yet an order of
	// magnitude below a full-table scan. costCeiling: the estimated total cost must sit far below a 500k
	// sequential-scan operations plan (a full seq scan of the wide obligation table alone costs tens of
	// thousands here plus the CTE joins/sorts); the natural index plan measured ~2.6k, so 8k leaves index
	// headroom while a compute-on-read regression trips it.
	const (
		obligationRowCeiling = 50_000.0
		costCeiling          = 8_000.0
	)
	assertAggregateIndexBoundAtScale(t, "vaccinationOperationsSQL", res, costCeiling, obligationRowCeiling)
}

// seedVaccinationExecutionLargeObligationFixture bulk-loads count distinct canonical obligations via one
// set-based INSERT ... SELECT generate_series, targeting a dedicated scale goat in the seeded shed/park so
// the UNIQUE(tenant, protocol_version, rule, target_type, target, due_at) dup guard never collides with the
// hand-seeded rows and each row gets a distinct due_at + id/idempotency_key/sequence. The join tables stay
// tiny, so the fixture stresses the obligation_instances index path specifically — the dominant cost of the
// operations aggregate at scale.
func seedVaccinationExecutionLargeObligationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, count int) {
	t.Helper()
	const veScaleGoat = "70000000-0000-4000-8000-000000000090"
	execProjectionSQL(t, ctx, pool, "scale goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
		   current_location_id, park_id, shed_id, management_stage, health_status)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K1', 'healthy')`,
		veScaleGoat, testTenant, testParty, testShed, testPark)
	// due_at = 2026-06-14 00:00:00+00 + g minutes gives each obligation a unique due_at; only the first ~1.4k
	// land inside the operations aggregate's `due_at <= 2026-06-15` window and the ~498k tail sits beyond it,
	// so the plan must prune the ~500k-row table on the tenant+due_at index. sequence starts above the
	// hand-seeded rows.
	execProjectionSQL(t, ctx, pool, "bulk canonical obligations at scale", `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id,
  target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence
)
SELECT
  gen_random_uuid(),
  $1::uuid,
  $2::uuid,
  $3::uuid,
  'goat',
  $4::uuid,
  'shed',
  $5::uuid,
  TIMESTAMPTZ '2026-06-14 00:00:00+00' + (g || ' minutes')::interval,
  'scheduled',
  've-scale-' || g::text,
  1000 + g
FROM generate_series(1, $6::int) AS g`,
		testTenant, testVersion, testRule, veScaleGoat, testShed, count)
}

// pgxQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, so the 500k-envelope EXPLAIN helper can run on
// either a pooled connection or a rolled-back transaction.
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// explainPlanNode is one node of an EXPLAIN (FORMAT JSON) plan tree. Only the fields the 500k-envelope gate
// asserts on are decoded.
type explainPlanNode struct {
	NodeType     string  `json:"Node Type"`
	RelationName string  `json:"Relation Name"`
	TotalCost    float64 `json:"Total Cost"`
	PlanRows     float64 `json:"Plan Rows"`
	ActualRows   float64 `json:"Actual Rows"`
	// ActualLoops is how many times the node was EXECUTED. It is the difference between a small
	// scan and a small scan repeated ten thousand times, which is the shape a nested-loop inner
	// scan and a repeated CTE re-scan both take -- neither of which is visible in Actual Rows
	// alone. commandboard_query_plan_test.go gates on rows x loops for exactly that reason.
	ActualLoops float64 `json:"Actual Loops"`
	// CTEName names the CTE a "CTE Scan" node reads. A materialised CTE re-scanned once per outer
	// row is the exact shape that produced the command board's 500, and naming it is the difference
	// between a usable failure message and "some CTE was re-scanned".
	CTEName string `json:"CTE Name"`
	// RowsRemovedByJoinFilter is how many rows a join evaluated and threw away, PER LOOP. A nested
	// loop chosen off a bad row estimate shows up here and almost nowhere else: the cohort-exception
	// probes discarded 13.9 MILLION rows this way while every node's own row count stayed small.
	RowsRemovedByJoinFilter float64 `json:"Rows Removed by Join Filter"`
	// RowsRemovedByFilter is how many rows a SCAN evaluated and discarded, per loop. A scan node
	// reports only the rows that survived its filter, so without this a statement that reads the
	// whole tenant and returns forty rows looks like a forty-row read.
	RowsRemovedByFilter float64           `json:"Rows Removed by Filter"`
	Plans               []explainPlanNode `json:"Plans"`
}

// explainAnalyzeResult is one top-level EXPLAIN (ANALYZE, FORMAT JSON) result object.
type explainAnalyzeResult struct {
	Plan          explainPlanNode `json:"Plan"`
	ExecutionTime float64         `json:"Execution Time"`
}

// explainAnalyzeJSON runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) on a parameterized query and returns the
// executed plan tree (real statistics: actual rows + estimated cost). ANALYZE means the query is actually
// run, so the returned actual-row counts are ground truth, not planner guesses. It is used by the
// 500k-envelope gate WITHOUT enable_seqscan disabled so the access path is the planner's own choice.
func explainAnalyzeJSON(t *testing.T, ctx context.Context, q pgxQuerier, sql string, args ...any) explainAnalyzeResult {
	t.Helper()
	rows, err := q.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)\n"+sql, args...)
	if err != nil {
		t.Fatalf("explain analyze query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			t.Fatalf("explain analyze: no plan row: %v", err)
		}
		t.Fatalf("explain analyze: no plan row returned")
	}
	var raw []byte
	if err := rows.Scan(&raw); err != nil {
		t.Fatalf("scan explain json: %v", err)
	}
	rows.Close()
	var parsed []explainAnalyzeResult
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal explain json: %v\nraw=%s", err, string(raw))
	}
	if len(parsed) == 0 {
		t.Fatalf("explain analyze: empty plan array\nraw=%s", string(raw))
	}
	return parsed[0]
}

// collectRelationScans walks the plan tree collecting every node whose base relation is rel (e.g. every
// scan of obligation_instances, which the operations query may reference more than once).
func collectRelationScans(n explainPlanNode, rel string, out *[]explainPlanNode) {
	if n.RelationName == rel {
		*out = append(*out, n)
	}
	for _, c := range n.Plans {
		collectRelationScans(c, rel, out)
	}
}

// planHasIndexAccess reports whether any node in the tree uses an index access path (Index Scan, Index Only
// Scan, or Bitmap Index Scan).
func planHasIndexAccess(n explainPlanNode) bool {
	if strings.Contains(n.NodeType, "Index") {
		return true
	}
	for _, c := range n.Plans {
		if planHasIndexAccess(c) {
			return true
		}
	}
	return false
}

// assertAggregateIndexBoundAtScale enforces the 500k-envelope thresholds on one executed aggregate plan:
// the driving obligation_instances scan must be an index path (no Seq Scan), must touch only the bounded
// due window (actual rows below obligationRowCeiling, i.e. NOT the whole ~500k table), the plan must use an
// index access path overall, and the estimated total cost must stay below costCeiling.
func assertAggregateIndexBoundAtScale(t *testing.T, label string, res explainAnalyzeResult, costCeiling, obligationRowCeiling float64) {
	t.Helper()
	root := res.Plan
	var oblScans []explainPlanNode
	collectRelationScans(root, "obligation_instances", &oblScans)
	if len(oblScans) == 0 {
		t.Fatalf("%s @500k: obligation_instances not referenced in executed plan; cannot prove index-bound access", label)
	}
	var maxObligationRows float64
	for _, s := range oblScans {
		if strings.Contains(s.NodeType, "Seq Scan") {
			t.Fatalf("%s @500k: obligation_instances hit a %q (compute-on-read regression); rootCost=%.0f execTime=%.1fms",
				label, s.NodeType, root.TotalCost, res.ExecutionTime)
		}
		if s.ActualRows > maxObligationRows {
			maxObligationRows = s.ActualRows
		}
	}
	if !planHasIndexAccess(root) {
		t.Fatalf("%s @500k: no index access path anywhere in executed plan", label)
	}
	if maxObligationRows > obligationRowCeiling {
		t.Fatalf("%s @500k: obligation_instances scan touched %.0f rows (> ceiling %.0f) — lost due-window selectivity, effectively a full scan",
			label, maxObligationRows, obligationRowCeiling)
	}
	if root.TotalCost > costCeiling {
		t.Fatalf("%s @500k: estimated total cost %.0f exceeds envelope ceiling %.0f (aggregate no longer bounded at scale)",
			label, root.TotalCost, costCeiling)
	}
	t.Logf("%s @500k index-bound: rootCost=%.0f rootActualRows=%.0f maxObligationScanRows=%.0f execTime=%.1fms",
		label, root.TotalCost, root.ActualRows, maxObligationRows, res.ExecutionTime)
}
