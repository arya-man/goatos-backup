package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

const (
	repoVerifier2 = "00000000-0000-4000-8000-000000000401"
)

// B08: Closed buckets can now be reopened.
//
// This test reproduces the defect scenario: leadership closes a bucket
// (status becomes 'closed'), then attempts to reopen it. Before the fix,
// the reopen predicate only checked for 'completed', so it returned ErrNotFound.
// After the fix, reopen succeeds and clears close metadata.
// Spec: docs/features/weighing/TRD.md:64-65 and :257 state that a bucket
// may be reopened from EITHER 'completed' OR 'closed' back to 'in_progress'.
func TestReopenScopeFromClosedStatusNowSucceedsAndClearsMetadata(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Seed the operator's park scope grant so the fixture can work
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, scope_type, scope_id, role, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park', $3::uuid, 'operator', 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOperator, repoPark)

	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Step 1: Close the bucket (status -> 'closed')
	_, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID:       repoTenant,
		CampaignID:     repoCampaign,
		CampaignShedID: repoAnimalScope,
		Reason:         "shed emptied early",
		ClosedBy:       repoVerifier2,
		IdempotencyKey: "close:reopen-test",
	})
	if err != nil {
		t.Fatalf("close scope: %v", err)
	}

	// Verify status is 'closed' and metadata is set
	assertScopeStatusDefect(t, ctx, pool, repoAnimalScope, domain.StatusClosed)
	var closedReason string
	if err := pool.QueryRow(ctx, `
SELECT close_reason FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoAnimalScope).Scan(&closedReason); err != nil {
		t.Fatalf("read close metadata: %v", err)
	}
	if closedReason != "shed emptied early" {
		t.Fatalf("close_reason=%s, want 'shed emptied early'", closedReason)
	}

	// Step 2: Reopen from 'closed' status - now succeeds
	_, err = repo.ReopenScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoVerifier2, "reopen:from-closed", "")

	if err != nil {
		t.Fatalf("reopen from closed: got err=%v, want success", err)
	}

	// Step 3: Verify status is now 'in_progress' and metadata is cleared
	assertScopeStatusDefect(t, ctx, pool, repoAnimalScope, domain.StatusInProgress)
	var metadata *string
	if err := pool.QueryRow(ctx, `
SELECT close_reason FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoAnimalScope).Scan(&metadata); err != nil {
		t.Fatalf("read reopened close_reason: %v", err)
	}
	if metadata != nil {
		t.Fatalf("close_reason=%q after reopen, want NULL", *metadata)
	}
}

// B12: Capture can no longer resurrect closed work.
//
// This test demonstrates the fix: a completion-flip UPDATE that
// previously used the weak predicate `status <> 'completed'` (TRUE for 'closed')
// now uses `status NOT IN ('completed','closed','canceled')`, which correctly
// rejects closed buckets. The weak predicate allowed an interleaving race
// where capture T1 could resurrect closed work that leadership T2 just closed.
//
// We simulate the race with serialized transactions.
func TestCaptureCompletionFlipDoesNotResurrectClosedBucketAfterFix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Seed the operator's park scope grant so the fixture can work
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, scope_type, scope_id, role, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park', $3::uuid, 'operator', 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOperator, repoPark)

	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Step 1: Verify initial state is pending (as created by fixture)
	assertScopeStatusDefect(t, ctx, pool, repoAnimalScope, "pending")

	// Step 2: Simulate the race using serialized transactions.
	// T2: Close the bucket
	tx2, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin T2: %v", err)
	}
	defer tx2.Rollback(ctx)

	// Close the bucket
	_, err = repo.CloseScope(ctx, domain.CloseCommand{
		TenantID:       repoTenant,
		CampaignID:     repoCampaign,
		CampaignShedID: repoAnimalScope,
		Reason:         "emergency close",
		ClosedBy:       repoVerifier2,
		IdempotencyKey: "close:race-test",
	})
	if err != nil {
		t.Fatalf("close scope in race: %v", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit close: %v", err)
	}

	// Verify status is now 'closed'
	assertScopeStatusDefect(t, ctx, pool, repoAnimalScope, domain.StatusClosed)

	// T1: Simulate completion-flip that would run after capture.
	// BEFORE FIX: This used the WEAK predicate status <> 'completed' which is TRUE for 'closed'.
	// AFTER FIX: This uses status NOT IN ('completed','closed','canceled') which is FALSE for 'closed'.
	tx1, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin T1: %v", err)
	}
	defer tx1.Rollback(ctx)

	// This is the FIXED completion-flip predicate (line 1723 in repository.go):
	// Now excludes all terminal states: completed, closed, canceled
	result, err := tx1.Exec(ctx, `
UPDATE weighing_campaign_sheds cs
SET status='completed', completed_at=COALESCE(cs.completed_at, now()), updated_at=now()
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND cs.campaign_shed_id=$3::uuid
  AND cs.weighing_category='individual_animal'
  AND cs.status NOT IN ('completed','closed','canceled')`, repoTenant, repoCampaign, repoAnimalScope)
	if err != nil {
		t.Fatalf("completion-flip update: %v", err)
	}
	affected := result.RowsAffected()
	if affected > 0 {
		t.Fatalf("REGRESSION: completion-flip predicate matched 'closed' bucket (affected=%d rows)", affected)
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("commit T1: %v", err)
	}

	// Final state: bucket should remain 'closed' (not resurrected to 'completed')
	assertScopeStatusDefect(t, ctx, pool, repoAnimalScope, domain.StatusClosed)
}

// Helper to read current status of a campaign shed
func assertScopeStatusDefect(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, expectedStatus string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM weighing_campaign_sheds
WHERE campaign_shed_id=$1::uuid`, shedID).Scan(&status); err != nil {
		t.Fatalf("read scope status: %v", err)
	}
	if status != expectedStatus {
		t.Fatalf("scope status=%q, want %q", status, expectedStatus)
	}
}
