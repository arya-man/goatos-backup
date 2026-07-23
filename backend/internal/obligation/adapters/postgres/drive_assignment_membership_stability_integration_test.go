package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestDriveAssignmentMembershipIsStableAcrossRecomputes is the BUG-035 regression proof.
//
// vaccination_drive_assignment_members binds ONE goat/obligation to ONE assignment row (an
// operator's day on a physical partition). When a batch/shed holds several assignment cells that
// plan the SAME vaccine but differ by partition, operator, or date, the binding used to be ranked
// by assignment_id -- a random UUID -- so the same fixture bound the same goat to a DIFFERENT
// operator's arm from run to run. Downstream that means the animal's exit decrements a stranger's
// route, and CT/PA/WF/AC report the wrong operator-day for a named animal.
//
// This test recomputes membership repeatedly with IDENTICAL business inputs, regenerating the
// assignment rows (and therefore their random assignment_ids) each round, and asserts the goat
// always lands on its OWN partition's cell.
func TestDriveAssignmentMembershipIsStableAcrossRecomputes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		goatID      = "10000000-0000-4000-8000-00000000fd01"
		shedID      = "00000000-0000-4000-8000-00000000dd01"
		operatorOne = "20000000-0000-4000-8000-000000000d01"
		operatorTwo = "20000000-0000-4000-8000-000000000d02"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "membership-stability-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)
	for id, code := range map[string]string{operatorOne: "OP-STAB-1", operatorTwo: "OP-STAB-2"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')`, id, tenantID, code); err != nil {
			t.Fatalf("seed operator %s: %v", code, err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'Gandhi A')`, tenantID, goatID, shedID); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.memberstability", Name: "Member stability", Category: "vaccination", Status: "draft",
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
		t.Fatalf("rule: %v", err)
	}

	planned := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	obligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleA,
		TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
		DueAt: planned, Status: "scheduled", IdempotencyKey: "membership-stability-dose", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation: applied=%v err=%v", applied, err)
	}
	opOne := operatorOne
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "membership-stability-drive", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 9, PlannedQuantity: "9", QuantityUnit: "dose", ConductedBy: &opOne,
	}, []string{obligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	opTwo := operatorTwo
	nextDay := planned.AddDate(0, 0, 1)
	// Two cells plan the SAME vaccine: the goat's own partition "A" (operator one, day 1) and a
	// sibling partition "B" (operator two, day 2). Only partition "A" is where the animal is.
	assignments := []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: planned, OperatorID: &opOne, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 5,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 5, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: nextDay, OperatorID: &opTwo, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "B", AnimalCount: 4,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 4, CapacityStatus: "within_cap"},
	}

	const rounds = 8
	for round := 1; round <= rounds; round++ {
		// Drop and rewrite the cells so every round mints FRESH random assignment_ids: any ranking
		// that depends on those UUIDs will flip within a handful of rounds.
		if _, err := pool.Exec(ctx,
			`DELETE FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); err != nil {
			t.Fatalf("round %d: clear assignments: %v", round, err)
		}
		if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, assignments); err != nil {
			t.Fatalf("round %d: upsert assignments: %v", round, err)
		}

		var partition, operator string
		var boundDate time.Time
		if err := pool.QueryRow(ctx, `
SELECT vda.partition_label, vda.operator_id::text, vda.planned_date
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda
  ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
WHERE m.tenant_id=$1 AND m.obligation_id=$2`, tenantID, obligationID).Scan(&partition, &operator, &boundDate); err != nil {
			t.Fatalf("round %d: read membership: %v", round, err)
		}
		if partition != "A" || operator != operatorOne || !boundDate.Equal(planned) {
			t.Fatalf("round %d: goat bound to partition=%s operator=%s date=%s, want A/%s/%s (membership must follow the animal's own placement, not a random assignment_id)",
				round, partition, operator, boundDate.Format("2006-01-02"), operatorOne, planned.Format("2006-01-02"))
		}
	}
}
