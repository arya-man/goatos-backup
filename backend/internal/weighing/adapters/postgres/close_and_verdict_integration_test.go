package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

const (
	repoExpectedShedProof = "00000000-0000-4000-8000-000000009401"
	// Additional completed video proofs scoped to the INDIVIDUAL bucket's shed
	// (repoExpectedShed). RecordAnimalObservation only accepts a proof whose
	// scope_id equals the bucket's location_id, so a test that replaces the
	// proof on an individual capture needs more than one of these. Without them
	// tests were reaching for repoShedProofTwo/Three, which the fixture scopes
	// to the LUMP-SUM shed, and the write correctly rejected them.
	repoExpectedShedProofTwo   = "00000000-0000-4000-8000-000000009402"
	repoExpectedShedProofThree = "00000000-0000-4000-8000-000000009403"
	repoVerifier               = "00000000-0000-4000-8000-000000000401"

	// Roster rows seeded ALREADY terminal, to prove close leaves them alone.
	repoTerminalUnavailableAnimal = "00000000-0000-4000-8000-000000009291"
	repoTerminalCanceledAnimal    = "00000000-0000-4000-8000-000000009292"

	// A second individual_animal bucket, to prove the same tag can be weighed once per bucket.
	freeFlowSecondBucket      = "00000000-0000-4000-8000-000000009103"
	freeFlowSecondBucketProof = "00000000-0000-4000-8000-000000009404"
)

// -----------------------------------------------------------------------------
// EXPLICIT CLOSE
// -----------------------------------------------------------------------------

// Closing a bucket that still holds work is the whole point of explicit close, and
// it must NOT launder that work into accepted work. The bucket reaches its own
// terminal status 'closed' (never 'completed'), the expected animal stays 'pending',
// and the reason/actor/not-accepted count are recorded on the row, in the audit
// trail, and on the outbox event.
func TestCloseScopeEndsBucketWithOpenWorkWithoutEverAcceptingIt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	result, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID:       repoTenant,
		CampaignID:     repoCampaign,
		CampaignShedID: repoAnimalScope,
		Reason:         "shed emptied early",
		ClosedBy:       repoVerifier,
		IdempotencyKey: "close:scope-open-work",
	})
	if err != nil {
		t.Fatalf("close scope with open work: %v", err)
	}
	if result.Status != domain.StatusClosed {
		t.Fatalf("close result status=%q, want %q", result.Status, domain.StatusClosed)
	}
	if result.NotAcceptedCount != 1 {
		t.Fatalf("not accepted count=%d, want 1 (the pending expected animal)", result.NotAcceptedCount)
	}
	if len(result.NotAccepted) != 1 {
		t.Fatalf("not accepted sample=%v, want one identifier", result.NotAccepted)
	}
	if result.ClosedAt.IsZero() {
		t.Fatal("close result carries no closed_at")
	}

	// 'closed' is a DISTINCT terminal status; a closed bucket must never read as completed.
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusClosed)
	// The unaccepted work stays unaccepted.
	assertExpectedAnimalStatus(t, ctx, pool, repoAnimal, "pending")
	if weighed := countExpectedAnimalsWithStatus(t, ctx, pool, "weighed"); weighed != 0 {
		t.Fatalf("close marked %d expected animals weighed; close must never accept work", weighed)
	}

	var reason, closedBy string
	var notAcceptedCount int
	if err := pool.QueryRow(ctx, `
SELECT close_reason, closed_by::text, closed_not_accepted_count
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoAnimalScope).Scan(&reason, &closedBy, &notAcceptedCount); err != nil {
		t.Fatalf("read close columns: %v", err)
	}
	if reason != "shed emptied early" || closedBy != repoVerifier || notAcceptedCount != 1 {
		t.Fatalf("close row reason=%q closedBy=%q notAccepted=%d", reason, closedBy, notAcceptedCount)
	}

	if got := countOutbox(t, ctx, pool, "weighing.shed.closed"); got != 1 {
		t.Fatalf("weighing.shed.closed outbox rows=%d, want 1", got)
	}
	if got := countAudit(t, ctx, pool, "weighing.scope_closed"); got != 1 {
		t.Fatalf("weighing.scope_closed audit rows=%d, want 1", got)
	}
	var payloadReason string
	var payloadNotAccepted int
	if err := pool.QueryRow(ctx, `
SELECT payload->'payload'->>'reason', (payload->'payload'->>'not_accepted_count')::int
FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='weighing.shed.closed'`, repoTenant).Scan(&payloadReason, &payloadNotAccepted); err != nil {
		t.Fatalf("read close event payload: %v", err)
	}
	if payloadReason != "shed emptied early" || payloadNotAccepted != 1 {
		t.Fatalf("close event payload reason=%q notAccepted=%d", payloadReason, payloadNotAccepted)
	}
}

// Exact replay: same key, same payload. The original result comes back and NO new
// side effect is produced -- no second outbox row, no second audit row, and the
// stored closed_at is untouched.
func TestCloseScopeExactReplayReturnsOriginalResultWithNoNewSideEffects(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	cmd := domain.CloseCommand{
		TenantID:       repoTenant,
		CampaignID:     repoCampaign,
		CampaignShedID: repoAnimalScope,
		Reason:         "shed emptied early",
		ClosedBy:       repoVerifier,
		IdempotencyKey: "close:scope-replay",
	}
	first, err := repo.CloseScope(ctx, cmd)
	if err != nil {
		t.Fatalf("first close: %v", err)
	}
	firstClosedAt := readScopeClosedAt(t, ctx, pool, repoAnimalScope)

	replay, err := repo.CloseScope(ctx, cmd)
	if err != nil {
		t.Fatalf("exact replay close: %v", err)
	}
	if replay.CampaignShedID != first.CampaignShedID || replay.Status != first.Status ||
		replay.Reason != first.Reason || replay.ClosedBy != first.ClosedBy ||
		replay.NotAcceptedCount != first.NotAcceptedCount {
		t.Fatalf("replay result %+v != original %+v", replay, first)
	}
	if !replay.ClosedAt.Equal(first.ClosedAt) {
		t.Fatalf("replay closed_at=%s != original %s", replay.ClosedAt, first.ClosedAt)
	}
	if got := readScopeClosedAt(t, ctx, pool, repoAnimalScope); !got.Equal(firstClosedAt) {
		t.Fatalf("replay rewrote closed_at from %s to %s", firstClosedAt, got)
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed.closed"); got != 1 {
		t.Fatalf("outbox rows after replay=%d, want 1", got)
	}
	if got := countAudit(t, ctx, pool, "weighing.scope_closed"); got != 1 {
		t.Fatalf("audit rows after replay=%d, want 1", got)
	}
}

// Same key, DIFFERENT payload is a client bug, not a replay: it must conflict and
// leave the recorded close exactly as it was.
func TestCloseSameKeyDifferentPayloadConflictsWithoutMutatingState(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	base := domain.CloseCommand{
		TenantID:       repoTenant,
		CampaignID:     repoCampaign,
		CampaignShedID: repoAnimalScope,
		Reason:         "shed emptied early",
		ClosedBy:       repoVerifier,
		IdempotencyKey: "close:scope-conflict",
	}
	if _, err := repo.CloseScope(ctx, base); err != nil {
		t.Fatalf("first close: %v", err)
	}
	closedAt := readScopeClosedAt(t, ctx, pool, repoAnimalScope)

	conflicting := base
	conflicting.Reason = "a completely different reason"
	if _, err := repo.CloseScope(ctx, conflicting); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key-different-payload err=%v, want ErrIdempotencyConflict", err)
	}

	var storedReason string
	if err := pool.QueryRow(ctx, `
SELECT close_reason FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoAnimalScope).Scan(&storedReason); err != nil {
		t.Fatalf("read stored reason: %v", err)
	}
	if storedReason != "shed emptied early" {
		t.Fatalf("conflicting replay overwrote the reason with %q", storedReason)
	}
	if got := readScopeClosedAt(t, ctx, pool, repoAnimalScope); !got.Equal(closedAt) {
		t.Fatalf("conflicting replay rewrote closed_at")
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed.closed"); got != 1 {
		t.Fatalf("outbox rows after conflict=%d, want 1", got)
	}
}

