package postgres

import (
	"context"
	"errors"
	"testing"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// Feed DISTRIBUTION verification gate -- proofs of the maintainer-2026-07-26 rule against the REAL
// feed_distribution_completions schema (the same wiring bootstrap uses, so the table's CHECK
// constraints, the natural/idempotency indexes, and the outbox tenant-parity trigger are all part of
// the assertion surface). Entirely separate from the untouched feed PACKING path
// (feed_direction_session_completions / feed.direction.completed), which these tests never touch.
//
// The rule: an operator's completion no longer completes the session. It records three mandatory proofs
// (feed weight photo + feed-distribution video + water-distribution video) and flips the session to
// 'pending_verification'; the
// session is 'completed' (and feed.distribution.completed is emitted) ONLY when a verifier approves,
// via ApplyVerifiedDistribution. A rejected video bounces the session to 'rework' for a re-shoot.
//
// Gated by pgtest.SkipIfNoDocker (inside setupFeedDirectionDB) + GOATOS_RUN_POSTGRES_TESTS, the same
// harness the rest of this package's integration tests use.

// recordingDistributionEnqueuer is a test double for the verifier-queue enqueue seam.
type recordingDistributionEnqueuer struct {
	calls []feeddirectionapp.FeedDistributionVerificationEnqueueRequest
}

func (e *recordingDistributionEnqueuer) EnqueueFeedDistributionVerification(
	_ context.Context, in feeddirectionapp.FeedDistributionVerificationEnqueueRequest,
) error {
	e.calls = append(e.calls, in)
	return nil
}

func distributionParams() ports.CompleteDistributionParams {
	return ports.CompleteDistributionParams{
		TenantID:             fdTenant,
		ParkID:               fdPark,
		ShedID:               fdShedA,
		SessionNo:            1,
		TargetDate:           businessDay(2026, 7, 22),
		Workflow:             domain.WorkflowNormal,
		FeedWeightProofRef:   "proof-feed-weight-photo-0001",
		DistributionProofRef: "proof-distribution-0001",
		WaterProofRef:        "proof-water-0001",
		CompletedBy:          fdActor,
		IdempotencyKey:       "feed-distribution-key-0001",
		ActorID:              fdActor,
		ActorType:            "operator",
		TraceID:              "trace-feed-distribution-1",
	}
}

// (a) A completion missing any required proof is rejected at the APP layer
// before any state changes -- there is nothing for a verifier to approve.
func TestCompleteDistributionRequiresBothProofsAtAppLayer(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	enq := &recordingDistributionEnqueuer{}
	svc := feeddirectionapp.NewService(nil, nil).
		WithDistributionStore(repo).
		WithDistributionVerificationEnqueuer(enq)

	base := feeddirectionapp.CompleteDistributionInput{
		TenantID:       fdTenant,
		ParkID:         fdPark,
		ShedID:         fdShedA,
		SessionNo:      1,
		TargetDate:     businessDay(2026, 7, 22),
		Workflow:       domain.WorkflowNormal,
		CompletedBy:    fdActor,
		IdempotencyKey: "feed-distribution-app-key-0001",
		ActorID:        fdActor,
		ActorType:      "operator",
	}

	missingWeightPhoto := base
	missingWeightPhoto.FeedWeightProofRef = ""
	missingWeightPhoto.DistributionProofRef = "proof-distribution-0001"
	missingWeightPhoto.WaterProofRef = "proof-water-0001"
	if _, err := svc.CompleteDistribution(ctx, missingWeightPhoto); !errors.Is(err, ports.ErrDistributionProofRequired) {
		t.Fatalf("missing feed weight photo err = %v, want ErrDistributionProofRequired", err)
	}

	missingWater := base
	missingWater.FeedWeightProofRef = "proof-feed-weight-photo-0001"
	missingWater.DistributionProofRef = "proof-distribution-0001"
	missingWater.WaterProofRef = ""
	if _, err := svc.CompleteDistribution(ctx, missingWater); !errors.Is(err, ports.ErrWaterProofRequired) {
		t.Fatalf("missing water video err = %v, want ErrWaterProofRequired", err)
	}

	missingVideo := base
	missingVideo.FeedWeightProofRef = "proof-feed-weight-photo-0001"
	missingVideo.DistributionProofRef = ""
	missingVideo.WaterProofRef = "proof-water-0001"
	if _, err := svc.CompleteDistribution(ctx, missingVideo); !errors.Is(err, ports.ErrDistributionProofRequired) {
		t.Fatalf("missing distribution video err = %v, want ErrDistributionProofRequired", err)
	}

	// Neither rejected request enqueued anything, and neither wrote a row.
	if len(enq.calls) != 0 {
		t.Fatalf("enqueue calls = %d, want 0 (rejected before write)", len(enq.calls))
	}
	verified, err := repo.ListVerifiedDistributions(ctx, fdTenant, fdPark, businessDay(2026, 7, 22))
	if err != nil {
		t.Fatalf("ListVerifiedDistributions: %v", err)
	}
	if len(verified) != 0 {
		t.Fatalf("verified distributions = %d, want 0", len(verified))
	}
}

func TestCompleteDistributionEnqueueRequestCarriesPartition(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	seedFeedDirectionPartition(t, ctx, pool, fdShedA, "2")

	enq := &recordingDistributionEnqueuer{}
	svc := feeddirectionapp.NewService(nil, nil).
		WithDistributionStore(repo).
		WithDistributionVerificationEnqueuer(enq)

	_, err := svc.CompleteDistribution(ctx, feeddirectionapp.CompleteDistributionInput{
		TenantID:             fdTenant,
		ParkID:               fdPark,
		ShedID:               fdShedA,
		PartitionLabel:       "2",
		SessionNo:            1,
		TargetDate:           businessDay(2026, 7, 22),
		Workflow:             domain.WorkflowNormal,
		FeedWeightProofRef:   "proof-weight-0001",
		DistributionProofRef: "proof-distribution-0001",
		WaterProofRef:        "proof-water-0001",
		CompletedBy:          fdActor,
		IdempotencyKey:       "feed-distribution-partition-enqueue-0001",
		ActorID:              fdActor,
		ActorType:            "operator",
	})
	if err != nil {
		t.Fatalf("CompleteDistribution: %v", err)
	}
	if len(enq.calls) != 1 {
		t.Fatalf("enqueue calls = %d, want 1", len(enq.calls))
	}
	if enq.calls[0].PartitionLabel != "" {
		t.Fatalf("enqueue partition = %q, want blank because shed id is already exact", enq.calls[0].PartitionLabel)
	}
}

// The weight photo REACHES THE COLUMN, and the enqueued item carries all three refs in CAPTURE ORDER.
//
// This is the assertion that a field-presence test cannot make: a struct field that is declared and
// threaded through the app layer but never projected into the INSERT compiles, passes every unit test,
// and writes NULL. It reads the value back out of Postgres and checks the enqueue payload the verifier
// queue is actually built from.
func TestCompleteDistributionPersistsWeightPhotoAndEnqueuesThreeProofsInCaptureOrder(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	enq := &recordingDistributionEnqueuer{}
	svc := feeddirectionapp.NewService(nil, nil).
		WithDistributionStore(repo).
		WithDistributionVerificationEnqueuer(enq)

	res, err := svc.CompleteDistribution(ctx, feeddirectionapp.CompleteDistributionInput{
		TenantID:             fdTenant,
		ParkID:               fdPark,
		ShedID:               fdShedA,
		SessionNo:            1,
		TargetDate:           businessDay(2026, 7, 22),
		Workflow:             domain.WorkflowNormal,
		FeedWeightProofRef:   "proof-weight-0001",
		DistributionProofRef: "proof-distribution-0001",
		WaterProofRef:        "proof-water-0001",
		CompletedBy:          fdActor,
		IdempotencyKey:       "feed-distribution-weight-persist-0001",
		ActorID:              fdActor,
		ActorType:            "operator",
	})
	if err != nil {
		t.Fatalf("CompleteDistribution: %v", err)
	}

	var storedWeight *string
	if err := pool.QueryRow(ctx, `
SELECT feed_weight_proof_ref FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, res.CompletionID).Scan(&storedWeight); err != nil {
		t.Fatalf("read weight proof: %v", err)
	}
	if storedWeight == nil || *storedWeight != "proof-weight-0001" {
		t.Fatalf("stored feed_weight_proof_ref = %v, want proof-weight-0001", storedWeight)
	}

	if len(enq.calls) != 1 {
		t.Fatalf("enqueue calls = %d, want 1", len(enq.calls))
	}
	got := enq.calls[0]
	if got.FeedWeightProofRef != "proof-weight-0001" {
		t.Fatalf("enqueued weight ref = %q, want proof-weight-0001", got.FeedWeightProofRef)
	}
	if got.DistributionProofRef != "proof-distribution-0001" || got.WaterProofRef != "proof-water-0001" {
		t.Fatalf("enqueued refs = %+v, want the distribution and water proofs preserved", got)
	}
}

// A row submitted BEFORE the weight photo existed keeps its two proofs and stays verdictable.
//
// This is the grandfathering half of migration 000151, and it has to REPRODUCE THE DEPLOY ORDER to
// mean anything. NOT VALID exempts the rows already on disk when the constraint is added; it does not
// exempt new inserts (a first attempt at this test inserted a legacy-shaped row after the migration
// and was correctly rejected -- the constraint was right, the test was wrong). So the sequence here
// is the real one: legacy row exists, THEN the migration runs over it.
//
// The load-bearing assertion is the ADD CONSTRAINT itself. Drop the NOT VALID and that statement
// fails on exactly the rows this decision protects -- every item sitting in a verifier's queue on
// deploy day.
func TestGrandfatheredDistributionRowWithoutWeightPhotoStillVerifies(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	// (1) Rewind to before 000151: the constraint did not exist yet.
	if _, err := pool.Exec(ctx, `
ALTER TABLE feed_distribution_completions
  DROP CONSTRAINT feed_distribution_completions_weight_proof_check`); err != nil {
		t.Fatalf("rewind to pre-000151 schema: %v", err)
	}

	// (2) The PREVIOUS binary writes a completion with two proofs and no weight photo.
	var completionID string
	if err := pool.QueryRow(ctx, `
INSERT INTO feed_distribution_completions (
  tenant_id, park_id, shed_id, session_no, target_date, workflow, status,
  distribution_proof_ref, water_proof_ref, idempotency_key
) VALUES ($1::uuid, $2::uuid, $3::uuid, 1, $4::date, $5, 'pending_verification',
  'legacy-distribution-proof', 'legacy-water-photo', 'legacy-key-0001')
RETURNING completion_id::text`,
		fdTenant, fdPark, fdShedA, businessDay(2026, 7, 22).Format("2006-01-02"), domain.WorkflowNormal).
		Scan(&completionID); err != nil {
		t.Fatalf("insert pre-migration row: %v", err)
	}

	// (3) The migration runs over that row. This is the grandfathering assertion: it must SUCCEED.
	// The CHECK text mirrors 000151 exactly -- including the narrowing to 'pending_verification',
	// which is what keeps step (4) below possible.
	if _, err := pool.Exec(ctx, `
ALTER TABLE feed_distribution_completions
  ADD CONSTRAINT feed_distribution_completions_weight_proof_check CHECK (
    status <> 'pending_verification'
    OR (feed_weight_proof_ref IS NOT NULL AND btrim(feed_weight_proof_ref) <> '')
  ) NOT VALID`); err != nil {
		t.Fatalf("000151 must not reject rows already on disk: %v", err)
	}

	// (4) And the verifier can still APPROVE it. This is the assertion that caught the first draft of
	// 000151: approval is an UPDATE, a NOT VALID check IS enforced on UPDATE, and a check that also
	// covered 'completed' made every in-flight queue item permanently un-approvable on deploy day.

	// It is still a normal queue item: a verifier can approve it and it completes.
	applied, err := repo.ApplyVerifiedDistribution(ctx, ports.ApplyDistributionParams{
		TenantID:     fdTenant,
		CompletionID: completionID,
		VerifiedBy:   fdActor,
		TraceID:      "trace-verify-grandfathered",
	})
	if err != nil {
		t.Fatalf("ApplyVerifiedDistribution on grandfathered row: %v", err)
	}
	if !applied {
		t.Fatal("grandfathered row applied = false, want true (it must stay verdictable)")
	}

	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, completionID).Scan(&status); err != nil {
		t.Fatalf("read grandfathered row: %v", err)
	}
	if status != domain.DistributionStatusCompleted {
		t.Fatalf("grandfathered status = %q, want completed", status)
	}
}

// A grandfathered row that is REJECTED and re-submitted must come back carrying a weight photo.
//
// The NOT VALID check is not enforced on the rows already on disk, but it IS enforced when one of them
// is UPDATED. That is deliberate: a re-shoot is new work and is captured under the new rule. This pins
// the boundary so a future change cannot quietly make the old shape writable again.
func TestGrandfatheredRowReSubmitMustCarryWeightPhoto(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	var completionID string
	if err := pool.QueryRow(ctx, `
INSERT INTO feed_distribution_completions (
  tenant_id, park_id, shed_id, session_no, target_date, workflow, status,
  distribution_proof_ref, water_proof_ref, idempotency_key
) VALUES ($1::uuid, $2::uuid, $3::uuid, 1, $4::date, $5, 'rework',
  'legacy-distribution-proof', 'legacy-water-photo', 'legacy-key-0002')
RETURNING completion_id::text`,
		fdTenant, fdPark, fdShedA, businessDay(2026, 7, 22).Format("2006-01-02"), domain.WorkflowNormal).
		Scan(&completionID); err != nil {
		t.Fatalf("insert grandfathered rework row: %v", err)
	}

	// The re-submit carries the weight photo (the app layer guarantees it), so the row returns to
	// pending_verification and the new CHECK is satisfied on the way in.
	resubmit := distributionParams()
	resubmit.IdempotencyKey = "feed-distribution-legacy-resubmit-0001"
	res, err := repo.CompleteDistribution(ctx, resubmit)
	if err != nil {
		t.Fatalf("re-submit of a grandfathered row: %v", err)
	}
	if res.CompletionID != completionID {
		t.Fatalf("re-submit completion id = %s, want the existing %s", res.CompletionID, completionID)
	}

	var storedWeight *string
	if err := pool.QueryRow(ctx, `
SELECT feed_weight_proof_ref FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, completionID).Scan(&storedWeight); err != nil {
		t.Fatalf("read weight proof after re-submit: %v", err)
	}
	if storedWeight == nil || *storedWeight != "proof-weight-0001" {
		t.Fatalf("weight proof after re-submit = %v, want it filled in", storedWeight)
	}
}

// (b) A completion with all three proofs writes a 'pending_verification' row -- NOTHING is completed yet.
func TestCompleteDistributionWritesPendingVerification(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	res, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatalf("CompleteDistribution: %v", err)
	}
	if !res.NewlyPending {
		t.Fatalf("first completion NewlyPending = false, want true")
	}
	if res.Status != domain.DistributionStatusPendingVerification {
		t.Fatalf("status = %q, want pending_verification", res.Status)
	}
	if res.CompletionID == "" {
		t.Fatal("first completion returned an empty completion id")
	}

	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, res.CompletionID).Scan(&status); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if status != domain.DistributionStatusPendingVerification {
		t.Fatalf("canonical status = %q, want pending_verification", status)
	}

	// Nothing is VERIFIED yet, so the direction overlay reads it as not-completed.
	verified, err := repo.ListVerifiedDistributions(ctx, fdTenant, fdPark, businessDay(2026, 7, 22))
	if err != nil {
		t.Fatalf("ListVerifiedDistributions: %v", err)
	}
	if len(verified) != 0 {
		t.Fatalf("verified distributions = %d, want 0 (nothing approved yet)", len(verified))
	}
}

