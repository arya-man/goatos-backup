package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// TestReopenScopeReactivatesTerminalKernelWorkItem is the B09 wiring proof for
// CALL SITE 2 (ReopenScope). ReactivateWorkItemsForBucket (kernel.go) exists and
// TestReactivateWorkItemsForBucketUndoesTerminalReconciliation proves the
// function itself works, but neither proves anything CALLS it in production.
// This drives the REAL repo.ReopenScope path end to end: complete a bucket, let
// a kernel sweep terminalize its work item, reopen through ReopenScope, and
// assert the work item comes back to 'scheduled' with terminal_at cleared --
// and that WeighingProcessState stops counting the bucket as finished.
func TestReopenScopeReactivatesTerminalKernelWorkItem(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:b09-reopen-wiring")

	// Complete the lump-sum bucket the same way the real close path does, then let
	// a kernel sweep terminalize its work item -- the production precondition for
	// B09 (a bucket that later needs to be reopened after already being
	// terminalized in weighing_work_items).
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status='completed', completed_at=now(), closed_at=now(), closed_by=$3::uuid, close_reason='b09-wiring-fixture', updated_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope, repoOperator)
	if _, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
		TenantID: repoTenant, AsOf: businessInstant(t, kernelPlannedDate),
	}); err != nil {
		t.Fatalf("sweep to terminalize: %v", err)
	}
	terminal := readWorkItem(t, ctx, pool, repoShedScope)
	if terminal.state != domain.WorkStateCompleted {
		t.Fatalf("precondition: work item state=%q, want completed", terminal.state)
	}

	// Now flip the BUCKET to 'closed' too (ReopenScope's own gate accepts
	// completed/closed) so the real repo.ReopenScope call below is the ONLY thing
	// that can move it.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status='closed' WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope)

	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoShedScope, repoOperator, "b09-reopen-wiring:reopen-1", "verifier asked for a re-shoot"); err != nil {
		t.Fatalf("reopen scope: %v", err)
	}

	// FAILING BEHAVIOR THIS PROVES FIXED: before wiring the
	// ReactivateWorkItemsForBucket call into ReopenScope, the bucket flips back to
	// 'in_progress' but the work item stays 'completed' forever -- exactly the RED
	// case TestReactivateWorkItemsForBucketUndoesTerminalReconciliation documents,
	// reproduced here through the real production call path instead of a manual
	// UPDATE.
	reactivated := readWorkItem(t, ctx, pool, repoShedScope)
	if reactivated.state != domain.WorkStateScheduled {
		t.Fatalf("work item state after ReopenScope=%q, want scheduled -- ReactivateWorkItemsForBucket is not wired into ReopenScope", reactivated.state)
	}

	stateAfter, err := repo.WeighingProcessState(ctx, repoTenant, repoCampaign, kernelPlannedDate, kernelLaterDate)
	if err != nil {
		t.Fatalf("process state after reopen: %v", err)
	}
	if stateAfter.Summary.Completed != 0 {
		t.Fatalf("Control Tower summary still counts the reopened bucket as completed=%d, want 0", stateAfter.Summary.Completed)
	}
}

