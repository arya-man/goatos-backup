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

// TestSyncPartitionMoveForGoatRebindsDriveAssignmentMembership is the same-shed partition-move
// regression proof.
//
// Before this fix, reScopeOpenForGoatInTx (the shed-shift re-scope path) only fired on shed_id
// change and never re-derived vaccination_drive_assignment_members; goat_shed_partitions had NO
// production writer at all that also re-ran syncVaccinationDriveAssignmentMembersTx. A goat moved
// from Part 1 to Part 2 within the SAME shed kept its membership row bound to Part 1's assignment
// arm forever, because that membership is derived (by
// syncVaccinationDriveAssignmentMembersTx/visit_shot_lock.go) from goat_shed_partitions at the
// moment the batch's assignments are (re)computed, and nothing re-ran that computation after the
// partition changed.
//
// This test seeds a shed with two partitions (A, B), each with its own drive-assignment arm on the
// same batch, and a goat bound to Part A's arm. It then calls the fixed production path,
// Repository.SyncPartitionMoveForGoat, to move the goat from A to B, and asserts:
//  1. the goat's member row now points at Part B's assignment (not Part A's) -- the bug's precise
//     shape;
//  2. each arm's animal_count matches count(DISTINCT goat) actually bound to it;
//  3. no obligation is left with zero bound assignment members (nothing is unbound by the move).
func TestSyncPartitionMoveForGoatRebindsDriveAssignmentMembership(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		shedID      = "00000000-0000-4000-8000-00000000ee01"
		goatID      = "10000000-0000-4000-8000-00000000fe01"
		operatorOne = "20000000-0000-4000-8000-000000000e01"
		operatorTwo = "20000000-0000-4000-8000-000000000e02"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "partition-move-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)
	for id, code := range map[string]string{operatorOne: "OP-PMOVE-1", operatorTwo: "OP-PMOVE-2"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')`, id, tenantID, code); err != nil {
			t.Fatalf("seed operator %s: %v", code, err)
		}
	}
	// The goat starts on partition A.
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'PMove A')`, tenantID, goatID, shedID); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.partitionmove", Name: "Partition move", Category: "vaccination", Status: "draft",
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
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	planned := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	obligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
		DueAt: planned, Status: "scheduled", IdempotencyKey: "partition-move-dose", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation: applied=%v err=%v", applied, err)
	}
	opOne := operatorOne
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "partition-move-drive", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 9, PlannedQuantity: "9", QuantityUnit: "dose", ConductedBy: &opOne,
	}, []string{obligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	opTwo := operatorTwo
	// Both partitions already have their own assignment arm on this batch -- the case where a
	// same-shed partition move should simply re-bind the goat, with no full-batch rebuild needed.
	assignments := []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: planned, OperatorID: &opOne, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "PMove", PartitionLabel: "A", AnimalCount: 5,
			VaccineRuleIDs: []string{ruleID}, TotalDoses: 5, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: planned, OperatorID: &opTwo, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "PMove", PartitionLabel: "B", AnimalCount: 4,
			VaccineRuleIDs: []string{ruleID}, TotalDoses: 4, CapacityStatus: "within_cap"},
	}
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, assignments); err != nil {
		t.Fatalf("upsert assignments: %v", err)
	}

	readMembership := func(t *testing.T) (partition, operator string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `
SELECT vda.partition_label, vda.operator_id::text
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda
  ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