func TestCompleteDistributionPendingConflictReturnsCanonicalProofRefs(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	first, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatalf("first CompleteDistribution: %v", err)
	}
	retry := distributionParams()
	retry.IdempotencyKey = "feed-distribution-key-0001-different"
	retry.FeedWeightProofRef = "proof-feed-weight-photo-0002"
	retry.DistributionProofRef = "proof-distribution-0002"
	retry.WaterProofRef = "proof-water-0002"
	second, err := repo.CompleteDistribution(ctx, retry)
	if err != nil {
		t.Fatalf("retry CompleteDistribution: %v", err)
	}
	if second.NewlyPending {
		t.Fatal("retry NewlyPending = true, want false for already-pending natural-key conflict")
	}
	if second.CompletionID != first.CompletionID || second.RowVersion != first.RowVersion {
		t.Fatalf("retry row=(%s,v%d), want original row=(%s,v%d)", second.CompletionID, second.RowVersion, first.CompletionID, first.RowVersion)
	}
	if second.FeedWeightProofRef != first.FeedWeightProofRef ||
		second.DistributionProofRef != first.DistributionProofRef ||
		second.WaterProofRef != first.WaterProofRef {
		t.Fatalf("retry refs=(%q,%q,%q), want canonical first refs=(%q,%q,%q)",
			second.FeedWeightProofRef, second.DistributionProofRef, second.WaterProofRef,
			first.FeedWeightProofRef, first.DistributionProofRef, first.WaterProofRef)
	}
}