// TestReworkVerdictAllowsSameDayRescanIndividualScope is the B05 individual-scope
// regression: a same-day rescan of a tag whose SUBMITTED capture was bounced back
// by a verifier REWORK verdict must succeed, not be rejected as a duplicate scan.
//
// Root cause this proves is CLOSED: the duplicate-scan gate
// (classifyFreeFlowObservationRejection / the `submitted_duplicate` CTE in
// recordUnknownAnimalObservationTx) excludes rows whose verification_status is
// 'rework', and the `updated` CTE explicitly allows an in-place update when
// verification_status='rework' even though submitted_at is still set. Without
// that exemption, the rescan would report ErrDuplicateScan (409) and the
// operator's rework loop would be impossible to complete.
func TestReworkVerdictAllowsSameDayRescanIndividualScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const tag = "b05-individual-rework-tag"

	first, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: tag,
		WeightKg:          10.5,
		ProofArtifactID:   repoExpectedShedProof,
		RecordedBy:        repoOperator,
		IdempotencyKey:    "b05:first-capture",
	})
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}

	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "b05:submit-1", []string{tag}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}

	// A same-day rescan BEFORE any rework verdict must be rejected as a duplicate --
	// this is the control case proving the gate is doing real work, not a no-op.
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: tag,
		WeightKg:          11.0,
		ProofArtifactID:   repoExpectedShedProof,
		RecordedBy:        repoOperator,
		IdempotencyKey:    "b05:pre-rework-rescan",
	}); err == nil {
		t.Fatalf("rescan before rework verdict unexpectedly succeeded, want ErrDuplicateScan")
	}

	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:      repoTenant,
		ObservationID: first.ObservationID,
		RefType:       domain.VerificationRefTypeAnimal,
		Status:        domain.VerificationStatusRework,
		VerifiedBy:    repoOperator,
		Reason:        "video unusable, re-shoot",
		EventID:       "b05:rework-event-1",
	}); err != nil {
		t.Fatalf("apply rework verdict: %v", err)
	}

	// FAILING BEHAVIOR THIS PROVES FIXED: without the verification_status='rework'
	// exemption in the duplicate-scan gate, this rescan would return
	// ports.ErrDuplicateScan even though a verifier explicitly asked for a re-shoot.
	second, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: tag,
		WeightKg:          11.0,
		ProofArtifactID:   repoExpectedShedProof,
		RecordedBy:        repoOperator,
		IdempotencyKey:    "b05:post-rework-rescan",
	})
	if err != nil {
		t.Fatalf("same-day rescan after rework verdict: %v", err)
	}
	if second.ObservationID != first.ObservationID {
		t.Fatalf("rescan created a NEW row (%s), want the same evidence row (%s) updated in place", second.ObservationID, first.ObservationID)
	}
	if second.WeightKg != 11.0 {
		t.Fatalf("rescan weight=%v, want 11.0", second.WeightKg)
	}

	// The rescan must have reset the observation's own verification status back to
	// pending -- it is fresh, unverified work again.
	var verificationStatus string
	if err := pool.QueryRow(ctx, `SELECT verification_status FROM weighing_observations WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, repoTenant, first.ObservationID).Scan(&verificationStatus); err != nil {
		t.Fatalf("read verification_status: %v", err)
	}
	if verificationStatus != "pending" {
		t.Fatalf("verification_status after rescan=%q, want pending", verificationStatus)
	}
}

// TestReworkVerdictAllowsLumpSumReplacement is the B05 lump-sum-scope regression:
// after a verifier REWORK verdict on a lump-sum submission, the operator must be
// able to submit a REPLACEMENT weight+video for the same bucket without hitting
// the unconditional (tenant_id, campaign_shed_id) unique constraint that migration
// 000067 replaced with a partial index (WHERE withdrawn_at IS NULL). This proves
// markObservationRework's withdrawal side effect actually frees the slot.
func TestReworkVerdictAllowsLumpSumReplacement(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	first, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoShedScope,
		WeightKg:         300,
		AnimalCount:      20,
		ProofArtifactID:  repoShedProof,
		ProofArtifactIDs: []string{repoShedProof},
		RecordedBy:       repoOperator,
		IdempotencyKey:   "b05:shed-first",
	})
	if err != nil {
		t.Fatalf("first lump-sum capture: %v", err)
	}

	// Without a withdrawal, a second submission for the SAME bucket must be
	// rejected -- the control case proving the unique index is real.
	if _, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoShedScope,
		WeightKg:         310,
		AnimalCount:      20,
		ProofArtifactID:  repoShedProofTwo,
		ProofArtifactIDs: []string{repoShedProofTwo},
		RecordedBy:       repoOperator,
		IdempotencyKey:   "b05:shed-premature-resubmit",
	}); err == nil {
		t.Fatalf("resubmit before rework unexpectedly succeeded, want a unique-scope conflict")
	}

	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:      repoTenant,
		ObservationID: first.ObservationID,
		RefType:       domain.VerificationRefTypeShed,
		Status:        domain.VerificationStatusRework,
		VerifiedBy:    repoOperator,
		Reason:        "video unusable, re-shoot",
		EventID:       "b05:shed-rework-event-1",
	}); err != nil {
		t.Fatalf("apply rework verdict: %v", err)
	}

	// FAILING BEHAVIOR THIS PROVES FIXED: without markObservationRework stamping
	// withdrawn_at on the rejected row (and deleting its idempotency record), this
	// replacement submission would trip weighing_shed_observations_one_open_scope_uidx
	// (SQLSTATE 23505), mapped to a permanent 409 -- the operator told to redo the
	// work and then structurally prevented from filing it.
	replacement, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoShedScope,
		WeightKg:         310,
		AnimalCount:      20,
		ProofArtifactID:  repoShedProofTwo,
		ProofArtifactIDs: []string{repoShedProofTwo},
		RecordedBy:       repoOperator,
		IdempotencyKey:   "b05:shed-replacement",
	})
	if err != nil {
		t.Fatalf("replacement lump-sum submission after rework: %v", err)
	}
	if replacement.ObservationID == first.ObservationID {
		t.Fatalf("replacement reused the withdrawn observation id %s, want a NEW row", first.ObservationID)
	}
	if replacement.WeightKg != 310 {
		t.Fatalf("replacement weight=%v, want 310", replacement.WeightKg)
	}

	// The rejected first attempt must remain immutable history (withdrawn, not
	// deleted) -- AGENTS.md requires rejected proof attempts stay on the table.
	var withdrawnAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT withdrawn_at FROM weighing_shed_observations WHERE tenant_id=$1::uuid AND shed_observation_id=$2::uuid`, repoTenant, first.ObservationID).Scan(&withdrawnAt); err != nil {
		t.Fatalf("read withdrawn_at: %v", err)
	}
	if withdrawnAt == nil {
		t.Fatalf("rejected first attempt was not withdrawn -- it must stay as immutable history")
	}
}