// Campaign close cascades to every bucket that was NOT already accepted, leaves an
// accepted (completed) bucket alone, and publishes the affected-bucket list with its
// assigned operator so the notifier can reach exactly those operators.
func TestCloseCampaignClosesOpenBucketsKeepsCompletedOnesAndPublishesAffectedBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The lump-sum bucket is genuinely finished: accepted work must survive a close.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status='completed', completed_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope)

	result, err := repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID:       repoTenant,
		CampaignID:     repoCampaign,
		Reason:         "monsoon",
		ClosedBy:       repoVerifier,
		IdempotencyKey: "close:campaign-1",
	})
	if err != nil {
		t.Fatalf("close campaign: %v", err)
	}
	if result.NotAcceptedCount != 1 {
		t.Fatalf("campaign not-accepted bucket count=%d, want 1 (only the open individual bucket)", result.NotAcceptedCount)
	}
	assertCampaignStatus(t, ctx, pool, domain.StatusClosed)
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusClosed)
	// An already-accepted bucket keeps its accepted status.
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)
	// And the stranded roster row is still stranded, not accepted.
	assertExpectedAnimalStatus(t, ctx, pool, repoAnimal, "pending")

	var bucketCount int
	var bucketOperator, bucketShed string
	if err := pool.QueryRow(ctx, `
SELECT jsonb_array_length(payload->'payload'->'buckets'),
  payload->'payload'->'buckets'->0->>'operator_id',
  payload->'payload'->'buckets'->0->>'campaign_shed_id'
FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='weighing.campaign.closed'`, repoTenant).Scan(&bucketCount, &bucketOperator, &bucketShed); err != nil {
		t.Fatalf("read campaign close payload: %v", err)
	}
	if bucketCount != 1 || bucketOperator != repoOperator || bucketShed != repoAnimalScope {
		t.Fatalf("campaign close buckets=%d operator=%q shed=%q, want the one open bucket and its assigned operator", bucketCount, bucketOperator, bucketShed)
	}
	if got := countAudit(t, ctx, pool, "weighing.campaign_closed"); got != 1 {
		t.Fatalf("weighing.campaign_closed audit rows=%d, want 1", got)
	}

	// Exact replay of the campaign close: original result, no new side effects.
	replay, err := repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID:       repoTenant,
		CampaignID:     repoCampaign,
		Reason:         "monsoon",
		ClosedBy:       repoVerifier,
		IdempotencyKey: "close:campaign-1",
	})
	if err != nil {
		t.Fatalf("campaign close replay: %v", err)
	}
	if replay.NotAcceptedCount != result.NotAcceptedCount || !replay.ClosedAt.Equal(result.ClosedAt) {
		t.Fatalf("campaign close replay %+v != original %+v", replay, result)
	}
	if got := countOutbox(t, ctx, pool, "weighing.campaign.closed"); got != 1 {
		t.Fatalf("campaign close outbox rows after replay=%d, want 1", got)
	}
}

// A campaign already closed cannot be closed again under a NEW key: that is a state
// error, not a replay.
func TestCloseCampaignUnderNewKeyAfterCloseIsRejected(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	cmd := domain.CloseCommand{TenantID: repoTenant, CampaignID: repoCampaign, Reason: "monsoon", ClosedBy: repoVerifier, IdempotencyKey: "close:campaign-a"}
	if _, err := repo.CloseCampaign(ctx, cmd); err != nil {
		t.Fatalf("first campaign close: %v", err)
	}
	cmd.IdempotencyKey = "close:campaign-b"
	if _, err := repo.CloseCampaign(ctx, cmd); !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("second close under a new key err=%v, want ErrImmutable", err)
	}
}

