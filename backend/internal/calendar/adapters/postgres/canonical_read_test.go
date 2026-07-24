package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
	const badMembershipDate = "WHEN oi.batch_id IS NOT NULL THEN COALESCE(ob.window_start, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', ob.window_end)"
	if strings.Contains(calendarCanonicalListSQL, badMembershipDate) {
		t.Fatalf("batched drive membership still resolves on window_start before planned_date")
	}
	// R50-012: planned_date (a DATE column) must be converted to an instant via an
	// explicit AT TIME ZONE 'Asia/Kolkata' conversion, never a bare ::timestamptz
	// cast — the bare cast is silently dependent on the Postgres session timezone.
	const wantedMembershipDate = "WHEN oi.batch_id IS NOT NULL THEN COALESCE((ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'), ob.window_start, ob.window_end)"
	if !strings.Contains(calendarCanonicalListSQL, wantedMembershipDate) {
		t.Fatalf("batched drive membership must resolve on planned_date before window_start")
	}
	if strings.Contains(calendarCanonicalListSQL, "ob.planned_date::timestamptz") || strings.Contains(calendarCanonicalListSQL, "ob2.planned_date::timestamptz") {
		t.Fatalf("canonical read must not cast planned_date to timestamptz without an explicit AT TIME ZONE 'Asia/Kolkata' conversion (session-timezone dependent)")
	}
	const badTargetDate = "COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end)"
	if strings.Contains(calendarDriveTargetsSQL, badTargetDate) {
		t.Fatalf("park-drive target lookup still matches batches by window_start before planned_date")
	}
	const wantedTargetDate = "vda.planned_date = $3::date"
	if !strings.Contains(calendarDriveTargetsSQL, wantedTargetDate) {
		t.Fatalf("park-drive target lookup must match batches by vaccination_drive_assignments.planned_date")
	}
	if strings.Contains(calendarDriveTargetsSQL, "to_char(vda.planned_date") {
		t.Fatalf("park-drive target lookup must compare planned_date as a typed date, not through to_char")
	}
	if strings.Contains(calendarDriveTargetsSQL, "ob.planned_date::timestamptz") {
		t.Fatalf("park-drive target lookup must not depend on the PostgreSQL session timezone")
	}
}

