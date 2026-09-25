package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TestRejectingAShiftingApprovalRetiresTheMovement is the production-path regression for the
// 2026-09-25 finding: rejecting a pen-move approval request flipped the REQUEST to 'rejected' and
// left its shifting_events row at authorization_state='pending', event_status='pending' forever.
// The raiser's read-only Pending tab kept listing a movement nobody would ever approve, and the feed
// projection -- which counts raised-but-unapproved movements until they are REJECTED (AFTERNOON FEED
// CORRECTION rule) -- kept feeding the destination pen for animals that were never coming.
//
// Raise -> reject through DecideApprovalRequest, then assert every reader that owns the movement
// lets go of it: the event row, the Actions pending bucket, the Actions work list and its tab
// counts, and the feed projection.
func TestRejectingAShiftingApprovalRetiresTheMovement(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})

	goatID := "00000000-0000-4000-8000-00000000e501"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "reject-retires", []string{goatID})

	now := time.Now().In(biztime.DefaultLocation())
	feedTarget := biztime.BusinessDayStart(now).AddDate(0, 0, 1)
	destDelta := func() int64 {
		t.Helper()
		got, err := repo.ProjectedShedCountsForFeed(ctx, domain.FeedProjectedCountQuery{
			TenantID: countsTenant, TargetDate: feedTarget, Limit: 100,
		})
		if err != nil {
			t.Fatalf("feed projection: %v", err)
		}
		var sum int64
		for _, row := range got.Items {
			if row.ShedID != nil && *row.ShedID == countsShedB {
				sum += row.PendingDelta
			}
		}
		return sum
	}
	// Premise: while raised the movement feeds the destination pen.
	if got := destDelta(); got != 1 {
		t.Fatalf("destination pending_delta before the decision=%d, want 1 -- the premise of the test", got)
	}

	if _, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  approvalID,
		Status:             domain.ApprovalStatusRejected,
		DecidedByUserID:    countsApprover,
		DecidedAt:          now,
		Reason:             "Destination pen is full",
		IdempotencyKey:     "decide-reject-retires",
		RequestFingerprint: "decide-fp-reject-retires",
	}); err != nil {
		t.Fatalf("reject shifting: %v", err)
	}

	var authState, eventStatus string
	var rowVersion int
	if err := pool.QueryRow(ctx, `
SELECT authorization_state, event_status, row_version FROM shifting_events WHERE shifting_event_id = $1::uuid`,
		eventID).Scan(&authState, &eventStatus, &rowVersion); err != nil {
		t.Fatalf("read shifting event: %v", err)
	}
	if authState != "rejected" || eventStatus != domain.ShiftingEventStatusRejected {
		t.Fatalf("shifting event after reject: authorization_state=%q event_status=%q, want rejected/rejected",
			authState, eventStatus)
	}
	if rowVersion < 2 {
		t.Fatalf("row_version=%d after reject, want it bumped", rowVersion)
	}

	if got := destDelta(); got != 0 {
		t.Fatalf("destination pending_delta after reject=%d, want 0 -- a rejected movement must stop feeding the pen", got)
	}

	dayStart := biztime.BusinessDayStart(now).AddDate(0, 0, -2)
	dayEnd := dayStart.AddDate(0, 0, 1)
	for _, status := range []string{"pending", "all", "authorized", "rework", "completed"} {
		page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
			TenantID: countsTenant, Status: status, RaisedFrom: &dayStart, RaisedBefore: &dayEnd,
		})
		if err != nil {
			t.Fatalf("list %s bucket: %v", status, err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("%s bucket lists %d rows after reject, want 0 -- a rejected movement is off every tab", status, len(page.Items))
		}
		c := page.StatusCounts
		if c.All+c.Pending+c.Authorized+c.Rework+c.Completed != 0 {
			t.Fatalf("tab counts after reject=%+v, want all zero", c)
		}
	}

	// Replay of the same reject is still a clean no-op and does not touch the event again.
	if _, replay, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID: countsTenant, ApprovalRequestID: approvalID, Status: domain.ApprovalStatusRejected,
		DecidedByUserID: countsApprover, DecidedAt: now, Reason: "Destination pen is full",
		IdempotencyKey: "decide-reject-retires", RequestFingerprint: "decide-fp-reject-retires",
	}); err != nil || !replay {
		t.Fatalf("exact replay of the reject: replay=%t err=%v, want replay=true err=nil", replay, err)
	}
}

// TestRejectingOneOfTwoPendingDeathReportsKeepsTheAnimalsDeathWork: two operators may each report
// the same death (a submit accepts both). The goat has ONE death workflow, so a counts.death.rejected
// for the first report -- which cancels that workflow and resumes held health work -- stranded the
// second, still-pending report with no steps to record. The rejection is announced only when no
// other death report of the animal is still pending.
func TestRejectingOneOfTwoPendingDeathReportsKeepsTheAnimalsDeathWork(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})
	goatID := "00000000-0000-4000-8000-00000000e502"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)

	submit := func(key string) domain.ApprovalRequest {
		t.Helper()
		req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
			TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeDeath,
			Payload:       json.RawMessage(`{"goat_id":"` + goatID + `","lifecycle_status":"dead","exit_reason":"died"}`),
			SubjectGoatID: &goatID, RaisedByUserID: countsOperator, RaisedAt: time.Now(),
			IdempotencyKey: key, RequestFingerprint: key + "-fp",
		})
		if err != nil {
			t.Fatalf("submit %s: %v", key, err)
		}
		return req
	}
	reject := func(req domain.ApprovalRequest, key string) {
		t.Helper()
		if _, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
			TenantID: countsTenant, ApprovalRequestID: req.ApprovalRequestID, Status: domain.ApprovalStatusRejected,
			DecidedByUserID: countsApprover, DecidedAt: time.Now(), Reason: "duplicate report",
			IdempotencyKey: key, RequestFingerprint: key + "-fp",
		}); err != nil {
			t.Fatalf("reject %s: %v", key, err)
		}
	}
	rejectedEvents := func() int {
		t.Helper()
		return countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = '`+domain.EventDeathRejected+`'`, countsTenant)
	}

	first, second := submit("death-dup-1"), submit("death-dup-2")
	reject(first, "reject-dup-1")
	if got := rejectedEvents(); got != 0 {
		t.Fatalf("counts.death.rejected after rejecting ONE of two pending reports = %d, want 0 -- the other "+
			"report still needs the animal's death steps", got)
	}
	reject(second, "reject-dup-2")
	if got := rejectedEvents(); got != 1 {
		t.Fatalf("counts.death.rejected after rejecting the LAST pending report = %d, want 1", got)
	}
}
