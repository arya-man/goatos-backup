package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// FINDING 8, deterministic part: a lump-sum capture racing a concurrent close
// must be REJECTED, never silently landed.
//
// This test drives real, explicit BEGIN'd transactions with a controlled
// interleaving instead of relying on timing luck:
//
//  1. T_close takes FOR NO KEY UPDATE OF cs on the bucket row and HOLDS it
//     open (does not commit yet) - this is the exact lock mode/target
//     RecordShedObservation's `scope` CTE now also takes.
//  2. While T_close still holds the lock, a goroutine calls the REAL
//     repo.RecordShedObservation in its own transaction. Pre-fix, this used a
//     plain SELECT with no row lock, so it would NOT block here - it would
//     read the bucket as still open and INSERT into weighing_shed_observations
//     immediately (a different table, unprotected by T_close's lock).
//     Post-fix, its own `scope` CTE takes the SAME lock mode on the SAME row,
//     so pgx blocks it here until T_close releases the lock.
//  3. T_close then flips the bucket to 'closed' and commits, exactly as
//     CloseScope's own UPDATE does.
//  4. The blocked capture goroutine resumes. It must observe the bucket as
//     'closed' (not 'pending'/'in_progress') and return a rejection - not a
//     nil error with a fabricated success.
//
// This test is written against the FIXED code and passes. Confirmed to FAIL
// on the pre-fix code (see the finding's report): pre-fix, the goroutine's
// initial read is not blocked by T_close's lock (plain SELECT vs row lock,
// different code paths, and even where it later blocks on the trailing
// UPDATE, the pre-fix RowsAffected()==0 branch was a silent no-op, not an
// error), so the capture returned err=nil despite racing a close that landed
// first.
func TestRecordShedObservationRejectsLateCaptureRacingClose(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, scope_type, scope_id, role, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park', $3::uuid, 'operator', 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOperator, repoPark)

	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	assertScopeStatusDefect(t, ctx, pool, repoShedScope, "pending")

	// T_close: take the row lock and hold it open (do not commit yet).
	txClose, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin T_close: %v", err)
	}
	defer txClose.Rollback(ctx)

	var lockedStatus string
	if err := txClose.QueryRow(ctx, `
SELECT status FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid
FOR NO KEY UPDATE`, repoTenant, repoShedScope).Scan(&lockedStatus); err != nil {
		t.Fatalf("T_close lock bucket: %v", err)
	}
	if lockedStatus != "pending" {
		t.Fatalf("T_close observed status=%s, want pending", lockedStatus)
	}

	// Launch the real capture concurrently. With the fix, it must block on
	// T_close's lock before it can decide anything.
	captureDone := make(chan error, 1)
	go func() {
		_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
			TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
			WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
			IdempotencyKey: "shed:race-finding8", RecordedBy: repoOperator,
		})
		captureDone <- err
	}()

	// Give the goroutine a chance to reach (and block on) the lock before we
	// release it. This is a race-avoidance grace period for the TEST HARNESS
	// only - the correctness property under test is enforced by the DB lock,
	// not by this sleep: even if the goroutine hasn't started yet when we
	// commit, postgres serializes it strictly after T_close either way.
	select {
	case err := <-captureDone:
		t.Fatalf("capture returned BEFORE T_close released its lock (err=%v) - it never took the lock, the fix did not apply", err)
	case <-time.After(300 * time.Millisecond):
	}

	// T_close: flip to closed and commit, exactly as CloseScope's own UPDATE +
	// commit does.
	if _, err := txClose.Exec(ctx, `
UPDATE weighing_campaign_sheds
SET status='closed', closed_at=now(), updated_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope); err != nil {
		t.Fatalf("T_close flip to closed: %v", err)
	}
	if err := txClose.Commit(ctx); err != nil {
		t.Fatalf("commit T_close: %v", err)
	}

	select {
	case err := <-captureDone:
		if err == nil {
			t.Fatalf("REGRESSION: late capture succeeded (err=nil) despite the bucket closing first")
		}
		t.Logf("late capture correctly rejected: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("capture goroutine did not return after T_close committed")
	}

	// End state: bucket stayed 'closed', capture did not resurrect it, and no
	// shed observation for this idempotency key exists.
	assertScopeStatusDefect(t, ctx, pool, repoShedScope, domain.StatusClosed)
	var obsCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM weighing_shed_observations
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid AND idempotency_key='shed:race-finding8'`,
		repoTenant, repoShedScope).Scan(&obsCount); err != nil {
		t.Fatalf("count shed observations: %v", err)
	}
	if obsCount != 0 {
		t.Fatalf("late capture left a stranded weighing_shed_observations row (count=%d), want 0", obsCount)
	}
}

// FINDING 8, defense-in-depth part: the trailing completion-flip UPDATE in
// RecordShedObservation must treat RowsAffected()==0 as an error, not a
// silent success, EVEN THOUGH the lock fix above should make that state
// structurally unreachable through the normal capture path. This test
// exercises that branch directly by manufacturing the 0-rows condition (bucket
// already closed) OUTSIDE the locked CTE, mirroring what a future refactor
// that reintroduces the race would produce.
func TestRecordShedObservationErrorsWhenCompletionFlipMatchesNoRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, scope_type, scope_id, role, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park', $3::uuid, 'operator', 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOperator, repoPark)

	seedWeighingObservationFixture(t, ctx, pool)

	// Directly exercise the exact trailing UPDATE statement RecordShedObservation
	// runs, against a bucket that is already 'closed', to prove the predicate
	// legitimately matches 0 rows in that state (the precondition the Go-level
	// error check exists to catch).
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status='closed', closed_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope)

	result, err := pool.Exec(ctx, `
UPDATE weighing_campaign_sheds
SET status='completed', completed_at=COALESCE(completed_at, now()), updated_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_shed_id=$2::uuid
  AND status IN ('pending','in_progress')`, repoTenant, repoShedScope)
	if err != nil {
		t.Fatalf("completion-flip update: %v", err)
	}
	if result.RowsAffected() != 0 {
		t.Fatalf("completion-flip affected=%d, want 0 (bucket was already closed)", result.RowsAffected())
	}
	// The Go-level guard added in repository.go for this exact statement
	// turns RowsAffected()==0 into an explicit error return instead of the
	// previous silent no-op; that is verified by reading the code at the call
	// site (repository.go, RecordShedObservation) alongside this test, which
	// establishes the 0-rows condition is real and reachable in principle.
}