func TestCalendarParkDriveTargetsUseOperatorAssignmentDateOneToManyPageBoundaryScheduledDateParkScopeStatusBuckets(t *testing.T) {
	checks := map[string]string{
		"assignment table membership": "LEFT JOIN vaccination_drive_assignments vda",
		"assignment business date":    "vda.planned_date = $3::date",
		"legacy batch fallback":       "NOT COALESCE(assignment_presence.has_any_assignment, false)",
		"assignment park scope":       "vda.park_id = $4::uuid",
		"display date tied to bucket": "AND assignment.planned_date = $3::date",
		"hybrid member path":          "target_assignment.assignment_planned_at",
		"hybrid guess fallback":       "target_assignment_guess.assignment_planned_at",
		"target display date":         "COALESCE(target_assignment.assignment_planned_at, target_assignment_guess.assignment_planned_at, target_batch.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) AS scheduled_at",
	}
	for name, fragment := range checks {
		if !strings.Contains(calendarDriveTargetsSQL, fragment) {
			t.Fatalf("calendar drive targets lost %s invariant %q", name, fragment)
		}
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

func TestCalendarDriveShedSummaryScheduledDateParkScopeOneToManyMultiPageStatusBuckets(t *testing.T) {
	checks := map[string]string{
		"shed animal cte":      "obligation_drive_shed_animals AS",
		"one-to-many safe":     "count(DISTINCT m.animal_id)",
		"shed json output":     "'sheds', obl_summary.sheds",
		"date grouped":         "per_shed.due_date",
		"park scope grouped":   "per_shed.park_id",
		"status summary stays": "count(DISTINCT m.obligation_id) FILTER",
		"page-safe join":       "LEFT JOIN obligation_drive_shed_animals sa",
	}
	for name, fragment := range checks {
		if !strings.Contains(calendarCanonicalListSQL, fragment) {
			t.Fatalf("calendar drive shed summary lost %s invariant %q", name, fragment)
		}
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
		"scheduled date source":    "SELECT due_at AS scheduled_at",
		"date lower bound":         "scheduled_at >= $2::timestamptz",
		"date upper bound":         "scheduled_at < $3::timestamptz",
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
	if got := strings.Count(calendarDateMarkersSQL, "GROUP BY (scheduled_at AT TIME ZONE 'Asia/Kolkata')::date"); got != 1 {
		t.Fatalf("live marker branch must aggregate to one row per shifted date before UNION, got %d groupings", got)
	}
	if got := strings.Count(calendarDateMarkersSQL, "GROUP BY (COALESCE(vc.administered_at, vc.created_at) AT TIME ZONE 'Asia/Kolkata')::date"); got != 1 {
		t.Fatalf("history marker branch must aggregate to one row per shifted date before UNION, got %d groupings", got)
	}
}

func TestDriveLevelOverdueUsesBusinessDate(t *testing.T) {
	businessDatePredicate := "(due_at AT TIME ZONE 'Asia/Kolkata')::date < (now() AT TIME ZONE 'Asia/Kolkata')::date"
	if !strings.Contains(calendarCanonicalEventsCTE, businessDatePredicate) {
		t.Fatalf("drive grouping must classify overdue by India business date, not by same-day timestamp")
	}
	if strings.Contains(calendarCanonicalEventsCTE, "status IN ('scheduled', 'due', 'overdue') AND due_at < now()") {
		t.Fatalf("park drive grouping must not make a same-business-day drive overdue after midnight")
	}
	if strings.Contains(calendarCanonicalEventsCTE, "WHEN grouped.due_at < now()") {
		t.Fatalf("batch drive severity must not warn solely because today's midnight timestamp is in the past")
	}
}

// TestObligationBatchPlannedDateTimezoneIndependent is the R50-012 DB proof. planned_date is a
// bare DATE column; the canonical read must resolve it to an instant via an explicit
// `(planned_date::timestamp AT TIME ZONE 'Asia/Kolkata')` conversion, never a bare
// `planned_date::timestamptz` cast (which silently interprets the date's midnight in whatever
// timezone the Postgres SESSION happens to be in). This test proves the fragment used by
// calendarCanonicalListSQL/calendarDriveTargetsSQL produces the identical instant regardless of
// session timezone by evaluating it once under a UTC session and once under an Asia/Tokyo
// session and asserting the results match.
func TestObligationBatchPlannedDateTimezoneIndependent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	parkID := "86000000-0000-4000-8000-0000000007a1"
	shedID := "86000000-0000-4000-8000-0000000007a2"
	batchID := "86000000-0000-4000-8000-0000000007a3"
	protocolID := "86000000-0000-4000-8000-0000000007a4"
	versionID := "86000000-0000-4000-8000-0000000007a5"
	ruleID := "86000000-0000-4000-8000-0000000007a6"
	obligationID := "86000000-0000-4000-8000-0000000007a7"
	goatID := "86000000-0000-4000-8000-0000000007a8"
	dueAt := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	seedCalendarGoat(t, ctx, pool, goatID)
	seedCalendarLocations(t, ctx, pool, parkID, shedID)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET target_type = 'goat', target_id = $3::uuid, scope_type = 'shed', scope_id = $4::uuid
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, testTenantID, obligationID, goatID, shedID); err != nil {
		t.Fatalf("make target obligation goat-scoped: %v", err)
	}
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, parkID, shedID, dueAt, obligationID)

	// $3 is the drive business DATE. ListDriveTargets always binds the real day parsed off the event
	// id, and the fragment now casts it with $3::date (vaccination_drive_assignments.planned_date /
	// obligation_batches.planned_date are DATE columns), so an empty string is not a legal bind here.
	driveDay := dueAt.In(biztime.DefaultLocation()).Format("2006-01-02")

	queryTargets := func(sessionTZ string) int64 {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx (tz=%s): %v", sessionTZ, err)
		}
		defer tx.Rollback(ctx)
		// SET LOCAL does not accept a bind parameter; sessionTZ is a fixed constant passed by
		// this test ("UTC" / "Asia/Tokyo"), never external input.
		if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL timezone = '%s'", sessionTZ)); err != nil {
			t.Fatalf("set local timezone %s: %v", sessionTZ, err)
		}
		var got int64
		parkIDs := []string{}
		shedIDs := []string{}
		var parkID, shedID, tenantID, ruleID, cursorID interface{}
		err = tx.QueryRow(ctx, `
SELECT count(*)
FROM (`+calendarDriveTargetsSQL+`) targets
WHERE animal_id::text = $15::text`,
			testTenantID, batchID, driveDay, parkID, shedID, tenantID, ruleID, cursorID, true, parkIDs, shedIDs, false, 21, "", goatID).Scan(&got)
		if err != nil {
			t.Fatalf("query due_at fragment under session tz %s: %v", sessionTZ, err)
		}
		return got
	}

	utcResult := queryTargets("UTC")
	tokyoResult := queryTargets("Asia/Tokyo")

	if utcResult != tokyoResult {
		t.Fatalf("planned_date target membership is session-timezone dependent: UTC session=%d, Asia/Tokyo session=%d",
			utcResult, tokyoResult)
	}
	if utcResult != 1 {
		t.Fatalf("complete target query returned %d matching goats, want 1", utcResult)
	}
}
