package postgres

import (
	"context"
	"errors"
	"testing"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// Feed PACKING verification gate -- proofs of the maintainer-2026-07-26 rule (SUPERSEDING the "packing
// stays instant" rule) against the REAL feed_packing_completions schema (the same wiring bootstrap uses,
// so the table's CHECK constraints, the natural/idempotency indexes, and the outbox tenant-parity
// trigger are all part of the assertion surface). Entirely separate from the old instant packing path
// (feed_direction_session_completions / feed.direction.completed) and from the distribution gate, which
// these tests never touch.
//
// The rule: an operator's packing completion no longer completes the session. It records ONE mandatory
// packing video and flips the session to 'pending_verification'; the session is 'completed' (and
// feed.packing.completed is emitted) ONLY when a verifier approves, via ApplyVerifiedPacking. A rejected
// video bounces the session to 'rework' for a re-shoot.
//
// Gated by pgtest.SkipIfNoDocker (inside setupFeedDirectionDB) + GOATOS_RUN_POSTGRES_TESTS, the same
// harness the rest of this package's integration tests use.

// recordingPackingEnqueuer is a test double for the verifier-queue enqueue seam.
type recordingPackingEnqueuer struct {
	calls []feeddirectionapp.FeedPackingVerificationEnqueueRequest
}

func (e *recordingPackingEnqueuer) EnqueueFeedPackingVerification(
	_ context.Context, in feeddirectionapp.FeedPackingVerificationEnqueueRequest,
) error {
	e.calls = append(e.calls, in)
	return nil
}

func packingParams() ports.CompletePackingParams {
	return ports.CompletePackingParams{
		TenantID:        fdTenant,
		ParkID:          fdPark,
		ShedID:          fdShedA,
		TargetDate:      businessDay(2026, 7, 22),
		Workflow:        domain.WorkflowNormal,
		PackingProofRef: "proof-packing-0001",
		CompletedBy:     fdActor,
		IdempotencyKey:  "feed-packing-key-0001",
		ActorID:         fdActor,
		ActorType:       "operator",
		TraceID:         "trace-feed-packing-1",
	}
}

// (a) A completion missing the packing video is rejected at the APP layer before any state changes --
// there is nothing for a verifier to approve.
func TestCompletePackingRequiresVideoAtAppLayer(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	enq := &recordingPackingEnqueuer{}
	svc := feeddirectionapp.NewService(nil, nil).
		WithPackingStore(repo).
		WithPackingVerificationEnqueuer(enq)

	missingVideo := feeddirectionapp.CompletePackingInput{
		TenantID:       fdTenant,
		ParkID:         fdPark,
		ShedID:         fdShedA,
		TargetDate:     businessDay(2026, 7, 22),
		Workflow:       domain.WorkflowNormal,
		CompletedBy:    fdActor,
		IdempotencyKey: "feed-packing-app-key-0001",
		ActorID:        fdActor,
		ActorType:      "operator",
	}
	if _, err := svc.CompletePacking(ctx, missingVideo); !errors.Is(err, ports.ErrPackingProofRequired) {
		t.Fatalf("missing packing video err = %v, want ErrPackingProofRequired", err)
	}

	// The rejected request enqueued nothing and wrote no row.
	if len(enq.calls) != 0 {
		t.Fatalf("enqueue calls = %d, want 0 (rejected before write)", len(enq.calls))
	}
	verified, err := repo.ListVerifiedPacking(ctx, fdTenant, fdPark, businessDay(2026, 7, 22))
	if err != nil {
		t.Fatalf("ListVerifiedPacking: %v", err)
	}
	if len(verified) != 0 {
		t.Fatalf("verified packing = %d, want 0", len(verified))
	}
}

// (b) A completion with the video writes a 'pending_verification' row -- NOTHING is completed yet.
func TestCompletePackingWritesPendingVerification(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	res, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatalf("CompletePacking: %v", err)
	}
	if !res.NewlyPending {
		t.Fatalf("first completion NewlyPending = false, want true")
	}
	if res.Status != domain.PackingStatusPendingVerification {
		t.Fatalf("status = %q, want pending_verification", res.Status)
	}
	if res.CompletionID == "" {
		t.Fatal("first completion returned an empty completion id")
	}

	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, res.CompletionID).Scan(&status); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if status != domain.PackingStatusPendingVerification {
		t.Fatalf("canonical status = %q, want pending_verification", status)
	}

	// Nothing is VERIFIED yet, so the packing overlay reads it as not-completed.
	verified, err := repo.ListVerifiedPacking(ctx, fdTenant, fdPark, businessDay(2026, 7, 22))
	if err != nil {
		t.Fatalf("ListVerifiedPacking: %v", err)
	}
	if len(verified) != 0 {
		t.Fatalf("verified packing = %d, want 0 (nothing approved yet)", len(verified))
	}
}

