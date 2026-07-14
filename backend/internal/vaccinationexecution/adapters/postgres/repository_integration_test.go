package postgres

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

const (
	testTenant   = "00000000-0000-4000-8000-000000000001"
	testParty    = "00000000-0000-4000-8000-000000001001"
	testPark     = "70000000-0000-4000-8000-000000000001"
	testShed     = "70000000-0000-4000-8000-000000000002"
	testStage    = "70000000-0000-4000-8000-000000000003"
	testGoat     = "70000000-0000-4000-8000-000000000004"
	testProtocol = "70000000-0000-4000-8000-000000000005"
	testVersion  = "70000000-0000-4000-8000-000000000006"
	testRule     = "70000000-0000-4000-8000-000000000007"
	testBatch    = "70000000-0000-4000-8000-000000000008"
	testObl      = "70000000-0000-4000-8000-000000000009"
	testComplete = "70000000-0000-4000-8000-000000000010"
	testOperator = "70000000-0000-4000-8000-000000000011"
	testParkHead = "70000000-0000-4000-8000-000000000012"
	testVerifier = "70000000-0000-4000-8000-000000000013"
	testTask     = "70000000-0000-4000-8000-000000000016"

	testCanceledGoat       = "70000000-0000-4000-8000-000000000020"
	testCanceledBatch      = "70000000-0000-4000-8000-000000000021"
	testCanceledObligation = "70000000-0000-4000-8000-000000000022"
	testCompletedGoat      = "70000000-0000-4000-8000-000000000030"
	testCompletedBatch     = "70000000-0000-4000-8000-000000000031"
	testCompletedObl       = "70000000-0000-4000-8000-000000000032"
	testCompletedProof     = "70000000-0000-4000-8000-000000000033"
	testCompletedSkipGoat  = "70000000-0000-4000-8000-000000000034"
	testCompletedSkipObl   = "70000000-0000-4000-8000-000000000035"
	testBlockedShed        = "70000000-0000-4000-8000-000000000040"
	testBlockedGoat        = "70000000-0000-4000-8000-000000000041"
	testBlockedBatch       = "70000000-0000-4000-8000-000000000042"
	testBlockedObligation  = "70000000-0000-4000-8000-000000000043"
	testRecentClosedGoat   = "70000000-0000-4000-8000-000000000050"
	testRecentClosedBatch  = "70000000-0000-4000-8000-000000000051"
	testRecentClosedObl    = "70000000-0000-4000-8000-000000000052"
	testRecentClosedProof  = "70000000-0000-4000-8000-000000000053"
	testOldClosedGoat      = "70000000-0000-4000-8000-000000000060"
	testOldClosedBatch     = "70000000-0000-4000-8000-000000000061"
	testOldClosedObl       = "70000000-0000-4000-8000-000000000062"
	testOldClosedProof     = "70000000-0000-4000-8000-000000000063"
	testVaccinationSOP     = "b0000000-0000-4000-8000-000000000001"
	testVaccinationSOPVer  = "b0000000-0000-4000-8000-000000000002"
)

