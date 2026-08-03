package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Two facts, two names, ONE definition each.
//
// Before this test there was one ambiguous number per surface. The task-detail
// bucket read reported captured_count (weight records held, submitted or not) and
// the director Operators read reported animals_weighed_count (animals held,
// submitted or not) -- and they were computed with DIFFERENT lump-sum grain: the
// bucket fragment counted the shed proof ROW (always 1) while the operator roll-up
// summed animal_count. A 40-animal lump-sum proof therefore read as "1 weighed" on
// the task detail and "40 animals weighed" on the Operators screen, at the same
// second, in the same app.
//
// Neither number could answer the question that actually loses work on the farm:
// an operator who has weighed animals but has NOT pressed Submit. Both surfaces now
// carry BOTH named facts:
//
//	animals_weighed_count    -- animals whose weight is RECORDED, submitted or not
//	animals_submitted_count  -- animals whose weight has been SUBMITTED for verification
//
// with the identical predicate at both grains, so no surface can invent a third.

// TestWeighedAndSubmittedAgreeMidShift is the decisive case: three recorded
// weighings, ZERO submitted. Both the per-bucket read and the per-operator read
// must report weighed=3, submitted=0 -- and after Submit, both must report 3/3.
//
// This is the state that is invisible today. captured_count says 3 and
// animals_weighed_count says 3, but nothing anywhere says "none of this has been
// submitted", which is precisely where work is silently lost when an operator
// walks away mid-shift.
func TestWeighedAndSubmittedAgreeMidShift(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	campaignID := lcpUUID(16001)
	bucketID := lcpUUID(16011)
	lcpInsertCampaign(t, ctx, pool, campaignID, repoPark, "2026-08-12", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, bucketID, campaignID, repoExpectedShed, domain.CategoryIndividualAnimal, repoOperator, 0, "pending")

	// The shared fixture already gives this operator work, so the person-grain
	// assertions are DELTAS against a baseline taken before this test writes
	// anything. The bucket-grain assertions are absolute: the bucket is new.
	baseWeighed, baseSubmitted := operatorFacts(t, ctx, repo)

	tags := []string{"ws-mid-a", "ws-mid-b", "ws-mid-c"}
	for i, tag := range tags {
		if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
			TenantID:          repoTenant,
			CampaignID:        campaignID,
			CampaignShedID:    bucketID,
			ScannedIdentifier: tag,
			WeightKg:          20 + float64(i),
			ProofArtifactID:   repoExpectedShedProof,
			RecordedBy:        repoOperator,
			IdempotencyKey:    "ws:capture:" + tag,
		}); err != nil {
			t.Fatalf("capture %s: %v", tag, err)
		}
	}

	// MID-SHIFT: recorded, nothing submitted.
	assertBucketFacts(t, ctx, repo, campaignID, bucketID, 3, 0, "mid-shift bucket")
	assertOperatorFacts(t, ctx, repo, baseWeighed+3, baseSubmitted+0, "mid-shift operator")

	if err := repo.SubmitIndividualScope(ctx, repoTenant, campaignID, bucketID, repoOperator, "ws:submit:1", tags); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// AFTER SUBMIT: both facts converge, on both surfaces.
	assertBucketFacts(t, ctx, repo, campaignID, bucketID, 3, 3, "post-submit bucket")
	assertOperatorFacts(t, ctx, repo, baseWeighed+3, baseSubmitted+3, "post-submit operator")
}

// TestLumpSumWeighedIsAnimalGrainOnBothSurfaces pins the grain half of the defect.
// A lump-sum proof IS the submission, so a standing (non-withdrawn) shed
// observation contributes its recorded animal_count to BOTH facts -- and it must
// contribute the same number to the per-bucket read as to the per-operator
// roll-up. The old bucket fragment counted the proof ROW, so this read 1 vs 40.
func TestLumpSumWeighedIsAnimalGrainOnBothSurfaces(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	campaignID := lcpUUID(16101)
	bucketID := lcpUUID(16111)
	lcpInsertCampaign(t, ctx, pool, campaignID, repoPark, "2026-08-13", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, bucketID, campaignID, repoExpectedShed, domain.CategoryPerShedPartition, repoOperator, 0, "pending")

	baseWeighed, baseSubmitted := operatorFacts(t, ctx, repo)

	const head = 40
	if _, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:        repoTenant,
		CampaignID:      campaignID,
		CampaignShedID:  bucketID,
		AnimalCount:     head,
		WeightKg:        1200,
		ProofArtifactID: repoExpectedShedProof,
		RecordedBy:      repoOperator,
		IdempotencyKey:  "ws:lump:1",
	}); err != nil {
		t.Fatalf("record lump sum: %v", err)
	}

	assertBucketFacts(t, ctx, repo, campaignID, bucketID, head, head, "lump-sum bucket")
	assertOperatorFacts(t, ctx, repo, baseWeighed+head, baseSubmitted+head, "lump-sum operator")
}

func assertBucketFacts(t *testing.T, ctx context.Context, repo *Repository, campaignID, bucketID string, wantWeighed, wantSubmitted int, label string) {
	t.Helper()
	page, err := repo.ListCampaignSheds(ctx, repoTenant, campaignID, "", "", 50)
	if err != nil {
		t.Fatalf("%s: list campaign sheds: %v", label, err)
	}
	for _, shed := range page.Items {
		if shed.CampaignShedID != bucketID {
			continue
		}
		if shed.AnimalsWeighedCount != wantWeighed {
			t.Fatalf("%s: animals_weighed_count=%d, want %d", label, shed.AnimalsWeighedCount, wantWeighed)
		}
		if shed.AnimalsSubmittedCount != wantSubmitted {
			t.Fatalf("%s: animals_submitted_count=%d, want %d", label, shed.AnimalsSubmittedCount, wantSubmitted)
		}
		return
	}
	t.Fatalf("%s: bucket %s not in page", label, bucketID)
}

func operatorFacts(t *testing.T, ctx context.Context, repo *Repository) (int, int) {
	t.Helper()
	summaries, err := repo.operatorSummaries(ctx, repoTenant, repoOperator, "")
	if err != nil {
		t.Fatalf("operator summaries: %v", err)
	}
	var weighed, submitted int
	for _, s := range summaries {
		if s.OperatorUserID != repoOperator {
			continue
		}
		weighed = s.AnimalsWeighedCount
		submitted = s.AnimalsSubmittedCount
	}
	return weighed, submitted
}

func assertOperatorFacts(t *testing.T, ctx context.Context, repo *Repository, wantWeighed, wantSubmitted int, label string) {
	t.Helper()
	gotWeighed, gotSubmitted := operatorFacts(t, ctx, repo)
	if gotWeighed != wantWeighed {
		t.Fatalf("%s: animals_weighed_count=%d, want %d", label, gotWeighed, wantWeighed)
	}
	if gotSubmitted != wantSubmitted {
		t.Fatalf("%s: animals_submitted_count=%d, want %d", label, gotSubmitted, wantSubmitted)
	}
}