// (c) Verifier approval flips the row to 'completed', shows it in the overlay, emits
// feed.distribution.completed, and a re-delivered verdict completes nobody twice.
func TestApplyVerifiedDistributionCompletesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	pending, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatalf("CompleteDistribution: %v", err)
	}

	applied, err := repo.ApplyVerifiedDistribution(ctx, ports.ApplyDistributionParams{
		TenantID:     fdTenant,
		CompletionID: pending.CompletionID,
		VerifiedBy:   fdActor,
		TraceID:      "trace-verify-1",
	})
	if err != nil {
		t.Fatalf("ApplyVerifiedDistribution: %v", err)
	}
	if !applied {
		t.Fatal("ApplyVerifiedDistribution applied = false, want true")
	}

	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, pending.CompletionID).Scan(&status); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if status != domain.DistributionStatusCompleted {
		t.Fatalf("canonical status = %q, want completed", status)
	}

	// The direction overlay now sees the verified session.
	verified, err := repo.ListVerifiedDistributions(ctx, fdTenant, fdPark, businessDay(2026, 7, 22))
	if err != nil {
		t.Fatalf("ListVerifiedDistributions: %v", err)
	}
	if len(verified) != 1 || verified[0].ShedID != fdShedA || verified[0].SessionNo != 1 || verified[0].Workflow != domain.WorkflowNormal {
		t.Fatalf("verified distributions = %+v, want one (Shed A, session 1, normal)", verified)
	}

	// feed.distribution.completed outbox event, emitted at approval.
	var outboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'feed.distribution.completed' AND aggregate_id = $2::uuid`,
		fdTenant, pending.CompletionID).Scan(&outboxCount); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox rows = %d, want 1", outboxCount)
	}

	// A re-delivered verdict completes nobody twice.
	replay, err := repo.ApplyVerifiedDistribution(ctx, ports.ApplyDistributionParams{
		TenantID:     fdTenant,
		CompletionID: pending.CompletionID,
		VerifiedBy:   fdActor,
		TraceID:      "trace-verify-1-replay",
	})
	if err != nil {
		t.Fatalf("ApplyVerifiedDistribution replay: %v", err)
	}
	if replay {
		t.Fatal("re-delivered verdict applied = true, want false")
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'feed.distribution.completed' AND aggregate_id = $2::uuid`,
		fdTenant, pending.CompletionID).Scan(&outboxCount); err != nil {
		t.Fatalf("read outbox after replay: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox rows after replay = %d, want 1 (no second event)", outboxCount)
	}
}

