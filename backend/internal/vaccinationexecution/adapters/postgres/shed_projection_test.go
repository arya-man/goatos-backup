package postgres

import (
	"context"
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
// parity/shadow proof: RecomputeShedProjection must reproduce, row for row, exactly what the live
// ShedSummary god-CTE serves today for the same as_of. This is what lets the read-model
// infrastructure land with zero risk to the request path -- ShedSummary itself is not touched by
// this change, but this test proves the projector it is landed alongside would already be a
// faithful substitute once a later change flips the read.
func TestRecomputeShedProjectionMatchesLiveShedSummary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	seedShedProjectionParityFixture(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 7, 11, 18, 0, 0, 0, time.UTC)

	// Before any recompute, the tenant has no serving projection version yet.
	if _, ok := repo.shedProjectionServingVersion(ctx, testTenant); ok {
		t.Fatalf("expected no serving shed projection version before the first recompute")
	}

	live, err := repo.ShedSummary(ctx, domain.ShedSummaryQuery{
		TenantID:  testTenant,
		AsOf:      asOf,
		DueBefore: asOf.Add(30 * 24 * time.Hour),
		Limit:     200,
	})
	if err != nil {
		t.Fatalf("ShedSummary: %v", err)
	}
	if len(live) < 2 {
		t.Fatalf("fixture setup failed: got %d live shed rows, want >= 2 (testShed + testShedB)", len(live))
	}

	result, err := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{
		TenantID: testTenant,
		AsOf:     asOf,
	})
	if err != nil {
		t.Fatalf("RecomputeShedProjection: %v", err)
	}
	if result.Rows != int64(len(live)) {
		t.Fatalf("projection rows = %d, want %d (same as live ShedSummary row count)", result.Rows, len(live))
	}

	servingVersion, ok := repo.shedProjectionServingVersion(ctx, testTenant)
	if !ok || servingVersion != result.ProjectionVersion {
		t.Fatalf("serving version = %d ok=%v, want %d after commit", servingVersion, ok, result.ProjectionVersion)
	}

	projected := readShedProjectionRows(t, ctx, pool, testTenant, result.ProjectionVersion)
	if len(projected) != len(live) {
		t.Fatalf("projected rows = %d, want %d (parity with live ShedSummary)", len(projected), len(live))
	}

	liveByShed := make(map[string]domain.ShedSummaryProjection, len(live))
	for _, row := range live {
		liveByShed[row.ShedID] = row
	}

	sawOverdue, sawDue := false, false
	for _, p := range projected {
		l, ok := liveByShed[p.ShedID]
		if !ok {
			t.Fatalf("projection has shed %s not present in live ShedSummary output", p.ShedID)
		}
		if !shedProjectionRowsEqual(p, l) {
			t.Fatalf("projection row for shed %s does not match live ShedSummary row:\n  live:      %#v\n  projected: %#v", p.ShedID, l, p)
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

	// A second recompute (a later projection_version) must still match live output and must not
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
