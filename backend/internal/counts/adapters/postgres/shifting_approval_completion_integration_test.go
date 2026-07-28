package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// These tests pin the maintainer-2026-07-28 shifting contract on the real counts + identity
// Postgres path. Approval and operator completion are independent gates; the second gate applies
// the move. Verification reviews evidence afterward and never owns census truth.

func TestRaisedShiftingAppearsInActionsBeforeApproval(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatID := "00000000-0000-4000-8000-00000000da01"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	eventID, _ := submitShiftingApproval(t, ctx, repo, "actions-before-approval", []string{goatID})

	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{TenantID: countsTenant})
	if err != nil {
		t.Fatalf("list shifting actions: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ShiftingEventID != eventID {
		t.Fatalf("actions=%v, want newly raised event %s before approval", page.Items, eventID)
	}
	if page.Items[0].AnimalCount != 1 {
		t.Fatalf("animal_count=%d, want 1 from the linked pending approval payload", page.Items[0].AnimalCount)
	}
}

func TestShiftingCompletionFirstMovesWhenParkHeadApproves(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatID := "00000000-0000-4000-8000-00000000da02"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "completion-first", []string{goatID})

	completed, replay, err := submitShiftingForVerification(repo, ctx, "completion-first", eventID, "")
	if err != nil {
		t.Fatalf("operator completion before approval: %v", err)
	}
	if replay || completed.EventStatus != domain.ShiftingEventStatusPending {
		t.Fatalf("completion result=(status=%q replay=%t), want pending fresh", completed.EventStatus, replay)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("goat shed=%s after completion only, want source %s", got, countsShedA)
	}

	if _, _, err := approveShifting(repo, ctx, "completion-first", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("park-head approval after completion: %v", err)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("goat shed=%s after both gates, want destination %s", got, countsShedB)
	}
	if got := shiftingEventStatus(t, ctx, pool, eventID); got != domain.ShiftingEventStatusApplied {
		t.Fatalf("event_status=%q after both gates, want applied", got)
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