// (d) A verifier rejection bounces the row to 'rework'; the operator re-submits, which returns it to
// 'pending_verification' with a fresh pending transition (NewlyPending true, row_version bumped).
func TestBounceDistributionForReworkThenResubmit(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	pending, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatalf("CompleteDistribution: %v", err)
	}

	bounced, err := repo.BounceDistributionForRework(ctx, ports.BounceDistributionParams{
		TenantID:     fdTenant,
		CompletionID: pending.CompletionID,
		Reason:       "video was too blurry",
		TraceID:      "trace-rework-1",
	})
	if err != nil {
		t.Fatalf("BounceDistributionForRework: %v", err)
	}
	if !bounced {
		t.Fatal("BounceDistributionForRework bounced = false, want true")
	}

	var status string
	var rowVersion int32
	if err := pool.QueryRow(ctx, `
SELECT status, row_version FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, pending.CompletionID).Scan(&status, &rowVersion); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if status != domain.DistributionStatusRework {
		t.Fatalf("canonical status = %q, want rework", status)
	}

	// Operator re-records and re-submits (a NEW client key for the same shed-session). The row returns
	// to pending_verification with a bumped row_version -- a fresh pending transition that would enqueue
	// a fresh verification item.
	resubmit := distributionParams()
	resubmit.IdempotencyKey = "feed-distribution-key-0002"
	resubmit.DistributionProofRef = "proof-distribution-0002"
	resubmit.WaterProofRef = "proof-water-0002"
	res, err := repo.CompleteDistribution(ctx, resubmit)
	if err != nil {
		t.Fatalf("re-submit CompleteDistribution: %v", err)
	}
	if !res.NewlyPending {
		t.Fatal("re-submit NewlyPending = false, want true")
	}
	if res.Status != domain.DistributionStatusPendingVerification {
		t.Fatalf("re-submit status = %q, want pending_verification", res.Status)
	}
	if res.CompletionID != pending.CompletionID {
		t.Fatalf("re-submit completion id = %s, want existing %s", res.CompletionID, pending.CompletionID)
	}
	if res.RowVersion <= rowVersion {
		t.Fatalf("re-submit row_version = %d, want > %d (bumped)", res.RowVersion, rowVersion)
	}
}