// (c) Verifier approval flips the row to 'completed', shows it in the overlay, emits
// feed.packing.completed, and a re-delivered verdict completes nobody twice.
func TestApplyVerifiedPackingCompletesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	pending, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatalf("CompletePacking: %v", err)
	}

	applied, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{
		TenantID:     fdTenant,
		CompletionID: pending.CompletionID,
		VerifiedBy:   fdActor,
		TraceID:      "trace-verify-packing-1",
	})
	if err != nil {
		t.Fatalf("ApplyVerifiedPacking: %v", err)
	}
	if !applied {
		t.Fatal("ApplyVerifiedPacking applied = false, want true")
	}

	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, pending.CompletionID).Scan(&status); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if status != domain.PackingStatusCompleted {
		t.Fatalf("canonical status = %q, want completed", status)
	}

	// The packing overlay now sees the verified session.
	verified, err := repo.ListVerifiedPacking(ctx, fdTenant, fdPark, businessDay(2026, 7, 22))
	if err != nil {
		t.Fatalf("ListVerifiedPacking: %v", err)
	}
	if len(verified) != 1 || verified[0].ShedID != fdShedA || verified[0].Workflow != domain.WorkflowNormal {
		t.Fatalf("verified packing = %+v, want one (Shed A, session 1, normal)", verified)
	}

	// feed.packing.completed outbox event, emitted at approval.
	var outboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'feed.packing.completed' AND aggregate_id = $2::uuid`,
		fdTenant, pending.CompletionID).Scan(&outboxCount); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox rows = %d, want 1", outboxCount)
	}

	// A re-delivered verdict completes nobody twice.
	replay, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{
		TenantID:     fdTenant,
		CompletionID: pending.CompletionID,
		VerifiedBy:   fdActor,
		TraceID:      "trace-verify-packing-1-replay",
	})
	if err != nil {
		t.Fatalf("ApplyVerifiedPacking replay: %v", err)
	}
	if replay {
		t.Fatal("re-delivered verdict applied = true, want false")
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'feed.packing.completed' AND aggregate_id = $2::uuid`,
		fdTenant, pending.CompletionID).Scan(&outboxCount); err != nil {
		t.Fatalf("read outbox after replay: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox rows after replay = %d, want 1 (no second event)", outboxCount)
	}
}

