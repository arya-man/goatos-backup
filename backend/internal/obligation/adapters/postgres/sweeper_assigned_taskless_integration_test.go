package postgres

import (
	"context"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// A vaccination batch that is already on an operator's drive assignment but has no SOP task (a
// manual/runbook drive, or a batch that moved to in_progress before it was tasked) left the phone
// with no task to write RFID scans and proof against ("shed-wide" POSTs). The sweeper's
// finalization must give it its task through the same idempotent batch-task path, while a
// taskless batch that is on nobody's roster is left alone.
func TestSweeperTasksAssignedBatchMissingSOPTask(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		assignedGoat   = "10000000-0000-4000-8000-00000000fb01"
		unassignedGoat = "10000000-0000-4000-8000-00000000fb02"
		shedID         = "00000000-0000-4000-8000-00000000db01"
		operatorID     = "20000000-0000-4000-8000-000000000b01"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "assigned-taskless-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, assignedGoat, unassignedGoat)
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'OP-TASKLESS', 'Taskless Operator', 'active', 'operator', $3)`, operatorID, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.assigned.taskless", Name: "Assigned taskless", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	skeletonVersion := "b0000000-0000-4000-8000-000000000002"
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
		SopVersionID: &skeletonVersion,
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	planned := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	operator := operatorID
	shed := shedID
	mkBatch := func(goatID, key string) (string, string) {
		t.Helper()
		obl, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: planned, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
		}
		batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
			TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
			Session: key, PlannedDate: &planned, Status: "planned",
			EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose", ConductedBy: &operator,
		}, []string{obl})
		if err != nil || attached != 1 {
			t.Fatalf("create batch %s: attached=%d err=%v", key, attached, err)
		}
		// Manual drive shape: the batch already moved past 'planned' without ever being tasked.
		if _, err := pool.Exec(ctx, `UPDATE obligation_batches SET status='in_progress' WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); err != nil {
			t.Fatalf("mark batch in_progress: %v", err)
		}
		return batchID, obl
	}
	assignedBatch, assignedObl := mkBatch(assignedGoat, "assigned-taskless")
	unassignedBatch, _ := mkBatch(unassignedGoat, "unassigned-taskless")

	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: assignedBatch, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
		PhysicalShed: "Yashoda", PartitionLabel: "3", AnimalCount: 1,
		VaccineRuleIDs: []string{ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed drive assignment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT tenant_id, assignment_id, $3::uuid, $4::uuid
FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2
ON CONFLICT DO NOTHING`, tenantID, assignedBatch, assignedObl, assignedGoat); err != nil {
		t.Fatalf("seed assignment member: %v", err)
	}

	creator := &rawTaskCreator{pool: pool}
	sweep := oblapp.NewSweeperService(repo, creator, nil)
	cfg := oblapp.SweepConfig{SOPVersionID: skeletonVersion}
	for pass := 0; pass < 2; pass++ {
		if err := sweep.FinalizePlannedBatches(ctx, tenantID, versionID, cfg); err != nil {
			t.Fatalf("finalize pass %d: %v", pass, err)
		}
	}
	if creator.n != 1 {
		t.Fatalf("expected exactly one task for the assigned taskless batch across two passes, got %d", creator.n)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2 AND sop_task_id IS NOT NULL`, tenantID, assignedBatch); got != 1 {
		t.Fatalf("assigned taskless batch still has no sop task")
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2 AND sop_task_id IS NULL`, tenantID, unassignedBatch); got != 1 {
		t.Fatalf("a taskless batch on nobody's roster must be left alone")
	}
}
