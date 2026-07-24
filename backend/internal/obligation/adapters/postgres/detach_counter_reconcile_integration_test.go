package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestPartitionMoveReconcilesArmCounters proves that a same-shed partition move leaves BOTH the
// vacated arm and the gained arm with animal_count == count(distinct member goat). The membership
// re-sync alone rewrites member rows but not the per-assignment counters, and the vacated arm SHRINKS
// (1 -> 0), which the grow-only reconcileDriveAssignmentCountersFromMembersTx (completeness gate
// ledger_animals >= animal_count) cannot do -- so this is the regression for the exact-recompute fix.
func TestPartitionMoveReconcilesArmCounters(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	env := newBug041Env(t, ctx, pool, "detach_partition_counters")
	ruleID := env.rule(t, "blue_tongue_adult_w1", 1)

	shed := "00000000-0000-4000-8000-0000000042a1"
	// Two goats: G1 in partition 1, G2 in partition 2 (so both arms have a member and neither arm is
	// deleted, isolating the counter-shrink on arm 1 when G1 moves to arm 2).
	seedParkConsolidationShed(t, ctx, pool, shed, "Gandhi")
	g1 := "96000000-0000-4000-8000-0000000042a1"
	g2 := "96000000-0000-4000-8000-0000000042a2"
	seedReserveGoats(t, ctx, pool, shed, env.parkID, g1, g2)
	for _, m := range []struct{ g, p string }{{g1, "1"}, {g2, "2"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name) VALUES ($1,$2,$3,$4,'Gandhi')`, tenantID, m.g, shed, m.p); err != nil {
			t.Fatalf("seed gsp: %v", err)
		}
	}
	day := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	o1 := env.obligationFor(t, ruleID, shed, g1, "bt-p1", day)
	o2 := env.obligationFor(t, ruleID, shed, g2, "bt-p2", day)
	batchID := env.parkBatch(t, "combo:BT", day, env.opOne, []string{o1, o2})
	shedID := shed
	// Two arms, each with its own partition + one goat.
	if err := env.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: day, OperatorID: &env.opOne, ParkID: env.parkID, ShedID: &shedID, PhysicalShed: "Gandhi", PartitionLabel: "Part 1", AnimalCount: 1, VaccineRuleIDs: []string{ruleID}, TotalDoses: 1, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: day, OperatorID: &env.opTwo, ParkID: env.parkID, ShedID: &shedID, PhysicalShed: "Gandhi", PartitionLabel: "Part 2", AnimalCount: 1, VaccineRuleIDs: []string{ruleID}, TotalDoses: 1, CapacityStatus: "within_cap"},
	}); err != nil {
		t.Fatalf("seed arms: %v", err)
	}
	// Sanity: G1 bound to Part 1 arm, G2 to Part 2 arm.
	armCount := func(part string) int {
		return countRows(t, ctx, pool, `SELECT count(DISTINCT m.goat_id) FROM vaccination_drive_assignments v JOIN vaccination_drive_assignment_members m ON m.assignment_id=v.assignment_id WHERE v.tenant_id=$1 AND v.batch_id=$2 AND v.partition_label=$3`, tenantID, batchID, part)
	}
	if armCount("Part 1") != 1 || armCount("Part 2") != 1 {
		t.Fatalf("pre-move members: Part1=%d Part2=%d want 1/1", armCount("Part 1"), armCount("Part 2"))
	}

	// Move G1 from partition 1 -> 2 (same shed) via the production primitive.
	if _, err := env.repo.SyncPartitionMoveForGoat(ctx, tenantID, g1, shed, "2", "Gandhi"); err != nil {
		t.Fatalf("SyncPartitionMoveForGoat: %v", err)
	}

	// Membership moved: Part 1 arm now empty, Part 2 arm has both goats.
	if armCount("Part 1") != 0 || armCount("Part 2") != 2 {
		t.Fatalf("post-move members: Part1=%d Part2=%d want 0/2", armCount("Part 1"), armCount("Part 2"))
	}
	// COUNTER PARITY: every remaining arm's animal_count == its distinct member goats (the fix).
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments v WHERE v.tenant_id=$1 AND v.batch_id=$2 AND v.animal_count <> (SELECT count(DISTINCT m.goat_id) FROM vaccination_drive_assignment_members m WHERE m.assignment_id=v.assignment_id)`, tenantID, batchID); n != 0 {
		t.Fatalf("arms with animal_count != distinct members after partition move = %d, want 0", n)
	}
	_ = o2
}

// TestRepairStaleMissedBatchLinksPrunesDriveMembership proves that RepairStaleMissedVaccinationBatchLinks
// removes the detached missed obligations from vaccination_drive_assignment_members and reconciles the
// assignment's animal_count, instead of leaving a repaired-missed goat counted on an operator's sheet.
func TestRepairStaleMissedMissedBatchLinksPrunesDriveMembership(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	env := newBug041Env(t, ctx, pool, "detach_missed_repair")
	ruleID := env.rule(t, "blue_tongue_adult_w1", 1)

	shed := "00000000-0000-4000-8000-0000000042b1"
	goats := env.shedWithGoats(t, shed, "Gandhi", "1", 2)
	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // past due
	obls := make([]string, 0, len(goats))
	for i, g := range goats {
		obls = append(obls, env.obligationFor(t, ruleID, shed, g, fmt.Sprintf("miss-%d", i), day))
	}
	batchID := env.parkBatch(t, "combo:BT", day, env.opOne, obls)
	shedID := shed
	if err := env.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: day, OperatorID: &env.opOne, ParkID: env.parkID, ShedID: &shedID, PhysicalShed: "Gandhi", PartitionLabel: "Part 1", AnimalCount: int32(len(goats)), VaccineRuleIDs: []string{ruleID}, TotalDoses: int32(len(goats)), CapacityStatus: "within_cap"},
	}); err != nil {
		t.Fatalf("seed arm: %v", err)
	}
	// Mark ONE obligation missed (still attached to the batch) -- the repair's candidate shape.
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='missed' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obls[0]); err != nil {
		t.Fatalf("mark missed: %v", err)
	}
	memberCount := func(oblID string) int {
		return countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, oblID)
	}
	if memberCount(obls[0]) != 1 {
		t.Fatalf("pre-repair: missed obligation has %d member rows, want 1", memberCount(obls[0]))
	}

	n, err := env.repo.RepairStaleMissedVaccinationBatchLinks(ctx, tenantID, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 1000)
	if err != nil {
		t.Fatalf("RepairStaleMissedVaccinationBatchLinks: %v", err)
	}
	if n < 1 {
		t.Fatalf("repaired %d, want >=1", n)
	}
	// The missed obligation is detached from the batch AND pruned from drive membership.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2 AND batch_id IS NULL`, tenantID, obls[0]); got != 1 {
		t.Fatalf("missed obligation not detached from batch")
	}
	if memberCount(obls[0]) != 0 {
		t.Fatalf("post-repair: missed obligation still has %d drive member rows, want 0", memberCount(obls[0]))
	}
	// Remaining assignment counters match remaining members (the still-open goat).
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments v WHERE v.tenant_id=$1 AND v.batch_id=$2 AND v.animal_count <> (SELECT count(DISTINCT m.goat_id) FROM vaccination_drive_assignment_members m WHERE m.assignment_id=v.assignment_id)`, tenantID, batchID); n != 0 {
		t.Fatalf("assignment animal_count != distinct members after missed repair = %d, want 0", n)
	}
}
