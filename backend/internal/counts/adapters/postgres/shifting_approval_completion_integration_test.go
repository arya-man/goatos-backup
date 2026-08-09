package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

// These tests pin the maintainer-2026-08-09 APPROVE-FIRST shifting contract on the real counts +
// identity Postgres path, RETIRING the 2026-07-28 order-free gates. A raised movement is out of the
// operator's work list and non-completable until a Park Head authorizes it; operator completion then
// applies the move. Verification reviews evidence afterward and never owns census truth.

// TestRaisedShiftingIsHiddenFromActionsUntilApproved pins the visibility half of approve-first: the
// work list ('all') does not show an unapproved movement, and the read-only Pending bucket that does
// show it offers no action.
func TestRaisedShiftingIsHiddenFromActionsUntilApproved(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatID := "00000000-0000-4000-8000-00000000da01"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "actions-before-approval", []string{goatID})

	work, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{TenantID: countsTenant})
	if err != nil {
		t.Fatalf("list shifting actions: %v", err)
	}
	if len(work.Items) != 0 {
		t.Fatalf("work list=%v, want EMPTY -- an unapproved movement is not the operator's work", work.Items)
	}

	pending, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, Status: "pending",
	})
	if err != nil {
		t.Fatalf("list pending bucket: %v", err)
	}
	if len(pending.Items) != 1 || pending.Items[0].ShiftingEventID != eventID {
		t.Fatalf("pending bucket=%v, want the raised event %s -- the raiser must still see it", pending.Items, eventID)
	}
	if got := pending.Items[0].PrimaryActionKey; got != "none" {
		t.Fatalf("primary_action_key=%q for an unapproved movement, want none -- the row is read-only", got)
	}
	if pending.Items[0].AnimalCount != 1 {
		t.Fatalf("animal_count=%d, want 1 from the linked pending approval payload", pending.Items[0].AnimalCount)
	}

	// Approval is what puts it in the work list, and makes it executable.
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	if _, _, err := approveShifting(repo, ctx, "actions-before-approval", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("park-head approval: %v", err)
	}
	work, err = repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{TenantID: countsTenant})
	if err != nil {
		t.Fatalf("list shifting actions after approval: %v", err)
	}
	if len(work.Items) != 1 || work.Items[0].ShiftingEventID != eventID {
		t.Fatalf("work list after approval=%v, want the approved event %s", work.Items, eventID)
	}
	if got := work.Items[0].PrimaryActionKey; got != "execute" {
		t.Fatalf("primary_action_key=%q after approval, want execute", got)
	}
}

// TestShiftingCompletionBeforeApprovalIsRefused pins the write half of approve-first. This is the
// case the retired rule ALLOWED: an operator shooting the video first and the move applying when the
// park head later approved. It must now write nothing at all.
func TestShiftingCompletionBeforeApprovalIsRefused(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatID := "00000000-0000-4000-8000-00000000da02"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "completion-first", []string{goatID})

	if _, _, err := submitShiftingForVerification(repo, ctx, "completion-first", eventID, ""); !errors.Is(err, ports.ErrShiftingNotAuthorized) {
		t.Fatalf("completion before approval: err=%v, want ErrShiftingNotAuthorized", err)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("goat shed=%s after a refused completion, want it untouched at %s", got, countsShedA)
	}
	// Nothing written: no proof, no completion stamps, no verification item to review. A refused
	// completion that still stored the video would leave a verifier reviewing a move nobody approved.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid
  AND (proof_ref IS NOT NULL OR completed_at IS NOT NULL OR completed_by IS NOT NULL)`,
		countsTenant, eventID); got != 0 {
		t.Fatalf("refused completion left %d row(s) carrying proof/completion stamps, want 0", got)
	}
	if got := shiftingEventStatus(t, ctx, pool, eventID); got != domain.ShiftingEventStatusPending {
		t.Fatalf("event_status=%q after a refused completion, want it still pending", got)
	}

	// Approve, then complete: the normal order still works and applies the move.
	if _, _, err := approveShifting(repo, ctx, "completion-first", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("park-head approval: %v", err)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("goat shed=%s after approval alone, want it still at source %s -- approval moves nothing", got, countsShedA)
	}
	completed, replay, err := submitShiftingForVerification(repo, ctx, "completion-first", eventID, "")
	if err != nil {
		t.Fatalf("operator completion after approval: %v", err)
	}
	if replay || completed.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("completion result=(status=%q replay=%t), want applied fresh", completed.EventStatus, replay)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("goat shed=%s after both gates, want destination %s", got, countsShedB)
	}
}

func TestShiftingApprovalFirstMovesAtCompletionAndVerificationCannotRollback(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatID := "00000000-0000-4000-8000-00000000da03"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "approval-first", []string{goatID})
	if _, _, err := approveShifting(repo, ctx, "approval-first", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("park-head approval: %v", err)
	}

	completed, _, err := submitShiftingForVerification(repo, ctx, "approval-first", eventID, "")
	if err != nil {
		t.Fatalf("operator completion after approval: %v", err)
	}
	if completed.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("completion status=%q, want applied when approval already exists", completed.EventStatus)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("goat shed=%s after both gates, want destination %s", got, countsShedB)
	}

	if err := repo.BounceShiftingEventForRework(ctx, domain.ShiftingReworkCommand{
		TenantID: countsTenant, ShiftingEventID: eventID, VerifiedBy: countsApprover,
		Reason: "video needs a clearer destination view",
	}); err != nil {
		t.Fatalf("verification rework: %v", err)
	}
	if got := shiftingEventStatus(t, ctx, pool, eventID); got != domain.ShiftingEventStatusApplied {
		t.Fatalf("event_status=%q after evidence rework, want applied", got)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("goat shed=%s after evidence rework, want destination %s (no rollback)", got, countsShedB)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 1 {
		t.Fatalf("location.changed events=%d after rework, want still 1", got)
	}
}