func TestListVaccinationExecutionProjection(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	rows, err := projectedExecutionList(t, ctx, repo, domain.ExecutionQuery{
		TenantID:  testTenant,
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListVaccinationExecution() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows want 1: %#v", len(rows), rows)
	}
	got := rows[0]
	if got.ParkID != testPark || got.ParkName != "CBE Park" {
		t.Fatalf("park = %s/%s", got.ParkID, got.ParkName)
	}
	if got.ShedID != testShed || got.ShedName != "K1 Shed" {
		t.Fatalf("shed = %s/%s", got.ShedID, got.ShedName)
	}
	if got.AnimalStage != "K1" {
		t.Fatalf("animal stage = %q want K1", got.AnimalStage)
	}
	if got.BatchID == nil || *got.BatchID != testBatch {
		t.Fatalf("batch = %v want %s", got.BatchID, testBatch)
	}
	if got.ObligationCount != 1 || got.InProgressCount != 1 || got.CompletionRecorded != 1 {
		t.Fatalf("counts = obligations %d inProgress %d recorded %d", got.ObligationCount, got.InProgressCount, got.CompletionRecorded)
	}
	if got.OperatorName == nil || *got.OperatorName != "Operator A" {
		t.Fatalf("operator = %v", got.OperatorName)
	}
	if got.ParkHeadName == nil || *got.ParkHeadName != "Park Head" {
		t.Fatalf("park head = %v", got.ParkHeadName)
	}
	if got.VerifierName == nil || *got.VerifierName != "Verifier" {
		t.Fatalf("verifier = %v", got.VerifierName)
	}
	if !got.UsableForVaccination || got.IsQuarantine || got.IsICU {
		t.Fatalf("defer flags usable=%v quarantine=%v icu=%v", got.UsableForVaccination, got.IsQuarantine, got.IsICU)
	}
}

func TestScanRosterUsesExactTaskIdentityCursorAndPinnedOptions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	const (
		secondGoat = "70000000-0000-4000-8000-000000000081"
		secondObl  = "70000000-0000-4000-8000-000000000082"
		itemID     = "70000000-0000-4000-8000-000000000083"
		lotEarly   = "70000000-0000-4000-8000-000000000084"
		lotLate    = "70000000-0000-4000-8000-000000000085"
		lotEmpty   = "70000000-0000-4000-8000-000000000086"
	)
	execProjectionSQL(t, ctx, pool, "task identity", `
INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state,
  assigned_to, scope_type, scope_id, context)
VALUES ($1,$2,$3,$4,'vaccination_drive','Exact drive','in_progress',$5,'shed',$6,
  jsonb_build_object('obligation_batch_id',$7::text))`, testTask, testTenant, testVaccinationSOP, testVaccinationSOPVer, testOperator, testShed, testBatch)
	execProjectionSQL(t, ctx, pool, "link task batch", `UPDATE obligation_batches SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, testTask, testTenant, testBatch)
	execProjectionSQL(t, ctx, pool, "link task obligations", `UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, testTask, testTenant, testBatch)
	execProjectionSQL(t, ctx, pool, "primary tag", `
INSERT INTO goat_identifiers (identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value, status, scope_key, normalizer_version, valid_from)
VALUES (gen_random_uuid(),$1,$2,'animal_identifier_1','RFID-ONE','rfid-one','active','global','v1',now())`, testTenant, testGoat)
	insertProjectionGoat(t, ctx, pool, secondGoat, testShed, testPark)
	insertProjectionObligation(t, ctx, pool, secondObl, testBatch, secondGoat, "due", "2026-06-24 00:00:00+00", "vaccexec-roster-second")
	execProjectionSQL(t, ctx, pool, "link second task", `UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND obligation_id=$3`, testTask, testTenant, secondObl)
	execProjectionSQL(t, ctx, pool, "second tag", `
INSERT INTO goat_identifiers (identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value, status, scope_key, normalizer_version, valid_from)
VALUES (gen_random_uuid(),$1,$2,'animal_identifier_1','RFID-TWO','rfid-two','active','global','v1',now())`, testTenant, secondGoat)
	execProjectionSQL(t, ctx, pool, "route site", `UPDATE protocol_versions SET rule_dsl='{"schedule":[{"route_site":"subcutaneous"}]}'::jsonb WHERE tenant_id=$1 AND protocol_version_id=$2`, testTenant, testVersion)
	execProjectionSQL(t, ctx, pool, "inventory item", `INSERT INTO inventory_items (item_id,tenant_id,item_code,name,category,base_unit,status) VALUES ($1,$2,'VAC-TEST','Test vaccine','vaccine','dose','active')`, itemID, testTenant)
	for _, lot := range []struct{ id, code, expiry, status, qty, reserved string }{
		{lotEarly, "LOT-EARLY", "2027-01-01", "active", "10", "2"},
		{lotLate, "LOT-LATE", "2027-06-01", "active", "10", "0"},
		{lotEmpty, "LOT-EMPTY", "2027-03-01", "depleted", "0", "0"},
	} {
		execProjectionSQL(t, ctx, pool, "lot "+lot.code, `INSERT INTO inventory_stock (stock_id,tenant_id,item_id,location_id,lot_code,expiry_date,quantity_in_stock,quantity_reserved,quantity_unit,status) VALUES ($1,$2,$3,$4,$5,$6::date,$7::numeric,$8::numeric,'dose',$9)`, lot.id, testTenant, itemID, testPark, lot.code, lot.expiry, lot.qty, lot.reserved, lot.status)
	}
	execProjectionSQL(t, ctx, pool, "batch reservation", `INSERT INTO inventory_stock_movements (movement_id,tenant_id,lot_id,item_id,location_id,movement_type,quantity,quantity_unit,batch_id,actor_id,idempotency_key) VALUES (gen_random_uuid(),$1,$2,$3,$4,'reserve',2,'dose',$5,$6,'vaccexec-option-reserve')`, testTenant, lotEarly, itemID, testPark, testBatch, testOperator)

	repo := NewRepository(pool, 5*time.Second)
	first, err := repo.ScanRoster(ctx, domain.ScanRosterQuery{TenantID: testTenant, ShedID: testShed, TaskID: testTask, Limit: 1})
	if err != nil {
		t.Fatalf("ScanRoster(first): %v", err)
	}
	if len(first.Rows) != 1 || first.NextCursor == nil {
		t.Fatalf("first=%#v", first)
	}
	row := first.Rows[0]
	if row.GoatID == "" || row.TaskID != testTask || row.BatchID != testBatch || row.SOPVersionID != testVaccinationSOPVer || row.TaskRowVersion != 1 {
		t.Fatalf("identity row=%#v", row)
	}
	second, err := repo.ScanRoster(ctx, domain.ScanRosterQuery{TenantID: testTenant, ShedID: testShed, TaskID: testTask, Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("ScanRoster(second): %v", err)
	}
	if len(second.Rows) != 1 || second.Rows[0].GoatID == row.GoatID || second.NextCursor != nil {
		t.Fatalf("second=%#v", second)
	}

	options, err := repo.TaskOptionValues(ctx, testTenant, testTask)
	if err != nil {
		t.Fatalf("TaskOptionValues: %v", err)
	}
	if options.TaskID != testTask || len(options.Sources) != 2 {
		t.Fatalf("options=%#v", options)
	}
	var lots, sites *domain.TaskOptionSource
	for i := range options.Sources {
		if options.Sources[i].Source == "inventory.vaccine_lots.fefo" {
			lots = &options.Sources[i]
		}
		if options.Sources[i].Source == "vaccination.route_sites" {
			sites = &options.Sources[i]
		}
	}
	if lots == nil || len(lots.Options) != 3 || lots.Options[0].Value != lotEarly || lots.Options[0].FEFORank == nil || *lots.Options[0].FEFORank != 1 || !lots.Options[2].Disabled || lots.Options[2].DisabledReason == nil {
		t.Fatalf("lots=%#v", lots)
	}
	if sites == nil || len(sites.Options) != 1 || sites.Options[0].Value != "subcutaneous" {
		t.Fatalf("sites=%#v", sites)
	}
}

func TestListVaccinationExecutionPageUsesStableCursorAndFilteredTotal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	secondGoat := "70000000-0000-4000-8000-000000000071"
	secondBatch := "70000000-0000-4000-8000-000000000072"
	secondObligation := "70000000-0000-4000-8000-000000000073"
	insertProjectionGoat(t, ctx, pool, secondGoat, testShed, testPark)
	insertProjectionBatch(t, ctx, pool, secondBatch, "planned")
	insertProjectionObligation(t, ctx, pool, secondObligation, secondBatch, secondGoat, "scheduled", "2026-06-26 00:00:00+00", "vaccexec-cursor-second")

	repo := NewRepository(pool, 5*time.Second)
	severity := domain.SeverityWatch
	query := domain.ExecutionQuery{
		TenantID:  testTenant,
		Severity:  &severity,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     1,
	}
	first, err := repo.ListVaccinationExecutionPage(ctx, query)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Rows) != 1 || first.TotalCount != 2 || first.NextCursor == nil {
		t.Fatalf("first page rows=%d total=%d cursor=%v", len(first.Rows), first.TotalCount, first.NextCursor)
	}
	query.Cursor = first.NextCursor
	second, err := repo.ListVaccinationExecutionPage(ctx, query)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Rows) != 1 || second.TotalCount != 2 || second.NextCursor != nil {
		t.Fatalf("second page rows=%d total=%d cursor=%v", len(second.Rows), second.TotalCount, second.NextCursor)
	}
	if first.Rows[0].SortRowKey == second.Rows[0].SortRowKey {
		t.Fatalf("cursor repeated row %q", first.Rows[0].SortRowKey)
	}
}

