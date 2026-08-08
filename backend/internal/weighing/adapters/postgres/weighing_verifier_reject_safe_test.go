package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// TestVerifierRejectAllowsResubmitOnSiblingShed proves weighing does NOT contain
// the vaccination deadlock class.
//
// Vaccination deadlock: one parent sop_tasks row covered two sheds via two
// sop_submissions. When shed B's submission was fully accepted, the shared parent
// task flipped to 'accepted' while shed A's submission still had 'needs_review'
// items. SubmitTask refused any submission on 'accepted' task, so shed A's rework
// could never be submitted.
//
// Weighing is SAFE because:
//  1. Campaign does NOT auto-close when one bucket reaches 'completed'.
//     Campaign only closes when ALL buckets are terminal ('closed'/'canceled').
//  2. Verifier REJECT (rework) reopens the bucket to 'in_progress' state.
//  3. Rework buckets never reach terminal state -- they go back to 'in_progress'.
//  4. Therefore, campaign can never enter a state that refuses resubmission
//     while a sibling bucket is still awaiting rework.
//
// This test models two buckets (A and B) in one campaign: both submitted to
// 'completed', bucket A's observations approved (auto-closing it), bucket B's
// observations rejected (bounced for rework). Verify bucket B can be re-submitted.
func TestVerifierRejectAllowsResubmitOnSiblingShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	// Seed the operator's park scope grant so the fixture can work
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, scope_type, scope_id, role, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park', $3::uuid, 'operator', 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOperator, repoPark)

	seedWeighingObservationFixture(t, ctx, pool)

	// Move campaign from 'published' to 'in_progress' for testing
	setCampaignStatus(t, ctx, pool, domain.StatusInProgress)

	// Use repoShedScope (bucket A) and create a second bucket (shedTwo) using a known location
	// to model the vaccination deadlock scenario
	shedTwo := "00000000-0000-4000-8000-000000009199"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Test Shed B', 'per_shed_partition', $5::uuid, 1)
ON CONFLICT (campaign_shed_id) DO UPDATE SET weighing_category=EXCLUDED.weighing_category, operator_user_id=EXCLUDED.operator_user_id`,
		shedTwo, repoCampaign, repoTenant, repoActualShed, repoOperator)

	// BUCKET A (repoShedScope): Submit lump-sum observation
	obsA, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoShedScope,
		WeightKg:         100.0,
		AverageWeightKg:  100.0,
		AnimalCount:      10,
		ProofArtifactIDs: []string{repoShedProof},
		RecordedBy:       repoOperator,
		IdempotencyKey:   "bucket-a-submit-" + repoShedScope,
	})
	if err != nil {
		t.Fatalf("bucket A submit failed: %v", err)
	}

	// Seed proof for bucket B
	proofB := "00000000-0000-4000-8000-000000009399"
	insertProof(t, ctx, pool, proofB, "video", "completed", "shed", repoActualShed, "shed", repoActualShed)

	// BUCKET B (shedTwo): Submit lump-sum observation
	obsB, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   shedTwo,
		WeightKg:         200.0,
		AverageWeightKg:  200.0,
		AnimalCount:      20,
		ProofArtifactIDs: []string{proofB},
		RecordedBy:       repoOperator,
		IdempotencyKey:   "bucket-b-submit-" + shedTwo,
	})
	if err != nil {
		t.Fatalf("bucket B submit failed: %v", err)
	}

	// Verifier approves bucket A's observation → bucket A auto-closes
	verifier := "00000000-0000-4000-8000-000000000401"
	eventIDA := "event-A-verified-" + repoShedScope
	resultA, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:        repoTenant,
		ObservationID:   obsA.ObservationID,
		RefType:         domain.VerificationRefTypeShed,
		Status:          domain.VerificationStatusVerified,
		VerifiedBy:      verifier,
		Reason:          "",
		EventID:         eventIDA,
		EvidenceProofID: repoShedProof,
	})
	if err != nil {
		t.Fatalf("bucket A verify failed: %v", err)
	}
	if !resultA.ShedClosed {
		t.Fatalf("bucket A should have auto-closed on verification, but ShedClosed=%v", resultA.ShedClosed)
	}

	// Verify bucket A is now 'closed'
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusClosed)

	// Verify campaign is still 'in_progress' (not auto-closed, because bucket B is still 'completed')
	assertCampaignStatus(t, ctx, pool, domain.StatusInProgress)

	// Verifier REJECTS bucket B's observation → bucket B should reopen to 'in_progress'
	eventIDB := "event-B-rework-" + shedTwo
	_, err = repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:        repoTenant,
		ObservationID:   obsB.ObservationID,
		RefType:         domain.VerificationRefTypeShed,
		Status:          domain.VerificationStatusRework,
		VerifiedBy:      verifier,
		Reason:          "Quality issue, please re-shoot",
		EventID:         eventIDB,
		EvidenceProofID: proofB,
	})
	if err != nil {
		t.Fatalf("bucket B rework failed: %v", err)
	}

	// Verify bucket B is now 'in_progress' (reopened from 'completed')
	assertScopeStatus(t, ctx, pool, shedTwo, domain.StatusInProgress)

	// THE CRITICAL TEST: Bucket B can now be RE-SUBMITTED even though bucket A is closed
	// and campaign is 'in_progress'. If the vaccination deadlock existed in weighing, this
	// would fail because the campaign would have been prematurely closed or refused the submit.
	proofB2 := "00000000-0000-4000-8000-000000009488"
	insertProof(t, ctx, pool, proofB2, "video", "completed", "shed", repoActualShed, "shed", repoActualShed)
	rescanB, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   shedTwo,
		WeightKg:         210.0, // slightly different weight
		AverageWeightKg:  210.0,
		AnimalCount:      21,
		ProofArtifactIDs: []string{proofB2},
		RecordedBy:       repoOperator,
		IdempotencyKey:   "bucket-b-rescan-" + shedTwo,
	})
	if err != nil {
		// THIS IS THE FAILURE CONDITION if the deadlock exists:
		// bucket B cannot re-submit because campaign reached a terminal state,
		// similar to vaccination's shared parent 'accepted' guard.
		t.Fatalf("DEADLOCK DETECTED: bucket B could not re-submit after verifier rework. Error: %v. "+
			"This indicates weighing contains the vaccination deadlock class.", err)
	}
	if rescanB.ObservationID == "" {
		t.Fatalf("bucket B rescan observation should have been created")
	}

	// Verify campaign is STILL 'in_progress' (not yet closed because bucket B is 'completed' again)
	assertCampaignStatus(t, ctx, pool, domain.StatusInProgress)
}
