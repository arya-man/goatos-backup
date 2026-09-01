package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
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

func TestListCampaignShedsTerminalShedStatusOneToManyPageBoundaryStatusMatrixBeatsStaleWorkItemStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	campaignID := lcpUUID(16051)
	bucketID := lcpUUID(16061)
	lcpInsertCampaign(t, ctx, pool, campaignID, repoPark, "2026-07-30", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, bucketID, campaignID, repoExpectedShed, domain.CategoryIndividualAnimal, repoOperator, 0, domain.StatusCompleted)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_work_items (
  tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id, weighing_category,
  shed_label, shed_location_id, planned_business_date, due_business_date, work_state
)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6,
        'Gandhi 1', $7::uuid, '2026-07-30'::date, '2026-08-11'::date, 'scheduled')`,
		repoTenant, campaignID, bucketID, repoPark, repoOperator, domain.CategoryIndividualAnimal, repoExpectedShed)

	page, err := repo.ListCampaignSheds(ctx, repoTenant, campaignID, "", 50, ports.CampaignAccess{Unrestricted: true})
	if err != nil {
		t.Fatalf("list campaign sheds: %v", err)
	}
	for _, shed := range page.Items {
		if shed.CampaignShedID != bucketID {
			continue
		}
		if shed.Status != domain.StatusCompleted {
			t.Fatalf("shed status=%s, want completed when terminal shed status conflicts with stale work item", shed.Status)
		}
		return
	}
	t.Fatalf("bucket %s not in page", bucketID)
}

func TestInProgressShedStatusBeatsStaleScheduledWorkItemStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	campaignID := lcpUUID(16071)
	bucketID := lcpUUID(16081)
	lcpInsertCampaign(t, ctx, pool, campaignID, repoPark, "2026-08-13", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, bucketID, campaignID, repoExpectedShed, domain.CategoryIndividualAnimal, repoOperator, 0, domain.StatusInProgress)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_work_items (
  tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id, weighing_category,
  shed_label, shed_location_id, planned_business_date, due_business_date, work_state
)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6,
        'Gandhi 1', $7::uuid, '2026-08-13'::date, '2026-08-13'::date, 'scheduled')`,
		repoTenant, campaignID, bucketID, repoPark, repoOperator, domain.CategoryIndividualAnimal, repoExpectedShed)

	page, err := repo.ListCampaignsForOperator(ctx, repoTenant, repoOperator, "", "", 50)
	if err != nil {
		t.Fatalf("list campaigns for operator: %v", err)
	}
	for _, campaign := range page.Items {
		if campaign.CampaignID != campaignID {
			continue
		}
		if len(campaign.Sheds) != 1 {
			t.Fatalf("campaign sheds=%d, want 1", len(campaign.Sheds))
		}
		if campaign.Sheds[0].Status != domain.StatusInProgress {
			t.Fatalf("list campaign shed status=%s, want in_progress when the bucket has started but its work item is still scheduled", campaign.Sheds[0].Status)
		}

		detail, err := repo.CampaignByID(ctx, repoTenant, campaignID, ports.CampaignAccess{Unrestricted: true})
		if err != nil {
			t.Fatalf("campaign by id: %v", err)
		}
		if detail.Sheds[0].Status != domain.StatusInProgress {
			t.Fatalf("campaign detail shed status=%s, want in_progress", detail.Sheds[0].Status)
		}

		buckets, err := repo.ListCampaignSheds(ctx, repoTenant, campaignID, "", 50, ports.CampaignAccess{Unrestricted: true})
		if err != nil {
			t.Fatalf("list campaign sheds: %v", err)
		}
		if buckets.Items[0].Status != domain.StatusInProgress {
			t.Fatalf("bucket page shed status=%s, want in_progress", buckets.Items[0].Status)
		}
		return
	}
	t.Fatalf("campaign %s not in operator list", campaignID)
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

	// The client-typed 40 is IGNORED since 2026-08-24: the stored head count is
	// the herd register's census for the bucket's shed (this fixture houses ONE
	// resident in repoExpectedShed). Both surfaces must agree on that snapshot.
	const typedHead = 40
	const censusHead = 1
	if _, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:        repoTenant,
		CampaignID:      campaignID,
		CampaignShedID:  bucketID,
		AnimalCount:     typedHead,
		WeightKg:        1200,
		ProofArtifactID: repoExpectedShedProof,
		RecordedBy:      repoOperator,
		IdempotencyKey:  "ws:lump:1",
	}); err != nil {
		t.Fatalf("record lump sum: %v", err)
	}

	assertBucketFacts(t, ctx, repo, campaignID, bucketID, censusHead, censusHead, "lump-sum bucket")
	assertOperatorFacts(t, ctx, repo, baseWeighed+censusHead, baseSubmitted+censusHead, "lump-sum operator")
}

func assertBucketFacts(t *testing.T, ctx context.Context, repo *Repository, campaignID, bucketID string, wantWeighed, wantSubmitted int, label string) {
	t.Helper()
	// Unrestricted is the internal-caller arm: this test asserts the count PREDICATE, so it must
	// read every bucket rather than the subset some park/assignee authority would admit.
	page, err := repo.ListCampaignSheds(ctx, repoTenant, campaignID, "", 50, ports.CampaignAccess{Unrestricted: true})
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
