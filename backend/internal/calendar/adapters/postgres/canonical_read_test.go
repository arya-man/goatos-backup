package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestCalendarCanonicalListKeysetPlanUsesIndex is the query-plan gate for the U4a canonical
// read-through (calendarCanonicalListSQL). It proves two things against a freshly migrated, empty
// database:
//
//  1. The full assembled canonical read is a valid, plannable query. EXPLAIN resolves every table
//     and column reference in the composed CTE chain, so a broken fragment splice fails here rather
//     than at request time.
//  2. The scale-critical access path — the tenant+due-window keyset page over obligation_instances
//     that drives the whole read — is index-backed, never a sequential scan. This is the "keyset,
//     ~20 rows" plan the ADR (operational-kernel-5k-50k-scale-envelope) requires for a
//     compute-on-read Calendar list at the 5k-to-50k envelope.
//
// The bash sibling `validate_calendar_canonical_read_plan` in
// backend/tests/integration/validate-sqlc-query-plans.sh runs the same obligation_instances keyset
// EXPLAIN under `make validate-sqlc-plans`.
func TestCalendarCanonicalListKeysetPlanUsesIndex(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	dateFrom := time.Now().Add(-time.Hour)
	dateToExclusive := time.Now().Add(45 * 24 * time.Hour)

	// (1) The full canonical read must plan without error against the real schema.
	explainRows, err := pool.Query(ctx, "EXPLAIN (COSTS OFF) "+calendarCanonicalListSQL,
		testTenantID, dateFrom, dateToExclusive, "", "", "", "",
		nil, "", 21, true, []string{}, []string{}, "")
	if err != nil {
		t.Fatalf("canonical read did not plan (composition/schema error): %v", err)
	}
	var full []string
	for explainRows.Next() {
		var line string
		if err := explainRows.Scan(&line); err != nil {
			explainRows.Close()
			t.Fatal(err)
		}
		full = append(full, line)
	}
	explainRows.Close()
	if err := explainRows.Err(); err != nil {
		t.Fatal(err)
	}
	fullPlan := strings.Join(full, "\n")
	if strings.Contains(fullPlan, "Seq Scan on obligation_instances") {
		t.Fatalf("canonical read sequentially scans obligation_instances (the keyset driver):\n%s", fullPlan)
	}

	// (2) The obligation_instances tenant+due-window keyset page — the scan that bounds the whole
	// canonical read — must be index-backed. Prove it in isolation with enable_seqscan off so the
	// planner cannot hide an unindexed scan behind cheap empty-table estimates.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN (COSTS OFF)
SELECT obligation_id
FROM obligation_instances
WHERE tenant_id = $1::uuid
  AND batch_id IS NULL
  AND due_at >= $2::timestamptz
  AND due_at < $3::timestamptz
ORDER BY due_at ASC, obligation_id ASC
LIMIT 21`, testTenantID, dateFrom, dateToExclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(lines, "\n")
	if strings.Contains(plan, "Seq Scan on obligation_instances") {
		t.Fatalf("canonical read keyset driver used a sequential scan:\n%s", plan)
	}
	if !strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Index Only Scan") && !strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("canonical read keyset driver is not index-backed:\n%s", plan)
	}
}

func TestCalendarDefaultListHidesDeferredHoldsButDateMarkersExposeThem(t *testing.T) {
	const defaultOpenPredicate = "status NOT IN ('completed', 'canceled', 'deferred')"
	if !strings.Contains(calendarCanonicalListSQL, defaultOpenPredicate) {
		t.Fatalf("calendar list default predicate does not hide deferred holds")
	}
	if strings.Contains(calendarDateMarkersSQL, defaultOpenPredicate) {
		t.Fatalf("calendar date marker default predicate must expose deferred holds for month status markers")
	}
	if !strings.Contains(calendarDateMarkersSQL, "count(*) FILTER (WHERE status = 'deferred')::bigint AS deferred_count") {
		t.Fatalf("calendar date marker query must expose deferred_count")
	}
}

func TestCalendarBatchedDriveRosterAndTargetsUsePlannedDateScheduledDateParkScopeOneToManyMultiPageStatusBuckets(t *testing.T) {
	const badMembershipDate = "WHEN oi.batch_id IS NOT NULL THEN COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end)"
	if strings.Contains(calendarCanonicalListSQL, badMembershipDate) {
		t.Fatalf("batched drive membership still resolves on window_start before planned_date")
	}
	const wantedMembershipDate = "WHEN oi.batch_id IS NOT NULL THEN COALESCE(ob.planned_date::timestamptz, ob.window_start, ob.window_end)"
	if !strings.Contains(calendarCanonicalListSQL, wantedMembershipDate) {
		t.Fatalf("batched drive membership must resolve on planned_date before window_start")
	}
	const badTargetDate = "COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end)"
	if strings.Contains(calendarDriveTargetsSQL, badTargetDate) {
		t.Fatalf("park-drive target lookup still matches batches by window_start before planned_date")
	}
	const wantedTargetDate = "COALESCE(ob.planned_date::timestamptz, ob.window_start, ob.window_end)"
	if !strings.Contains(calendarDriveTargetsSQL, wantedTargetDate) {
		t.Fatalf("park-drive target lookup must match batches by planned_date before window_start")
	}
}

func TestCalendarParkDriveScheduledCountIgnoresCompletedBatchSourcesStatusBucketsScheduledDateParkScopeOneToManyMultiPage(t *testing.T) {
	const oldBucket = "sum(target_count) FILTER (WHERE source_target_type = 'batch')::int AS scheduled_count"
	if strings.Contains(calendarCanonicalListSQL, oldBucket) {
		t.Fatalf("park drive scheduled_count still counts completed batch sources")
	}
	const statusBucket = "AND status IN ('scheduled', 'due', 'overdue', 'in_progress', 'proof_pending', 'verification_pending', 'rejected', 'rework_due')"
	if !strings.Contains(calendarCanonicalListSQL, statusBucket) {
		t.Fatalf("park drive scheduled_count must be filtered by active scheduled/review statuses")
	}
}

func TestCalendarVaccineFilterOneToManyMultiPageDateShiftParkScopeStatusBuckets(t *testing.T) {
	vaccinePredicate := "AND (\n      $14::text = ''"
	keysetPredicate := "AND ($8::timestamptz IS NULL"
	limitClause := "LIMIT $10"
	vaccineIndex := strings.Index(calendarCanonicalListSQL, vaccinePredicate)
	keysetIndex := strings.Index(calendarCanonicalListSQL, keysetPredicate)
	limitIndex := strings.Index(calendarCanonicalListSQL, limitClause)
	if vaccineIndex < 0 || keysetIndex < 0 || limitIndex < 0 {
		t.Fatalf("canonical vaccine, keyset, or limit clause is missing")
	}
	if vaccineIndex > keysetIndex || vaccineIndex > limitIndex {
		t.Fatalf("vaccine filtering must happen before keyset paging and LIMIT")
	}
	if !strings.Contains(calendarCanonicalListSQL, "(detail->'summary'->'vaccine_labels') ? $14::text") {
		t.Fatalf("aggregated drive vaccine labels are not filterable")
	}
	historyStart := strings.Index(calendarDateMarkersSQL, "-- Accepted vaccination administration history")
	if historyStart < 0 {
		t.Fatal("canonical history marker branch is missing")
	}
	historyBranch := calendarDateMarkersSQL[historyStart:]
	if strings.Contains(historyBranch, "LEFT JOIN protocol_rule_dimensions") {
		t.Fatal("history markers must not fan out one completion across protocol-rule dimensions")
	}
	if !strings.Contains(historyBranch, "OR EXISTS (\n        SELECT 1\n        FROM protocol_rule_dimensions prd") {
		t.Fatal("history vaccine filtering must use a cardinality-preserving EXISTS predicate")
	}
	for name, fragment := range map[string]string{
		"date shift":        "AT TIME ZONE 'Asia/Kolkata'",
		"park scope":        "park_id::text = nullif($6::text, '')",
		"status buckets":    "count(*) FILTER (WHERE status = 'overdue')",
		"published options": "SELECT DISTINCT COALESCE(",
	} {
		if !strings.Contains(calendarDateMarkersSQL+calendarFilterOptionsSQL, fragment) {
			t.Fatalf("calendar vaccine filter lost %s invariant %q", name, fragment)
		}
	}
}

func TestCalendarDateMarkersStatusBucketsDateShiftParkScopeMultiPageOneToMany(t *testing.T) {
	checks := map[string]string{
		"date lower bound":         "due_at >= $2::timestamptz",
		"date upper bound":         "due_at < $3::timestamptz",
		"india date shift":         "AT TIME ZONE 'Asia/Kolkata'",
		"park scope":               "park_id::text = nullif($6::text, '')",
		"shed scope":               "shed_id::text = nullif($7::text, '')",
		"authorization scope":      "park_id = ANY($9::uuid[]) OR shed_id = ANY($10::uuid[])",
		"open status bucket":       "count(*) FILTER (WHERE status NOT IN ('completed', 'canceled'))::bigint AS open_count",
		"due status bucket":        "count(*) FILTER (WHERE status = 'due')::bigint AS due_count",
		"overdue status bucket":    "count(*) FILTER (WHERE status = 'overdue')::bigint AS overdue_count",
		"deferred status bucket":   "count(*) FILTER (WHERE status = 'deferred')::bigint AS deferred_count",
		"completed status bucket":  "count(*)::bigint AS completed_count",
		"history one-to-many base": "FROM vaccination_completions vc",
		"final one row per date":   "GROUP BY marker_date\nORDER BY marker_date",
	}
	for name, fragment := range checks {
		if !strings.Contains(calendarDateMarkersSQL, fragment) {
			t.Fatalf("calendar date marker query lost %s invariant %q", name, fragment)
		}
	}
	if strings.Contains(calendarDateMarkersSQL, "LIMIT ") {
		t.Fatalf("calendar date marker aggregation must not page inside the month; the caller pages event lists only")
	}
	if got := strings.Count(calendarDateMarkersSQL, "GROUP BY (due_at AT TIME ZONE 'Asia/Kolkata')::date"); got != 1 {
		t.Fatalf("live marker branch must aggregate to one row per shifted date before UNION, got %d groupings", got)
	}
	if got := strings.Count(calendarDateMarkersSQL, "GROUP BY (COALESCE(vc.administered_at, vc.created_at) AT TIME ZONE 'Asia/Kolkata')::date"); got != 1 {
		t.Fatalf("history marker branch must aggregate to one row per shifted date before UNION, got %d groupings", got)
	}
}
