package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// testShedB is a second shed under the same park as testShed, used only by the shed-projection
// parity test to get status/capacity variety (an overdue, capacity-breached shed) alongside
// testShed's plain "due" shed from seedVaccinationExecutionProjection, without disturbing any
// other test's fixture on testShed.
const testShedB = "70000000-0000-4000-8000-000000000099"

// TestRecomputeShedProjectionMatchesLiveShedSummary is the C35-002 (vaccinationexecution half)
// parity proof: RecomputeShedProjection must reproduce, row for row, exactly what an independent
// from-first-principles reconstruction of shed status (canonicalShedSummarySQLForParity below --
// the same god-CTE ShedSummary served over the request path before the C35-002 flip) computes for
// the same as_of. Production code no longer contains that god-CTE; ShedSummary (repository.go) now
// reads this projection exclusively. This test proves the projection is a faithful substitute, not a
// tautology against itself.
func TestRecomputeShedProjectionMatchesLiveShedSummary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	seedShedProjectionParityFixture(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 7, 11, 18, 0, 0, 0, time.UTC)
	dueBefore := asOf.Add(30 * 24 * time.Hour)

	// Before any recompute, the tenant has no serving projection version yet, and the flipped
	// ShedSummary must refuse a canonical compute-on-read fallback (C35-002).
	if _, ok := repo.shedProjectionServingVersion(ctx, testTenant); ok {
		t.Fatalf("expected no serving shed projection version before the first recompute")
	}
	if _, err := repo.ShedSummary(ctx, domain.ShedSummaryQuery{TenantID: testTenant, AsOf: asOf, DueBefore: dueBefore, Limit: 200}); !errors.Is(err, domain.ErrProjectionUnavailable) {
		t.Fatalf("ShedSummary before first recompute: err = %v, want ErrProjectionUnavailable (no canonical fallback)", err)
	}

	// canonicalMaxPerDay/canonicalMaxBufferDays mirror seedShedProjectionParityFixture's tight
	// capacity config (max_per_day=2, max_buffer_days=1) so the independent canonical query and
	// RecomputeShedProjection's own CapacityConfig read use the same session-split inputs.
	canonical := queryCanonicalShedSummaryForParity(t, ctx, pool, testTenant, asOf, dueBefore, 2, 1)
	if len(canonical) < 2 {
		t.Fatalf("fixture setup failed: got %d canonical shed rows, want >= 2 (testShed + testShedB)", len(canonical))
	}

	result, err := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{
		TenantID: testTenant,
		AsOf:     asOf,
	})
	if err != nil {
		t.Fatalf("RecomputeShedProjection: %v", err)
	}
	if result.Rows != int64(len(canonical)) {
		t.Fatalf("projection rows = %d, want %d (same as canonical shed row count)", result.Rows, len(canonical))
	}

	servingVersion, ok := repo.shedProjectionServingVersion(ctx, testTenant)
	if !ok || servingVersion != result.ProjectionVersion {
		t.Fatalf("serving version = %d ok=%v, want %d after commit", servingVersion, ok, result.ProjectionVersion)
	}

	projected := readShedProjectionRows(t, ctx, pool, testTenant, result.ProjectionVersion)
	if len(projected) != len(canonical) {
		t.Fatalf("projected rows = %d, want %d (parity with canonical shed summary)", len(projected), len(canonical))
	}

	canonicalByShed := make(map[string]domain.ShedSummaryProjection, len(canonical))
	for _, row := range canonical {
		canonicalByShed[row.ShedID] = row
	}

	sawOverdue, sawDue := false, false
	for _, p := range projected {
		c, ok := canonicalByShed[p.ShedID]
		if !ok {
			t.Fatalf("projection has shed %s not present in canonical shed summary output", p.ShedID)
		}
		if !shedProjectionRowsEqual(p, c) {
			t.Fatalf("projection row for shed %s does not match canonical shed summary row:\n  canonical: %#v\n  projected: %#v", p.ShedID, c, p)
		}
		if p.Status == domain.ShedStatusOverdue {
			sawOverdue = true
		}
		if p.Status == domain.ShedStatusDue {
			sawDue = true
		}
	}
	if !sawOverdue || !sawDue {
		t.Fatalf("fixture setup failed: parity check needs both a due shed and an overdue shed, got overdue=%v due=%v", sawOverdue, sawDue)
	}

	// Now that a serving version exists, the flipped ShedSummary (the actual production request
	// path) must serve the same rows the parity check just proved match the canonical query --
	// proving the request-path flip itself, not just the projector, is correct.
	live, err := repo.ShedSummary(ctx, domain.ShedSummaryQuery{TenantID: testTenant, AsOf: asOf, DueBefore: dueBefore, Limit: 200})
	if err != nil {
		t.Fatalf("ShedSummary after recompute: %v", err)
	}
	if len(live) != len(canonical) {
		t.Fatalf("ShedSummary (flipped request path) rows = %d, want %d (parity with canonical shed summary)", len(live), len(canonical))
	}
	for _, l := range live {
		c, ok := canonicalByShed[l.ShedID]
		if !ok {
			t.Fatalf("ShedSummary has shed %s not present in canonical shed summary output", l.ShedID)
		}
		if !shedProjectionRowsEqual(l, c) {
			t.Fatalf("ShedSummary row for shed %s does not match canonical shed summary row:\n  canonical: %#v\n  live:      %#v", l.ShedID, c, l)
		}
	}

	// A second recompute (a later projection_version) must still match canonical output and must not
	// leave the earlier version's rows lying around forever -- pruneOldShedProjectionRows runs at
	// the end of every recompute. Sleep past the millisecond-resolution version stamp so the two
	// runs are guaranteed distinct versions.
	time.Sleep(2 * time.Millisecond)
	second, err := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{TenantID: testTenant, AsOf: asOf})
	if err != nil {
		t.Fatalf("RecomputeShedProjection (second run): %v", err)
	}
	if second.ProjectionVersion == result.ProjectionVersion {
		t.Fatalf("second recompute reused the same projection_version %d; expected a new version", second.ProjectionVersion)
	}
	firstVersionRows := readShedProjectionRows(t, ctx, pool, testTenant, result.ProjectionVersion)
	if len(firstVersionRows) != 0 {
		t.Fatalf("first projection_version %d still has %d rows after the second recompute pruned it", result.ProjectionVersion, len(firstVersionRows))
	}
}

