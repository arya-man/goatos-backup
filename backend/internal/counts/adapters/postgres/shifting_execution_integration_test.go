package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Shifting EXECUTION -- cancellation, state-machine gating, and the operator's pending-execution
// queue, all against the real schema.
//
// The completion/relocation proofs live in approval_relocate_integration_test.go, wired to
// identity's actual Postgres repository.

func cancelShifting(
	repo *Repository, ctx context.Context, key, shiftingEventID, reason string,
) (domain.ShiftingExecutionResult, bool, error) {
	return repo.CancelShiftingEvent(ctx, domain.ShiftingCancellationCommand{
		TenantID:         countsTenant,
		ShiftingEventID:  shiftingEventID,
		CanceledByUserID: countsOperator,
		CanceledAt:       time.Now().In(biztime.DefaultLocation()),
		Reason:           reason,
		IdempotencyKey:   "cancel-" + key,
		// The fingerprint covers the cancellation's MEANING, which includes the reason -- exactly as
		// the HTTP handler derives it. Deriving it from the key alone would make a same-key replay
		// carrying a DIFFERENT reason look identical, and the conflict check below would pass
		// vacuously.
		RequestFingerprint: "cancel-fp-" + key + ":" + reason,
	})
}

// authorizedShifting drives raise -> approve and returns an AUTHORIZED movement with its animals
// still standing in the source shed.
func authorizedShifting(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, key string, goatIDs []string,
) string {
	t.Helper()
	for _, goatID := range goatIDs {
		seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	}
	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, key, goatIDs)
	if _, _, err := approveShifting(repo, ctx, key, approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting %s: %v", key, err)
	}
	return shiftingEventID
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

// TestCancelShiftingMovesNothing is the whole point of the endpoint: an authorized movement nobody
// is going to walk gets retired, and NO animal changes shed.
func TestCancelShiftingMovesNothing(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d001"
	shiftingEventID := authorizedShifting(t, ctx, pool, repo, "cancel-1", []string{goatA})

	result, replayed, err := cancelShifting(repo, ctx, "cancel-1", shiftingEventID, "shed flooded, movement abandoned")
	if err != nil {
		t.Fatalf("cancel shifting: %v", err)
	}
	if replayed {
		t.Fatalf("first cancellation reported replayed=true, want a fresh cancellation")
	}
	if result.EventStatus != domain.ShiftingEventStatusCanceled {
		t.Fatalf("event_status=%q, want %q", result.EventStatus, domain.ShiftingEventStatusCanceled)
	}
	if len(result.MovedGoatIDs) != 0 {
		t.Fatalf("cancellation reported %d moved animals, want 0", len(result.MovedGoatIDs))
	}

	// THE ASSERTION: the animal never moved.
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s after cancellation, want it untouched at %s", got, countsShedA)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("identity location events after cancellation=%d, want 0", got)
	}

	// The reason, the actor, and the time are all on the row -- an abandoned movement that vanished
	// with no explanation would be indistinguishable from one that was executed.
	var (
		status       string
		canceledAt   *time.Time
		canceledBy   *string
		cancelReason *string
		authState    string
	)
	if err := pool.QueryRow(ctx, `
SELECT event_status, canceled_at, canceled_by::text, cancel_reason, authorization_state
FROM shifting_events WHERE shifting_event_id = $1::uuid`,
		shiftingEventID).Scan(&status, &canceledAt, &canceledBy, &cancelReason, &authState); err != nil {
		t.Fatalf("read shifting event: %v", err)
	}
	if status != domain.ShiftingEventStatusCanceled {
		t.Fatalf("event_status=%q, want canceled", status)
	}
	if canceledAt == nil || canceledBy == nil || cancelReason == nil {
		t.Fatalf("cancellation stamp incomplete: at=%v by=%v reason=%v -- "+
			"shifting_events_canceled_shape_check should have made this unrepresentable",
			canceledAt, canceledBy, cancelReason)
	}
	if *cancelReason != "shed flooded, movement abandoned" {
		t.Fatalf("cancel_reason=%q, want the supplied reason", *cancelReason)
	}
	// authorization_state is deliberately LEFT as 'authorized': the movement genuinely was
	// authorized, and that is history a cancellation does not revise. It also keeps approval's
	// `authorization_state = 'pending'` guard permanently closed against this row.
	if authState != "authorized" {
		t.Fatalf("authorization_state=%q after cancellation, want it left at authorized", authState)
	}
}

