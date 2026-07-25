package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Regression for the drive-membership-orphan bug: CancelOpenObligationByIdempotencyKey and
// MarkMissedBefore (and reapStrandedInProgress, which MarkMissedBefore also drives) set an
// obligation to canceled/missed and NULL its batch_id, but never removed the goat from
// vaccination_drive_assignment_members nor reconciled vaccination_drive_assignments.animal_count/
// total_doses. That left stale member rows and inflated animal_count -- a canceled/missed goat kept
// occupying operator drive capacity forever. pruneDetachedDriveMembershipTx (repository.go) closes
// this by deleting the member row for the terminated obligation and recomputing animal_count/
// total_doses from the REMAINING member rows (never a blind subtract), deleting any assignment row
// left with zero members.
//
// These tests reuse the BUG-041 harness (newBug041Env) to seed a real park+shed+protocol+operator,
// attach obligations to a real batch via CreateBatchWithObligations, and write the drive-assignment
// row + its member rows via the production UpsertVaccinationDriveAssignments path (which internally
// calls syncVaccinationDriveAssignmentMembersTx) -- not a hand-inserted fixture.

func TestPruneDriveMembershipOnIdempotencyKeyCancel(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	env := newBug041Env(t, ctx, pool, "prune_key_cancel")
	ruleID := env.rule(t, "blue_tongue_first", 1)

	shed := "00000000-0000-4000-8000-0000000042a1"
	goats := env.shedWithGoats(t, shed, "Gandhi", "1", 2)
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

	const keyA = "prune-key-cancel-a"
	const keyB = "prune-key-cancel-b"
	oblA := env.obligationFor(t, ruleID, shed, goats[0], keyA, day)
	oblB := env.obligationFor(t, ruleID, shed, goats[1], keyB, day)
	batchID := env.parkBatch(t, "combo:prune-key", day, env.opOne, []string{oblA, oblB})

	shedID := shed
	assignmentID := writeSingleDriveCell(t, ctx, env, batchID, day, shedID, ruleID, len(goats))

	// --- before: both goats are members, animal_count = 2 ---
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, oblA); n != 1 {
		t.Fatalf("pre-cancel member rows for oblA = %d, want 1", n)
	}
	assertAssignmentCounts(t, ctx, pool, assignmentID, 2, 2)

	// --- act: cancel obligation A by its idempotency key (the production single-key cancel path) ---
	canceledID, ok, err := env.repo.CancelOpenObligationByIdempotencyKey(ctx, tenantID, keyA, "superseded", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || !ok || canceledID != oblA {
		t.Fatalf("CancelOpenObligationByIdempotencyKey: id=%q ok=%v err=%v", canceledID, ok, err)
	}

	// --- after: no member row for the canceled obligation ---
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, oblA); n != 0 {
		t.Fatalf("post-cancel member rows for oblA = %d, want 0 (stale membership not pruned)", n)
	}
	// --- animal_count/total_doses reconciled from the ONE remaining member (goat B) ---
	assertAssignmentCounts(t, ctx, pool, assignmentID, 1, 1)
	// goat B's own member row must survive untouched.
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, oblB); n != 1 {
		t.Fatalf("post-cancel member rows for surviving oblB = %d, want 1", n)
	}

	// --- act: cancel obligation B too -- assignment now has zero members and must be deleted ---
	canceledID2, ok2, err := env.repo.CancelOpenObligationByIdempotencyKey(ctx, tenantID, keyB, "superseded", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || !ok2 || canceledID2 != oblB {
		t.Fatalf("CancelOpenObligationByIdempotencyKey (B): id=%q ok=%v err=%v", canceledID2, ok2, err)
	}
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND assignment_id=$2`, tenantID, assignmentID); n != 0 {
		t.Fatalf("assignment row emptied of all members still exists: %d rows, want 0", n)
	}
}

// TestPruneDriveMembershipCancelKeepsSharedGoatCounted proves the "shared assignment" edge case
// explicitly required by the fix: a goat with TWO obligations bound to the SAME assignment row
// (two vaccine lanes covered by one combo drive cell) must stay counted in animal_count after ONE of
// its obligations is canceled, because the goat is still physically covered by the surviving
// obligation's member row.
func TestPruneDriveMembershipCancelKeepsSharedGoatCounted(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	env := newBug041Env(t, ctx, pool, "prune_shared_goat")
	ruleHS := env.rule(t, "hs_adult_w1", 1)
	ruleFMD := env.rule(t, "fmd_adult_w1", 2)

	shed := "00000000-0000-4000-8000-0000000042b1"
	goats := env.shedWithGoats(t, shed, "Gandhi", "1", 1)
	day := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)

	const keyHS = "prune-shared-hs"
	const keyFMD = "prune-shared-fmd"
	oblHS := env.obligationFor(t, ruleHS, shed, goats[0], keyHS, day)
	oblFMD := env.obligationFor(t, ruleFMD, shed, goats[0], keyFMD, day)
	batchID := env.parkBatch(t, "combo:prune-shared", day, env.opOne, []string{oblHS, oblFMD})

	shedID := shed
	if err := env.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: day, OperatorID: &env.opOne, ParkID: env.parkID, ShedID: &shedID,
		PhysicalShed: "Gandhi", PartitionLabel: "Part 1", AnimalCount: 1,
		VaccineRuleIDs: []string{ruleHS, ruleFMD}, TotalDoses: 2, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed shared HS+FMD cell: %v", err)
	}
	assignmentID := singleAssignmentID(t, ctx, pool, batchID)
	assertAssignmentCounts(t, ctx, pool, assignmentID, 1, 2)

	// Cancel only the HS obligation; the goat's FMD obligation still covers it in the same assignment.
	if _, ok, err := env.repo.CancelOpenObligationByIdempotencyKey(ctx, tenantID, keyHS, "superseded", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil || !ok {
		t.Fatalf("cancel HS: ok=%v err=%v", ok, err)
	}

	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, oblHS); n != 0 {
		t.Fatalf("HS member row survived cancel: %d, want 0", n)
	}
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, oblFMD); n != 1 {
		t.Fatalf("FMD member row missing: %d, want 1", n)
	}
	// animal_count stays 1 (goat still covered by FMD); total_doses drops to 1 (one dose-key remains).
	assertAssignmentCounts(t, ctx, pool, assignmentID, 1, 1)
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND assignment_id=$2`, tenantID, assignmentID); n != 1 {
		t.Fatalf("shared assignment wrongly deleted: %d rows, want 1", n)
	}
}

