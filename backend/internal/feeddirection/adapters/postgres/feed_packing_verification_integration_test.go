package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
		TenantID: fdTenant,
		ParkID:   fdPark,
		ShedID:   fdShedA,
		// A REAL session. The table's CHECK is session_no >= 1 again (migration 000150), so a zero
		// here is a constraint violation rather than a "whole day" sentinel.
		SessionNo:       1,
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
		SessionNo:      1,
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

	// A completion that does not say WHICH bag it proves is rejected on the same terms. 0 is not "the
	// whole day" -- that was the 2026-08-10 grain and it is reverted; accepting it would write a row
	// no worklist line matches, leaving the operator's bag still showing as owed.
	noSession := missingVideo
	noSession.SessionNo = 0
	noSession.PackingProofRef = "proof-packing-no-session"
	noSession.IdempotencyKey = "feed-packing-app-key-no-session"
	if _, err := svc.CompletePacking(ctx, noSession); !errors.Is(err, ports.ErrInvalidSession) {
		t.Fatalf("session 0 err = %v, want ErrInvalidSession", err)
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
// A packing line accepts exactly ONE video
// ---------------------------------------------------------------------------

// TestCompletePackingRejectsASecondDifferentVideoForTheSameShedSession is the SILENT-DATA-LOSS
// regression.
//
// A line that already holds a video used to take the "already awaiting verification" branch and
// return SUCCESS for a second, DIFFERENT one: that video was never stored, no verification item was
// ever raised for it, and the outbox row was marked synced. The operator was told their recording was
// accepted while nothing recorded it.
//
// The morning and evening bags no longer collide -- they key different rows again since 2026-08-11 --
// but this branch is still reachable: a re-send after a rework the server never recorded, a duplicated
// queue drain, or any client that re-films and re-submits against the same line. Silent loss of work
// an operator physically did must fail loudly whatever produced the second clip.
func TestCompletePackingRejectsASecondDifferentVideoForTheSameShedSession(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	// The first video for session 1 is recorded.
	firstSubmit := packingParams()
	firstSubmit.PackingProofRef = "proof-session-1-first"
	firstSubmit.IdempotencyKey = "feed-packing-session-1-first"
	first, err := repo.CompletePacking(ctx, firstSubmit)
	if err != nil {
		t.Fatalf("CompletePacking(first): %v", err)
	}
	if !first.NewlyPending {
		t.Fatalf("the first submission must be a fresh pending transition, got NewlyPending=false")
	}

	// A second, DIFFERENT video for the SAME session, under a DIFFERENT idempotency key -- so the
	// idempotency reservation proceeds and the natural-key conflict is what decides the outcome.
	second := packingParams()
	second.PackingProofRef = "proof-session-1-second"
	second.IdempotencyKey = "feed-packing-session-1-second"
	if _, err := repo.CompletePacking(ctx, second); !errors.Is(err, ports.ErrPackingAlreadyRecorded) {
		t.Fatalf("second differing video returned err=%v, want ErrPackingAlreadyRecorded — "+
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
	if storedProof != "proof-session-1-first" {
		t.Fatalf("stored packing_proof_ref = %q, want the first video to be untouched", storedProof)
	}

	// Exactly one row exists — the conflict must not have inserted a second.
	var rows int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_packing_completions WHERE tenant_id = $1::uuid`, fdTenant).Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("feed_packing_completions rows = %d, want exactly 1 line", rows)
	}

	// AND THE SIBLING BAG IS UNAFFECTED. Session 2 is a different line, so its own video is accepted
	// on its own row -- the conflict above must be about ONE bag, never about the pen. Between
	// 2026-08-10 and 2026-08-11 this submission was the one that came back 200 with its clip discarded.
	evening := packingParams()
	evening.SessionNo = 2
	evening.PackingProofRef = "proof-session-2"
	evening.IdempotencyKey = "feed-packing-session-2"
	eveningRes, err := repo.CompletePacking(ctx, evening)
	if err != nil {
		t.Fatalf("the pen's OTHER session must be recordable on its own row, got: %v", err)
	}
	if eveningRes.CompletionID == first.CompletionID {
		t.Fatalf("session 2 reused session 1's row (%s) — one video would prove both bags", first.CompletionID)
	}
	if !eveningRes.NewlyPending {
		t.Fatal("session 2 must be a fresh pending transition of its own")
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
		t.Fatalf("completion id %s -> %s, want the same line's row", first.CompletionID, again.CompletionID)
	}
	if again.NewlyPending {
		t.Fatal("a re-send must not count as a fresh pending transition, or it enqueues a second verification item")
	}
}

// The same protection applies once the line is COMPLETED. A verified bag accepts no new video either
// -- it is terminal until the afternoon correction or a verifier rejection reopens it.
func TestCompletePackingRejectsADifferentVideoAgainstACompletedShedSession(t *testing.T) {
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
		t.Fatalf("a different video against a COMPLETED line returned err=%v, want ErrPackingAlreadyRecorded", err)
	}
}

// ---------------------------------------------------------------------------
// The afternoon correction's reopen, against the real schema
// ---------------------------------------------------------------------------

// TestReopenPackingWithdrawsPendingItemsAndKeepsCastVerdicts covers the branch the kernel story
// cannot reach.
//
// In that story both clips were already APPROVED by the time the 14:00 correction landed, so it proves
// the "a cast verdict stays as history" half. This proves the other half against Postgres: an item
// still PENDING when the correction lands must LEAVE the verifier's queue, because it points at a
// video of the old quantity and approving it would flip the bag straight back to completed behind the
// operator who is at that moment repacking it.
//
// It also pins the three things the reopen must NOT do, each of which is a way to get this wrong:
//
//   - it must reopen EVERY session of a named pen, because head count scales both rations;
//   - it must not touch a pen the correction did not name;
//   - it must not touch a row already in 'rework', whose reason a verifier may have written.
func TestReopenPackingWithdrawsPendingItemsAndKeepsCastVerdicts(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	// Two sessions of the pen under correction, plus a sibling shed the correction never names.
	submit := func(shedID string, sessionNo int32, proof, key string) ports.CompletePackingResult {
		t.Helper()
		p := packingParams()
		p.ShedID = shedID
		p.SessionNo = sessionNo
		p.PackingProofRef = proof
		p.IdempotencyKey = key
		res, err := repo.CompletePacking(ctx, p)
		if err != nil {
			t.Fatalf("CompletePacking(%s session %d): %v", shedID, sessionNo, err)
		}
		return res
	}

	morning := submit(fdShedA, 1, "proof-reopen-morning", "feed-packing-reopen-morning")
	evening := submit(fdShedA, 2, "proof-reopen-evening", "feed-packing-reopen-evening")
	sibling := submit(fdShedB, 1, "proof-reopen-sibling", "feed-packing-reopen-sibling")

	// The MORNING clip has already been approved; the EVENING clip is still awaiting a verdict. That
	// mix is the point: one row is 'completed', one is 'pending_verification', and the reopen must
	// take both.
	if _, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{
		TenantID: fdTenant, CompletionID: morning.CompletionID, VerifiedBy: fdActor,
		TraceID: "trace-reopen-verify-morning",
	}); err != nil {
		t.Fatalf("ApplyVerifiedPacking(morning): %v", err)
	}

	// One verification item per bag, in the state its clip is actually in. Seeded directly because the
	// enqueue seam lives in the app layer; what is under test here is the SQL that retires them.
	seedItem := func(itemID, completionID, status string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO verification_items (
  item_id, tenant_id, vertical, module, category, source_module, source_ref_type, source_ref_id,
  media_refs, status, park_id, shed_id, captured_at, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, 'feed', 'feed', 'feed_packing', 'feed', 'feed_packing_completion', $3::uuid,
  '["proof"]'::jsonb, $4, $5::uuid, $6::uuid, now(), $7
)`, itemID, fdTenant, completionID, status, fdPark, fdShedA, "reopen-item:"+itemID); err != nil {
			t.Fatalf("seed verification item %s: %v", itemID, err)
		}
	}
	const (
		itemMorning = "fd000000-0000-4000-8000-0000000091a1"
		itemEvening = "fd000000-0000-4000-8000-0000000091a2"
		itemSibling = "fd000000-0000-4000-8000-0000000091a3"
	)
	seedItem(itemMorning, morning.CompletionID, "approved")
	seedItem(itemEvening, evening.CompletionID, "pending")
	seedItem(itemSibling, sibling.CompletionID, "pending")

	res, err := repo.ReopenPackingForFeedChange(ctx, ports.ReopenPackingParams{
		TenantID:   fdTenant,
		ParkID:     fdPark,
		TargetDate: businessDay(2026, 7, 22),
		Workflow:   domain.WorkflowNormal,
		// The pen is named WITHOUT a session, and that is the contract: every session of it moves.
		Pens:    []domain.PenKey{{ShedID: fdShedA, PartitionKey: domain.PartitionMatchKey("")}},
		Reason:  "Animals moved in or out of this pen, so the feed quantities changed.",
		TraceID: "trace-reopen",
	})
	if err != nil {
		t.Fatalf("ReopenPackingForFeedChange: %v", err)
	}

	// BOTH of the pen's bags come back -- the completed one and the pending one.
	if len(res.ReopenedCompletionIDs) != 2 {
		t.Fatalf("reopened %d rows (%v), want 2 — every session of a named pen must move, because head "+
			"count scales both rations", len(res.ReopenedCompletionIDs), res.ReopenedCompletionIDs)
	}
	statusOf := func(completionID string) (string, string) {
		t.Helper()
		var status, reason string
		if err := pool.QueryRow(ctx, `
SELECT status, coalesce(rework_reason, '') FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, completionID).Scan(&status, &reason); err != nil {
			t.Fatalf("read completion %s: %v", completionID, err)
		}
		return status, reason
	}
	for _, tc := range []struct{ name, id string }{{"morning", morning.CompletionID}, {"evening", evening.CompletionID}} {
		status, reason := statusOf(tc.id)
		if status != domain.PackingStatusRework {
			t.Errorf("%s status = %q, want rework", tc.name, status)
		}
		if reason == "" {
			t.Errorf("%s carries no rework reason — the operator cannot tell a reopened bag from an unpacked one", tc.name)
		}
	}

	// The sibling shed the correction never named is untouched, status AND reason.
	if status, reason := statusOf(sibling.CompletionID); status != domain.PackingStatusPendingVerification || reason != "" {
		t.Errorf("sibling shed status = %q reason = %q, want pending_verification with no reason — "+
			"making an operator refilm work that did not change is the cost this narrowing exists to avoid",
			status, reason)
	}

	// The verifier's queue: the PENDING item is withdrawn, the CAST verdict is preserved, and the
	// untouched shed's item stays in the queue.
	itemStatus := func(itemID string) string {
		t.Helper()
		var status string
		if err := pool.QueryRow(ctx, `
SELECT status FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid`,
			fdTenant, itemID).Scan(&status); err != nil {
			t.Fatalf("read item %s: %v", itemID, err)
		}
		return status
	}
	if got := itemStatus(itemEvening); got != "withdrawn" {
		t.Errorf("pending item status = %q, want withdrawn — left in the queue, approving it would flip "+
			"the bag back to completed behind the operator repacking it", got)
	}
	if got := itemStatus(itemMorning); got != "approved" {
		t.Errorf("already-approved item status = %q, want it left as approved — a verifier really watched "+
			"and passed that clip, and rewriting the verdict erases that", got)
	}
	if got := itemStatus(itemSibling); got != "pending" {
		t.Errorf("untouched shed's item status = %q, want pending", got)
	}
	if res.WithdrawnItemCount != 1 {
		t.Errorf("WithdrawnItemCount = %d, want 1", res.WithdrawnItemCount)
	}

	// IDEMPOTENT: running the same correction again moves nothing, because both rows are now in
	// 'rework' and that state is excluded — re-running must not bump row_version or overwrite a
	// verifier's real rejection reason with the correction's sentence.
	again, err := repo.ReopenPackingForFeedChange(ctx, ports.ReopenPackingParams{
		TenantID: fdTenant, ParkID: fdPark, TargetDate: businessDay(2026, 7, 22),
		Workflow: domain.WorkflowNormal,
		Pens:     []domain.PenKey{{ShedID: fdShedA, PartitionKey: domain.PartitionMatchKey("")}},
		Reason:   "a second sentence that must not land",
		TraceID:  "trace-reopen-again",
	})
	if err != nil {
		t.Fatalf("ReopenPackingForFeedChange (second run): %v", err)
	}
	if len(again.ReopenedCompletionIDs) != 0 {
		t.Errorf("second run reopened %v, want nothing — a row already in rework is already back with the operator",
			again.ReopenedCompletionIDs)
	}
}

// THE VERIFIER RECORDS WHAT WAS PACKED, PER FEED ITEM (maintainer decision 2026-08-21) -- proofs of
// the readings store and the leadership-only intended-vs-entered variance against the REAL schema.
//
// The rule under test, end to end: her readings UPSERT per (completion, feed_item_key); a replayed
// approve replaces rather than duplicates; a completion this tenant does not have is refused; an
// implausible weight is refused; and the execution analytics variance lists every measured bag,
// including exact matches, because a match is independent confirmation.
func TestPackingVerifiedQuantitiesUpsertAndVariance(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	// The FROZEN sheet the variance compares against: one pen-session with two items. Same tenant,
	// park, shed, undivided pen and session the packing completion below is keyed by.
	issuedAt := time.Date(2026, 7, 21, 9, 0, 0, 0, biztime.DefaultLocation())
	conc := "2.000"
	hay := "1.000"
	cells := []domain.StoredCell{
		{
			ParkID: fdPark, ParkLabel: "CBE", ShedID: fdShedA, ShedLabel: "Castro",
			PartitionLabel: "", ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", QuantityKg: &conc,
			SessionTotalKg: "3.000", RowSeq: 0, ItemSeq: 0,
		},
		{
			ParkID: fdPark, ParkLabel: "CBE", ShedID: fdShedA, ShedLabel: "Castro",
			PartitionLabel: "", ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: "Hay", FeedItemKey: "hay", QuantityKg: &hay,
			SessionTotalKg: "3.000", RowSeq: 0, ItemSeq: 1,
		},
	}
	if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
		TenantID: fdTenant, ParkID: fdPark, FeedDay: "2026-07-22", Workflow: domain.WorkflowNormal,
		IssuedAt: issuedAt, Fingerprint: "fp-variance",
		IdempotencyKey: "issue:variance:1", GeneratedBy: "test", Cells: cells,
	}); err != nil {
		t.Fatalf("PersistIssue: %v", err)
	}

	pending, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatalf("CompletePacking: %v", err)
	}

	// A completion that does not exist for this tenant is refused by name.
	if err := repo.RecordPackingVerifiedQuantities(ctx, ports.RecordPackingVerifiedQuantitiesParams{
		TenantID: fdTenant, CompletionID: "fd000000-0000-4000-8000-00000000dead",
		Entries:    []ports.PackingVerifiedQuantity{{FeedItemKey: "concentrate", FeedItemLabel: "Concentrate", EnteredKg: 1}},
		RecordedBy: fdActor,
	}); !errors.Is(err, ports.ErrPackingCompletionNotFound) {
		t.Fatalf("unknown completion: want ErrPackingCompletionNotFound, got %v", err)
	}
	// A fat-fingered 125000 must be refused, never recorded.
	if err := repo.RecordPackingVerifiedQuantities(ctx, ports.RecordPackingVerifiedQuantitiesParams{
		TenantID: fdTenant, CompletionID: pending.CompletionID,
		Entries:    []ports.PackingVerifiedQuantity{{FeedItemKey: "concentrate", FeedItemLabel: "Concentrate", EnteredKg: 125000}},
		RecordedBy: fdActor,
	}); !errors.Is(err, ports.ErrPackingQuantityOutOfRange) {
		t.Fatalf("out-of-range: want ErrPackingQuantityOutOfRange, got %v", err)
	}
	if recorded, err := repo.PackingVerifiedQuantitiesRecorded(ctx, fdTenant, pending.CompletionID); err != nil || recorded {
		t.Fatalf("before any write: recorded=%v err=%v, want false/nil", recorded, err)
	}

	// First reading: concentrate sits EXACTLY 0.200 kg over the sheet's 2.000 and hay is short by
	// half. Both rows must appear now: the bag table carries the raw difference, not a tolerance
	// filter. ZERO would also be a real reading -- the store must accept the full 0..10000 range.
	first := ports.RecordPackingVerifiedQuantitiesParams{
		TenantID: fdTenant, CompletionID: pending.CompletionID,
		Entries: []ports.PackingVerifiedQuantity{
			{FeedItemKey: "concentrate", FeedItemLabel: "Concentrate", EnteredKg: 2.2},
			{FeedItemKey: "hay", FeedItemLabel: "Hay", EnteredKg: 0.5},
		},
		RecordedBy: fdActor, IdempotencyKey: "verdict-key-1:measurement", TraceID: "trace-q-1",
	}
	if err := repo.RecordPackingVerifiedQuantities(ctx, first); err != nil {
		t.Fatalf("RecordPackingVerifiedQuantities: %v", err)
	}
	if recorded, err := repo.PackingVerifiedQuantitiesRecorded(ctx, fdTenant, pending.CompletionID); err != nil || !recorded {
		t.Fatalf("after write: recorded=%v err=%v, want true/nil", recorded, err)
	}
	// The verdict lands after the readings (the approve carries both), and variance counts ONLY
	// completed rows -- a pending or reworked completion's readings are not yet a finding.
	if _, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{
		TenantID: fdTenant, CompletionID: pending.CompletionID, VerifiedBy: fdActor, TraceID: "trace-q-2",
	}); err != nil {
		t.Fatalf("ApplyVerifiedPacking: %v", err)
	}

	window := domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 22, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 22, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
	exec, err := repo.ExecutionAnalytics(ctx, fdTenant, window)
	if err != nil {
		t.Fatalf("ExecutionAnalytics: %v", err)
	}
	if len(exec.PackingVariance) != 2 {
		t.Fatalf("variance rows = %+v, want every measured bag", exec.PackingVariance)
	}
	row := exec.PackingVariance[0]
	if row.FeedItemKey != "hay" || row.FeedItemLabel != "Hay" {
		t.Errorf("variance item = %q/%q, want hay/Hay", row.FeedItemKey, row.FeedItemLabel)
	}
	if row.PlannedKg != "1.000" {
		t.Errorf("planned = %q, want the frozen sheet's 1.000", row.PlannedKg)
	}
	if row.VerifiedKg != "0.500" {
		t.Errorf("verified = %q, want her 0.500 reading", row.VerifiedKg)
	}
	if row.VarianceKg != "-0.500" {
		t.Errorf("variance = %q, want -0.500 (short by half)", row.VarianceKg)
	}
	if row.FeedDay != "2026-07-22" || row.SessionNo != 1 || row.SessionLabel != "Morning" {
		t.Errorf("row identity = %+v, want the pen-session the reading was taken on", row)
	}
	// Labels come from the completion's own canonical locations rows, NOT the sheet's copies, so a
	// reading whose planned row is absent ("not on sheet") still names its farm and shed.
	if row.ParkLabel != "CPT" || row.ShedLabel != "Shed A" || row.OperationalLocationDisplay != "Shed A" {
		t.Errorf("row labels = park %q shed %q display %q, want the completion's canonical location names with the oploc display", row.ParkLabel, row.ShedLabel, row.OperationalLocationDisplay)
	}
	if got := exec.PackingVariance[1]; got.FeedItemKey != "concentrate" || got.VarianceKg != "0.200" {
		t.Errorf("second variance row = %+v, want concentrate on the tolerance boundary", got)
	}

	// REPLACE semantics on a replayed/re-cast approve: the new set stands, keys it no longer names
	// are removed, and the variance follows the readings that stand.
	second := first
	second.Entries = []ports.PackingVerifiedQuantity{
		{FeedItemKey: "concentrate", FeedItemLabel: "Concentrate", EnteredKg: 2.5},
		{FeedItemKey: "hay", FeedItemLabel: "Hay", EnteredKg: 1},
	}
	if err := repo.RecordPackingVerifiedQuantities(ctx, second); err != nil {
		t.Fatalf("RecordPackingVerifiedQuantities (replace): %v", err)
	}
	exec, err = repo.ExecutionAnalytics(ctx, fdTenant, window)
	if err != nil {
		t.Fatalf("ExecutionAnalytics (after replace): %v", err)
	}
	if len(exec.PackingVariance) != 2 {
		t.Fatalf("variance after replace = %+v, want every measured bag", exec.PackingVariance)
	}
	if got := exec.PackingVariance[0]; got.FeedItemKey != "concentrate" || got.VarianceKg != "0.500" {
		t.Errorf("variance after replace = %+v, want concentrate over by 0.500", got)
	}
	if got := exec.PackingVariance[1]; got.FeedItemKey != "hay" || got.VarianceKg != "0.000" {
		t.Errorf("variance after replace match = %+v, want hay exact match", got)
	}
}