func TestListVaccinationExecutionExcludesCanceledObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	insertProjectionGoat(t, ctx, pool, testCanceledGoat, testShed, testPark)
	insertProjectionBatch(t, ctx, pool, testCanceledBatch, "planned")
	insertProjectionObligation(t, ctx, pool, testCanceledObligation, testCanceledBatch, testCanceledGoat, "canceled", "2026-06-25 00:00:00+00", "vaccexec-canceled-only")

	insertProjectionGoat(t, ctx, pool, testCompletedGoat, testShed, testPark)
	insertProjectionGoat(t, ctx, pool, testCompletedSkipGoat, testShed, testPark)
	insertProjectionBatch(t, ctx, pool, testCompletedBatch, "completed")
	insertProjectionObligation(t, ctx, pool, testCompletedObl, testCompletedBatch, testCompletedGoat, "completed", "2026-06-26 00:00:00+00", "vaccexec-completed-active")
	insertProjectionObligation(t, ctx, pool, testCompletedSkipObl, testCompletedBatch, testCompletedSkipGoat, "canceled", "2026-06-26 00:00:00+00", "vaccexec-completed-canceled")
	execProjectionSQL(t, ctx, pool, "completed proof",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-26 09:00:00+00', 'accepted', 'vaccexec-completed-proof', $6)`,
		testCompletedProof, testTenant, testCompletedObl, testCompletedBatch, testCompletedGoat, testOperator)

	repo := NewRepository(pool, 5*time.Second)
	// Pin as_of after the accepted dose (administered 2026-06-26): completions are now as_of-bounded, so an
	// unset as_of would default to wall-clock now and (depending on the run date) drop the future dose. This
	// test asserts canceled-obligation exclusion, not as_of behavior, so it must use a deterministic as_of.
	rows, err := projectedExecutionList(t, ctx, repo, domain.ExecutionQuery{
		TenantID:  testTenant,
		AsOf:      time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListVaccinationExecution() error = %v", err)
	}
	if rowByBatch(rows, testCanceledBatch) != nil {
		t.Fatalf("canceled-only batch %s should not appear in vaccination execution rows: %#v", testCanceledBatch, rows)
	}
	completed := rowByBatch(rows, testCompletedBatch)
	if completed == nil {
		t.Fatalf("completed batch %s not found in rows: %#v", testCompletedBatch, rows)
	}
	if completed.ObligationCount != 1 || completed.CompletedCount != 1 || completed.CanceledCount != 0 {
		t.Fatalf("completed+canceled counts = obligations %d completed %d canceled %d; want active denominator 1/1/0",
			completed.ObligationCount, completed.CompletedCount, completed.CanceledCount)
	}
}

func TestListVaccinationExecutionFiltersWorkStateBeforeLimit(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
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
	insertProjectionObligation(t, ctx, pool, testBlockedObligation, testBlockedBatch, testBlockedGoat, "scheduled", "2026-06-30 00:00:00+00", "vaccexec-blocked-filter")

	repo := NewRepository(pool, 5*time.Second)
	state := domain.WorkStateBlocked
	rows, err := projectedExecutionList(t, ctx, repo, domain.ExecutionQuery{
		TenantID:  testTenant,
		WorkState: &state,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListVaccinationExecution() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d blocked rows want 1: %#v", len(rows), rows)
	}
	if rows[0].BatchID == nil || *rows[0].BatchID != testBlockedBatch {
		t.Fatalf("got batch %v want blocked batch %s", rows[0].BatchID, testBlockedBatch)
	}
	if rows[0].UsableForVaccination {
		t.Fatal("blocked row should carry usable_for_vaccination=false")
	}
}

func TestListVaccinationExecutionSurfacesTaskReworkAsRejected(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	execProjectionSQL(t, ctx, pool, "rework task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, assigned_to, scope_type, scope_id, due_at)
		 VALUES ($1, $2, $3, $4, 'vaccination_drive', 'Vaccination rework', 'rework_requested', $5, 'shed', $6, TIMESTAMPTZ '2026-06-24 00:00:00+00')`,
		testTask, testTenant, testVaccinationSOP, testVaccinationSOPVer, testOperator, testShed)
	execProjectionSQL(t, ctx, pool, "link rework task",
		`UPDATE obligation_batches SET sop_task_id = $1 WHERE tenant_id = $2 AND batch_id = $3`,
		testTask, testTenant, testBatch)
	execProjectionSQL(t, ctx, pool, "clear recorded completion",
		`UPDATE vaccination_completions SET status = 'reversed' WHERE tenant_id = $1 AND completion_id = $2`,
		testTenant, testComplete)

	state := domain.WorkStateRejected
	repo := NewRepository(pool, 5*time.Second)
	rows, err := projectedExecutionList(t, ctx, repo, domain.ExecutionQuery{
		TenantID:  testTenant,
		WorkState: &state,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListVaccinationExecution() error = %v", err)
	}
	if len(rows) != 1 || rows[0].TaskState == nil || *rows[0].TaskState != "rework_requested" {
		t.Fatalf("rows = %#v, want rejected rework task row", rows)
	}
}

func TestListVaccinationExecutionPrioritizesActionableRowsOverClosedHistory(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	insertProjectionGoat(t, ctx, pool, testRecentClosedGoat, testShed, testPark)
	insertProjectionBatch(t, ctx, pool, testRecentClosedBatch, "completed")
	insertProjectionObligation(t, ctx, pool, testRecentClosedObl, testRecentClosedBatch, testRecentClosedGoat, "completed", "2026-06-20 00:00:00+00", "vaccexec-recent-closed")
	insertProjectionCompletion(t, ctx, pool, testRecentClosedProof, testRecentClosedObl, testRecentClosedBatch, testRecentClosedGoat, "vaccexec-recent-closed-proof")

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
	insertProjectionObligation(t, ctx, pool, testBlockedObligation, testBlockedBatch, testBlockedGoat, "scheduled", "2026-06-30 00:00:00+00", "vaccexec-blocked-priority")

	repo := NewRepository(pool, 5*time.Second)
	rows, err := projectedExecutionList(t, ctx, repo, domain.ExecutionQuery{
		TenantID:  testTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListVaccinationExecution() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows want 1: %#v", len(rows), rows)
	}
	if rows[0].BatchID == nil || *rows[0].BatchID != testBlockedBatch {
		t.Fatalf("got batch %v want blocked batch %s", rows[0].BatchID, testBlockedBatch)
	}
}

func TestListVaccinationExecutionSkipsOldClosedRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)
	insertProjectionGoat(t, ctx, pool, testOldClosedGoat, testShed, testPark)
	insertProjectionBatch(t, ctx, pool, testOldClosedBatch, "completed")
	insertProjectionObligation(t, ctx, pool, testOldClosedObl, testOldClosedBatch, testOldClosedGoat, "completed", "2026-05-01 00:00:00+00", "vaccexec-old-closed")
	insertProjectionCompletion(t, ctx, pool, testOldClosedProof, testOldClosedObl, testOldClosedBatch, testOldClosedGoat, "vaccexec-old-closed-proof")

	repo := NewRepository(pool, 5*time.Second)
	state := domain.WorkStateCompleted
	rows, err := projectedExecutionList(t, ctx, repo, domain.ExecutionQuery{
		TenantID:  testTenant,
		WorkState: &state,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListVaccinationExecution() error = %v", err)
	}
	if rowByBatch(rows, testOldClosedBatch) != nil {
		t.Fatalf("old completed batch %s should not remain in the default vaccination execution board: %#v", testOldClosedBatch, rows)
	}
}

func TestVaccinationExecutionProductionQueryPlanUsesIndexes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("set enable_seqscan: %v", err)
	}

	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	dueBefore := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rows, err := tx.Query(
		ctx,
		"EXPLAIN (COSTS OFF)\n"+vaccinationExecutionSQL,
		testTenant,
		"",
		"",
		dueBefore,
		200,
		domain.WorkStateBlocked,
		asOf,
		asOf.Add(-defaultClosedHistoryAge),
		"",       // severity filter (none)
		false,    // cursorPresent
		0,        // cursorRank
		int64(0), // cursorDueMicros
		"",       // cursorRowKey
	)
	if err != nil {
		t.Fatalf("explain production vaccination execution query: %v", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan rows: %v", err)
	}
	plan := strings.Join(lines, "\n")

	for _, forbidden := range []string{
		"Seq Scan on obligation_instances",
		"Seq Scan on goats",
		"Seq Scan on vaccination_completions",
		"Seq Scan on locations",
		"Seq Scan on workforce_members",
		"Seq Scan on shed_profiles",
		"Seq Scan on animal_stage_lookup",
		"Seq Scan on location_operational_attributes",
	} {
		if strings.Contains(plan, forbidden) {
			t.Fatalf("production vaccination execution plan used %q:\n%s", forbidden, plan)
		}
	}

	if !strings.Contains(plan, "Index Scan") &&
		!strings.Contains(plan, "Index Only Scan") &&
		!strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("production vaccination execution plan did not use an index scan:\n%s", plan)
	}
}

func TestVaccinationOperationsProductionQueryPlanUsesIndexes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("set enable_seqscan: %v", err)
	}

	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	dueBefore := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rows, err := tx.Query(
		ctx,
		"EXPLAIN (COSTS OFF)\n"+vaccinationOperationsSQL,
		testTenant,
		asOf,
		dueBefore,
		"", // park filter
		"", // shed filter
		"", // cursor park
		"", // cursor shed
		"", // cursor stage
		501,
		"", // cursor park name
		"", // cursor shed name
	)
	if err != nil {
		t.Fatalf("explain production vaccination operations query: %v", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan rows: %v", err)
	}
	plan := strings.Join(lines, "\n")

	// The new as_of reconstruction joins the partitioned obligation_status_events log; it must stay indexed
	// (tenant+event_type), like the other hot tables, so this query is safe at million-goat scale.
	for _, forbidden := range []string{
		"Seq Scan on obligation_instances",
		"Seq Scan on goats",
		"Seq Scan on vaccination_completions",
		"Seq Scan on locations",
		// Substring also matches partition scans (obligation_status_events_2026_06, …).
		"Seq Scan on obligation_status_events",
	} {
		if strings.Contains(plan, forbidden) {
			t.Fatalf("production vaccination operations plan used %q:\n%s", forbidden, plan)
		}
	}

	if !strings.Contains(plan, "Index Scan") &&
		!strings.Contains(plan, "Index Only Scan") &&
		!strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("production vaccination operations plan did not use an index scan:\n%s", plan)
	}
}

func seedVaccinationExecutionProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(label, sql string, args ...any) { execProjectionSQL(t, ctx, pool, label, sql, args...) }
	exec("park",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'PARK-PROJ', 'CBE Park', 'active')`,
		testPark, testTenant)
	exec("shed",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-PROJ', 'K1 Shed', $3, 'active')`,
		testShed, testTenant, testPark)
	exec("stage",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status)
		 VALUES ($1, $2, 'K1', 'K1 kids', 'active')`,
		testStage, testTenant)
	exec("park profile",
		`INSERT INTO park_profiles (location_id, tenant_id, park_code)
		 VALUES ($1, $2, 'CBE')`,
		testPark, testTenant)
	exec("shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 500)`,
		testShed, testTenant, testStage)
	exec("shed ops",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		testTenant, testShed)
	exec("operator",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'OP-PROJ', 'Operator A', 'active', 'operator', $3)`,
		testOperator, testTenant, testShed)
	exec("park head",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'PH-PROJ', 'Park Head', 'active', 'park_head', $3)`,
		testParkHead, testTenant, testPark)
	exec("verifier",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'VER-PROJ', 'Verifier', 'active', 'verifier', $3)`,
		testVerifier, testTenant, testPark)
	exec("goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, health_status)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K1', 'healthy')`,
		testGoat, testTenant, testParty, testShed, testPark)
	exec("protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination.projection', 'Rabies', 'vaccination', 'draft')`,
		testProtocol, testTenant)
	exec("protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', DATE '2026-06-01', '{}'::jsonb, '{}'::jsonb)`,
		testVersion, testTenant, testProtocol)
	exec("protocol rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy)
		 VALUES ($1, $2, $3, 'D1', 1, 'birth_age', '{}'::jsonb, '{}'::jsonb)`,
		testRule, testTenant, testVersion)
	exec("batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
		 VALUES ($1, $2, $3, 'shed', $4, 'in_progress', DATE '2026-06-24', $5)`,
		testBatch, testTenant, testVersion, testShed, testOperator)
	exec("obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, TIMESTAMPTZ '2026-06-24 00:00:00+00', 'in_progress', 'vaccexec-proj-1', 1)`,
		testObl, testTenant, testVersion, testRule, testBatch, testGoat, testShed)
	exec("completion",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-24 09:00:00+00', 'recorded', 'vaccexec-comp-1', $6)`,
		testComplete, testTenant, testObl, testBatch, testGoat, testOperator)
}

func execProjectionSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

func insertProjectionGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shedID, parkID string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "goat "+goatID,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, health_status)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K1', 'healthy')`,
		goatID, testTenant, testParty, shedID, parkID)
}

func insertProjectionBatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, status string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "batch "+batchID,
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
		 VALUES ($1, $2, $3, 'shed', $4, $5, DATE '2026-06-24', $6)`,
		batchID, testTenant, testVersion, testShed, status, testOperator)
}

func insertProjectionObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID, batchID, goatID, status, dueAt, key string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "obligation "+obligationID,
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, $8::timestamptz, $9, $10, 1)`,
		obligationID, testTenant, testVersion, testRule, batchID, goatID, testShed, dueAt, status, key)
}

func insertProjectionCompletion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, completionID, obligationID, batchID, goatID, key string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "completion "+completionID,
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-20 09:00:00+00', 'accepted', $6, $7)`,
		completionID, testTenant, obligationID, batchID, goatID, key, testOperator)
}

func rowByBatch(rows []domain.ExecutionProjection, batchID string) *domain.ExecutionProjection {
	for i := range rows {
		if rows[i].BatchID != nil && *rows[i].BatchID == batchID {
			return &rows[i]
		}
	}
	return nil
}

// TestVaccinationOperationsAggregatesCohortsAndDedupesRework proves the operations read model:
//   - cohort × protocol cells from real obligations/completions
//   - rejected-then-accepted rework collapses to ONE effective row (no overcount, not stuck "rejected")
//   - last_dose is the latest ACCEPTED administered_at
//   - park scope filters cohorts
func TestVaccinationOperationsAggregatesCohortsAndDedupesRework(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Base cohort K1: in_progress obligation + a recorded (awaiting-verify) completion.
	seedVaccinationExecutionProjection(t, ctx, pool)

	const (
		reworkGoat = "70000000-0000-4000-8000-000000000070"
		reworkObl  = "70000000-0000-4000-8000-000000000071"
		reworkRej  = "70000000-0000-4000-8000-000000000072"
		reworkAcc  = "70000000-0000-4000-8000-000000000073"
	)
	exec := func(label, sql string, args ...any) { execProjectionSQL(t, ctx, pool, label, sql, args...) }
	// A K2 cohort goat in the same shed: a completed obligation whose first attempt was REJECTED then a
	// later attempt was ACCEPTED. The partial unique index allows the rejected history alongside one active row.
	exec("rework goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, health_status, age_band)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K2', 'healthy', '2-4 mo')`,
		reworkGoat, testTenant, testParty, testShed, testPark)
	insertProjectionObligation(t, ctx, pool, reworkObl, testBatch, reworkGoat, "completed", "2026-06-24 00:00:00+00", "vaccexec-rework-obl")
	exec("rework rejected attempt",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-18 09:00:00+00', 'rejected', 'vaccexec-rework-rej', $6)`,
		reworkRej, testTenant, reworkObl, testBatch, reworkGoat, testOperator)
	exec("rework accepted attempt",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-22 09:00:00+00', 'accepted', 'vaccexec-rework-acc', $6)`,
		reworkAcc, testTenant, reworkObl, testBatch, reworkGoat, testOperator)

	repo := NewRepository(pool, 5*time.Second)
	// as_of = inclusive end of 2026-06-24, so doses administered during that day (K1's 09:00 recorded
	// completion, K2's accepted rework) are seen "as of June 24".
	asOf := time.Date(2026, 6, 24, 23, 59, 59, 0, time.UTC)
	rows, err := projectedOperations(t, ctx, repo, domain.OperationsQuery{
		TenantID:  testTenant,
		AsOf:      asOf,
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     50,
	})
	if err != nil {
		t.Fatalf("VaccinationOperations: %v", err)
	}

	// One protocol column (Rabies), keyed by protocol_id (not version/rule).
	protocols := map[string]bool{}
	for _, r := range rows {
		protocols[r.ProtocolID] = true
	}
	if len(protocols) != 1 {
		t.Fatalf("want 1 distinct protocol, got %d", len(protocols))
	}

	k1 := opsRowByStage(rows, "K1")
	k2 := opsRowByStage(rows, "K2")
	if k1 == nil || k2 == nil {
		t.Fatalf("want K1 and K2 cohort rows, got K1=%v K2=%v (rows=%d)", k1 != nil, k2 != nil, len(rows))
	}

	// K1: base in_progress obligation + recorded completion → no overdue, awaiting verification, no last dose.
	if k1.Animals != 1 {
		t.Errorf("K1 animals: want 1, got %d", k1.Animals)
	}
	if k1.ProofPendingCount != 1 || k1.AcceptedCount != 0 {
		t.Errorf("K1 counts: want proofPending=1 accepted=0, got proofPending=%d accepted=%d", k1.ProofPendingCount, k1.AcceptedCount)
	}
	if k1.LastDose != nil {
		t.Errorf("K1 lastDose: want nil (nothing accepted), got %v", k1.LastDose)
	}

	// K2: rejected-then-accepted REWORK must collapse to one effective (accepted) row.
	if k2.TotalCount != 1 {
		t.Errorf("K2 total: want 1 obligation (no fan-out from rejected history), got %d", k2.TotalCount)
	}
	if k2.RejectedCount != 0 {
		t.Errorf("K2 rejected: want 0 (rework was accepted), got %d", k2.RejectedCount)
	}
	if k2.AcceptedCount != 1 {
		t.Errorf("K2 accepted: want 1, got %d", k2.AcceptedCount)
	}
	if k2.LastDose == nil || !k2.LastDose.Equal(time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("K2 lastDose: want 2026-06-22T09:00Z (latest accepted), got %v", k2.LastDose)
	}
	if k2.AgeBand == nil || *k2.AgeBand != "2-4 mo" {
		t.Errorf("K2 ageBand: want '2-4 mo', got %v", k2.AgeBand)
	}

	// Park scope: matching park returns rows, a different park returns none.
	other := "70000000-0000-4000-8000-0000000000ff"
	scoped, err := repo.VaccinationOperations(ctx, domain.OperationsQuery{TenantID: testTenant, ParkID: &other, AsOf: asOf, DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 50})
	if err != nil {
		t.Fatalf("scoped VaccinationOperations: %v", err)
	}
	if len(scoped) != 0 {
		t.Errorf("park filter: want 0 rows for a non-matching park, got %d", len(scoped))
	}
}