// -----------------------------------------------------------------------------
// VERIFICATION VERDICT
// -----------------------------------------------------------------------------

// APPROVED marks the observation verified and leaves the bucket where it was. The
// replay is a no-op on state, emits no second event, and returns Applied=false.
func TestApplyVerificationVerdictApprovedMarksObservationVerifiedAndIsReplaySafe(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	// Proof requirement is now SHED-scoped to the bucket's location, not goat-scoped.
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "verdict-approve-rfid",
		WeightKg:          12.4, ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:verdict-approve", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}
	if got := readObservationVerificationStatus(t, ctx, pool, obs.ObservationID); got != domain.VerificationStatusPending {
		t.Fatalf("fresh observation verification_status=%q, want %q", got, domain.VerificationStatusPending)
	}

	// Free-flow: the bucket only reaches 'completed' via an explicit
	// SubmitIndividualScope call, not automatically when a weight is recorded.
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:verdict-approve", []string{"verdict-approve-rfid"}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	verdict := domain.VerificationVerdict{
		TenantID:      repoTenant,
		ObservationID: obs.ObservationID,
		RefType:       domain.VerificationRefTypeAnimal,
		Status:        domain.VerificationStatusVerified,
		VerifiedBy:    repoVerifier,
		EventID:       "11111111-1111-4111-8111-111111111abc",
	}
	first, err := repo.ApplyVerificationVerdict(ctx, verdict)
	if err != nil {
		t.Fatalf("apply approved verdict: %v", err)
	}
	if !first.Applied || first.Status != domain.VerificationStatusVerified {
		t.Fatalf("first verdict result=%+v, want applied verified", first)
	}
	if first.OperatorID != repoOperator || first.CampaignShedID != repoAnimalScope {
		t.Fatalf("verdict result operator=%q shed=%q, want the bucket's assigned operator", first.OperatorID, first.CampaignShedID)
	}
	if got := readObservationVerificationStatus(t, ctx, pool, obs.ObservationID); got != domain.VerificationStatusVerified {
		t.Fatalf("observation verification_status=%q, want verified", got)
	}
	if got := countOutbox(t, ctx, pool, "weighing.observation.verified"); got != 1 {
		t.Fatalf("weighing.observation.verified outbox rows=%d, want 1", got)
	}
	// Approval must NOT reopen the bucket.
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	replay, err := repo.ApplyVerificationVerdict(ctx, verdict)
	if err != nil {
		t.Fatalf("replay approved verdict: %v", err)
	}
	// The contract is "exact replay returns the ORIGINAL result": the readback is the
	// stored snapshot of the first application, Applied flag included. Proof that the
	// replay did no work is the unchanged outbox/audit counts asserted below.
	//
	// DecidedAt is compared with Equal, not ==: the replay value is rehydrated from
	// the JSON idempotency snapshot, so it carries a fixed-offset location while the
	// first value carries the named Asia/Kolkata location. Those are the SAME INSTANT
	// and struct equality would wrongly call them different (time.Time's == compares
	// the location pointer).
	if !replay.DecidedAt.Equal(first.DecidedAt) {
		t.Fatalf("replay decided_at=%s != original %s", replay.DecidedAt, first.DecidedAt)
	}
	replayComparable, firstComparable := replay, first
	replayComparable.DecidedAt, firstComparable.DecidedAt = time.Time{}, time.Time{}
	if replayComparable != firstComparable {
		t.Fatalf("replay result %+v != original %+v", replay, first)
	}
	if got := countOutbox(t, ctx, pool, "weighing.observation.verified"); got != 1 {
		t.Fatalf("outbox rows after replay=%d, want 1", got)
	}
	if got := countAudit(t, ctx, pool, "weighing.observation_verified"); got != 1 {
		t.Fatalf("audit rows after replay=%d, want 1", got)
	}
}

// REWORK bounces the observation AND makes the owning bucket operator-actionable
// again: a bucket completed via SubmitIndividualScope returns to in_progress so the
// operator's app shows the work. Free-flow: RecordAnimalObservation never resolves
// an animal_id or writes weighing_expected_animals (that roster is a label source
// only, never populated by a scan), so the roster row for repoAnimal is never
// flipped to 'weighed' in the first place -- it stays 'pending' throughout, which
// this test still pins so a future roster write-back regression is caught.
func TestApplyVerificationVerdictReworkMakesOwningBucketOperatorActionableAgain(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	// Proof requirement is now SHED-scoped to the bucket's location, not goat-scoped.
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "verdict-rework-rfid",
		WeightKg:          12.4, ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:verdict-rework", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}
	// Free-flow: the bucket only reaches 'completed' via an explicit
	// SubmitIndividualScope call, not automatically when a weight is recorded.
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:verdict-rework", []string{"verdict-rework-rfid"}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
	assertExpectedAnimalStatus(t, ctx, pool, repoAnimal, "pending")

	result, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:      repoTenant,
		ObservationID: obs.ObservationID,
		RefType:       domain.VerificationRefTypeAnimal,
		Status:        domain.VerificationStatusRework,
		VerifiedBy:    repoVerifier,
		Reason:        "video too dark",
		EventID:       "22222222-2222-4222-8222-222222222abc",
	})
	if err != nil {
		t.Fatalf("apply rework verdict: %v", err)
	}
	if !result.Applied {
		t.Fatal("rework verdict was not applied")
	}
	if got := readObservationVerificationStatus(t, ctx, pool, obs.ObservationID); got != domain.VerificationStatusRework {
		t.Fatalf("observation verification_status=%q, want rework", got)
	}
	var reworkReason string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(rework_reason,'') FROM weighing_observations
WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, repoTenant, obs.ObservationID).Scan(&reworkReason); err != nil {
		t.Fatalf("read rework reason: %v", err)
	}
	if reworkReason != "video too dark" {
		t.Fatalf("rework reason=%q, want the verifier reason", reworkReason)
	}
	// Operator-actionable again.
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusInProgress)
	assertExpectedAnimalStatus(t, ctx, pool, repoAnimal, "pending")
	if got := countOutbox(t, ctx, pool, "weighing.observation.rework"); got != 1 {
		t.Fatalf("weighing.observation.rework outbox rows=%d, want 1", got)
	}
	var operatorActionable bool
	if err := pool.QueryRow(ctx, `
SELECT (payload->'payload'->>'operator_actionable')::bool
FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='weighing.observation.rework'`, repoTenant).Scan(&operatorActionable); err != nil {
		t.Fatalf("read rework payload: %v", err)
	}
	if !operatorActionable {
		t.Fatal("rework event says operator_actionable=false; the operator owns the redo")
	}

	correction, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "verdict-rework-rfid",
		WeightKg:          13.1, ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:verdict-rework-correction", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("same RFID correction after rework: %v", err)
	}
	if correction.ObservationID != obs.ObservationID {
		t.Fatalf("correction observation_id=%s, want rework row %s", correction.ObservationID, obs.ObservationID)
	}
	var submittedAt *time.Time
	var verificationStatus string
	var weight float64
	if err := pool.QueryRow(ctx, `
SELECT submitted_at, verification_status, weight_kg::float8
FROM weighing_observations
WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, repoTenant, obs.ObservationID).Scan(&submittedAt, &verificationStatus, &weight); err != nil {
		t.Fatalf("read corrected rework row: %v", err)
	}
	if submittedAt != nil || verificationStatus != domain.VerificationStatusPending || weight != 13.1 {
		t.Fatalf("corrected row submitted_at=%v verification_status=%q weight=%v, want draft pending 13.1", submittedAt, verificationStatus, weight)
	}
}

// A CLOSED bucket is a deliberate leadership decision. A verifier's rework verdict
// must not silently undo it.
func TestApplyVerificationVerdictReworkDoesNotReopenAClosedBucket(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	// Proof requirement is now SHED-scoped to the bucket's location, not goat-scoped.
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "verdict-closed-rfid",
		WeightKg:          12.4, ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:verdict-closed", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}
	if _, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason: "week over", ClosedBy: repoVerifier, IdempotencyKey: "close:before-verdict",
	}); err != nil {
		t.Fatalf("close scope: %v", err)
	}

	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: obs.ObservationID, RefType: domain.VerificationRefTypeAnimal,
		Status: domain.VerificationStatusRework, VerifiedBy: repoVerifier, Reason: "blurry",
		EventID: "33333333-3333-4333-8333-333333333abc",
	}); err != nil {
		t.Fatalf("apply rework on closed bucket: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusClosed)
}

// A verdict for an observation this tenant does not have must be ErrNotFound, which
// the consumer turns into a permanent (DLQ) failure instead of retrying forever.
func TestApplyVerificationVerdictRejectsUnknownObservation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: "99999999-9999-4999-8999-999999999999",
		RefType: domain.VerificationRefTypeAnimal, Status: domain.VerificationStatusVerified,
		VerifiedBy: repoVerifier, EventID: "44444444-4444-4444-8444-444444444abc",
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("unknown observation err=%v, want ErrNotFound", err)
	}
}

// -----------------------------------------------------------------------------
// FREE-FLOW REGRESSION
// -----------------------------------------------------------------------------

// Weighing is FREE-FLOW. An observation with only a raw scanned identifier (there
// is no animal_id column at all -- dropped by
// 000078_weighing_observations_drop_animal_id.sql) must be accepted on its own
// merits: no goat row, no herd roster entry, no vaccination record, and no
// expected-animal row is required or created. The same raw identifier must ALSO
// be independently acceptable in a different bucket, so no constraint collapses
// it across campaign_shed_id.
func TestFreeFlowObservationWithScannedIdentifierIsAcceptedAndNeverValidatedAgainstHerdOrVaccination(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// A shed-scoped proof for the individual bucket's location. The lump-sum bucket
	// already has repoShedProof scoped to its own location.
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)

	const freeFlowTag = "FREE-RFID-NO-SUCH-GOAT"
	goatsBefore := countRows(t, ctx, pool, `SELECT count(*)::int FROM goats WHERE tenant_id=$1::uuid`, repoTenant)
	// FREE-FLOW: weighing_expected_animals was DROPPED (migration 000079); there
	// is no roster table left to touch.

	firstBucket, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: freeFlowTag, WeightKg: 11.25, ProofArtifactID: repoExpectedShedProof,
		IdempotencyKey: "free-flow:bucket-a", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("free-flow observation with only a scanned identifier was rejected: %v", err)
	}
	if firstBucket.ObservationID == "" {
		t.Fatal("free-flow observation returned no observation id")
	}

	// The stored row carries only the raw scanned identifier -- there is no
	// animal_id column on this table at all, so there is nothing for the write
	// path to have resolved the scan to.
	var storedTag string
	if err := pool.QueryRow(ctx, `
SELECT scanned_identifier
FROM weighing_observations
WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, repoTenant, firstBucket.ObservationID).Scan(&storedTag); err != nil {
		t.Fatalf("read free-flow observation: %v", err)
	}
	if storedTag != freeFlowTag {
		t.Fatalf("stored scanned_identifier=%q, want the raw tag %q kept verbatim", storedTag, freeFlowTag)
	}

	// SAME raw identifier in a DIFFERENT bucket is its own independent observation.
	// The second bucket must also be an individual_animal bucket: the per-animal writer
	// refuses a lump-sum bucket on MODE, which is a weighing-owned check, not a herd one.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Gandhi 1 - Part 2', 'individual_animal', $5::uuid, 1)
