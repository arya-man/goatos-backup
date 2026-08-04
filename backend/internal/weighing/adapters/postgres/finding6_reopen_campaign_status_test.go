package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// FINDING 6: ReopenScope reopens the bucket but leaves the parent campaign
// stranded in 'closed', so capture stays permanently rejected.
//
// Reproduction: close the whole campaign (CloseCampaign flips both the bucket
// AND the campaign to 'closed'), then ReopenScope on that one bucket. Before
// the fix, ReopenScope's campaign-status UPDATE predicate only matched
// status='completed', never 'closed', so the campaign stayed 'closed' and a
// subsequent capture attempt (gated on campaign.status IN
// ('published','in_progress','delayed')) was rejected even though the bucket
// itself was back to 'in_progress'.
func TestReopenScopeAfterCampaignCloseRestoresCampaignCapturability(t *testing.T) {
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

	// Step 1: Close the WHOLE campaign. This cascades both buckets (including
	// repoAnimalScope, still 'pending') to 'closed' and flips the campaign
	// itself to 'closed'.
	_, err := repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID:       repoTenant,
		CampaignID:     repoCampaign,
		Reason:         "season ended",
		ClosedBy:       repoVerifier2,
		IdempotencyKey: "close-campaign:finding6",
	})
	if err != nil {
		t.Fatalf("close campaign: %v", err)
	}
	assertCampaignStatus(t, ctx, pool, domain.StatusClosed)
	assertScopeStatusDefect(t, ctx, pool, repoAnimalScope, domain.StatusClosed)

	// Step 2: Reopen just the one bucket.
	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoVerifier2, "reopen:finding6", "rework needed"); err != nil {
		t.Fatalf("reopen scope: %v", err)
	}
	assertScopeStatusDefect(t, ctx, pool, repoAnimalScope, domain.StatusInProgress)

	// Step 3: the campaign itself must be capturable again. This is the
	// defect: before the fix the campaign stayed 'closed' here.
	assertCampaignStatus(t, ctx, pool, domain.StatusInProgress)

	// Step 4: prove capture actually works end-to-end now (the real-world
	// symptom), by exercising the SAME gate SubmitObservation-style writes use.
	var gateOK bool
	if err := pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM weighing_campaigns
  WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid
    AND status IN ('published','in_progress','delayed')
)`, repoTenant, repoCampaign).Scan(&gateOK); err != nil {
		t.Fatalf("check capture gate: %v", err)
	}
	if !gateOK {
		t.Fatalf("capture gate still rejects campaign after reopen (campaign not in capturable status)")
	}
}