func TestVaccinationOperationsAsOfExcludesFutureCompletions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)

	const (
		futGoat = "70000000-0000-4000-8000-000000000080"
		futObl  = "70000000-0000-4000-8000-000000000081"
		futAcc  = "70000000-0000-4000-8000-000000000082"
	)
	exec := func(label, sql string, args ...any) { execProjectionSQL(t, ctx, pool, label, sql, args...) }
	// A K9 cohort goat with a completed obligation whose ACCEPTED dose was administered 2026-06-30.
	exec("future-dose goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, health_status)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K9', 'healthy')`,
		futGoat, testTenant, testParty, testShed, testPark)
	insertProjectionObligation(t, ctx, pool, futObl, testBatch, futGoat, "completed", "2026-06-24 00:00:00+00", "vaccexec-future-obl")
	exec("future accepted dose",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-30 09:00:00+00', 'accepted', 'vaccexec-future-acc', $6)`,
		futAcc, testTenant, futObl, testBatch, futGoat, testOperator)
	// MarkObligationCompleted sets completed_at atomically with status='completed'; mirror that so the
	// as_of reconstruction exercises the PRIMARY completed_at path (not just the completion-row fallback).
	exec("future completed_at",
		`UPDATE obligation_instances SET completed_at = TIMESTAMPTZ '2026-06-30 09:00:00+00' WHERE tenant_id = $1 AND obligation_id = $2`,
		testTenant, futObl)

	repo := NewRepository(pool, 5*time.Second)
	svc := vaccexecapp.NewService(repo)
	dueBefore := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	asOfBefore := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	asOfAfter := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// as_of = 2026-06-25 (BEFORE the dose): the accepted dose must be invisible AND, because completed_at is
	// after as_of, the obligation must NOT read as completed/scheduled — it must re-bucket to overdue
	// (due_at 2026-06-24 already passed as_of). This is the bucket/workState correctness the dose-only
	// assertion missed.
	before, err := projectedOperations(t, ctx, repo, domain.OperationsQuery{
		TenantID: testTenant, AsOf: asOfBefore, DueBefore: dueBefore, Limit: 50,
	})
	if err != nil {
		t.Fatalf("VaccinationOperations(as_of before): %v", err)
	}
	k9 := opsRowByStage(before, "K9")
	if k9 == nil {
		t.Fatalf("K9 cohort missing as-of before dose: %#v", before)
	}
	if k9.AcceptedCount != 0 {
		t.Errorf("as_of before dose: K9 accepted want 0, got %d", k9.AcceptedCount)
	}
	if k9.LastDose != nil {
		t.Errorf("as_of before dose: K9 lastDose want nil (dose is after as_of), got %v", k9.LastDose)
	}
	if k9.OverdueCount != 1 || k9.TotalCount != 1 {
		t.Errorf("as_of before dose: K9 buckets want overdue=1 total=1 (completed_at after as_of must re-bucket to overdue), got overdue=%d total=%d", k9.OverdueCount, k9.TotalCount)
	}
	if k9.ScheduledCount != 0 || k9.DueCount != 0 {
		t.Errorf("as_of before dose: K9 want scheduled=0 due=0 (due_at already passed as_of), got scheduled=%d due=%d", k9.ScheduledCount, k9.DueCount)
	}
	// Service-derived workState (what /vaccination renders): overdue, not the buggy scheduled/completed.
	if cohort := opsCohortByStage(t, repo, svc, ctx, domain.OperationsQuery{TenantID: testTenant, AsOf: asOfBefore, DueBefore: dueBefore, Limit: 50}, "K9"); cohort.WorkState != domain.WorkStateOverdue {
		t.Errorf("as_of before dose: K9 cohort workState want overdue, got %q (cells=%#v)", cohort.WorkState, cohort.Cells)
	}

	// as_of = 2026-07-01 (AFTER the dose): same data, the accepted dose now counts and the obligation reads
	// completed. Proves as_of changes both the dose state AND the bucket/workState.
	after, err := projectedOperations(t, ctx, repo, domain.OperationsQuery{
		TenantID: testTenant, AsOf: asOfAfter, DueBefore: dueBefore, Limit: 50,
	})
	if err != nil {
		t.Fatalf("VaccinationOperations(as_of after): %v", err)
	}
	k9after := opsRowByStage(after, "K9")
	if k9after == nil {
		t.Fatalf("K9 cohort missing as-of after dose: %#v", after)
	}
	if k9after.AcceptedCount != 1 {
		t.Errorf("as_of after dose: K9 accepted want 1, got %d", k9after.AcceptedCount)
	}
	if k9after.OverdueCount != 0 {
		t.Errorf("as_of after dose: K9 overdue want 0 (now completed), got %d", k9after.OverdueCount)
	}
	if k9after.LastDose == nil || !k9after.LastDose.Equal(time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("as_of after dose: K9 lastDose want 2026-06-30T09:00Z, got %v", k9after.LastDose)
	}
	if cohort := opsCohortByStage(t, repo, svc, ctx, domain.OperationsQuery{TenantID: testTenant, AsOf: asOfAfter, DueBefore: dueBefore, Limit: 50}, "K9"); cohort.WorkState != domain.WorkStateCompleted {
		t.Errorf("as_of after dose: K9 cohort workState want completed, got %q (cells=%#v)", cohort.WorkState, cohort.Cells)
	}
}

