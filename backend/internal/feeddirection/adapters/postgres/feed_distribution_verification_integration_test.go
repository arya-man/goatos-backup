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
	// FEED SOP (2026-09-16): a missing capture is refused by the pinned card, naming the slot.
	if _, err := svc.CompleteDistribution(ctx, missingWeightPhoto); !errors.Is(err, ports.ErrSOPProofSlotInvalid) {
		t.Fatalf("missing feed weight photo err = %v, want ErrSOPProofSlotInvalid", err)
	} else if key, _, _ := feeddirectionapp.SOPProofSlotError(err); key != domain.SlotFeedWeightPhoto {
		t.Fatalf("missing feed weight photo must name %s, got %v", domain.SlotFeedWeightPhoto, err)
	}

	missingWater := base
	missingWater.FeedWeightProofRef = "proof-feed-weight-photo-0001"
	missingWater.DistributionProofRef = "proof-distribution-0001"
	missingWater.WaterProofRef = ""
	if _, err := svc.CompleteDistribution(ctx, missingWater); !errors.Is(err, ports.ErrSOPProofSlotInvalid) {
		t.Fatalf("missing water video err = %v, want ErrSOPProofSlotInvalid", err)
	}

	missingVideo := base
	missingVideo.FeedWeightProofRef = "proof-feed-weight-photo-0001"
	missingVideo.DistributionProofRef = ""
	missingVideo.WaterProofRef = "proof-water-0001"
	if _, err := svc.CompleteDistribution(ctx, missingVideo); !errors.Is(err, ports.ErrSOPProofSlotInvalid) {
		t.Fatalf("missing distribution video err = %v, want ErrSOPProofSlotInvalid", err)
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

// TestCompleteDistributionSecondOperatorOnAPartNPenReplays pins the partition-key defect found on
// the QA clone 2026-09-16 (see partition_key.go): the natural-key conflict re-read bound
// domain.PartitionMatchKey ("part_1") against the generated column ("part 1"), so a second
// operator's submit for ANY pen whose label carries a separator -- every "Part N" pen -- answered
// 500 "read existing distribution completion: no rows" instead of replaying the first write. The
// phone treats a 500 as retryable and would retry it forever. Castro "1"/"2" never hit it, which
// is why the existing conflict test (a blank partition) stayed green.
func TestCompleteDistributionSecondOperatorOnAPartNPenReplays(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	seedFeedDirectionPartition(t, ctx, pool, fdShedA, "Part 1")

	first := distributionParams()
	first.PartitionLabel = "Part 1"
	first.SOPProofs = map[string]string{
		"feed_distribution_feed_weight_photo": "proof-feed-weight-photo-0001",
		"feed_distribution_video":             "proof-distribution-0001",
		"feed_distribution_water_video":       "proof-water-0001",
	}
	one, err := repo.CompleteDistribution(ctx, first)
	if err != nil {
		t.Fatalf("first CompleteDistribution: %v", err)
	}
	second := first
	second.IdempotencyKey = "feed-distribution-key-teammate"
	second.CompletedBy = "40000000-0000-4000-8000-000000000002"
	second.ActorID = second.CompletedBy
	two, err := repo.CompleteDistribution(ctx, second)
	if err != nil {
		t.Fatalf("second operator's CompleteDistribution on a Part-N pen: %v (want the first write replayed)", err)
	}
	if two.NewlyPending || two.CompletionID != one.CompletionID {
		t.Fatalf("second=(id %s, newlyPending %v), want the first row %s replayed", two.CompletionID, two.NewlyPending, one.CompletionID)
	}
}