ON CONFLICT (campaign_shed_id) DO UPDATE SET weighing_category='individual_animal'`,
		freeFlowSecondBucket, repoCampaign, repoTenant, repoActualShed, repoOperator)
	insertProof(t, ctx, pool, freeFlowSecondBucketProof, "video", "completed", "shed", repoActualShed, "shed", repoActualShed)
	secondBucket, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: freeFlowSecondBucket,
		ScannedIdentifier: freeFlowTag, WeightKg: 13.5, ProofArtifactID: freeFlowSecondBucketProof,
		IdempotencyKey: "free-flow:bucket-b", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("same scanned identifier in a second bucket was rejected: %v", err)
	}
	if secondBucket.ObservationID == firstBucket.ObservationID {
		t.Fatal("the second bucket reused the first bucket's observation; buckets must not collapse on scanned_identifier")
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)::int FROM weighing_observations
WHERE tenant_id=$1::uuid AND scanned_identifier=$2`, repoTenant, freeFlowTag); got != 2 {
		t.Fatalf("free-flow rows for %q=%d, want one per bucket", freeFlowTag, got)
	}

	// Nothing was written to, or required from, the herd or the roster.
	if got := countRows(t, ctx, pool, `SELECT count(*)::int FROM goats WHERE tenant_id=$1::uuid`, repoTenant); got != goatsBefore {
		t.Fatalf("goat rows changed from %d to %d; free-flow weighing must not touch herd identity", goatsBefore, got)
	}
	// And a verdict on a free-flow observation stays free-flow: no roster row to reopen.
	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: firstBucket.ObservationID, RefType: domain.VerificationRefTypeAnimal,
		Status: domain.VerificationStatusRework, VerifiedBy: repoVerifier, Reason: "reshoot",
		EventID: "55555555-5555-4555-8555-555555555abc",
	}); err != nil {
		t.Fatalf("verdict on a free-flow observation errored: %v", err)
	}
}

// -----------------------------------------------------------------------------
// PAGINATION
// -----------------------------------------------------------------------------

// The operator predicate must be part of the WHERE clause, evaluated BEFORE LIMIT.
// If it were applied after the page was cut, an operator paging through their work
// would hit a page that is empty (or worse, shows a foreign operator's campaign)
// purely because an unassigned campaign happened to sort into that page. This drives
// page 2 explicitly with a foreign campaign sorting between the operator's two.
func TestListCampaignsForOperatorAppliesOperatorPredicateBeforeLimitOnPageTwo(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Sort order is period_start_date DESC. Newest = a foreign operator's campaign,
	// middle = ours, oldest (fixture, 2026-07-27) = ours. So page 1 and page 2 for our
	// operator are both ours only if the predicate runs before LIMIT.
	foreignCampaign := "00000000-0000-4000-8000-00000000a001"
	foreignShed := "00000000-0000-4000-8000-00000000a002"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-08-17', '2026-08-23', '2026-08-17', 'published', 100, $4::uuid, $4::uuid)`,
		foreignCampaign, repoTenant, repoPark, repoOtherOp)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Foreign Shed', 'individual_animal', $5::uuid, 1)`,
		foreignShed, foreignCampaign, repoTenant, repoActualShed, repoOtherOp)

	ourSecondCampaign := "00000000-0000-4000-8000-00000000a003"
	ourSecondShed := "00000000-0000-4000-8000-00000000a004"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-08-10', '2026-08-16', '2026-08-10', 'published', 100, $4::uuid, $4::uuid)`,
		ourSecondCampaign, repoTenant, repoPark, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Our Second Shed', 'individual_animal', $5::uuid, 1)`,
		ourSecondShed, ourSecondCampaign, repoTenant, repoPark, repoOperator)

	pageOne, err := repo.ListCampaignsForOperator(ctx, repoTenant, repoOperator, "", "", 1)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(pageOne.Items) != 1 || pageOne.Items[0].CampaignID != ourSecondCampaign {
		t.Fatalf("page 1=%+v, want only our newest campaign (the foreign newer campaign must be filtered before LIMIT)", pageOne.Items)
	}
	if pageOne.NextCursor == "" {
		t.Fatal("page 1 returned no cursor; the operator's second campaign is unreachable")
	}

	pageTwo, err := repo.ListCampaignsForOperator(ctx, repoTenant, repoOperator, "", pageOne.NextCursor, 1)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(pageTwo.Items) != 1 {
		t.Fatalf("page 2 items=%d, want 1; an empty page 2 means the operator predicate ran after LIMIT", len(pageTwo.Items))
	}
	if pageTwo.Items[0].CampaignID != repoCampaign {
		t.Fatalf("page 2 campaign=%q, want the operator's older campaign %q", pageTwo.Items[0].CampaignID, repoCampaign)
	}
	for _, item := range append(pageOne.Items, pageTwo.Items...) {
		if item.CampaignID == foreignCampaign {
			t.Fatal("a foreign operator's campaign leaked into the operator's pages")
		}
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func countOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType string) int {
	t.Helper()
	return countRows(t, ctx, pool, `
SELECT count(*)::int FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type=$2`, repoTenant, eventType)
}

func countAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action string) int {
	t.Helper()
	return countRows(t, ctx, pool, `
SELECT count(*)::int FROM audit_log WHERE tenant_id=$1::uuid AND action=$2`, repoTenant, action)
}

// countExpectedAnimalsWithStatus is a NO-OP survivor of the deleted
// expected-animal roster (weighing_expected_animals was DROPPED, migration
// 000079). It always returns 0: there is no roster row left to ever be
// "weighed" or "closed_by_override", which is a stronger guarantee than the
// original assertion, not a weaker one.
func countExpectedAnimalsWithStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, status string) int {
	t.Helper()
	_ = ctx
	_ = pool
	_ = status
	return 0
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		t.Fatalf("count query failed: %v\n%s", err, sql)
	}
	return count
}

func readScopeClosedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID string) time.Time {
	t.Helper()
	var closedAt time.Time
	if err := pool.QueryRow(ctx, `
SELECT closed_at FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, campaignShedID).Scan(&closedAt); err != nil {
		t.Fatalf("read closed_at: %v", err)
	}
	return closedAt
}

func readObservationVerificationStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, observationID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `
SELECT verification_status FROM weighing_observations
WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, repoTenant, observationID).Scan(&status); err != nil {
		t.Fatalf("read verification_status: %v", err)
	}
	return status
}

// -----------------------------------------------------------------------------
// BLOCKER 11 — the verdict's decision time
// -----------------------------------------------------------------------------

// The decision time must be the instant the verdict was PERSISTED, expressed in
// India business time, and it must survive redelivery unchanged.
//
// The original defect had two halves. (1) The payload read `time.Now().UTC()`,
// but every Goat OS business meaning derives from Asia/Kolkata, never UTC
// (AGENTS.md). (2) More seriously, that wall-clock read was a SECOND clock: the
// row stored `verified_at = now()` from the database while the event payload
// stamped its own time, so the two could disagree — and on an at-least-once
// redelivery the payload would mint a brand-new decision time for a decision
// that had already happened, telling downstream consumers the verifier acted at
// a moment they did not.
//
// The fix makes the persisted `verified_at` the single source: it is RETURNED by
// the same UPDATE, carried on the result, and therefore captured in the
// idempotency snapshot that a replay reads back.
func TestApplyVerificationVerdictDecidedAtIsPersistedIndiaTimeAndStableAcrossReplay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	// Proof requirement is now SHED-scoped to the bucket's location, not goat-scoped.
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "verdict-decided-at-rfid",
		WeightKg:          12.4, ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:verdict-decided-at", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}

	verdict := domain.VerificationVerdict{
		TenantID:      repoTenant,
		ObservationID: obs.ObservationID,
		RefType:       domain.VerificationRefTypeAnimal,
		Status:        domain.VerificationStatusVerified,
		VerifiedBy:    repoVerifier,
		EventID:       "11111111-1111-4111-8111-1111111decaf",
	}
	first, err := repo.ApplyVerificationVerdict(ctx, verdict)
	if err != nil {
		t.Fatalf("apply verdict: %v", err)
	}
	if first.DecidedAt.IsZero() {
		t.Fatal("verdict result carries no decided_at")
	}

	// (1) India business time, not UTC.
	if got := first.DecidedAt.Location().String(); got != biztime.DefaultLocation().String() {
		t.Fatalf("decided_at location=%q, want %q — business meaning must derive from India business time, never UTC",
			got, biztime.DefaultLocation().String())
	}

	// (2) It is the PERSISTED instant, not a second wall-clock read.
	var storedVerifiedAt time.Time
	if err := pool.QueryRow(ctx,
		`SELECT verified_at FROM weighing_observations WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`,
		repoTenant, obs.ObservationID).Scan(&storedVerifiedAt); err != nil {
		t.Fatalf("read stored verified_at: %v", err)
	}
	if !first.DecidedAt.Equal(storedVerifiedAt) {
		t.Fatalf("decided_at=%s but the row stored verified_at=%s — the event and the row must report the SAME decision instant",
			first.DecidedAt, storedVerifiedAt)
	}

	// (3) A redelivery replays the original instant; it never mints a new one.
	replay, err := repo.ApplyVerificationVerdict(ctx, verdict)
	if err != nil {
		t.Fatalf("replay verdict: %v", err)
	}
	if !replay.DecidedAt.Equal(first.DecidedAt) {
		t.Fatalf("replay decided_at=%s != original %s — an at-least-once redelivery must not invent a new decision time",
			replay.DecidedAt, first.DecidedAt)
	}
	if !replay.DecidedAt.Equal(storedVerifiedAt) {
		t.Fatalf("replay decided_at=%s drifted from the persisted verified_at=%s", replay.DecidedAt, storedVerifiedAt)
	}
}

// TestCloseScopePreservesPreexistingTerminalExpectedAnimalStatuses was DELETED
// (free-flow weighing mandate): weighing_expected_animals, the per-animal
// roster this test pinned, was DROPPED entirely (migration 000079). Close ends
// a BUCKET; there is no per-animal roster state left to preserve or clobber.
// See AGENTS.md, SKILLS.md, and migration 000059.

// -----------------------------------------------------------------------------
// CLOSE GATE (maintainer decision 2026-07-31)
// -----------------------------------------------------------------------------

// Leadership may not close a bucket while a submitted video is still unreviewed.
// The gate is only about closing EARLY: the operator may still scan and submit, and
// the verifier may still review. Work that will never finish ends via AbandonScope.
func TestCloseScopeBlockedWhileVerificationPending(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	obs := seedSubmittedObservation(t, ctx, pool, repo, "close-gate-pending")

	_, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason: "leadership tried to close early", ClosedBy: repoVerifier,
		IdempotencyKey: "close:gate-pending",
	})
	if !errors.Is(err, ports.ErrVerificationPending) {
		t.Fatalf("close with an unverified submitted video err=%v, want ErrVerificationPending", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	// Verify it, and the same close now succeeds.
	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: obs, RefType: domain.VerificationRefTypeAnimal,
		Status: domain.VerificationStatusVerified, VerifiedBy: repoVerifier,
		EventID: "aaaaaaaa-0000-4000-8000-00000000ab01",
	}); err != nil {
		t.Fatalf("verify observation: %v", err)
	}
	if _, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason: "all videos checked", ClosedBy: repoVerifier,
		IdempotencyKey: "close:gate-verified",
	}); err != nil {
		t.Fatalf("close after every video verified: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusClosed)
}

// A bounced video is unfinished work the operator still owes, so 'rework' counts as
// pending and the normal close stays shut.
func TestCloseScopeBlockedWhileReworkOutstanding(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	obs := seedSubmittedObservation(t, ctx, pool, repo, "close-gate-rework")
	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: obs, RefType: domain.VerificationRefTypeAnimal,
		Status: domain.VerificationStatusRework, VerifiedBy: repoVerifier, Reason: "reshoot",
		EventID: "aaaaaaaa-0000-4000-8000-00000000ab02",
	}); err != nil {
		t.Fatalf("bounce observation: %v", err)
	}

	if _, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason: "closing over a rework", ClosedBy: repoVerifier,
		IdempotencyKey: "close:gate-rework",
	}); !errors.Is(err, ports.ErrVerificationPending) {
		t.Fatalf("close with an outstanding rework err=%v, want ErrVerificationPending", err)
	}
}

