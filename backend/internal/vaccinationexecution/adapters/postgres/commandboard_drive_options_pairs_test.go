package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestDriveOptionsPairsOneToManyPageBoundaryScheduledDateParkScopeStatusBuckets pins the 2026-09-19
// rewrite of driveOptionsSQL's `pairs` CTE (DISTINCT (batch, shed) over batch_id IS NOT NULL rows,
// BEFORE the locations joins) against the grain it must keep:
//
//   - cardinality: a batch whose obligations span two sheds in two parks is TWO (batch, park) rows;
//     five obligations in one shed are still ONE row; an obligation with no batch_id (61k of the
//     85k on staging) never surfaces;
//   - status buckets: in_progress ranks before planned (rank 0 < 2) regardless of planned date;
//   - scheduled date: within a rank, the newer planned date comes first;
//   - park scope: a park filter keeps only that park's (batch, park) row of the split batch;
//   - page boundary: limit 2 returns exactly 2 with a resumable cursor and the third row follows.
func TestDriveOptionsPairsOneToManyPageBoundaryScheduledDateParkScopeStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000d7"
	parkA := "70000000-0000-4000-8000-0000010000d7"
	shedA := "70000000-0000-4000-8000-0000020000d7"
	parkB := "70000000-0000-4000-8000-0000010000d8"
	shedB := "70000000-0000-4000-8000-0000020000d8"
	protocolID := "70000000-0000-4000-8000-0000060000d7"
	protocolVersionID := "70000000-0000-4000-8000-0000060000d8"
	ruleID := "70000000-0000-4000-8000-0000070000d7"
	partyID := "70000000-0000-4000-8000-00000a0000d7"
	goatID := "70000000-0000-4000-8000-0000030000d7"

	seedKPIFixtureBase(t, ctx, pool, tenantID, parkA, shedA, protocolID, protocolVersionID, ruleID, partyID, "d7")
	execProjectionSQL(t, ctx, pool, "park B",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'Park d8', 'park', NULL, 'active')`, parkB, tenantID)
	execProjectionSQL(t, ctx, pool, "shed B",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'Shed d8', 'shed', $3, 'active')`, shedB, tenantID, parkB)
	seedKPIGoat(t, ctx, pool, tenantID, shedA, partyID, goatID)

	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	batchSplit := "70000000-0000-4000-8000-0000090000d1"   // planned, spans shed A and shed B
	batchOld := "70000000-0000-4000-8000-0000090000d2"     // planned, older date, five obligations in shed A
	batchRunning := "70000000-0000-4000-8000-0000090000d3" // in_progress, oldest date, one obligation
	for _, b := range []struct{ id, status, planned string }{
		{batchSplit, "planned", "2026-08-10"},
		{batchOld, "planned", "2026-08-01"},
		{batchRunning, "in_progress", "2026-07-20"},
	} {
		execProjectionSQL(t, ctx, pool, "batch "+b.id,
			`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, window_start, window_end)
			 VALUES ($1, $2, $3, 'shed', $4, $5, $6::date, $6::timestamptz, $6::timestamptz)`,
			b.id, tenantID, protocolVersionID, shedA, b.status, b.planned)
	}
	seq := 0
	obligation := func(batchID any, shedID string, rule any, label string) {
		seq++
		execProjectionSQL(t, ctx, pool, label,
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, batch_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
			 VALUES ($1, $2, $3, $4, $5, 'goat', 'shed', $6, $7, 'scheduled', $8::timestamptz, $9)`,
			fmt.Sprintf("70000000-0000-4000-8000-0000080000%02d", seq), tenantID, protocolVersionID, batchID, goatID, shedID, rule,
			asOf.Add(time.Duration(seq)*time.Hour), fmt.Sprintf("pairs-d7-%d", seq))
	}
	obligation(batchSplit, shedA, ruleID, "split batch in shed A")
	obligation(batchSplit, shedB, ruleID, "split batch in shed B")
	for i := 0; i < 5; i++ {
		obligation(batchOld, shedA, ruleID, "old batch obligation")
	}
	obligation(batchRunning, shedA, ruleID, "running batch obligation")
	// Adversarial row that must NOT surface: an unbatched obligation (the bulk of a live tenant --
	// 61k of 85k on staging). A rule-less obligation cannot be seeded (obligation_instances_rule_tenant_fk),
	// so the protocol_rules inner join is pinned by the FK rather than by a row here.
	obligation(nil, shedA, ruleID, "unbatched obligation")

	repo := NewRepository(pool, 5*time.Second)
	all, err := repo.CommandBoardDriveOptions(ctx, domain.CommandBoardDriveOptionsQuery{TenantID: tenantID, Limit: 10})
	if err != nil {
		t.Fatalf("CommandBoardDriveOptions() error = %v", err)
	}
	got := make([]string, 0, len(all.Options))
	for _, o := range all.Options {
		got = append(got, o.DriveBatchID+"@"+o.ParkID)
	}
	want := []string{
		batchRunning + "@" + parkA, // in_progress outranks planned whatever the date
		batchSplit + "@" + parkA,   // newest planned date next, one row per park...
		batchSplit + "@" + parkB,   // ...so the split batch is two rows
		batchOld + "@" + parkA,     // five obligations, still one row
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("drive option rows = %v, want %v (unbatched obligations must not appear)", got, want)
	}
	if all.NextCursor != "" {
		t.Fatalf("4 rows under a limit of 10 must not carry a cursor, got %q", all.NextCursor)
	}

	// Park scope: only park B's half of the split batch.
	scoped, err := repo.CommandBoardDriveOptions(ctx, domain.CommandBoardDriveOptionsQuery{TenantID: tenantID, ParkID: &parkB, Limit: 10})
	if err != nil {
		t.Fatalf("park-scoped CommandBoardDriveOptions() error = %v", err)
	}
	if len(scoped.Options) != 1 || scoped.Options[0].DriveBatchID != batchSplit || scoped.Options[0].ParkID != parkB {
		t.Fatalf("park B scope = %+v, want exactly the split batch's park-B row", scoped.Options)
	}

	// Page boundary: limit 2 stops after the split batch's park-A row and resumes on its park-B row.
	page1, err := repo.CommandBoardDriveOptions(ctx, domain.CommandBoardDriveOptionsQuery{TenantID: tenantID, Limit: 2})
	if err != nil {
		t.Fatalf("page 1 error = %v", err)
	}
	if len(page1.Options) != 2 || page1.NextCursor == "" {
		t.Fatalf("page 1 = %d rows cursor %q, want 2 rows and a cursor", len(page1.Options), page1.NextCursor)
	}
	page2, err := repo.CommandBoardDriveOptions(ctx, domain.CommandBoardDriveOptionsQuery{TenantID: tenantID, Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("page 2 error = %v", err)
	}
	if len(page2.Options) != 2 || page2.Options[0].DriveBatchID != batchSplit || page2.Options[0].ParkID != parkB || page2.Options[1].DriveBatchID != batchOld {
		t.Fatalf("page 2 = %+v, want the split batch's park-B row then the old batch", page2.Options)
	}
	if page2.NextCursor != "" {
		t.Fatalf("the last page must not carry a cursor, got %q", page2.NextCursor)
	}
}
