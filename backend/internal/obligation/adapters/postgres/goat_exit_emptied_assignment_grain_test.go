package postgres

import (
	"context"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestGoatExitedDeletesOnlyTheAssignmentRowsItEmptied is the GRAIN proof for the
// delete-emptied-rows step of the SM-3 planned-drive removal (BUG-028).
//
// The decrement above it is exact: membership names the ONE assignment row the exiting animal
// occupied. The cleanup that follows it was not -- it deleted every zero-animal row of the whole
// BATCH:
//
//	DELETE FROM vaccination_drive_assignments
//	WHERE tenant_id = $1 AND batch_id = ANY($2::uuid[]) AND animal_count = 0
//
// batch_id is NOT the assignment row's identity. The persisted uniqueness key is
// (tenant, batch, planned_date, park, shed, physical_shed, partition_label, operator), so ONE batch
// legitimately holds MANY assignment rows. Any sibling row of that batch that already stood at zero
// -- a different partition, a different operator, a different date, one this animal was never in and
// this exit never decremented -- is deleted as collateral. That is real data loss, not a cosmetic
// one: the row carries the arm's own operator/date/partition record, and
// vaccination_drive_assignment_members has ON DELETE CASCADE from assignment_id, so the arm's exact
// per-goat membership ledger is destroyed with it.
//
// Fixture: one batch, one shed, three assignment rows --
//
//	R_OWN   partition "A", operator 1, 2026-11-05, 2 animals  (the exiting animal's member row)
//	R_ZERO  partition "B", operator 2, 2026-11-06, 0 animals  (sibling, ALREADY zero, never occupied)
//	R_LIVE  partition "C", operator 2, 2026-11-06, 3 animals  (sibling, untouched control)
//
// The exit empties NOTHING (R_OWN goes 2 -> 1), so the correct number of deleted rows is ZERO.
// R_ZERO must survive: this exit did not empty it.
func TestGoatExitedDeletesOnlyTheAssignmentRowsItEmptied(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		exitingGoat = "10000000-0000-4000-8000-00000000fe01"
		shedID      = "00000000-0000-4000-8000-00000000de01"
		operatorOne = "20000000-0000-4000-8000-000000000e01"
		operatorTwo = "20000000-0000-4000-8000-000000000e02"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "goat-exit-emptied-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat)
	for id, code := range map[string]string{operatorOne: "OP-EMPTY-1", operatorTwo: "OP-EMPTY-2"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')`, id, tenantID, code); err != nil {
			t.Fatalf("seed operator %s: %v", code, err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'Gandhi A')`, tenantID, exitingGoat, shedID); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.exitemptied", Name: "Exit emptied", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleA, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule A: %v", err)
	}

	own := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	sibling := own.AddDate(0, 0, 1)
	obligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleA,
		TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
		DueAt: own, Status: "scheduled", IdempotencyKey: "exit-emptied-dose-a", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation: applied=%v err=%v", applied, err)
	}
	opOne := operatorOne
	opTwo := operatorTwo
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-emptied-drive", PlannedDate: &own, Status: "planned",
		EstimatedTargets: 5, PlannedQuantity: "5", QuantityUnit: "dose", ConductedBy: &opOne,
	}, []string{obligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: own, OperatorID: &opOne, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 2,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 2, CapacityStatus: "within_cap"},
		// Sibling arm of the SAME batch that already stands at zero animals. The exiting animal was
		// never in it (different partition, different operator, different date) and this exit never
		// decrements it -- so this exit has no business deleting it.
		{BatchID: batchID, PlannedDate: sibling, OperatorID: &opTwo, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "B", AnimalCount: 0,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 0, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: sibling, OperatorID: &opTwo, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "C", AnimalCount: 3,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 3, CapacityStatus: "within_cap"},
	}); err != nil {
		t.Fatalf("seed drive assignments: %v", err)
	}

	var ownAssignmentID string
	if err := pool.QueryRow(ctx, `
SELECT assignment_id::text FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND partition_label='A'`, tenantID, batchID).Scan(&ownAssignmentID); err != nil {
		t.Fatalf("read own assignment id: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, obligation_id) DO UPDATE SET assignment_id = EXCLUDED.assignment_id`,
		tenantID, ownAssignmentID, obligationID, exitingGoat); err != nil {
		t.Fatalf("seed drive assignment membership: %v", err)
	}

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatExitedHandler(repo).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		ID: "event-exit-emptied", Type: oblapp.EventGoatExited, TenantID: tenantID, Key: exitingGoat,
		OccurredAt: time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}

	// The animal's own arm was decremented, not emptied, so it survives with one animal fewer.
	if got := countRows(t, ctx, pool, `
SELECT animal_count FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND partition_label='A'`, tenantID, batchID); got != 1 {
		t.Fatalf("member arm animal_count = %d, want 1", got)
	}
	// THE DEFECT: the already-zero sibling arm, which this exit never touched, must still exist.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND partition_label='B'`, tenantID, batchID); got != 1 {
		t.Fatalf("already-zero sibling arm rows = %d, want 1 -- the exit emptied only partition A's row; deleting zero-count rows by whole batch_id destroys sibling arms (and their membership ledger via ON DELETE CASCADE) the exiting animal was never in", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != 3 {
		t.Fatalf("assignment rows for batch = %d, want 3 (nothing was emptied by this exit)", got)
	}
}

// TestGoatExitedStillDeletesTheRowItActuallyEmptied is the counter-proof for BUG-028: narrowing the
// cleanup to the rows this exit emptied must NOT stop it deleting a row the exit genuinely did
// empty, while an already-zero sibling of the same batch still survives.
//
// Fixture: one batch, one shed --
//
//	R_SOLO  partition "A", operator 1, 2026-12-03, 1 animal   (the exiting animal is its only member)
//	R_ZERO  partition "B", operator 2, 2026-12-04, 0 animals  (sibling, already zero, never occupied)
//
// After the exit R_SOLO must be gone (a zero-animal drive row is phantom planned work) and R_ZERO
// must remain.
func TestGoatExitedStillDeletesTheRowItActuallyEmptied(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		exitingGoat = "10000000-0000-4000-8000-00000000ff01"
		shedID      = "00000000-0000-4000-8000-00000000df01"
		operatorOne = "20000000-0000-4000-8000-000000000f01"
		operatorTwo = "20000000-0000-4000-8000-000000000f02"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "goat-exit-solo-grain-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat)
	for id, code := range map[string]string{operatorOne: "OP-SOLOG-1", operatorTwo: "OP-SOLOG-2"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')`, id, tenantID, code); err != nil {
			t.Fatalf("seed operator %s: %v", code, err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'Gandhi A')`, tenantID, exitingGoat, shedID); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.exitsolograin", Name: "Exit solo grain", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleA, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule A: %v", err)
	}

	own := time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC)
	sibling := own.AddDate(0, 0, 1)
	obligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleA,
		TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
		DueAt: own, Status: "scheduled", IdempotencyKey: "exit-solo-grain-dose-a", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation: applied=%v err=%v", applied, err)
	}
	opOne := operatorOne
	opTwo := operatorTwo
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-solo-grain-drive", PlannedDate: &own, Status: "planned",
		EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose", ConductedBy: &opOne,
	}, []string{obligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: own, OperatorID: &opOne, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 1,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 1, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: sibling, OperatorID: &opTwo, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "B", AnimalCount: 0,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 0, CapacityStatus: "within_cap"},
	}); err != nil {
		t.Fatalf("seed drive assignments: %v", err)
	}

	var ownAssignmentID string
	if err := pool.QueryRow(ctx, `
SELECT assignment_id::text FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND partition_label='A'`, tenantID, batchID).Scan(&ownAssignmentID); err != nil {
		t.Fatalf("read own assignment id: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, obligation_id) DO UPDATE SET assignment_id = EXCLUDED.assignment_id`,
		tenantID, ownAssignmentID, obligationID, exitingGoat); err != nil {
		t.Fatalf("seed drive assignment membership: %v", err)
	}

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatExitedHandler(repo).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		ID: "event-exit-solo-grain", Type: oblapp.EventGoatExited, TenantID: tenantID, Key: exitingGoat,
		OccurredAt: time.Date(2026, 12, 1, 6, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}

	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND partition_label='A'`, tenantID, batchID); got != 0 {
		t.Fatalf("emptied member arm rows = %d, want 0 (a zero-animal drive row is phantom planned work)", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND partition_label='B'`, tenantID, batchID); got != 1 {
		t.Fatalf("already-zero sibling arm rows = %d, want 1 (this exit did not empty it)", got)
	}
}