// Abandon is the explicit way out for work that will never finish: it skips the
// gate, demands a reason, and records itself as its own event so it can never read
// as a verified close.
func TestAbandonScopeEndsUnverifiedBucketAndIsRecordedDistinctly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	seedSubmittedObservation(t, ctx, pool, repo, "abandon-unverified")

	if _, err := repo.AbandonScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason: "", ClosedBy: repoVerifier, IdempotencyKey: "abandon:no-reason",
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("abandon without a reason err=%v, want ErrInvalidArgument", err)
	}

	if _, err := repo.AbandonScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason:   "operator left the farm; videos will never be shot",
		ClosedBy: repoVerifier, IdempotencyKey: "abandon:never-finishing",
	}); err != nil {
		t.Fatalf("abandon an unverified bucket: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusClosed)

	if got := countOutbox(t, ctx, pool, "weighing.shed.abandoned"); got != 1 {
		t.Fatalf("weighing.shed.abandoned outbox rows=%d, want 1", got)
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed.closed"); got != 0 {
		t.Fatalf("weighing.shed.closed outbox rows=%d, want 0 — an abandon must never look like a verified close", got)
	}
	if got := countAudit(t, ctx, pool, "weighing.scope_abandoned"); got != 1 {
		t.Fatalf("weighing.scope_abandoned audit rows=%d, want 1", got)
	}
}

// The bucket read contract must surface ready_to_close / pending_verification_count
// from real submitted/verified evidence — false while ANY verification is pending,
// true only once every submitted video has been verified. No expected-animal
// denominator is involved anywhere in this computation.
func TestBucketReadyToCloseReflectsVerificationState(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	shedByID := func(t *testing.T) domain.CampaignShed {
		t.Helper()
		page, err := repo.ListCampaigns(ctx, repoTenant, "", "", 50)
		if err != nil {
			t.Fatalf("list campaigns: %v", err)
		}
		for _, campaign := range page.Items {
			if campaign.CampaignID != repoCampaign {
				continue
			}
			for _, shed := range campaign.Sheds {
				if shed.CampaignShedID == repoAnimalScope {
					return shed
				}
			}
		}
		t.Fatalf("campaign shed %s not found in list", repoAnimalScope)
		return domain.CampaignShed{}
	}

	obs := seedSubmittedObservation(t, ctx, pool, repo, "ready-to-close")

	shed := shedByID(t)
	if shed.PendingVerificationCount != 1 {
		t.Fatalf("pending_verification_count=%d, want 1 while the submitted video is unverified", shed.PendingVerificationCount)
	}
	if shed.ReadyToClose {
		t.Fatalf("ready_to_close=true while a submitted video is still unverified, want false")
	}

	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: obs, RefType: domain.VerificationRefTypeAnimal,
		Status: domain.VerificationStatusVerified, VerifiedBy: repoVerifier,
		EventID: "aaaaaaaa-0000-4000-8000-00000000ab03",
	}); err != nil {
		t.Fatalf("verify observation: %v", err)
	}

	shed = shedByID(t)
	if shed.PendingVerificationCount != 0 {
		t.Fatalf("pending_verification_count=%d after verifying the only video, want 0", shed.PendingVerificationCount)
	}
	if !shed.ReadyToClose {
		t.Fatalf("ready_to_close=false once every submitted video is verified, want true")
	}
}

// seedSubmittedObservation records one free-flow scan in the individual bucket and
// submits it, leaving the bucket at 'completed' with exactly one unverified video.
func seedSubmittedObservation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, tag string) string {
	t.Helper()
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: tag, WeightKg: 12.0, ProofArtifactID: repoExpectedShedProof,
		ActualLocationID: repoExpectedShed, IdempotencyKey: "seed:" + tag, RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record observation for %s: %v", tag, err)
	}
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
		"submit:"+tag, []string{tag}); err != nil {
		t.Fatalf("submit scope for %s: %v", tag, err)
	}
	return obs.ObservationID
}

// The campaign-level close must obey the same verification gate as the per-bucket
// close. It was previously a SECOND, ungated door to 'closed': it takes no reason
// and is not the explicit abandon path, so leadership could sweep shut the very
// bucket CloseScope had just refused.
func TestCloseCampaignBlockedWhileAnyBucketHasPendingVerification(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// A SUBMITTED bucket holding an unreviewed video. Its status is 'completed',
	// which in this module means "operator submitted, awaiting verification" — the
	// single most common real state, and the one the first version of this test
	// wrongly flipped back to 'in_progress' before asserting. That flip made the
	// test pass against a gate that excluded 'completed' buckets entirely, hiding
	// the fact that the whole normal flow walked straight through. Leave the bucket
	// exactly as submit left it.
	obs := seedSubmittedObservation(t, ctx, pool, repo, "campaign-gate-pending")
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	if _, err := repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign,
		Reason: "leadership bulk close", ClosedBy: repoVerifier,
		IdempotencyKey: "close-campaign:gate-pending",
	}); !errors.Is(err, ports.ErrVerificationPending) {
		t.Fatalf("campaign close with an unverified submitted video err=%v, want ErrVerificationPending", err)
	}
	// The refused close changed nothing.
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	// Once the verifier has reviewed it, the same campaign close succeeds.
	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: obs, RefType: domain.VerificationRefTypeAnimal,
		Status: domain.VerificationStatusVerified, VerifiedBy: repoVerifier,
		EventID: "aaaaaaaa-0000-4000-8000-00000000ac01",
	}); err != nil {
		t.Fatalf("verify observation: %v", err)
	}
	if _, err := repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign,
		Reason: "all videos checked", ClosedBy: repoVerifier,
		IdempotencyKey: "close-campaign:gate-verified",
	}); err != nil {
		t.Fatalf("campaign close after every video verified: %v", err)
	}
}