// queryCanonicalShedSummaryForParity runs canonicalShedSummarySQLForParity -- a TEST-ONLY, from-first-
// principles reconstruction of shed status straight from raw obligation/goat/capacity-config history,
// independent of RecomputeShedProjection -- and returns every tenant shed ordered by shed_id (no
// filter/pagination, unlike the request-path shape) so the parity test can compare it against the
// projection row for row.
func queryCanonicalShedSummaryForParity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, asOf, dueBefore time.Time, maxPerDay, maxBufferDays int) []domain.ShedSummaryProjection {
	t.Helper()
	rows, err := pool.Query(ctx, canonicalShedSummarySQLForParity, tenantID, asOf, dueBefore, maxPerDay, maxBufferDays)
	if err != nil {
		t.Fatalf("canonical shed summary parity query: %v", err)
	}
	defer rows.Close()

	out := []domain.ShedSummaryProjection{}
	for rows.Next() {
		var row domain.ShedSummaryProjection
		var capacityStatus, shedStatus string
		var lastDone, nextDue *time.Time
		if err := rows.Scan(
			&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName,
			&row.Animals, &row.DueAnimals, &row.OpenCells, &row.Sessions,
			&capacityStatus, &shedStatus, &lastDone, &nextDue,
		); err != nil {
			t.Fatalf("scan canonical shed summary parity row: %v", err)
		}
		row.Capacity = domain.CapacityStatus(capacityStatus)
		row.Status = domain.ShedStatus(shedStatus)
		row.LastDone = lastDone
		row.NextDue = nextDue
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate canonical shed summary parity rows: %v", err)
	}
	return out
}

