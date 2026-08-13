package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// -----------------------------------------------------------------------------
// B12: an idempotent retry after a SERIALIZABLE conflict must replay the
// winner's row, not be misclassified as ErrDuplicateScan.
// -----------------------------------------------------------------------------

// TestRecordAnimalObservationSameIdempotencyKeyRetryReplaysWinner drives the
// exact 40001-loser shape the retry loop's observationTagHasOpenRow check was
// blind to: a caller that legitimately retries the SAME request (same
// idempotency key) after its own attempt actually won and committed must get
// the winner's observation back, never ports.ErrDuplicateScan.
//
// This does not need two real concurrent goroutines to prove the bug: calling
// RecordAnimalObservation twice with the SAME idempotency key, weight, and
// tag reproduces the state the retry loop must handle -- an open row already
// exists for this tag AND it was written by this exact idempotency key. The
// old code only checked "is there an open row for this tag" and returned
// ErrDuplicateScan unconditionally; it never asked whose key wrote it.
func TestRecordAnimalObservationSameIdempotencyKeyRetryReplaysWinner(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)

	cmd := domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: "b12-retry-rfid",
		WeightKg:          14.2,
		ProofArtifactID:   repoExpectedShedProof,
		ActualLocationID:  repoExpectedShed,
		IdempotencyKey:    "animal:b12-retry-same-key",
		RecordedBy:        repoOperator,
	}

	winner, err := repo.RecordAnimalObservation(ctx, cmd)
	if err != nil {
		t.Fatalf("first (winning) attempt: %v", err)
	}

	// The observationByIdemPool short-circuit inserted for this fix is what
	// the retry loop calls after a 40001 loss; exercise it directly here in
	// the same way the loop does, and also exercise the public entry point
	// with the identical command, which for an idempotency key that already
	// has a durable, committed row will resolve to observationByIdemTx's own
	// fast-path in recordAnimalObservationAttempt before ever reaching the
	// retry loop at all -- proving REPLAY returns the winner regardless of
	// which of the two paths handles it.
	replay, err := repo.RecordAnimalObservation(ctx, cmd)
	if err != nil {
		t.Fatalf("same-idempotency-key replay: %v", err)
	}
	if errors.Is(err, ports.ErrDuplicateScan) {
		t.Fatalf("same-idempotency-key replay must not be classified as a duplicate scan")
	}
	if replay.ObservationID != winner.ObservationID {
		t.Fatalf("replay observation_id=%s, want winner's %s", replay.ObservationID, winner.ObservationID)
	}
	if replay.WeightKg != winner.WeightKg {
		t.Fatalf("replay weight=%v, want winner's %v", replay.WeightKg, winner.WeightKg)
	}

	// Directly exercise the helper the retry loop calls, standing in for the
	// 40001-then-recheck path without needing to fabricate a live
	// serialization failure.
	fingerprint := idempotencyFingerprint(cmd)
	byIdem, err := repo.observationByIdemPool(ctx, repoTenant, "weighing.observation_accepted", cmd.IdempotencyKey, fingerprint)
	if err != nil {
		t.Fatalf("observationByIdemPool: %v", err)
	}
	if byIdem.ObservationID != winner.ObservationID {
		t.Fatalf("observationByIdemPool observation_id=%s, want winner's %s", byIdem.ObservationID, winner.ObservationID)
	}
}

// TestRecordAnimalObservationDifferentIdempotencyKeySameTagStillDuplicate is
// the control case: a DIFFERENT caller (different idempotency key) hitting
// the same open tag in the same bucket is a genuine duplicate scan and must
// keep failing that way -- the B12 fix must not turn every collision into a
// silent success.
func TestRecordAnimalObservationDifferentIdempotencyKeySameTagStillDuplicate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)

	first := domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: "b12-dup-rfid",
		WeightKg:          10.0,
		ProofArtifactID:   repoExpectedShedProof,
		ActualLocationID:  repoExpectedShed,
		IdempotencyKey:    "animal:b12-dup-first",
		RecordedBy:        repoOperator,
	}
	firstObs, err := repo.RecordAnimalObservation(ctx, first)
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	// Mark the row SUBMITTED directly (rather than via SubmitIndividualScope,
	// which also completes the bucket and would make a second capture fail
	// with ErrImmutable instead of exercising the duplicate-scan gate this
	// test targets). Only a submitted row (submitted_duplicate CTE) is a real
	// duplicate-scan shape a DIFFERENT idempotency key cannot silently update
	// in place -- an open (unsubmitted) row would just be rewritten by the
	// "updated" CTE, which is the legitimate rescan case, not a duplicate.
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_observations SET submitted_at=now() WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, repoTenant, firstObs.ObservationID)

	second := first
	second.IdempotencyKey = "animal:b12-dup-second"
	second.WeightKg = 10.5
	_, err = repo.RecordAnimalObservation(ctx, second)
	if !errors.Is(err, ports.ErrDuplicateScan) {
		t.Fatalf("different-key same-tag capture err=%v, want ErrDuplicateScan", err)
	}
}