// TestCancelShiftingIsIdempotent proves a retried cancellation returns the original result rather
// than erroring or rewriting the recorded reason.
func TestCancelShiftingIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d011"
	shiftingEventID := authorizedShifting(t, ctx, pool, repo, "cancel-idem", []string{goatA})

	if _, _, err := cancelShifting(repo, ctx, "cancel-idem", shiftingEventID, "operator reassigned"); err != nil {
		t.Fatalf("cancel shifting: %v", err)
	}
	result, replayed, err := cancelShifting(repo, ctx, "cancel-idem", shiftingEventID, "operator reassigned")
	if err != nil {
		t.Fatalf("exact replay of the cancellation: %v", err)
	}
	if !replayed {
		t.Fatalf("replay reported replayed=false, want the original result returned")
	}
	if result.CancelReason == nil || *result.CancelReason != "operator reassigned" {
		t.Fatalf("replay cancel_reason=%v, want the original reason", result.CancelReason)
	}

	// Same key, DIFFERENT reason: a conflict, not a silent overwrite of why the movement was
	// abandoned.
	if _, _, err := cancelShifting(repo, ctx, "cancel-idem", shiftingEventID, "a completely different story"); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("err=%v, want ErrIdempotencyConflict for a same-key/different-payload replay", err)
	}
}

// ---------------------------------------------------------------------------
// State machine gating
// ---------------------------------------------------------------------------

// TestCompleteRejectsUnauthorizedStates pins that pending, canceled, and unknown movements are all
// non-completable. Pending is the one that CHANGED: under the retired 2026-07-28 rule a raised
// movement could record completion and wait for approval to apply it; approve-first (maintainer
// decision 2026-08-09) refuses it outright.
func TestCompleteRejectsUnauthorizedStates(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d021"
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)

	// PENDING: raised but never approved.
	pendingEventID, _ := submitShiftingApproval(t, ctx, repo, "gate-pending", []string{goatA})
	if _, _, err := completeShifting(repo, ctx, "gate-pending", pendingEventID); !errors.Is(err, ports.ErrShiftingNotAuthorized) {
		t.Fatalf("completing a PENDING (unapproved) movement: err=%v, want ErrShiftingNotAuthorized", err)
	}
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s after a refused completion, want it untouched at %s", got, countsShedA)
	}
	if got := shiftingEventStatus(t, ctx, pool, pendingEventID); got != domain.ShiftingEventStatusPending {
		t.Fatalf("event_status=%q after a refused completion, want it still pending", got)
	}

	// CANCELED: authorized, then retired.
	goatB := "00000000-0000-4000-8000-00000000d022"
	canceledEventID := authorizedShifting(t, ctx, pool, repo, "gate-canceled", []string{goatB})
	if _, _, err := cancelShifting(repo, ctx, "gate-canceled", canceledEventID, "not happening"); err != nil {
		t.Fatalf("cancel shifting: %v", err)
	}
	if _, _, err := completeShifting(repo, ctx, "gate-canceled", canceledEventID); !errors.Is(err, ports.ErrShiftingNotAuthorized) {
		t.Fatalf("completing a CANCELED movement: err=%v, want ErrShiftingNotAuthorized", err)
	}
	if got := goatShed(t, ctx, pool, goatB); got != countsShedA {
		t.Fatalf("goat shed=%s after completing a canceled movement, want it untouched at %s", got, countsShedA)
	}

	// UNKNOWN id.
	if _, _, err := completeShifting(repo, ctx, "gate-missing", "00000000-0000-4000-8000-0000000000ff"); !errors.Is(err, ports.ErrShiftingEventNotFound) {
		t.Fatalf("completing an unknown movement: err=%v, want ErrShiftingEventNotFound", err)
	}
}