// TestVaccinationExecutionReconstructsObligationStatusAsOf proves the execution board + shed detail use
// point-in-time obligation status: a 'completed' obligation finalized AFTER as_of reads as overdue (open)
// before its completion and as completed after, in both VaccinationExecution and ShedDrilldown.

func TestVaccinationOperationsBoundsVerificationByVerifiedAt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)

	const (
		vGoat  = "70000000-0000-4000-8000-0000000000b0"
		vBatch = "70000000-0000-4000-8000-0000000000b1"
		vObl   = "70000000-0000-4000-8000-0000000000b2"
		vComp  = "70000000-0000-4000-8000-0000000000b3"
	)
	// K7 cohort: obligation completed on accept; dose ADMINISTERED 2026-06-20 but ACCEPTED (verified_at) and
	// completed_at on 2026-06-30.
	execProjectionSQL(t, ctx, pool, "verify goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, health_status)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K7', 'healthy')`,
		vGoat, testTenant, testParty, testShed, testPark)
	insertProjectionBatch(t, ctx, pool, vBatch, "completed")
	insertProjectionObligation(t, ctx, pool, vObl, vBatch, vGoat, "completed", "2026-06-20 00:00:00+00", "vaccexec-verify-obl")
	execProjectionSQL(t, ctx, pool, "verify completed_at",
		`UPDATE obligation_instances SET completed_at = TIMESTAMPTZ '2026-06-30 10:00:00+00' WHERE tenant_id = $1 AND obligation_id = $2`,
		testTenant, vObl)
	execProjectionSQL(t, ctx, pool, "verify dose",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, verified_at, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-20 09:00:00+00', 'accepted', TIMESTAMPTZ '2026-06-30 10:00:00+00', 'vaccexec-verify-comp', $6)`,
		vComp, testTenant, vObl, vBatch, vGoat, testOperator)

	repo := NewRepository(pool, 5*time.Second)
	dueBefore := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	// as_of BEFORE verified_at (dose already administered): proof recorded, NOT accepted, no last_dose.
	before, err := projectedOperations(t, ctx, repo, domain.OperationsQuery{
		TenantID: testTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: dueBefore, Limit: 50,
	})
	if err != nil {
		t.Fatalf("VaccinationOperations(before verify): %v", err)
	}
	k7 := opsRowByStage(before, "K7")
	if k7 == nil {
		t.Fatalf("K7 cohort missing: %#v", before)
	}
	if k7.AcceptedCount != 0 || k7.ProofPendingCount != 1 {
		t.Errorf("before verify: want accepted=0 proofPending=1, got accepted=%d proofPending=%d", k7.AcceptedCount, k7.ProofPendingCount)
	}
	if k7.LastDose != nil {
		t.Errorf("before verify: lastDose want nil (accept is after as_of), got %v", k7.LastDose)
	}

	// as_of AFTER verified_at: the accept now counts; last_dose is the administered time.
	after, err := projectedOperations(t, ctx, repo, domain.OperationsQuery{
		TenantID: testTenant, AsOf: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), DueBefore: dueBefore, Limit: 50,
	})
	if err != nil {
		t.Fatalf("VaccinationOperations(after verify): %v", err)
	}
	k7a := opsRowByStage(after, "K7")
	if k7a == nil || k7a.AcceptedCount != 1 || k7a.ProofPendingCount != 0 {
		t.Fatalf("after verify: want accepted=1 proofPending=0, got %#v", k7a)
	}
	if k7a.LastDose == nil || !k7a.LastDose.Equal(time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("after verify: lastDose want 2026-06-20T09:00Z, got %v", k7a.LastDose)
	}
}

func opsRowByStage(rows []domain.OperationsRow, stage string) *domain.OperationsRow {
	for i := range rows {
		if rows[i].Stage == stage {
			return &rows[i]
		}
	}
	return nil
}