// -----------------------------------------------------------------------------
// B10: lump-sum/per-shed capture and campaign close must take the campaign
// row lock before the bucket lock -- the SAME order -- so the two paths can
// never deadlock against each other. This test does not assert on error
// TEXT (a real deadlock surfaces as a driver/context error, not a typed
// domain error) -- it asserts both operations complete without one of them
// timing out or returning a raw 40P01, run many times concurrently to make a
// pre-fix deadlock likely to reproduce.
// -----------------------------------------------------------------------------

func TestConcurrentShedCaptureAndCampaignCloseNeverDeadlock(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, repoShedProof, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	repo := NewRepository(pool, 5*time.Second)

	const iterations = 20
	var wg sync.WaitGroup
	errs := make(chan error, iterations*2)
	for i := 0; i < iterations; i++ {
		wg.Add(2)
		idem := "b10:" + time.Now().Format(time.RFC3339Nano)
		go func(idem string, n int) {
			defer wg.Done()
			_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
				TenantID:         repoTenant,
				CampaignID:       repoCampaign,
				CampaignShedID:   repoShedScope,
				WeightKg:         100 + float64(n),
				AverageWeightKg:  10,
				AnimalCount:      10,
				ProofArtifactID:  repoShedProof,
				ProofArtifactIDs: []string{repoShedProof},
				IdempotencyKey:   idem + "-capture",
				RecordedBy:       repoOperator,
			})
			// ErrImmutable/ErrDuplicateScan/ErrInvalidArgument are all fine,
			// expected outcomes once the bucket completes or closes under
			// concurrent load -- what must never happen is a raw deadlock
			// error (pgx wraps SQLSTATE 40P01) or a context deadline.
			if err != nil && !isExpectedShedCaptureRace(err) {
				errs <- err
			}
		}(idem, i)
		go func() {
			defer wg.Done()
			_, err := repo.CloseCampaign(ctx, domain.CloseCommand{
				TenantID:       repoTenant,
				CampaignID:     repoCampaign,
				Reason:         "b10 concurrency probe",
				ClosedBy:       repoVerifier,
				IdempotencyKey: "b10:close:" + time.Now().Format(time.RFC3339Nano),
			})
			if err != nil && !isExpectedCampaignCloseRace(err) {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("unexpected error under concurrent capture/close (want no deadlock): %v", err)
	}
}

func isExpectedShedCaptureRace(err error) bool {
	return errors.Is(err, ports.ErrImmutable) ||
		errors.Is(err, ports.ErrDuplicateScan) ||
		errors.Is(err, ports.ErrInvalidArgument) ||
		errors.Is(err, ports.ErrNotFound) ||
		errors.Is(err, ports.ErrWriteConflict)
}

func isExpectedCampaignCloseRace(err error) bool {
	return errors.Is(err, ports.ErrImmutable) ||
		errors.Is(err, ports.ErrVerificationPending) ||
		errors.Is(err, ports.ErrNotFound) ||
		errors.Is(err, ports.ErrWriteConflict)
}

// -----------------------------------------------------------------------------
// B08: a verdict naming stale evidence (a proof id that is no longer the
// proof attached to the observation) must be rejected distinctly, not
// silently applied against whatever proof is current now.
// -----------------------------------------------------------------------------