// TestCompleteReportsNotAuthorizedBeforeFeedEvidenceOnAHighPriorityMovement pins ERROR PRECEDENCE.
//
// A HIGH-priority movement needs three videos and a feed-config fingerprint. If those checks run
// before the approval check, an operator completing an unapproved high-priority movement is told to
// "record the feed videos" when the real answer is "nobody has approved this yet" -- and the server
// runs the whole feed-requirement resolution for a request it is about to refuse. The approval
// answer must come first, and nothing may be written.
func TestCompleteReportsNotAuthorizedBeforeFeedEvidenceOnAHighPriorityMovement(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d0a1"
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	eventID, _ := submitShiftingApproval(t, ctx, repo, "gate-high-unapproved", []string{goatA})
	if _, err := pool.Exec(ctx, `
UPDATE shifting_events SET priority = 'high'
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`, countsTenant, eventID); err != nil {
		t.Fatalf("mark movement high priority: %v", err)
	}

	// No feed proofs and no fingerprint supplied -- the feed checks would all fire if they ran first.
	_, _, err := repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    eventID,
		CompletedByUserID:  countsOperator,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		ProofRef:           "proof-artifact-high-unapproved",
		IdempotencyKey:     "complete-high-unapproved",
		RequestFingerprint: "complete-fp-high-unapproved",
	})
	if !errors.Is(err, ports.ErrShiftingNotAuthorized) {
		t.Fatalf("completing an unapproved HIGH-priority movement: err=%v, want ErrShiftingNotAuthorized "+
			"-- the operator must be told it is unapproved, not told to record feed videos", err)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid
  AND (proof_ref IS NOT NULL OR completed_at IS NOT NULL OR feed_requirement_snapshot IS NOT NULL)`,
		countsTenant, eventID); got != 0 {
		t.Fatalf("refused completion left %d row(s) carrying proof/completion/feed state, want 0", got)
	}
}

// TestCancelRejectsNonAuthorizedStates pins the deliberate design choice that cancellation starts
// ONLY from 'authorized'.
//
// A PENDING movement is deliberately NOT cancellable here: it already has a retirement path that
// records a decision by somebody with the authority to make it -- the approver REJECTS it. Allowing
// an operator to cancel it instead would leave the linked counts_approval_requests row still
// pending and still approvable (approval's guard keys on authorization_state, which a cancel does
// not touch), so an approver could re-authorize a movement that had been canceled underneath them.
func TestCancelRejectsNonAuthorizedStates(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d031"
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)

	pendingEventID, _ := submitShiftingApproval(t, ctx, repo, "cancel-gate-pending", []string{goatA})
	if _, _, err := cancelShifting(repo, ctx, "cancel-gate-pending", pendingEventID, "changed my mind"); !errors.Is(err, ports.ErrShiftingNotAuthorized) {
		t.Fatalf("cancelling a PENDING movement: err=%v, want ErrShiftingNotAuthorized -- a pending "+
			"movement is retired by REJECTING its approval request, which keeps the decision with "+
			"the person who holds the authority to make it", err)
	}
	if got := shiftingEventStatus(t, ctx, pool, pendingEventID); got != domain.ShiftingEventStatusPending {
		t.Fatalf("event_status=%q after a refused cancellation, want it still pending", got)
	}

	// APPLIED: already executed, and the animals are physically at the destination. Cancelling it
	// would claim a movement that demonstrably happened did not.
	goatB := "00000000-0000-4000-8000-00000000d032"
	appliedEventID := authorizedShifting(t, ctx, pool, repo, "cancel-gate-applied", []string{goatB})
	// Occupied destination so the completion derives its cohort tag (this test is about the cancel gate).
	seedApprovalGoatWithStage(t, ctx, pool, "00000000-0000-4000-8000-00000000d039", countsShedB, "adult")
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	if _, _, err := completeShifting(repo, ctx, "cancel-gate-applied", appliedEventID); err != nil {
		t.Fatalf("complete shifting: %v", err)
	}
	if _, _, err := cancelShifting(repo, ctx, "cancel-gate-applied", appliedEventID, "too late"); !errors.Is(err, ports.ErrShiftingNotAuthorized) {
		t.Fatalf("cancelling an APPLIED movement: err=%v, want ErrShiftingNotAuthorized", err)
	}
	if got := goatShed(t, ctx, pool, goatB); got != countsShedB {
		t.Fatalf("goat shed=%s, want it to stay at the destination %s -- a refused cancellation must "+
			"not un-move an executed movement", got, countsShedB)
	}
}

// ---------------------------------------------------------------------------
// Pending-execution queue
// ---------------------------------------------------------------------------

// TestListPendingExecutionReturnsOnlyAuthorizedRows proves the operator's work list ('all') carries
// approved and completed movements and EXCLUDES both canceled and unapproved ones, and that the
// whole-filter status counts agree with what each bucket actually lists.
func TestListPendingExecutionReturnsOnlyAuthorizedRows(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	// One of each state.
	pendingGoat := "00000000-0000-4000-8000-00000000e001"
	seedApprovalGoat(t, ctx, pool, pendingGoat, countsShedA)
	pendingEventID, _ := submitShiftingApproval(t, ctx, repo, "queue-pending", []string{pendingGoat})

	authorizedEventID := authorizedShifting(t, ctx, pool, repo, "queue-authorized",
		[]string{"00000000-0000-4000-8000-00000000e002"})

	appliedEventID := authorizedShifting(t, ctx, pool, repo, "queue-applied",
		[]string{"00000000-0000-4000-8000-00000000e003"})
	// Occupied destination so the completion derives its cohort tag (this test is about the queue filter).
	seedApprovalGoatWithStage(t, ctx, pool, "00000000-0000-4000-8000-00000000e009", countsShedB, "adult")
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	if _, _, err := completeShifting(repo, ctx, "queue-applied", appliedEventID); err != nil {
		t.Fatalf("complete shifting: %v", err)
	}
	// Model the final completed-history state after evidence verification. The Actions history must
	// retain this row; the old actionable-only query dropped it as soon as rework was closed.
	if _, err := pool.Exec(ctx, `
UPDATE shifting_events
SET event_status = 'applied', verification_state = 'verified',
    applied_at = coalesce(applied_at, now()), applied_by = coalesce(applied_by, $3::uuid)
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`, countsTenant, appliedEventID, countsOperator); err != nil {
		t.Fatalf("mark completed movement verified: %v", err)
	}

	canceledEventID := authorizedShifting(t, ctx, pool, repo, "queue-canceled",
		[]string{"00000000-0000-4000-8000-00000000e004"})
	if _, _, err := cancelShifting(repo, ctx, "queue-canceled", canceledEventID, "abandoned"); err != nil {
		t.Fatalf("cancel shifting: %v", err)
	}

	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant,
	})
	if err != nil {
		t.Fatalf("list pending execution: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("work-list rows=%d, want authorized + completed only -- an unapproved movement is "+
			"not the operator's work (maintainer decision 2026-08-09)", len(page.Items))
	}
	var row domain.ShiftingExecutionRow
	foundAuthorized, foundCompleted := false, false
	var completedRow domain.ShiftingExecutionRow
	for _, candidate := range page.Items {
		switch candidate.ShiftingEventID {
		case pendingEventID:
			t.Fatalf("work list included the UNAPPROVED movement %s", pendingEventID)
		case authorizedEventID:
			foundAuthorized = true
			row = candidate
		case appliedEventID:
			foundCompleted = true
			completedRow = candidate
		}
	}
	if !foundAuthorized || !foundCompleted {
		t.Fatalf("actions did not include authorized=%t completed=%t", foundAuthorized, foundCompleted)
	}
	// The unapproved movement is not lost -- it is reachable, read-only, under its own bucket.
	pendingPage, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, Status: "pending",
	})
	if err != nil {
		t.Fatalf("list pending bucket: %v", err)
	}
	if len(pendingPage.Items) != 1 || pendingPage.Items[0].ShiftingEventID != pendingEventID {
		t.Fatalf("pending bucket=%v, want only the unapproved movement %s", pendingPage.Items, pendingEventID)
	}
	if got := pendingPage.Items[0].PrimaryActionKey; got != "none" {
		t.Fatalf("pending primary_action_key=%q, want none", got)
	}
	if row.PrimaryActionKey != "execute" || completedRow.PrimaryActionKey != "none" {
		t.Fatalf("primary actions authorized=%q completed=%q, want execute/none",
			row.PrimaryActionKey, completedRow.PrimaryActionKey)
	}
	// A window wide enough to hold every movement this test raised. It was previously RaisedAt±1s,
	// which did NOT isolate the completed row -- the four fixture movements are all raised inside the
	// same second -- so the "want all=1" assertion below could never hold and this test was red on
	// main before this change. Widening the window makes it deterministic and lets it assert the
	// thing the comment always claimed: the counts are WHOLE-FILTER aggregates, unaffected by the
	// completed filter or by page size.
	from := completedRow.RaisedAt.Add(-time.Hour)
	before := completedRow.RaisedAt.Add(time.Hour)
	completedPage, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, RaisedFrom: &from, RaisedBefore: &before, Status: "completed",
	})
	if err != nil {
		t.Fatalf("list completed actions by raised date: %v", err)
	}
	if len(completedPage.Items) != 1 || completedPage.Items[0].ShiftingEventID != appliedEventID {
		t.Fatalf("completed/date rows=%v, want only %s", completedPage.Items, appliedEventID)
	}
	// Every bucket at once, so a tab's badge always equals what that tab lists: the four movements
	// are pending / authorized / applied / canceled, and All counts the operator's work list only --
	// authorized + applied, with the unapproved one carried by Pending and the canceled one nowhere.
	wantCounts := domain.ShiftingActionStatusCounts{All: 2, Pending: 1, Authorized: 1, Rework: 0, Completed: 1}
	if completedPage.StatusCounts != wantCounts {
		t.Fatalf("whole-date status counts=%+v, want %+v independent of the completed filter and page size",
			completedPage.StatusCounts, wantCounts)
	}
	for _, excluded := range []struct{ id, why string }{
		{canceledEventID, "canceled"},
	} {
		for _, candidate := range page.Items {
			if candidate.ShiftingEventID == excluded.id {
				t.Fatalf("queue included a movement that is %s", excluded.why)
			}
		}
	}

	// The row carries what an operator needs in order to act.
	if row.SourceShedID == nil || *row.SourceShedID != countsShedA {
		t.Fatalf("source_shed_id=%v, want the shed the animals are in now (%s)", row.SourceShedID, countsShedA)
	}
	if row.DestinationShedID != countsShedB {
		t.Fatalf("destination_shed_id=%s, want %s", row.DestinationShedID, countsShedB)
	}
	if row.DestinationShedName == "" {
		t.Fatalf("destination_shed_name is empty -- an operator needs the shed's NAME, not only its uuid")
	}
	if row.AuthorizedByUserID == nil || *row.AuthorizedByUserID != countsApprover {
		t.Fatalf("authorized_by=%v, want the approver %s", row.AuthorizedByUserID, countsApprover)
	}
	if row.AuthorizedAt == nil {
		t.Fatalf("authorized_at is nil -- the operator's basis for acting must carry a timestamp")
	}
	if row.RaisedByUserID != countsOperator {
		t.Fatalf("raised_by_user_id=%q, want %q", row.RaisedByUserID, countsOperator)
	}
	if row.Priority != "low" || row.Category != "growth" {
		t.Fatalf("priority/category=(%q,%q), want (low,growth)", row.Priority, row.Category)
	}
	if row.AnimalCount != 1 {
		t.Fatalf("animal_count=%d, want 1", row.AnimalCount)
	}
	if len(row.Animals) != 1 {
		t.Fatalf("animals preview=%d, want 1", len(row.Animals))
	}
	if row.Animals[0].DisplayID == "" {
		t.Fatalf("animal display_id is empty -- an operator identifies an animal by its display id")
	}
}

// TestListPendingExecutionStatusBucketsAreDisjointAndTotal is the adversarial status matrix for the
// approve-first bucket rule (maintainer decision 2026-08-09).
//
// It drives one movement into EVERY reachable status and proves three things at once: each bucket
// lists exactly the rows it should, the buckets are disjoint AND total (no row is unreachable, which
// is the failure mode of excluding 'pending' from 'all' carelessly), and every whole-filter
// status_count equals the number of rows its own bucket lists — so a tab's badge can never disagree
// with the tab.
func TestListPendingExecutionStatusBucketsAreDisjointAndTotal(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	// Completions relocate into shed B, so it needs its cohort profile.
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	pendingID, _ := func() (string, string) {
		seedApprovalGoat(t, ctx, pool, "00000000-0000-4000-8000-00000000f001", countsShedA)
		return submitShiftingApproval(t, ctx, repo, "matrix-pending", []string{"00000000-0000-4000-8000-00000000f001"})
	}()
	authorizedID := authorizedShifting(t, ctx, pool, repo, "matrix-authorized", []string{"00000000-0000-4000-8000-00000000f002"})

	completedID := authorizedShifting(t, ctx, pool, repo, "matrix-completed", []string{"00000000-0000-4000-8000-00000000f003"})
	if _, _, err := completeShifting(repo, ctx, "matrix-completed", completedID); err != nil {
		t.Fatalf("complete matrix-completed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE shifting_events SET verification_state = 'verified'
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`, countsTenant, completedID); err != nil {
		t.Fatalf("mark completed movement verified: %v", err)
	}

	reworkID := authorizedShifting(t, ctx, pool, repo, "matrix-rework", []string{"00000000-0000-4000-8000-00000000f004"})
	if _, _, err := completeShifting(repo, ctx, "matrix-rework", reworkID); err != nil {
		t.Fatalf("complete matrix-rework: %v", err)
	}
	if err := repo.BounceShiftingEventForRework(ctx, domain.ShiftingReworkCommand{
		TenantID: countsTenant, ShiftingEventID: reworkID, VerifiedBy: countsApprover, Reason: "reshoot",
	}); err != nil {
		t.Fatalf("bounce matrix-rework: %v", err)
	}

	canceledID := authorizedShifting(t, ctx, pool, repo, "matrix-canceled", []string{"00000000-0000-4000-8000-00000000f005"})
	if _, _, err := cancelShifting(repo, ctx, "matrix-canceled", canceledID, "abandoned"); err != nil {
		t.Fatalf("cancel matrix-canceled: %v", err)
	}

	ids := func(page domain.ShiftingExecutionPage) []string {
		out := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			out = append(out, item.ShiftingEventID)
		}
		sort.Strings(out)
		return out
	}
	want := func(values ...string) []string { sort.Strings(values); return values }

	for _, tc := range []struct {
		status string
		want   []string
	}{
		// 'all' is the operator's WORK LIST: no unapproved movement, no canceled movement.
		{"all", want(authorizedID, completedID, reworkID)},
		{"pending", want(pendingID)},
		{"authorized", want(authorizedID)},
		{"rework", want(reworkID)},
		{"completed", want(completedID)},
	} {
		page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
			TenantID: countsTenant, Status: tc.status,
		})
		if err != nil {
			t.Fatalf("list status=%s: %v", tc.status, err)
		}
		if got := ids(page); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("status=%s rows=%v, want %v", tc.status, got, tc.want)
		}
	}

	// Disjoint AND total, for every movement that is VISIBLE: the four leaf buckets partition each
	// one exactly once, so excluding 'pending' from the work list hid nothing.
	//
	// "Visible" is the honest qualifier and not a weasel word. An approved movement still inside its
	// ACTIONS LEAD TIME is deliberately in NO bucket at all until it is due -- that is what "hide
	// until due" means, and it is asserted directly by
	// TestListPendingExecutionAppliesTheActionsLeadTime. Every movement in THIS test is past its lead
	// time (the shared fixture raises two days back), so totality is a real claim here rather than a
	// vacuous one.
	seen := map[string]int{}
	for _, status := range []string{"pending", "authorized", "rework", "completed"} {
		page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
			TenantID: countsTenant, Status: status,
		})
		if err != nil {
			t.Fatalf("list status=%s: %v", status, err)
		}
		for _, item := range page.Items {
			seen[item.ShiftingEventID]++
		}
	}
	for _, id := range []string{pendingID, authorizedID, completedID, reworkID} {
		if seen[id] != 1 {
			t.Fatalf("movement %s appears in %d leaf buckets, want exactly 1 (disjoint and total)", id, seen[id])
		}
	}
	if seen[canceledID] != 0 {
		t.Fatalf("canceled movement %s appears in %d buckets, want 0", canceledID, seen[canceledID])
	}

	// Every whole-filter count equals what its own bucket lists.
	var raisedAt time.Time
	if err := pool.QueryRow(ctx, `
SELECT raised_at FROM shifting_events WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`,
		countsTenant, pendingID).Scan(&raisedAt); err != nil {
		t.Fatalf("read raised_at: %v", err)
	}
	from, before := raisedAt.Add(-time.Hour), raisedAt.Add(time.Hour)
	dated, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, RaisedFrom: &from, RaisedBefore: &before,
	})
	if err != nil {
		t.Fatalf("list dated: %v", err)
	}
	wantCounts := domain.ShiftingActionStatusCounts{All: 3, Pending: 1, Authorized: 1, Rework: 1, Completed: 1}
	if dated.StatusCounts != wantCounts {
		t.Fatalf("status_counts=%+v, want %+v -- each badge must equal what its tab lists",
			dated.StatusCounts, wantCounts)
	}
}