// opsCohortByStage runs the read model through the app service (the same path /vaccination renders) and
// returns the cohort for a stage, so tests can assert the derived WorkState, not only raw SQL counts.
func opsCohortByStage(t *testing.T, repo *Repository, svc *vaccexecapp.Service, ctx context.Context, q domain.OperationsQuery, stage string) domain.OperationsCohort {
	t.Helper()
	resp, err := svc.VaccinationOperations(ctx, q)
	if err != nil {
		t.Fatalf("svc.VaccinationOperations(stage %s): %v", stage, err)
	}
	for _, c := range resp.Cohorts {
		if c.Stage == stage {
			return c
		}
	}
	t.Fatalf("cohort for stage %q not found in %#v", stage, resp.Cohorts)
	return domain.OperationsCohort{}
}

func insertOpsStageGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, stage string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "stage goat "+goatID,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, health_status)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, $6, 'healthy')`,
		goatID, testTenant, testParty, testShed, testPark, stage)
}

func insertObligationStatusEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID, eventType, occurredAt, key string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "status event "+key,
		`INSERT INTO obligation_status_events (tenant_id, obligation_id, event_type, occurred_at, idempotency_key)
		 VALUES ($1, $2, $3, $4::timestamptz, $5)`,
		testTenant, obligationID, eventType, occurredAt, key)
}

// TestVaccinationOperationsReconstructsMissedWaivedAsOf proves point-in-time obligation status for the
// terminal states that have no timestamp column (missed/waived): the obligation_status_events log decides
// whether the transition was already true at as_of.
//   - missed AFTER as_of  -> re-bucket to the open state at as_of (overdue), NOT deferred.
//   - missed AT/BEFORE as_of -> missed/blocked (the real point-in-time state).
//   - missed with NO event -> trusted as missed/blocked (documented fallback; we never fake an earlier time).
//   - churn (missed AT/BEFORE as_of AND missed AFTER as_of) -> missed/blocked: the latest terminal event AT OR
//     BEFORE as_of wins, instead of an unbounded MAX() picking the future event and wrongly re-bucketing open.
func TestVaccinationOperationsReconstructsMissedWaivedAsOf(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)

	const (
		missedAfterGoat  = "70000000-0000-4000-8000-000000000090"
		missedAfterObl   = "70000000-0000-4000-8000-000000000091"
		missedBeforeGoat = "70000000-0000-4000-8000-000000000092"
		missedBeforeObl  = "70000000-0000-4000-8000-000000000093"
		missedNoEvtGoat  = "70000000-0000-4000-8000-000000000094"
		missedNoEvtObl   = "70000000-0000-4000-8000-000000000095"
		missedChurnGoat  = "70000000-0000-4000-8000-000000000096"
		missedChurnObl   = "70000000-0000-4000-8000-000000000097"
	)

	// MA: currently 'missed', but the missed transition happened 2026-06-26 — AFTER as_of.
	insertOpsStageGoat(t, ctx, pool, missedAfterGoat, "MA")
	insertProjectionObligation(t, ctx, pool, missedAfterObl, testBatch, missedAfterGoat, "missed", "2026-06-24 00:00:00+00", "vaccexec-missed-after")
	insertObligationStatusEvent(t, ctx, pool, missedAfterObl, "missed", "2026-06-26 10:00:00+00", "vaccexec-missed-after-evt")

	// MB: 'missed' with the transition 2026-06-22 — AT/BEFORE as_of.
	insertOpsStageGoat(t, ctx, pool, missedBeforeGoat, "MB")
	insertProjectionObligation(t, ctx, pool, missedBeforeObl, testBatch, missedBeforeGoat, "missed", "2026-06-20 00:00:00+00", "vaccexec-missed-before")
	insertObligationStatusEvent(t, ctx, pool, missedBeforeObl, "missed", "2026-06-22 10:00:00+00", "vaccexec-missed-before-evt")

	// MN: 'missed' with NO status event — transition time unknown, so trust the stored status.
	insertOpsStageGoat(t, ctx, pool, missedNoEvtGoat, "MN")
	insertProjectionObligation(t, ctx, pool, missedNoEvtObl, testBatch, missedNoEvtGoat, "missed", "2026-06-20 00:00:00+00", "vaccexec-missed-noevt")

	// MC: churn — missed AT/BEFORE as_of (2026-06-22) AND missed AFTER as_of (2026-06-26). The latest terminal
	// event at/before as_of (06-22) was in effect at as_of, so it must read deferred, not overdue.
	insertOpsStageGoat(t, ctx, pool, missedChurnGoat, "MC")
	insertProjectionObligation(t, ctx, pool, missedChurnObl, testBatch, missedChurnGoat, "missed", "2026-06-20 00:00:00+00", "vaccexec-missed-churn")
	insertObligationStatusEvent(t, ctx, pool, missedChurnObl, "missed", "2026-06-22 10:00:00+00", "vaccexec-missed-churn-old")
	insertObligationStatusEvent(t, ctx, pool, missedChurnObl, "missed", "2026-06-26 10:00:00+00", "vaccexec-missed-churn-new")

	repo := NewRepository(pool, 5*time.Second)
	svc := vaccexecapp.NewService(repo)
	q := domain.OperationsQuery{
		TenantID:  testTenant,
		AsOf:      time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
		Limit:     50,
	}

	rows, err := repo.VaccinationOperations(ctx, q)
	if err != nil {
		t.Fatalf("VaccinationOperations: %v", err)
	}

	ma := opsRowByStage(rows, "MA")
	mb := opsRowByStage(rows, "MB")
	mn := opsRowByStage(rows, "MN")
	mc := opsRowByStage(rows, "MC")
	if ma == nil || mb == nil || mn == nil || mc == nil {
		t.Fatalf("want MA/MB/MN/MC cohorts, got MA=%v MB=%v MN=%v MC=%v", ma != nil, mb != nil, mn != nil, mc != nil)
	}

	// MA: missed-after-as_of must re-bucket open (overdue), not deferred.
	if ma.OverdueCount != 1 || ma.DeferredCount != 0 {
		t.Errorf("MA (missed after as_of): want overdue=1 deferred=0, got overdue=%d deferred=%d", ma.OverdueCount, ma.DeferredCount)
	}
	if c := opsCohortByStage(t, repo, svc, ctx, q, "MA"); c.WorkState != domain.WorkStateOverdue {
		t.Errorf("MA cohort workState want overdue, got %q", c.WorkState)
	}

	// MB: missed-before-as_of is genuinely missed at as_of.
	if mb.MissedCount != 1 || mb.DeferredCount != 0 || mb.OverdueCount != 0 {
		t.Errorf("MB (missed before as_of): want missed=1 deferred=0 overdue=0, got missed=%d deferred=%d overdue=%d", mb.MissedCount, mb.DeferredCount, mb.OverdueCount)
	}
	if c := opsCohortByStage(t, repo, svc, ctx, q, "MB"); c.WorkState != domain.WorkStateMissed {
		t.Errorf("MB cohort workState want missed, got %q", c.WorkState)
	}

	// MN: no event -> trust stored missed. We do not fake an earlier open state.
	if mn.MissedCount != 1 || mn.DeferredCount != 0 || mn.OverdueCount != 0 {
		t.Errorf("MN (missed no event): want missed=1 deferred=0 overdue=0 (trusted), got missed=%d deferred=%d overdue=%d", mn.MissedCount, mn.DeferredCount, mn.OverdueCount)
	}
	if c := opsCohortByStage(t, repo, svc, ctx, q, "MN"); c.WorkState != domain.WorkStateMissed {
		t.Errorf("MN cohort workState want missed, got %q", c.WorkState)
	}

	// MC: churn — latest terminal event at/before as_of wins -> missed, NOT overdue (regression guard for
	// the old unbounded MAX() that would pick the after-as_of event).
	if mc.MissedCount != 1 || mc.DeferredCount != 0 || mc.OverdueCount != 0 {
		t.Errorf("MC (missed churn): want missed=1 deferred=0 overdue=0, got missed=%d deferred=%d overdue=%d", mc.MissedCount, mc.DeferredCount, mc.OverdueCount)
	}
	if c := opsCohortByStage(t, repo, svc, ctx, q, "MC"); c.WorkState != domain.WorkStateMissed {
		t.Errorf("MC cohort workState want missed, got %q", c.WorkState)
	}
}

const (
	testGapsPark        = "71000000-0000-4000-8000-000000000001"
	testGapsShed        = "71000000-0000-4000-8000-000000000002"
	testGapsGoatNoDOB   = "71000000-0000-4000-8000-000000000003"
	testGapsGoatNoBreed = "71000000-0000-4000-8000-000000000004"
	testGapsGoatOK      = "71000000-0000-4000-8000-000000000005"
)

func seedVaccinationGaps(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(label, sql string, args ...any) { execProjectionSQL(t, ctx, pool, label, sql, args...) }
	exec("gaps park",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'PARK-GAPS', 'Gaps Park', 'active')`,
		testGapsPark, testTenant)
	exec("gaps shed",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-GAPS', 'Gaps Shed', $3, 'active')`,
		testGapsShed, testTenant, testGapsPark)
	exec("goat no dob",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, breed, dob)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'Beetal', NULL)`,
		testGapsGoatNoDOB, testTenant, testParty, testGapsShed, testGapsPark)
	exec("goat no breed",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, breed, breed_id, dob)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, NULL, NULL, DATE '2025-01-01')`,
		testGapsGoatNoBreed, testTenant, testParty, testGapsShed, testGapsPark)
	exec("goat complete",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, breed, dob)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'Beetal', DATE '2025-01-01')`,
		testGapsGoatOK, testTenant, testParty, testGapsShed, testGapsPark)
}

func TestVaccinationGapsExcludesCompleteAnimalsAndPaginates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationGaps(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	rows, err := repo.VaccinationGaps(ctx, domain.GapsQuery{TenantID: testTenant, Limit: 10})
	if err != nil {
		t.Fatalf("VaccinationGaps() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows want 2 (complete animal must be excluded): %#v", len(rows), rows)
	}
	byGoat := map[string]domain.GapProjectionRow{}
	for _, r := range rows {
		byGoat[r.GoatID] = r
	}
	if got, ok := byGoat[testGapsGoatNoDOB]; !ok || got.ReasonCode != domain.GapReasonNoDateOfBirth {
		t.Fatalf("no-dob goat row = %#v want reason no_date_of_birth", got)
	}
	if got, ok := byGoat[testGapsGoatNoBreed]; !ok || got.ReasonCode != domain.GapReasonNoBreedOnRecord {
		t.Fatalf("no-breed goat row = %#v want reason no_breed_on_record", got)
	}
	if _, ok := byGoat[testGapsGoatOK]; ok {
		t.Fatalf("complete goat must not appear in gaps: %#v", rows)
	}

	// Keyset pagination: limit=1 must return exactly one row plus a usable cursor for the next page.
	page1, err := repo.VaccinationGaps(ctx, domain.GapsQuery{TenantID: testTenant, Limit: 1})
	if err != nil {
		t.Fatalf("VaccinationGaps(page1) error = %v", err)
	}
	if len(page1) != 1 {
		t.Fatalf("page1 = %#v want 1 row", page1)
	}
	cursor := page1[0].GoatID
	page2, err := repo.VaccinationGaps(ctx, domain.GapsQuery{TenantID: testTenant, Limit: 10, Cursor: &cursor})
	if err != nil {
		t.Fatalf("VaccinationGaps(page2) error = %v", err)
	}
	if len(page2) != 1 || page2[0].GoatID == cursor {
		t.Fatalf("page2 = %#v want the one remaining row, distinct from cursor %q", page2, cursor)
	}

	// park_id scoping: an unrelated park returns no rows.
	otherPark := "71000000-0000-4000-8000-000000000099"
	scoped, err := repo.VaccinationGaps(ctx, domain.GapsQuery{TenantID: testTenant, ParkID: &otherPark, Limit: 10})
	if err != nil {
		t.Fatalf("VaccinationGaps(scoped) error = %v", err)
	}
	if len(scoped) != 0 {
		t.Fatalf("scoped rows = %#v want empty", scoped)
	}
}

func projectedExecutionList(t *testing.T, ctx context.Context, repo *Repository, q domain.ExecutionQuery) ([]domain.ExecutionProjection, error) {
	t.Helper()
	return repo.ListVaccinationExecution(ctx, q)
}

func projectedExecutionPage(t *testing.T, ctx context.Context, repo *Repository, q domain.ExecutionQuery) (domain.ExecutionProjectionPage, error) {
	t.Helper()
	return repo.ListVaccinationExecutionPage(ctx, q)
}

func projectedOperations(t *testing.T, ctx context.Context, repo *Repository, q domain.OperationsQuery) ([]domain.OperationsRow, error) {
	t.Helper()
	return repo.VaccinationOperations(ctx, q)
}

func TestVaccinationOperationsProjectionReadLatency(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	q := domain.OperationsQuery{TenantID: testTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 500}
	durations := make([]time.Duration, 40)
	for i := range durations {
		started := time.Now()
		rows, err := repo.VaccinationOperations(ctx, q)
		if err != nil || len(rows) == 0 {
			t.Fatalf("read %d rows=%d err=%v", i, len(rows), err)
		}
		durations[i] = time.Since(started)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50, p95, p99 := durations[19], durations[37], durations[39]
	t.Logf("vaccination operations projection non_empty=true samples=40 p50=%s p95=%s p99=%s", p50, p95, p99)
	if p95 > 250*time.Millisecond {
		t.Fatalf("projection p95=%s exceeds 250ms local gate", p95)
	}
}