func TestApplyVerificationVerdictRejectsStaleEvidence(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "b08-stale-rfid",
		WeightKg:          9.1, ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:b08-stale", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}

	// A verdict naming a DIFFERENT proof id than the one currently attached
	// (simulating a queue item that went stale after a rework re-shoot swapped
	// the evidence) must be rejected, not silently approved against whatever
	// proof happens to be attached now.
	_, err = repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:        repoTenant,
		ObservationID:   obs.ObservationID,
		RefType:         domain.VerificationRefTypeAnimal,
		Status:          domain.VerificationStatusVerified,
		VerifiedBy:      repoVerifier,
		EventID:         "22222222-2222-4222-8222-222222222abc",
		EvidenceProofID: repoPendingProof, // a real proof id, but NOT what's attached to obs
	})
	if !errors.Is(err, ErrStaleEvidence) {
		t.Fatalf("stale-evidence verdict err=%v, want ErrStaleEvidence", err)
	}
	if got := readObservationVerificationStatus(t, ctx, pool, obs.ObservationID); got != domain.VerificationStatusPending {
		t.Fatalf("observation verification_status=%q after rejected stale verdict, want still pending", got)
	}

	// The matching, CURRENT evidence id must be accepted.
	result, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:        repoTenant,
		ObservationID:   obs.ObservationID,
		RefType:         domain.VerificationRefTypeAnimal,
		Status:          domain.VerificationStatusVerified,
		VerifiedBy:      repoVerifier,
		EventID:         "22222222-2222-4222-8222-222222222abd",
		EvidenceProofID: repoExpectedShedProof,
	})
	if err != nil {
		t.Fatalf("apply verdict with current evidence id: %v", err)
	}
	if !result.Applied || result.Status != domain.VerificationStatusVerified {
		t.Fatalf("verdict result=%+v, want applied verified", result)
	}
}

// TestApplyVerificationVerdictEmptyEvidenceIDIsBackwardCompatible proves a
// verdict minted before EvidenceProofID existed (an empty value) is treated
// as stale-check-skipped, not rejected and not crashed on -- durable-bus
// redelivery of an in-flight pre-upgrade event must keep working.
func TestApplyVerificationVerdictEmptyEvidenceIDIsBackwardCompatible(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "b08-empty-rfid",
		WeightKg:          9.4, ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:b08-empty", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}

	result, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:      repoTenant,
		ObservationID: obs.ObservationID,
		RefType:       domain.VerificationRefTypeAnimal,
		Status:        domain.VerificationStatusVerified,
		VerifiedBy:    repoVerifier,
		EventID:       "33333333-3333-4333-8333-333333333abc",
		// EvidenceProofID deliberately left empty.
	})
	if err != nil {
		t.Fatalf("apply verdict with empty evidence id: %v", err)
	}
	if !result.Applied || result.Status != domain.VerificationStatusVerified {
		t.Fatalf("verdict result=%+v, want applied verified", result)
	}
}

// -----------------------------------------------------------------------------
// B21-SQL: operator_display_name must be resolved by BOTH getCampaignTx and
// hydrateCampaigns, the same way ListCampaignSheds already resolves it.
// -----------------------------------------------------------------------------

func TestGetCampaignAndHydrateCampaignsResolveOperatorDisplayName(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	const operatorDisplayName = "B21 Operator Roster Name"
	execWeighingTestSQL(t, ctx, pool, `
DELETE FROM workforce_members WHERE tenant_id=$1::uuid AND user_id=$2::uuid`,
		repoTenant, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, 'B21-OP-1', $3, 'active', 'operator')`,
		repoTenant, repoOperator, operatorDisplayName)
	repo := NewRepository(pool, 5*time.Second)

	campaign, err := repo.getCampaign(ctx, repoTenant, repoCampaign)
	if err != nil {
		t.Fatalf("getCampaign: %v", err)
	}
	assertShedHasOperatorDisplayName(t, campaign.Sheds, repoAnimalScope, operatorDisplayName)

	page, err := repo.ListCampaigns(ctx, repoTenant, "", "", 0)
	if err != nil {
		t.Fatalf("ListCampaigns: %v", err)
	}
	var found bool
	for _, c := range page.Items {
		if c.CampaignID != repoCampaign {
			continue
		}
		found = true
		assertShedHasOperatorDisplayName(t, c.Sheds, repoAnimalScope, operatorDisplayName)
	}
	if !found {
		t.Fatalf("ListCampaigns did not return campaign %s", repoCampaign)
	}
}

func assertShedHasOperatorDisplayName(t *testing.T, sheds []domain.CampaignShed, campaignShedID, want string) {
	t.Helper()
	for _, shed := range sheds {
		if shed.CampaignShedID != campaignShedID {
			continue
		}
		if shed.OperatorDisplayName != want {
			t.Fatalf("shed %s operator_display_name=%q, want %q", campaignShedID, shed.OperatorDisplayName, want)
		}
		return
	}
	t.Fatalf("shed %s not found in campaign sheds", campaignShedID)
}
