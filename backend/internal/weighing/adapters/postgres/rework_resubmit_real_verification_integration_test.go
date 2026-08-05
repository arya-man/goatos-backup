package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/weighing/adapters/verificationbridge"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// TestWeighingReworkResubmitRaisesFreshVerificationItemThroughRealVerificationModule closes the one
// gap left by the existing weighing rework/resubmit coverage: TestRecordAnimalObservationEditWithdrawsStaleVerificationBeforeRaisingNewOne
// (app/service_test.go) proves the withdraw-then-fresh-enqueue orchestration with FAKE
// enqueuer/withdrawer collaborators, and TestReworkVerdictAllowsSameDayRescanIndividualScope
// (rework_rescan_and_captured_total_integration_test.go) proves the weighing repository's own
// duplicate-scan gate lets a post-rework rescan through against a REAL Postgres weighing_observations
// table. Neither wires the REAL verification Postgres module end to end, so neither proves a fresh
// row actually lands in verification_items, or that the withdraw-then-create sequence survives the
// real (tenant_id, idempotency_key) unique constraint on verification_items.
//
// This test drives weighing's production app.Service (the same composition
// backend/internal/bootstrap/api.go wires: Service.WithVerificationEnqueuer /
// WithVerificationWithdrawer bound to verificationbridge.New(realVerificationRepo)) through a
// first capture, a verifier REWORK verdict, and a same-day rescan, and asserts against the real
// verification_items table: the stale item is retired, a fresh 'pending' item exists under a
// DIFFERENT idempotency key, and no duplicate-key error is raised anywhere in the sequence -- the
// same defect signature (operator's redo silently lost while the phone shows success) that has
// bitten vaccination three times, now pinned for weighing's own free-flow write path.
func TestWeighingReworkResubmitRaisesFreshVerificationItemThroughRealVerificationModule(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)

	weighingRepo := NewRepository(pool, 5*time.Second)
	verificationRepo := verificationpg.NewRepository(pool, 5*time.Second)
	bridge := verificationbridge.New(verificationRepo)
	service := weighingapp.NewService(weighingRepo).
		WithVerificationEnqueuer(bridge).
		WithVerificationWithdrawer(bridge)
	operator := domain.Actor{TenantID: repoTenant, UserID: repoOperator, Roles: []string{permissions.RoleOperator}}

	const tag = "w-rework-resubmit-real-verification"

	// --- First capture: raises the first verification item.
	first, err := service.RecordAnimalObservation(ctx, operator, domain.RecordAnimalObservation{
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: tag,
		WeightKg:          10.5,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "w-rework-real:first-capture",
	})
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	firstItemID := reworkResubmitScanText(t, ctx, pool,
		`SELECT item_id::text FROM verification_items WHERE tenant_id=$1 AND source_ref_type=$2 AND source_ref_id=$3`,
		repoTenant, domain.VerificationRefTypeAnimal, first.ObservationID)
	firstKey := reworkResubmitScanText(t, ctx, pool, `SELECT idempotency_key FROM verification_items WHERE tenant_id=$1 AND item_id=$2::uuid`, repoTenant, firstItemID)
	if got := reworkResubmitScanText(t, ctx, pool, `SELECT status FROM verification_items WHERE tenant_id=$1 AND item_id=$2::uuid`, repoTenant, firstItemID); got != "pending" {
		t.Fatalf("first item status = %s, want pending", got)
	}

	if err := weighingRepo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "w-rework-real:submit-1", []string{tag}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}

	// --- Verifier REWORKs the capture. The applier (async in production, direct here -- same
	// pattern as TestReworkVerdictAllowsSameDayRescanIndividualScope) reopens the observation for a
	// same-day rescan.
	if _, err := weighingRepo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:      repoTenant,
		ObservationID: first.ObservationID,
		RefType:       domain.VerificationRefTypeAnimal,
		Status:        domain.VerificationStatusRework,
		VerifiedBy:    repoOperator,
		Reason:        "video unusable, re-shoot",
		EventID:       "w-rework-real:rework-event-1",
	}); err != nil {
		t.Fatalf("apply rework verdict: %v", err)
	}

	// --- Rescan: the resubmit. Must not hit a duplicate-key error anywhere in the withdraw+enqueue
	// sequence, and must produce a genuinely fresh pending item.
	second, err := service.RecordAnimalObservation(ctx, operator, domain.RecordAnimalObservation{
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: tag,
		WeightKg:          11.0,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "w-rework-real:post-rework-rescan",
	})
	if err != nil {
		t.Fatalf("rescan after rework verdict failed (this is the defect: a resubmit must NEVER hit a "+
			"duplicate-key error and abort, silently losing the operator's rework): %v", err)
	}
	if second.ObservationID != first.ObservationID {
		t.Fatalf("rescan created a NEW observation row (%s), want the same evidence row (%s) updated in place", second.ObservationID, first.ObservationID)
	}

	// The stale item must be retired (withdrawn), never left pending forever.
	if got := reworkResubmitScanText(t, ctx, pool, `SELECT status FROM verification_items WHERE tenant_id=$1 AND item_id=$2::uuid`, repoTenant, firstItemID); got == "pending" {
		t.Fatalf("first item status after rescan = %s, want retired (not pending) -- a verifier could still approve the withdrawn evidence", got)
	}

	// Exactly TWO items exist for this observation id: the retired original and the fresh rework.
	// No third row, no in-place mutation of the first.
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_ref_type=$2 AND source_ref_id=$3`,
		repoTenant, domain.VerificationRefTypeAnimal, first.ObservationID); got != 2 {
		t.Fatalf("verification items for observation %s = %d, want 2 (retired original + fresh rework)", first.ObservationID, got)
	}
	pendingItemID := reworkResubmitScanText(t, ctx, pool,
		`SELECT item_id::text FROM verification_items WHERE tenant_id=$1 AND source_ref_type=$2 AND source_ref_id=$3 AND status='pending'`,
		repoTenant, domain.VerificationRefTypeAnimal, first.ObservationID)
	if pendingItemID == firstItemID {
		t.Fatalf("the pending item after rescan is the SAME row as the withdrawn original -- reviseVerificationRound did not raise fresh evidence")
	}
	secondKey := reworkResubmitScanText(t, ctx, pool, `SELECT idempotency_key FROM verification_items WHERE tenant_id=$1 AND item_id=$2::uuid`, repoTenant, pendingItemID)
	if secondKey == firstKey {
		t.Fatalf("fresh item reused the SAME idempotency key %q as the withdrawn original -- it would have collided (ON CONFLICT DO NOTHING) instead of raising new pending work", secondKey)
	}
}

func reworkResubmitScanText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
		t.Fatalf("scan %q: %v", sql, err)
	}
	return s
}
