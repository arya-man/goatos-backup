package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// This file pins the SQL TRUTH of VaccinationExecutionCardSummaries (cardSummariesSQL) against a
// real Postgres, exercising the shared executionClassifiedCTE the same way production does. The
// app-layer fake in app/service_test.go mirrors this same production semantics in Go for cheap,
// fast unit coverage; these Postgres tests are the source of truth for the SQL itself. Opt-in per
// repo convention: set GOATOS_RUN_POSTGRES_TESTS=1 to run (see pgtest.SkipIfNoDocker/StartPostgres).

// TestVaccinationExecutionCardSummariesCoversAllCardsRegardlessOfPageLimit is case (a): a
// whole-filter set spanning more cards than a page LIMIT would return must still be counted for
// EVERY card, not only the card(s) the paginated page would surface at that LIMIT. This pins the
// round-1 regression (summary previously aggregated only the paginated page).
func TestVaccinationExecutionCardSummariesCoversAllCardsRegardlessOfPageLimit(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	execProjectionSQL(t, ctx, pool, "seed drive assignment gives card A an operator",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, '2026-06-24', $3, $4, $5, 'K1 Shed', 'whole', 1)`,
		testTenant, testBatch, testOperator, testPark, testShed)

	const (
		secondShed  = "70000000-0000-4000-8000-000000000801"
		secondBatch = "70000000-0000-4000-8000-000000000802"
		secondGoat1 = "70000000-0000-4000-8000-000000000803"
		secondGoat2 = "70000000-0000-4000-8000-000000000804"
		secondObl1  = "70000000-0000-4000-8000-000000000805"
		secondObl2  = "70000000-0000-4000-8000-000000000806"
	)
	execProjectionSQL(t, ctx, pool, "second shed",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-CARD-B', 'Card B Shed', $3, 'active')`,
		secondShed, testTenant, testPark)
	execProjectionSQL(t, ctx, pool, "second shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 200)`,
		secondShed, testTenant, testStage)
	execProjectionSQL(t, ctx, pool, "second shed ops",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		testTenant, secondShed)
	insertProjectionGoat(t, ctx, pool, secondGoat1, secondShed, testPark)
	insertProjectionGoat(t, ctx, pool, secondGoat2, secondShed, testPark)
	insertProjectionBatch(t, ctx, pool, secondBatch, "planned")
	insertProjectionObligation(t, ctx, pool, secondObl1, secondBatch, secondGoat1, "scheduled", "2026-06-25 00:00:00+00", "vaccexec-cardb-1")
	insertProjectionObligation(t, ctx, pool, secondObl2, secondBatch, secondGoat2, "scheduled", "2026-06-25 00:00:00+00", "vaccexec-cardb-2")
	execProjectionSQL(t, ctx, pool, "card B drive assignment",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, '2026-06-25', $3, $4, $5, 'Card B Shed', 'whole', 2)`,
		testTenant, secondBatch, testOperator, testPark, secondShed)

	repo := NewRepository(pool, 5*time.Second)
	summaries, err := repo.VaccinationExecutionCardSummaries(ctx, domain.ExecutionQuery{
		TenantID:  testTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		// LIMIT=1 would truncate the PAGE to one card. The summary must not inherit that truncation.
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("VaccinationExecutionCardSummaries() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("card summaries at page Limit=1 = %d cards, want 2 (both cards, page-independent): %#v", len(summaries), summaries)
	}
	cardA := domain.BuildCardID(testShed, "whole", "", testBatch, "")
	cardB := domain.BuildCardID(secondShed, "whole", "", secondBatch, "")
	if summaries[cardA] == nil {
		t.Fatalf("missing card A summary (shed=%s batch=%s) in %#v", testShed, testBatch, summaries)
	}
	if summaries[cardB] == nil || summaries[cardB].TargetCount != 2 {
		t.Fatalf("card B summary = %#v, want ObligationCount=2 (both scheduled obligations counted, not truncated by page LIMIT)", summaries[cardB])
	}
}

// TestVaccinationExecutionCardSummariesMatchesPageClassificationForBlockedAndRejected is case (b):
// the summary's work_state filter must reach the SAME card-grain classification the page itself
// computes in stateful/classified -- including work_state values ('blocked', 'rejected') the old
// per-row eff_status proxy in filtered_by_state could never produce. This pins the round-3
// regression directly: before the executionClassifiedCTE sharing fix, filtering card summaries by
// WorkState=blocked or WorkState=rejected always returned zero cards, no matter how many blocked or
// rejected cards existed, because filtered_by_state.eff_status never takes those values.
func TestVaccinationExecutionCardSummariesMatchesPageClassificationForBlockedAndRejected(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	// Give the seeded default card an operator so it does not itself classify 'blocked' and
	// compete with the purpose-built blocked/rejected cards this test asserts on.
	execProjectionSQL(t, ctx, pool, "seed drive assignment gives the default card an operator",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, '2026-06-24', $3, $4, $5, 'K1 Shed', 'whole', 1)`,
		testTenant, testBatch, testOperator, testPark, testShed)

	// --- Blocked card: shed marked NOT usable_for_vaccination, no operator assigned. ---
	execProjectionSQL(t, ctx, pool, "blocked shed",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-BLOCKED', 'Blocked Shed', $3, 'active')`,
		testBlockedShed, testTenant, testPark)
	execProjectionSQL(t, ctx, pool, "blocked shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 200)`,
		testBlockedShed, testTenant, testStage)
	execProjectionSQL(t, ctx, pool, "blocked shed ops",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, false, false, false)`,
		testTenant, testBlockedShed)
	insertProjectionGoat(t, ctx, pool, testBlockedGoat, testBlockedShed, testPark)
	insertProjectionBatch(t, ctx, pool, testBlockedBatch, "planned")
	insertProjectionObligation(t, ctx, pool, testBlockedObligation, testBlockedBatch, testBlockedGoat, "scheduled", "2026-06-30 00:00:00+00", "vaccexec-cardsum-blocked")

	// --- Rejected card: SOP task in rework_requested, linked to a fresh batch. ---
	const (
		rejectedGoat  = "70000000-0000-4000-8000-000000000901"
		rejectedBatch = "70000000-0000-4000-8000-000000000902"
		rejectedObl   = "70000000-0000-4000-8000-000000000903"
		rejectedTask  = "70000000-0000-4000-8000-000000000904"
	)
	insertProjectionGoat(t, ctx, pool, rejectedGoat, testShed, testPark)
	insertProjectionBatch(t, ctx, pool, rejectedBatch, "in_progress")
	insertProjectionObligation(t, ctx, pool, rejectedObl, rejectedBatch, rejectedGoat, "in_progress", "2026-06-24 00:00:00+00", "vaccexec-cardsum-rejected")
	execProjectionSQL(t, ctx, pool, "rejected drive assignment",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, '2026-06-24', $3, $4, $5, 'K1 Shed', 'whole', 1)`,
		testTenant, rejectedBatch, testOperator, testPark, testShed)
	execProjectionSQL(t, ctx, pool, "rework task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, assigned_to, scope_type, scope_id, due_at)
		 VALUES ($1, $2, $3, $4, 'vaccination_drive', 'Vaccination rework', 'rework_requested', $5, 'shed', $6, TIMESTAMPTZ '2026-06-24 00:00:00+00')`,
		rejectedTask, testTenant, testVaccinationSOP, testVaccinationSOPVer, testOperator, testShed)
	execProjectionSQL(t, ctx, pool, "link rework task",
		`UPDATE obligation_batches SET sop_task_id = $1 WHERE tenant_id = $2 AND batch_id = $3`,
		rejectedTask, testTenant, rejectedBatch)

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	dueBefore := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	blockedState := domain.WorkStateBlocked
	blockedSummaries, err := repo.VaccinationExecutionCardSummaries(ctx, domain.ExecutionQuery{
		TenantID:  testTenant,
		WorkState: &blockedState,
		AsOf:      asOf,
		DueBefore: dueBefore,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("VaccinationExecutionCardSummaries(blocked) error = %v", err)
	}
	blockedCard := domain.BuildCardID(testBlockedShed, "whole", "", testBlockedBatch, "")
	if len(blockedSummaries) != 1 || blockedSummaries[blockedCard] == nil {
		t.Fatalf("blocked card summaries = %#v, want exactly the blocked card (shed=%s batch=%s); "+
			"before the executionClassifiedCTE fix this was always empty for WorkStateBlocked",
			blockedSummaries, testBlockedShed, testBlockedBatch)
	}

	rejectedState := domain.WorkStateRejected
	rejectedSummaries, err := repo.VaccinationExecutionCardSummaries(ctx, domain.ExecutionQuery{
		TenantID:  testTenant,
		WorkState: &rejectedState,
		AsOf:      asOf,
		DueBefore: dueBefore,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("VaccinationExecutionCardSummaries(rejected) error = %v", err)
	}
	// obligation_batches.sop_task_id is set to rejectedTask below, and the shared classification
	// CTE resolves sop_task_id via COALESCE(oi.sop_task_id, ob.sop_task_id), so the card identity
	// carries the TASK id (BuildCardID prefers task over batch), not the batch id.
	rejectedCard := domain.BuildCardID(testShed, "whole", rejectedTask, "", "")
	if len(rejectedSummaries) != 1 || rejectedSummaries[rejectedCard] == nil {
		t.Fatalf("rejected card summaries = %#v, want exactly the rejected card (shed=%s batch=%s); "+
			"before the executionClassifiedCTE fix this was always empty for WorkStateRejected",
			rejectedSummaries, testShed, rejectedBatch)
	}
	if rejectedSummaries[rejectedCard].Status != domain.WorkStateRejected {
		t.Fatalf("rejected card summary status = %q, want %q", rejectedSummaries[rejectedCard].Status, domain.WorkStateRejected)
	}
}

// TestVaccinationExecutionCardSummariesScopesToRequestedPartition is case (c): on a shed split into
// multiple partitions, a partition-scoped card-summary request must aggregate only that partition's
// card, not the whole shed. Pins the round-2/partition-match regression class at the summary layer.
func TestVaccinationExecutionCardSummariesScopesToRequestedPartition(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	const (
		secondGoat = "70000000-0000-4000-8000-000000000911"
		secondObl  = "70000000-0000-4000-8000-000000000912"
	)
	otherOperator := "70000000-0000-4000-8000-000000000913"
	execProjectionSQL(t, ctx, pool, "other operator",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'OP-PART-SUM-B', 'Operator Summary B', 'active', 'operator', $3)`,
		otherOperator, testTenant, testShed)
	insertProjectionGoat(t, ctx, pool, secondGoat, testShed, testPark)
	insertProjectionObligation(t, ctx, pool, secondObl, testBatch, secondGoat, "due", "2026-06-24 00:00:00+00", "vaccexec-cardsum-partition-second")
	execProjectionSQL(t, ctx, pool, "first goat partition",
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
		 VALUES ($1, $2, $3, 'Part 1', 'K1 Shed - Part 1')`,
		testTenant, testGoat, testShed)
	execProjectionSQL(t, ctx, pool, "second goat partition",
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
		 VALUES ($1, $2, $3, 'Part 2', 'K1 Shed - Part 2')`,
		testTenant, secondGoat, testShed)
	execProjectionSQL(t, ctx, pool, "operator a partition assignment",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, '2026-06-24', $3, $4, $5, 'K1 Shed', 'Part 1', 1)`,
		testTenant, testBatch, testOperator, testPark, testShed)
	execProjectionSQL(t, ctx, pool, "operator b partition assignment",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, '2026-06-24', $3, $4, $5, 'K1 Shed', 'Part 2', 1)`,
		testTenant, testBatch, otherOperator, testPark, testShed)

	repo := NewRepository(pool, 5*time.Second)
	part1 := "Part 1"
	summaries, err := repo.VaccinationExecutionCardSummaries(ctx, domain.ExecutionQuery{
		TenantID:       testTenant,
		ShedID:         strPtr(testShed),
		PartitionLabel: &part1,
		AsOf:           time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore:      time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("VaccinationExecutionCardSummaries(partition=Part 1) error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("Part 1 card summaries = %#v, want exactly 1 card (Part 1 only, not Part 2)", summaries)
	}
	part1Card := domain.BuildCardID(testShed, "Part 1", "", testBatch, "")
	summary := summaries[part1Card]
	if summary == nil {
		t.Fatalf("missing Part 1 card summary (%s) in %#v", part1Card, summaries)
	}
	if summary.TargetCount != 1 {
		t.Fatalf("Part 1 card summary ObligationCount = %d, want 1 (Part 2's obligation must not leak in)", summary.TargetCount)
	}
}