// canonicalShedSummarySQLForParity is a TEST-ONLY copy of the compute-on-read god-CTE that used to
// serve GET /vaccination/sheds before the C35-002 request-path flip (see repository.go's
// shedSummaryProjectedSQL, which replaced it). It survives here ONLY so this parity test has an
// independent, from-first-principles answer to compare RecomputeShedProjection's output against --
// production code never executes this query again. Mirrors the alive/completions/asof_terminal/raw/
// effective/due_agg/shed_rows/scored/classified chain vaccinationShedProjectionInsertSQL
// (shed_projection.go) also replays for the projector; the two are kept textually independent on
// purpose so a bug in one is very unlikely to be mirrored in the other.
const canonicalShedSummarySQLForParity = `
WITH alive AS (
  SELECT g.shed_id AS shed_uuid, COUNT(*)::bigint AS animals
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.lifecycle_status = 'alive'
    AND g.merged_into_goat_id IS NULL
    AND g.shed_id IS NOT NULL
  GROUP BY g.shed_id
),
completions AS (
  SELECT
    obligation_id,
    (ARRAY_AGG(asof_status ORDER BY
      CASE WHEN asof_status IN ('recorded', 'accepted') THEN 0 ELSE 1 END,
      administered_at DESC,
      created_at DESC))[1] AS effective_status,
    MAX(administered_at) FILTER (WHERE asof_status = 'accepted') AS last_accepted_at
  FROM (
    SELECT
      obligation_id, administered_at, created_at,
      CASE
        WHEN status IN ('accepted', 'rejected') AND verified_at IS NOT NULL AND verified_at > $2::timestamptz THEN 'recorded'
        ELSE status
      END AS asof_status
    FROM vaccination_completions
    WHERE tenant_id = $1::uuid
      AND COALESCE(administered_at, created_at) <= $2::timestamptz
  ) c
  GROUP BY obligation_id
),
asof_terminal AS (
  SELECT
    obligation_id,
    (ARRAY_AGG(event_type ORDER BY occurred_at DESC, obligation_event_id DESC)
       FILTER (WHERE occurred_at <= $2::timestamptz))[1] AS asof_terminal_type,
    true AS has_terminal_event
  FROM obligation_status_events
  WHERE tenant_id = $1::uuid
    AND event_type IN ('missed', 'waived', 'deferred')
  GROUP BY obligation_id
),
raw AS (
  SELECT
    oi.obligation_id,
    oi.due_at,
    oi.window_start,
    oi.completed_at,
    oi.status AS stored_status,
    te.asof_terminal_type,
    te.has_terminal_event,
    oi.target_id AS goat_id,
    g.shed_id AS shed_uuid,
    c.effective_status AS completion_status,
    c.last_accepted_at
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
   AND g.lifecycle_status = 'alive'
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND oi.due_at <= $3::timestamptz
    AND g.shed_id IS NOT NULL
),
effective AS (
  SELECT
    raw.goat_id,
    raw.shed_uuid,
    raw.due_at,
    raw.last_accepted_at,
    raw.completion_status,
    CASE
      WHEN raw.stored_status = 'completed' THEN
        CASE
          WHEN raw.completed_at IS NOT NULL AND raw.completed_at <= $2::timestamptz THEN 'completed'
          WHEN raw.completed_at IS NULL AND raw.completion_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN raw.due_at < $2::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due' ELSE 'scheduled' END)
        END
      WHEN raw.stored_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          WHEN raw.asof_terminal_type IS NOT NULL THEN raw.asof_terminal_type
          WHEN raw.has_terminal_event THEN (CASE WHEN raw.due_at < $2::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due' ELSE 'scheduled' END)
          ELSE raw.stored_status
        END
      WHEN raw.stored_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN raw.due_at < $2::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM raw
),
due_agg AS (
  SELECT
    effective.shed_uuid,
    COUNT(DISTINCT effective.goat_id) FILTER (
      WHERE effective.eff_status IN ('overdue', 'due', 'in_progress')
         OR effective.completion_status IN ('recorded', 'rejected')
    )::bigint AS due_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'overdue')::bigint AS overdue_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'scheduled')::bigint AS scheduled_animals,
    COUNT(*) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress'))::bigint AS open_cells,
    MAX(effective.last_accepted_at) AS last_done,
    MIN(effective.due_at) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress', 'scheduled')) AS next_due
  FROM effective
  GROUP BY effective.shed_uuid
),
shed_rows AS (
  SELECT
    park.location_id::text AS park_id,
    park.name AS park_name,
    shed.location_id::text AS shed_id,
    shed.name AS shed_name,
    alive.animals,
    COALESCE(due_agg.due_animals, 0) AS due_animals,
    COALESCE(due_agg.overdue_animals, 0) AS overdue_animals,
    COALESCE(due_agg.scheduled_animals, 0) AS scheduled_animals,
    COALESCE(due_agg.open_cells, 0) AS open_cells,
    due_agg.last_done,
    due_agg.next_due
  FROM alive
  JOIN locations shed
    ON shed.tenant_id = $1::uuid
   AND shed.location_id = alive.shed_uuid
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  JOIN locations park
    ON park.tenant_id = $1::uuid
   AND park.location_id = shed.parent_location_id
   AND park.location_type = 'park'
   AND park.status = 'active'
  LEFT JOIN due_agg ON due_agg.shed_uuid = alive.shed_uuid
),
scored AS (
  SELECT
    shed_rows.*,
    CASE WHEN open_cells <= 0 THEN 0 ELSE CEIL(open_cells::numeric / GREATEST($4::numeric, 1))::int END AS sessions
  FROM shed_rows
),
classified AS (
  SELECT
    scored.*,
    CASE
      WHEN sessions <= 1 THEN 'within_cap'
      WHEN sessions <= ($5::int + 1) THEN 'over_cap'
      ELSE 'capacity_breach'
    END AS capacity_status,
    CASE
      WHEN overdue_animals > 0 THEN 'overdue'
      WHEN sessions > ($5::int + 1) THEN 'needs_review'
      WHEN sessions > 1 THEN 'split'
      WHEN due_animals > 0 THEN 'due'
      WHEN scheduled_animals > 0 THEN 'scheduled'
      ELSE 'on_track'
    END AS shed_status
  FROM scored
)
SELECT
  park_id, park_name, shed_id, shed_name,
  animals, due_animals, open_cells, sessions, capacity_status, shed_status,
  last_done, next_due
FROM classified
ORDER BY shed_id;
`