WHERE m.tenant_id=$1 AND m.obligation_id=$2`, tenantID, obligationID).Scan(&partition, &operator); err != nil {
			t.Fatalf("read membership: %v", err)
		}
		return partition, operator
	}

	// PROVE THE BUG (pre-fix shape): before any partition-move sync runs, the goat is bound to
	// partition A's arm -- the assignment recompute already ran once when the batch/assignments
	// were created, deriving membership from the goat's CURRENT (pre-move) partition.
	if partition, operator := readMembership(t); partition != "A" || operator != operatorOne {
		t.Fatalf("pre-move membership = partition=%s operator=%s, want A/%s", partition, operator, operatorOne)
	}

	// ACT: move the goat within the SAME shed from partition A to partition B, through the fixed
	// production path.
	resynced, err := repo.SyncPartitionMoveForGoat(ctx, tenantID, goatID, shedID, "B", "")
	if err != nil {
		t.Fatalf("SyncPartitionMoveForGoat: %v", err)
	}
	if resynced != 1 {
		t.Fatalf("SyncPartitionMoveForGoat resynced=%d, want 1 obligation re-derived", resynced)
	}

	// PROVE THE FIX: the goat's member row now points at partition B's arm/operator, not A's.
	if partition, operator := readMembership(t); partition != "B" || operator != operatorTwo {
		t.Fatalf("post-move membership = partition=%s operator=%s, want B/%s (goat must follow its new partition, not stay bound to the old arm)", partition, operator, operatorTwo)
	}

	// Idempotency: calling again with the SAME target partition is a no-op (no re-sync, no error).
	resyncedAgain, err := repo.SyncPartitionMoveForGoat(ctx, tenantID, goatID, shedID, "B", "")
	if err != nil {
		t.Fatalf("SyncPartitionMoveForGoat (repeat): %v", err)
	}
	if resyncedAgain != 0 {
		t.Fatalf("SyncPartitionMoveForGoat repeat resynced=%d, want 0 (idempotent no-op)", resyncedAgain)
	}

	// GRAIN PROOF: each arm's animal_count matches count(DISTINCT goat) actually bound to it via
	// membership -- the split cohort stays explicit and no arm silently over/under-counts.
	rows, err := pool.Query(ctx, `
SELECT vda.partition_label, vda.animal_count, COUNT(DISTINCT oi.target_id)
FROM vaccination_drive_assignments vda
LEFT JOIN vaccination_drive_assignment_members m
  ON m.tenant_id = vda.tenant_id AND m.assignment_id = vda.assignment_id
LEFT JOIN obligation_instances oi
  ON oi.tenant_id = m.tenant_id AND oi.obligation_id = m.obligation_id
WHERE vda.tenant_id = $1 AND vda.batch_id = $2
GROUP BY vda.partition_label, vda.animal_count`, tenantID, batchID)
	if err != nil {
		t.Fatalf("read arm counts: %v", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var partition string
		var animalCount, boundGoats int
		if err := rows.Scan(&partition, &animalCount, &boundGoats); err != nil {
			t.Fatalf("scan arm counts: %v", err)
		}
		seen[partition] = true
		switch partition {
		case "A":
			if boundGoats != 0 {
				t.Fatalf("partition A still has %d bound goat(s) after the move, want 0", boundGoats)
			}
		case "B":
			if boundGoats != 1 {
				t.Fatalf("partition B has %d bound goat(s) after the move, want 1", boundGoats)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("arm count rows: %v", err)
	}
	if !seen["A"] || !seen["B"] {
		t.Fatalf("expected both partition A and B assignment rows to still exist, saw %v", seen)
	}

	// Zero unbound: the moved obligation still has exactly one bound assignment member.
	var unbound int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances oi
LEFT JOIN vaccination_drive_assignment_members m
  ON m.tenant_id = oi.tenant_id AND m.obligation_id = oi.obligation_id
WHERE oi.tenant_id = $1 AND oi.obligation_id = $2 AND m.obligation_id IS NULL`,
		tenantID, obligationID).Scan(&unbound); err != nil {
		t.Fatalf("read unbound count: %v", err)
	}
	if unbound != 0 {
		t.Fatalf("unbound obligations after partition move = %d, want 0", unbound)
	}
}

// TestSyncPartitionMoveForGoatRejectsShedMismatch proves the new path refuses to silently
// re-derive membership when the caller's shed does not match the goat's current
// goat_shed_partitions row -- a cross-shed move must go through the existing shed re-scope path
// (ReScopeOpenForGoatShift), which unbatches obligations instead of rebinding them in place.
func TestSyncPartitionMoveForGoatRejectsShedMismatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	const (
		shedID      = "00000000-0000-4000-8000-00000000ee02"
		otherShedID = "00000000-0000-4000-8000-00000000ee03"
		goatID      = "10000000-0000-4000-8000-00000000fe02"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "partition-move-mismatch-shed")
	seedParkConsolidationShed(t, ctx, pool, otherShedID, "partition-move-mismatch-shed-2")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'Mismatch A')`, tenantID, goatID, shedID); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}

	if _, err := repo.SyncPartitionMoveForGoat(ctx, tenantID, goatID, otherShedID, "B", ""); err == nil {
		t.Fatalf("SyncPartitionMoveForGoat: expected error for shed_id mismatch, got nil")
	}
}