// Same rule, LUMP-SUM bucket. A per-shed observation IS the submission, so the
// bucket reaches its natural post-submit state without any individual scans, and
// campaign close must still wait for the verifier.
func TestCloseCampaignBlockedWhileLumpSumVideoPendingVerification(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	shedObs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 92.0, AnimalCount: 8,
		ProofArtifactID: repoShedProof,
		IdempotencyKey:  "lumpsum:campaign-gate", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record lump-sum observation: %v", err)
	}
	// Natural post-submit state — nothing hand-edited.
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)

	if _, err := repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign,
		Reason: "bulk close over an unverified lump-sum video", ClosedBy: repoVerifier,
		IdempotencyKey: "close-campaign:lumpsum-pending",
	}); !errors.Is(err, ports.ErrVerificationPending) {
		t.Fatalf("campaign close with an unverified lump-sum video err=%v, want ErrVerificationPending", err)
	}

	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: shedObs.ObservationID,
		RefType: domain.VerificationRefTypeShed, Status: domain.VerificationStatusVerified,
		VerifiedBy: repoVerifier, EventID: "aaaaaaaa-0000-4000-8000-00000000ad01",
	}); err != nil {
		t.Fatalf("verify lump-sum observation: %v", err)
	}
	if _, err := repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign,
		Reason: "lump-sum video checked", ClosedBy: repoVerifier,
		IdempotencyKey: "close-campaign:lumpsum-verified",
	}); err != nil {
		t.Fatalf("campaign close after the lump-sum video was verified: %v", err)
	}
}

// A bounced video is NOT a verdict that lets leadership close. 'rework' is work the
// operator still owes, so campaign close stays blocked until it is re-shot and
// verified — otherwise a close would bury the rework request.
func TestCloseCampaignBlockedWhileSubmittedVideoIsInRework(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	obs := seedSubmittedObservation(t, ctx, pool, repo, "campaign-gate-rework")
	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: obs, RefType: domain.VerificationRefTypeAnimal,
		Status: domain.VerificationStatusRework, VerifiedBy: repoVerifier, Reason: "reshoot",
		EventID: "aaaaaaaa-0000-4000-8000-00000000ad02",
	}); err != nil {
		t.Fatalf("bounce observation: %v", err)
	}

	if _, err := repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign,
		Reason: "closing over a rework", ClosedBy: repoVerifier,
		IdempotencyKey: "close-campaign:rework",
	}); !errors.Is(err, ports.ErrVerificationPending) {
		t.Fatalf("campaign close with an outstanding rework err=%v, want ErrVerificationPending", err)
	}
}

// The campaign close must SERIALISE against concurrent bucket writes, and must do
// so BEFORE it reads the verification gate.
//
// The write-skew this guards: the gate SELECT reads "nothing pending", an in-flight
// submit then commits, the cascade skips the now-'completed' bucket, and the campaign
// closes with that bucket's unverified video stranded under it. Locking only the
// weighing_campaigns row did not prevent this, because nothing forced the gate to
// wait for in-flight bucket writes.
//
// NOTE ON WHY THIS TEST IS SHAPED THIS WAY: an earlier version simply held a bucket
// row and asserted CloseCampaign hit its deadline. That test PASSED with the lock
// removed — without FOR UPDATE the close still blocks, just later, on the cascade
// UPDATE. It proved "blocks somewhere", not "gate runs after in-flight writes settle".
// This version discriminates: the competing transaction commits a submitted,
// unverified observation while the close is waiting. With the bucket lock the gate
// runs afterwards and refuses; without it the gate has already read a clean campaign
// and the close succeeds.
func TestCloseCampaignSerialisesAgainstConcurrentBucketWrites(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 8*time.Second)
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)

	// An in-flight submit: holds the bucket row, has not committed yet.
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holding transaction: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `
SELECT 1 FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid
FOR UPDATE`, repoTenant, repoAnimalScope); err != nil {
		t.Fatalf("lock bucket row: %v", err)
	}

	closeErr := make(chan error, 1)
	go func() {
		_, err := repo.CloseCampaign(context.Background(), domain.CloseCommand{
			TenantID: repoTenant, CampaignID: repoCampaign,
			Reason: "close racing an in-flight submit", ClosedBy: repoVerifier,
			IdempotencyKey: "close-campaign:race",
		})
		closeErr <- err
	}()

	// Give the close time to reach (and, with the fix, block on) the bucket lock.
	time.Sleep(400 * time.Millisecond)

	// The in-flight submit lands: an unverified observation, bucket now submitted.
	if _, err := holder.Exec(ctx, `
INSERT INTO weighing_observations
  (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg,
   proof_artifact_id, recorded_by, idempotency_key, submitted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'race-tag', 12.0, $4::uuid, $5::uuid,
        'race:submit', now(), 'pending')`,
		repoTenant, repoCampaign, repoAnimalScope, repoExpectedShedProof, repoOperator); err != nil {
		t.Fatalf("insert racing observation: %v", err)
	}
	if _, err := holder.Exec(ctx, `
UPDATE weighing_campaign_sheds SET status='completed'
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoAnimalScope); err != nil {
		t.Fatalf("mark bucket submitted: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit racing submit: %v", err)
	}

	select {
	case err := <-closeErr:
		if !errors.Is(err, ports.ErrVerificationPending) {
			t.Fatalf("campaign close raced an in-flight submit and returned err=%v, want ErrVerificationPending — the gate read the campaign before the submit settled, so the bucket's unverified video would be stranded under a closed campaign", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("CloseCampaign never returned")
	}
}