func TestPruneDriveMembershipOnMarkMissedBefore(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	env := newBug041Env(t, ctx, pool, "prune_mark_missed")
	ruleID := env.rule(t, "blue_tongue_first", 1)

	shed := "00000000-0000-4000-8000-0000000042c1"
	goats := env.shedWithGoats(t, shed, "Gandhi", "1", 2)
	// due well in the past so MarkMissedBefore(now) sweeps it. obligationFor sets window_end =
	// due + 30d, and the sweep filters on COALESCE(window_end, due_at) < missedBefore, so the due
	// date must be more than 30 days in the past for the window to have already closed too.
	past := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Add(-40 * 24 * time.Hour)

	oblA := env.obligationFor(t, ruleID, shed, goats[0], "prune-missed-a", past)
	oblB := env.obligationFor(t, ruleID, shed, goats[1], "prune-missed-b", past)
	batchID := env.parkBatch(t, "combo:prune-missed", past, env.opOne, []string{oblA, oblB})

	shedID := shed
	assignmentID := writeSingleDriveCell(t, ctx, env, batchID, past, shedID, ruleID, len(goats))
	assertAssignmentCounts(t, ctx, pool, assignmentID, 2, 2)

	n, err := env.repo.MarkMissedBefore(ctx, tenantID, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 1000)
	if err != nil {
		t.Fatalf("MarkMissedBefore: %v", err)
	}
	if n != 2 {
		t.Fatalf("MarkMissedBefore swept %d obligations, want 2", n)
	}

	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND assignment_id=$2`, tenantID, assignmentID); n != 0 {
		t.Fatalf("member rows survived MarkMissedBefore sweep: %d, want 0", n)
	}
	// Both goats' obligations were swept in the same batch, so the assignment emptied entirely.
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND assignment_id=$2`, tenantID, assignmentID); n != 0 {
		t.Fatalf("assignment emptied by MarkMissedBefore still exists: %d rows, want 0", n)
	}

	// Statuses actually flipped to missed (sanity on the production path, not just membership).
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND obligation_id IN ($2,$3) AND status='missed'`, tenantID, oblA, oblB); got != 2 {
		t.Fatalf("obligations marked missed = %d, want 2", got)
	}
}

// writeSingleDriveCell seeds one drive-assignment row covering the given rule for `count` goats via
// the production UpsertVaccinationDriveAssignments path (which writes the real member rows through
// syncVaccinationDriveAssignmentMembersTx), and returns the resulting assignment_id.
func writeSingleDriveCell(t *testing.T, ctx context.Context, env *bug041Env, batchID string, day time.Time, shedID, ruleID string, count int) string {
	t.Helper()
	if err := env.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: day, OperatorID: &env.opOne, ParkID: env.parkID, ShedID: &shedID,
		PhysicalShed: "Gandhi", PartitionLabel: "Part 1", AnimalCount: int32(count),
		VaccineRuleIDs: []string{ruleID}, TotalDoses: int32(count), CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed drive cell: %v", err)
	}
	return singleAssignmentID(t, ctx, env.pool, batchID)
}

// singleAssignmentID returns the one vaccination_drive_assignments row for a batch, failing the test
// if there is not exactly one (every test in this file seeds exactly one cell per batch).
func singleAssignmentID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
SELECT assignment_id::text FROM vaccination_drive_assignments
WHERE tenant_id = $1 AND batch_id = $2`, tenantID, batchID).Scan(&id); err != nil {
		t.Fatalf("single assignment id for batch %s: %v", batchID, err)
	}
	return id
}

// assertAssignmentCounts asserts the persisted animal_count/total_doses on one assignment row.
func assertAssignmentCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, assignmentID string, wantAnimals, wantDoses int) {
	t.Helper()
	var animals, doses int
	if err := pool.QueryRow(ctx, `
SELECT animal_count, total_doses FROM vaccination_drive_assignments
WHERE tenant_id = $1 AND assignment_id = $2`, tenantID, assignmentID).Scan(&animals, &doses); err != nil {
		t.Fatalf("read assignment counts %s: %v", assignmentID, err)
	}
	if animals != wantAnimals || doses != wantDoses {
		t.Fatalf("assignment %s counts = (animal_count=%d, total_doses=%d), want (%d, %d)", assignmentID, animals, doses, wantAnimals, wantDoses)
	}
}