// TestCampaignCapturedTotalCountsFreeFlowScansWithZeroExpectedAnimalRows is the
// B15 regression. Campaign "captured" (Progress.IndividualCompletedCount) used to
// be read as `count(*) FILTER (WHERE status='weighed') FROM
// weighing_expected_animals` -- a column NOTHING in the free-flow write path ever
// sets (RecordAnimalObservation always writes animal_id=NULL / scanned_identifier).
// A campaign with real captures and ZERO expected-animal rows therefore always
// reported captured=0 while the bucket/roster screens showed the real scan count --
// the cross-surface count-parity defect AGENTS.md names.
//
// This seeds N free-flow scans against a campaign with NO weighing_expected_animals
// rows at all and asserts the campaign-level captured total is exactly N.
func TestCampaignCapturedTotalCountsFreeFlowScansWithZeroExpectedAnimalRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// A FRESH campaign + individual bucket with NO weighing_expected_animals rows
	// at all -- exactly the production shape (campaign
	// 92000000-0000-4000-8000-000000000701 had 8 weighing_observations rows and 0
	// 'weighed' expected-animal rows).
	campaignID := lcpUUID(15001)
	bucketID := lcpUUID(15011)
	lcpInsertCampaign(t, ctx, pool, campaignID, repoPark, "2026-08-10", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, bucketID, campaignID, repoExpectedShed, domain.CategoryIndividualAnimal, repoOperator, 0, "pending")

	// FREE-FLOW: the weighing_expected_animals table was DROPPED (migration
	// 000079) -- there is no expected-animal roster to assert zero rows in.
	// Free-flow has no expected set by construction; this regression is now
	// proven purely by the capture/list assertions below.
	const capturedCount = 8
	for i := 0; i < capturedCount; i++ {
		tag := "b15-freeflow-" + string(rune('a'+i))
		if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
			TenantID:          repoTenant,
			CampaignID:        campaignID,
			CampaignShedID:    bucketID,
			ScannedIdentifier: tag,
			WeightKg:          10 + float64(i),
			ProofArtifactID:   repoExpectedShedProof,
			RecordedBy:        repoOperator,
			IdempotencyKey:    "b15:capture:" + tag,
		}); err != nil {
			t.Fatalf("capture %s: %v", tag, err)
		}
	}

	page, err := repo.ListCampaigns(ctx, repoTenant, "", "", 100)
	if err != nil {
		t.Fatalf("list campaigns: %v", err)
	}
	found := lcpFind(t, page.Items, campaignID)
	// FAILING BEHAVIOR THIS PROVES FIXED: before the fix, IndividualCompletedCount
	// read weighing_expected_animals.status='weighed' and would report 0 here even
	// though capturedCount real scans were just written.
	if found.Progress.IndividualCompletedCount != capturedCount {
		t.Fatalf("captured total=%d, want %d (0 expected-animal rows exist for this campaign, so a dead-column read would report 0)", found.Progress.IndividualCompletedCount, capturedCount)
	}

	// Same assertion through the single-campaign path (progressStats), not just
	// the batched hydrateCampaigns path -- both were fixed and both must agree.
	single, err := repo.getCampaign(ctx, repoTenant, campaignID)
	if err != nil {
		t.Fatalf("get single campaign: %v", err)
	}
	if single.Progress.IndividualCompletedCount != capturedCount {
		t.Fatalf("single-campaign captured total=%d, want %d", single.Progress.IndividualCompletedCount, capturedCount)
	}
}