// (d) A verifier rejection bounces the row to 'rework'; the operator re-submits, which returns it to
// 'pending_verification' with a fresh pending transition (NewlyPending true, row_version bumped).
func TestBouncePackingForReworkThenResubmit(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	pending, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatalf("CompletePacking: %v", err)
	}

	bounced, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{
		TenantID:     fdTenant,
		CompletionID: pending.CompletionID,
		Reason:       "video was too blurry",
		TraceID:      "trace-rework-packing-1",
	})
	if err != nil {
		t.Fatalf("BouncePackingForRework: %v", err)
	}
	if !bounced {
		t.Fatal("BouncePackingForRework bounced = false, want true")
	}

	var status string
	var rowVersion int32
	if err := pool.QueryRow(ctx, `
SELECT status, row_version FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, pending.CompletionID).Scan(&status, &rowVersion); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if status != domain.PackingStatusRework {
		t.Fatalf("canonical status = %q, want rework", status)
	}

	// Operator re-records and re-submits (a NEW client key for the same shed-session). The row returns to
	// pending_verification with a bumped row_version -- a fresh pending transition that would enqueue a
	// fresh verification item.
	resubmit := packingParams()
	resubmit.IdempotencyKey = "feed-packing-key-0002"
	resubmit.PackingProofRef = "proof-packing-0002"
	res, err := repo.CompletePacking(ctx, resubmit)
	if err != nil {
		t.Fatalf("re-submit CompletePacking: %v", err)
	}
	if !res.NewlyPending {
		t.Fatal("re-submit NewlyPending = false, want true")
	}
	if res.Status != domain.PackingStatusPendingVerification {
		t.Fatalf("re-submit status = %q, want pending_verification", res.Status)
	}
	if res.CompletionID != pending.CompletionID {
		t.Fatalf("re-submit completion id = %s, want existing %s", res.CompletionID, pending.CompletionID)
	}
	if res.RowVersion <= rowVersion {
		t.Fatalf("re-submit row_version = %d, want > %d (bumped)", res.RowVersion, rowVersion)
	}
}

// ---------------------------------------------------------------------------
// A pen-day accepts exactly ONE video (maintainer decision 2026-08-10)
// ---------------------------------------------------------------------------

// TestCompletePackingRejectsASecondDifferentVideoForTheSamePenDay is the SILENT-DATA-LOSS regression.
//
// The pen-day merge left the phone able to hold TWO legacy queued packing rows for one pen -- Morning
// and Evening, queued before the upgrade, each carrying its OWN video and its OWN idempotency key.
// Both drain to the same pen-day row. The second used to take the "already awaiting verification"
// branch and return SUCCESS: its video was never stored, no verification item was ever raised for it,
// and the outbox row was marked synced. The operator was told their recording was accepted while
// nothing recorded it.
//
// That is the accepted-and-ignored failure the strict `session_no` rejection on the route exists to
// prevent, reappearing one layer above the API -- so it must fail LOUDLY.
func TestCompletePackingRejectsASecondDifferentVideoForTheSamePenDay(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	// The legacy MORNING row drains first and is recorded.
	morning := packingParams()
	morning.PackingProofRef = "proof-legacy-morning"
	morning.IdempotencyKey = "feed-packing-legacy-morning"
	first, err := repo.CompletePacking(ctx, morning)
	if err != nil {
		t.Fatalf("CompletePacking(morning): %v", err)
	}
	if !first.NewlyPending {
		t.Fatalf("the morning submission must be a fresh pending transition, got NewlyPending=false")
	}

	// The legacy EVENING row: same pen-day, DIFFERENT video, DIFFERENT idempotency key -- so the
	// idempotency reservation proceeds and the natural-key conflict is what decides the outcome.
	evening := packingParams()
	evening.PackingProofRef = "proof-legacy-evening"
	evening.IdempotencyKey = "feed-packing-legacy-evening"
	if _, err := repo.CompletePacking(ctx, evening); !errors.Is(err, ports.ErrPackingAlreadyRecorded) {
		t.Fatalf("second legacy video returned err=%v, want ErrPackingAlreadyRecorded — "+
			"answering success here discards the operator's recording silently", err)
	}

	// The stored row still holds the FIRST video. The second must not have overwritten it either:
	// silently replacing the morning clip would lose the video a verifier may already be reviewing.
	var storedProof string
	if err := pool.QueryRow(ctx, `
SELECT packing_proof_ref FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, first.CompletionID).Scan(&storedProof); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if storedProof != "proof-legacy-morning" {
		t.Fatalf("stored packing_proof_ref = %q, want the first video to be untouched", storedProof)
	}

	// Exactly one pen-day row exists — the conflict must not have inserted a second.
	var rows int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_packing_completions WHERE tenant_id = $1::uuid`, fdTenant).Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("feed_packing_completions rows = %d, want exactly 1 pen-day row", rows)
	}
}

// A genuine RE-SEND must still be a quiet no-op. The conflict is about a DIFFERENT video, never about
// a client retrying the same one under a fresh key -- if this failed, an ordinary network retry would
// surface to the operator as an error and they would re-film work that was already recorded.
func TestCompletePackingAcceptsTheSameVideoResentUnderANewKey(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	first, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatalf("CompletePacking: %v", err)
	}

	resend := packingParams() // same PackingProofRef
	resend.IdempotencyKey = "feed-packing-key-retry"
	again, err := repo.CompletePacking(ctx, resend)
	if err != nil {
		t.Fatalf("re-sending the SAME video under a new key must be a no-op, got: %v", err)
	}
	if again.CompletionID != first.CompletionID {
		t.Fatalf("completion id %s -> %s, want the same pen-day row", first.CompletionID, again.CompletionID)
	}
	if again.NewlyPending {
		t.Fatal("a re-send must not count as a fresh pending transition, or it enqueues a second verification item")
	}
}

// The same protection applies once the pen-day is COMPLETED. A verified pen accepts no new video
// either -- it is terminal until the afternoon correction or a verifier rejection reopens it.
func TestCompletePackingRejectsADifferentVideoAgainstACompletedPenDay(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	pending, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatalf("CompletePacking: %v", err)
	}
	if _, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{
		TenantID: fdTenant, CompletionID: pending.CompletionID, VerifiedBy: fdActor,
		TraceID: "trace-verify-packing-complete",
	}); err != nil {
		t.Fatalf("ApplyVerifiedPacking: %v", err)
	}

	late := packingParams()
	late.PackingProofRef = "proof-late-different"
	late.IdempotencyKey = "feed-packing-late"
	if _, err := repo.CompletePacking(ctx, late); !errors.Is(err, ports.ErrPackingAlreadyRecorded) {
		t.Fatalf("a different video against a COMPLETED pen-day returned err=%v, want ErrPackingAlreadyRecorded", err)
	}
}
