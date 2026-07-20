package postgres

import (
	"context"
	"errors"
	"fmt"
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

// TestCompleteRejectsUnauthorizedStates pins that the relocation is reachable ONLY from
// 'authorized'. A caller addressing a still-pending movement's id must not be able to execute a
// movement no approver signed off on.
func TestCompleteRejectsUnauthorizedStates(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d021"
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)

	// PENDING: raised but never approved.
	pendingEventID, _ := submitShiftingApproval(t, ctx, repo, "gate-pending", []string{goatA})
	if _, _, err := completeShifting(repo, ctx, "gate-pending", pendingEventID); !errors.Is(err, ports.ErrShiftingNotAuthorized) {
		t.Fatalf("completing a PENDING movement: err=%v, want ErrShiftingNotAuthorized -- an "+
			"unapproved movement must not be executable", err)
	}
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s after a refused completion, want it untouched at %s", got, countsShedA)
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

// TestListPendingExecutionReturnsOnlyAuthorizedRows proves the queue is exactly the outstanding
// execution work: not pending (unapproved) movements, not applied ones, not canceled ones.
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
	if len(page.Items) != 1 {
		t.Fatalf("pending-execution rows=%d, want exactly 1 (only the authorized movement)", len(page.Items))
	}
	row := page.Items[0]
	if row.ShiftingEventID != authorizedEventID {
		t.Fatalf("row=%s, want the authorized movement %s", row.ShiftingEventID, authorizedEventID)
	}
	for _, excluded := range []struct{ id, why string }{
		{pendingEventID, "still awaiting approval"},
		{appliedEventID, "already executed"},
		{canceledEventID, "canceled"},
	} {
		if row.ShiftingEventID == excluded.id {
			t.Fatalf("queue included a movement that is %s", excluded.why)
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

// TestListPendingExecutionPaginatesByKeyset proves the queue pages at the mobile bound and that the
// cursor walks forward without skipping or repeating a movement.
func TestListPendingExecutionPaginatesByKeyset(t *testing.T) {
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
func TestListPendingExecutionFiltersBySourcePark(t *testing.T) {
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