// TestListPendingExecutionAppliesTheActionsLeadTime is the adversarial date-shift case for the
// ACTIONS LEAD TIME (maintainer decision 2026-08-09): high priority is work the second it is
// approved, low priority is planned work that lands the next day, or the day after when it was
// raised past 13:30 IST.
//
// Every instant here is FIXED, and the query's clock is pinned through ShiftingExecutionQuery.Now,
// so the test asserts the rule rather than whatever time of day it happens to run at.
func TestListPendingExecutionAppliesTheActionsLeadTime(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	ist := biztime.DefaultLocation()
	at := func(day, hour, min int) time.Time {
		return time.Date(2026, time.August, day, hour, min, 0, 0, ist)
	}

	// Three approved movements that differ ONLY in priority and raise time.
	morning := authorizedShifting(t, ctx, pool, repo, "lead-morning", []string{"00000000-0000-4000-8000-00000000f201"})
	afternoon := authorizedShifting(t, ctx, pool, repo, "lead-afternoon", []string{"00000000-0000-4000-8000-00000000f202"})
	urgent := authorizedShifting(t, ctx, pool, repo, "lead-urgent", []string{"00000000-0000-4000-8000-00000000f203"})
	setShiftingRaise := func(id, priority string, raisedAt time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
UPDATE shifting_events SET priority = $3, raised_at = $4::timestamptz
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`, countsTenant, id, priority, raisedAt.UTC()); err != nil {
			t.Fatalf("set raise for %s: %v", id, err)
		}
	}
	setShiftingRaise(morning, "low", at(10, 9, 0))    // before 13:30 -> due 11 Aug
	setShiftingRaise(afternoon, "low", at(10, 14, 0)) // after 13:30  -> due 12 Aug
	setShiftingRaise(urgent, "high", at(10, 14, 0))   // high         -> due at once

	visible := func(now time.Time) map[string]bool {
		t.Helper()
		page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
			TenantID: countsTenant, Now: now,
		})
		if err != nil {
			t.Fatalf("list at %s: %v", now, err)
		}
		out := map[string]bool{}
		for _, item := range page.Items {
			out[item.ShiftingEventID] = true
		}
		return out
	}

	for _, tc := range []struct {
		name                                   string
		now                                    time.Time
		wantMorning, wantAfternoon, wantUrgent bool
	}{
		// The moment all three are approved: only the urgent one is work.
		{"same afternoon", at(10, 14, 1), false, false, true},
		{"late that night", at(10, 23, 59), false, false, true},
		// Next day: the before-cutoff movement lands. The after-cutoff one has not.
		{"next day", at(11, 0, 0), true, false, true},
		{"next day, late", at(11, 20, 0), true, false, true},
		// Day after: everything is work.
		{"day after next", at(12, 0, 0), true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := visible(tc.now)
			if got[morning] != tc.wantMorning {
				t.Fatalf("at %s low/before-cutoff visible=%t, want %t", tc.now, got[morning], tc.wantMorning)
			}
			if got[afternoon] != tc.wantAfternoon {
				t.Fatalf("at %s low/after-cutoff visible=%t, want %t", tc.now, got[afternoon], tc.wantAfternoon)
			}
			if got[urgent] != tc.wantUrgent {
				t.Fatalf("at %s HIGH priority visible=%t, want %t -- high priority waits for nothing",
					tc.now, got[urgent], tc.wantUrgent)
			}
		})
	}

	// The status counts obey the same filter, so a tab badge never advertises work the tab hides.
	from, before := at(10, 0, 0), at(11, 0, 0)
	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, RaisedFrom: &from, RaisedBefore: &before, Now: at(10, 14, 1),
	})
	if err != nil {
		t.Fatalf("list dated: %v", err)
	}
	if page.StatusCounts.Authorized != 1 || page.StatusCounts.All != 1 {
		t.Fatalf("status_counts=%+v on the raise day, want all=1 authorized=1 -- only the high-priority "+
			"movement is due, and the two low-priority ones must not be counted", page.StatusCounts)
	}
}

// TestListPendingExecutionMultipleDimensionsDoNotInflateRowOrCount is the adversarial cardinality
// case for the request/animal LATERAL joins: a movement naming many animals, with a SECOND approval
// request attached to the same event, must still be ONE row carrying the true animal count. If the
// request selector ever loses its LIMIT 1, or the animal preview joins onto the row instead of
// through its own LATERAL, this row multiplies and the operator's queue double-counts work.
func TestListPendingExecutionMultipleDimensionsDoNotInflateRowOrCount(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	movers := []string{
		"00000000-0000-4000-8000-00000000f101",
		"00000000-0000-4000-8000-00000000f102",
		"00000000-0000-4000-8000-00000000f103",
	}
	eventID := authorizedShifting(t, ctx, pool, repo, "fanout", movers)

	// A second, still-pending approval request against the SAME movement -- the many side the
	// bounded LATERAL selector exists to collapse.
	payload, err := json.Marshal(map[string]any{
		"shifting_event_id":   eventID,
		"destination_park_id": countsPark,
		"destination_shed_id": countsShedB,
		"goat_ids":            movers[:1],
	})
	if err != nil {
		t.Fatalf("marshal duplicate payload: %v", err)
	}
	if _, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID:           countsTenant,
		RequestType:        domain.ApprovalRequestTypeShifting,
		Payload:            payload,
		ShiftingEventID:    &eventID,
		RaisedByUserID:     countsOperator,
		RaisedAt:           time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "submit-fanout-dup",
		RequestFingerprint: "submit-fp-fanout-dup",
	}); err != nil {
		t.Fatalf("create duplicate approval request: %v", err)
	}

	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{TenantID: countsTenant})
	if err != nil {
		t.Fatalf("list actions: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("rows=%d for ONE movement with 3 animals and 2 requests, want 1 -- a join is fanning out", len(page.Items))
	}
	// The count comes from the APPROVED request's full animal array, never from the bounded preview
	// and never from the second, pending request.
	if page.Items[0].AnimalCount != len(movers) {
		t.Fatalf("animal_count=%d, want %d from the approved request's full animal set",
			page.Items[0].AnimalCount, len(movers))
	}
	if len(page.Items[0].Animals) != len(movers) {
		t.Fatalf("animals preview=%d, want %d (all three fit under the preview bound)",
			len(page.Items[0].Animals), len(movers))
	}
}

// TestListPendingExecutionPageBoundaryKeysetDoesNotSkipOrRepeat proves the queue pages at the mobile
// bound and that the cursor walks forward across the page boundary without skipping or repeating a
// movement.
func TestListPendingExecutionPageBoundaryKeysetDoesNotSkipOrRepeat(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	const total = 5
	want := map[string]bool{}
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("page-%d", i)
		goatID := fmt.Sprintf("00000000-0000-4000-8000-0000000f%04d", i)
		want[authorizedShifting(t, ctx, pool, repo, key, []string{goatID})] = false
		// Distinct authorized_at values, so the keyset has a strict order to walk rather than a tie
		// resolved only by id.
		time.Sleep(2 * time.Millisecond)
	}

	const pageSize = 2
	seen := 0
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > total+2 {
			t.Fatalf("pagination did not terminate after %d pages -- the cursor is not advancing", pages)
		}
		decoded, err := domain.DecodeShiftingExecutionCursor(cursor)
		if err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
			TenantID: countsTenant,
			PageSize: pageSize,
			Cursor:   decoded,
		})
		if err != nil {
			t.Fatalf("list pending execution: %v", err)
		}
		if len(page.Items) > pageSize {
			t.Fatalf("page returned %d rows, want at most the requested %d", len(page.Items), pageSize)
		}
		for _, row := range page.Items {
			already, known := want[row.ShiftingEventID]
			if !known {
				t.Fatalf("page returned an unexpected movement %s", row.ShiftingEventID)
			}
			if already {
				t.Fatalf("movement %s appeared on two pages -- a keyset walk must not repeat rows",
					row.ShiftingEventID)
			}
			want[row.ShiftingEventID] = true
			seen++
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if seen != total {
		t.Fatalf("walked %d movements across all pages, want %d -- a keyset walk must not skip rows",
			seen, total)
	}
}

// TestListPendingExecutionFiltersBySourcePark proves the optional park filter narrows to the park
// the animals are currently standing in, and that omitting it returns every park.
func TestListPendingExecutionParkScopeFiltersBySourcePark(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	inPark := authorizedShifting(t, ctx, pool, repo, "park-filter-1",
		[]string{"00000000-0000-4000-8000-00000000f101"})

	// Supplying a park that owns no movement must return an empty page rather than everything --
	// a filter that silently degrades to "all" is worse than no filter.
	otherPark := "00000000-0000-4000-8000-0000000000aa"
	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID:     countsTenant,
		SourceParkID: otherPark,
	})
	if err != nil {
		t.Fatalf("list pending execution filtered to another park: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("rows=%d for a park with no movements, want 0", len(page.Items))
	}

	// The movement's actual source park returns it.
	page, err = repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID:     countsTenant,
		SourceParkID: countsPark,
	})
	if err != nil {
		t.Fatalf("list pending execution filtered to the source park: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ShiftingEventID != inPark {
		t.Fatalf("rows=%d, want exactly the movement sourced in %s", len(page.Items), countsPark)
	}

	// No filter: every park.
	page, err = repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant,
	})
	if err != nil {
		t.Fatalf("list pending execution unfiltered: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("unfiltered rows=%d, want 1", len(page.Items))
	}
}

func TestListPendingExecutionSourceFiltersAlsoNarrowDatedSummaries(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	inPark := authorizedShifting(t, ctx, pool, repo, "park-summary-filter",
		[]string{"00000000-0000-4000-8000-00000000f301"})

	var raisedAt time.Time
	if err := pool.QueryRow(ctx, `
SELECT raised_at FROM shifting_events WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`,
		countsTenant, inPark).Scan(&raisedAt); err != nil {
		t.Fatalf("read raised_at: %v", err)
	}
	from, before := raisedAt.Add(-time.Hour), raisedAt.Add(time.Hour)

	otherPark := "00000000-0000-4000-8000-0000000000aa"
	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, RaisedFrom: &from, RaisedBefore: &before, SourceParkID: otherPark,
	})
	if err != nil {
		t.Fatalf("list dated summary for other park: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("rows=%d for other park, want 0", len(page.Items))
	}
	if page.StatusCounts != (domain.ShiftingActionStatusCounts{}) {
		t.Fatalf("status_counts=%+v for other park, want all zero like the page", page.StatusCounts)
	}
	if len(page.PreviousDates) != 0 {
		t.Fatalf("previous_dates=%v for other park, want none", page.PreviousDates)
	}

	page, err = repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, RaisedFrom: &from, RaisedBefore: &before,
		SourceParkID: countsPark, SourceShedID: countsShedA,
	})
	if err != nil {
		t.Fatalf("list dated summary for source park/shed: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ShiftingEventID != inPark {
		t.Fatalf("rows=%v, want only %s", page.Items, inPark)
	}
	wantCounts := domain.ShiftingActionStatusCounts{All: 1, Authorized: 1}
	if page.StatusCounts != wantCounts {
		t.Fatalf("status_counts=%+v, want %+v narrowed to the same source scope as the page",
			page.StatusCounts, wantCounts)
	}
}

// TestListPendingExecutionFiltersBySourceShed proves the optional shed filter is the second half of
// the operator's farm -> shed cascade: it narrows to the SOURCE shed the animals stand in, returns
// empty for a shed with no movement, and returns everything when omitted. authorizedShifting seeds
// its animals into countsShedA, so that is the movement's source shed.
func TestListPendingExecutionFiltersBySourceShed(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	inShed := authorizedShifting(t, ctx, pool, repo, "shed-filter-1",
		[]string{"00000000-0000-4000-8000-00000000f201"})

	// A shed that owns no movement returns an empty page, not everything.
	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID:     countsTenant,
		SourceShedID: countsShedB,
	})
	if err != nil {
		t.Fatalf("list pending execution filtered to another shed: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("rows=%d for a shed with no movements, want 0", len(page.Items))
	}

	// The movement's actual source shed returns it -- including with the park pinned too, the shape
	// the cascade always sends.
	page, err = repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID:     countsTenant,
		SourceParkID: countsPark,
		SourceShedID: countsShedA,
	})
	if err != nil {
		t.Fatalf("list pending execution filtered to the source park+shed: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ShiftingEventID != inShed {
		t.Fatalf("rows=%d, want exactly the movement sourced in %s", len(page.Items), countsShedA)
	}

	// A correct park but the wrong shed still excludes it: the shed is an AND, not an OR, with park.
	page, err = repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID:     countsTenant,
		SourceParkID: countsPark,
		SourceShedID: countsShedB,
	})
	if err != nil {
		t.Fatalf("list pending execution filtered to source park but wrong shed: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("rows=%d for the right park but wrong shed, want 0", len(page.Items))
	}
}