// seedShedProjectionParityFixture adds a second shed (testShedB, same park as testShed) with a
// tight capacity config and one overdue + four due animals -- capacity_breach + overdue, the
// opposite corner of the status/capacity space from testShed's plain "due" seed -- so the parity
// check exercises the session-split planner and the overdue-outranks-capacity branch, not just the
// single-goat base fixture.
func seedShedProjectionParityFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "shed b",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-PROJ-B', 'K2 Shed', $3, 'active')`,
		testShedB, testTenant, testPark)
	execProjectionSQL(t, ctx, pool, "tight capacity config",
		`INSERT INTO vaccination_capacity_config (tenant_id, max_per_day, capacity_scope, max_buffer_days, overflow_policy)
		 VALUES ($1, 2, 'tenant', 1, 'split_within_safe_window_then_mark_needs_review')
		 ON CONFLICT (tenant_id) DO UPDATE
		   SET max_per_day = EXCLUDED.max_per_day,
		       capacity_scope = EXCLUDED.capacity_scope,
		       max_buffer_days = EXCLUDED.max_buffer_days,
		       overflow_policy = EXCLUDED.overflow_policy,
		       row_version = vaccination_capacity_config.row_version + 1,
		       updated_at = now()`,
		testTenant)

	batchID := "72500000-0000-4000-8000-000000000098"
	insertProjectionBatch(t, ctx, pool, batchID, "planned")
	for i := 1; i <= 5; i++ {
		goatID := fmt.Sprintf("71500000-0000-4000-8000-%012d", i)
		obligationID := fmt.Sprintf("73500000-0000-4000-8000-%012d", i)
		insertProjectionGoat(t, ctx, pool, goatID, testShedB, testPark)
		dueAt := "2026-07-11 18:00:00+00"
		if i == 1 {
			dueAt = "2026-07-10 00:00:00+00" // overdue relative to the test's as_of
		}
		insertProjectionObligation(t, ctx, pool, obligationID, batchID, goatID, "scheduled", dueAt, fmt.Sprintf("vaccexec-shed-proj-b-%03d", i))
	}
}

// readShedProjectionRows reads vaccination_shed_projection_rows directly (white-box, same package)
// for one projection_version and converts each row into the same domain.ShedSummaryProjection
// shape ShedSummary returns, so the parity comparison can use ordinary struct equality.
func readShedProjectionRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, projectionVersion int64) []domain.ShedSummaryProjection {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT park_id, park_name, shed_id, shed_name, animals, due_animals, open_cells, sessions,
       capacity_status, shed_status, last_done, next_due
FROM vaccination_shed_projection_rows
WHERE tenant_id = $1::uuid
  AND projection_version = $2::bigint
ORDER BY shed_id`, tenantID, projectionVersion)
	if err != nil {
		t.Fatalf("read vaccination_shed_projection_rows: %v", err)
	}
	defer rows.Close()

	out := []domain.ShedSummaryProjection{}
	for rows.Next() {
		var row domain.ShedSummaryProjection
		var capacityStatus, shedStatus string
		var lastDone, nextDue *time.Time
		if err := rows.Scan(
			&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName,
			&row.Animals, &row.DueAnimals, &row.OpenCells, &row.Sessions,
			&capacityStatus, &shedStatus, &lastDone, &nextDue,
		); err != nil {
			t.Fatalf("scan vaccination_shed_projection_rows: %v", err)
		}
		row.Capacity = domain.CapacityStatus(capacityStatus)
		row.Status = domain.ShedStatus(shedStatus)
		row.LastDone = lastDone
		row.NextDue = nextDue
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate vaccination_shed_projection_rows: %v", err)
	}
	return out
}

// shedProjectionRowsEqual compares the fields the projection actually stores. It deliberately
// excludes TotalCount (a live per-page window count over the filtered/paginated set, not a
// per-shed projected fact) and dereferences the LastDone/NextDue timestamps instead of comparing
// pointer identity, which plain struct equality would get wrong for two independently-scanned rows.
func shedProjectionRowsEqual(a, b domain.ShedSummaryProjection) bool {
	return a.ParkID == b.ParkID &&
		a.ParkName == b.ParkName &&
		a.ShedID == b.ShedID &&
		a.ShedName == b.ShedName &&
		a.Animals == b.Animals &&
		a.DueAnimals == b.DueAnimals &&
		a.OpenCells == b.OpenCells &&
		a.Sessions == b.Sessions &&
		a.Capacity == b.Capacity &&
		a.Status == b.Status &&
		timePtrEqual(a.LastDone, b.LastDone) &&
		timePtrEqual(a.NextDue, b.NextDue)
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
