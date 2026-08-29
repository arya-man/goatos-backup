package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	countsports "github.com/vgoats/goatos/backend/internal/counts/ports"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TestKernelStory_ShiftingRejectLifecycle is the whole-lifecycle E2E proof for the shifting
// decision fork, through the REAL production writes and reads only:
//
//	RAISE    counts.RecordShiftingEvent + CreateApprovalRequest — the raiser sees the movement
//	         read-only on the Pending tab, and the raised movement already counts toward the feed
//	         sheet (maintainer decision 2026-08-10: a raised shifting feeds its destination
//	         before approval; only rejection stops that clock).
//	REJECT   counts.DecideApprovalRequest(rejected) — the decision transaction flips the
//	         shifting_events row itself (defect fixed 2026-08-29: previously only the approval
//	         request flipped and the movement sat on the Pending tab forever), the movement
//	         leaves EVERY Actions tab, and the feed projection stops feeding the destination.
//	AFTER    a rejected movement is terminal: the reject replays idempotently, a contrary
//	         approve conflicts, and completion is refused writing nothing.
//	RE-RAISE the same animals raise again under a new movement, approve, complete — proving a
//	         rejection strands neither the animals nor the workflow.
//
// Fixture inputs are external facts only (tenant/park/sheds/profiles/animal); every asserted
// outcome is produced by the same repository paths the API serves.
func TestKernelStory_ShiftingRejectLifecycle(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-reject-lifecycle",
		"Shifting reject lifecycle: raise -> reject leaves every tab, stops the feed clock, and is terminal",
		"An operator raises a shifting; the Park Head rejects it. The rejection must flip the movement "+
			"itself (not just the approval paperwork) so it leaves the raiser's Pending tab and every other "+
			"Actions bucket, stops counting toward the destination's feed sheet, refuses completion, and "+
			"replays idempotently — while the same animals can immediately be raised again and moved "+
			"through the normal approve -> complete path.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx
	repo := countspg.NewRepository(fx.Pool, 10*time.Second).
		WithIdentityTxWriter(identitypg.NewRepository(fx.Pool, 10*time.Second))

	const (
		sourceShed = "5e000000-0000-4000-8000-000000000001"
		destShed   = "5e000000-0000-4000-8000-000000000002"
		stageID    = "5e000000-0000-4000-8000-00000000000a"
		mover      = "5e000000-0000-4000-8000-000000000010"
	)
	fx.SeedShed(sourceShed, "E2E-SRL-SRC", stageID)
	fx.SeedShed(destShed, "E2E-SRL-DST", stageID)
	fx.SeedGoat(GoatSpec{GoatID: mover, ShedID: sourceShed, Stage: "K1", Breed: "Beetal"})

	// Fixed IST business dates (no now± arithmetic): raised well before "now", so the Actions lead
	// time cannot be the reason a row is hidden, and the raised movement is already feed-effective
	// on the feed target date.
	raisedAt := time.Date(2026, 8, 20, 9, 0, 0, 0, biztime.DefaultLocation())
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, biztime.DefaultLocation())
	feedTarget := time.Date(2026, 8, 25, 0, 0, 0, 0, biztime.DefaultLocation())
	windowFrom := time.Date(2026, 8, 20, 0, 0, 0, 0, biztime.DefaultLocation())
	windowTo := windowFrom.AddDate(0, 0, 1)

	listTab := func(status string) countsdomain.ShiftingExecutionPage {
		page, err := repo.ListShiftingEventsPendingExecution(ctx, countsdomain.ShiftingExecutionQuery{
			TenantID: fxTenant, Status: status, Now: now,
			RaisedFrom: &windowFrom, RaisedBefore: &windowTo,
		})
		if err != nil {
			t.Fatalf("list %s tab: %v", status, err)
		}
		return page
	}
	tabLists := func(status, eventID string) bool {
		for _, row := range listTab(status).Items {
			if row.ShiftingEventID == eventID {
				return true
			}
		}
		return false
	}
	destFeedDelta := func() int64 {
		counts, err := repo.ProjectedShedCountsForFeed(ctx, countsdomain.FeedProjectedCountQuery{
			TenantID: fxTenant, TargetDate: feedTarget, Limit: 100,
		})
		if err != nil {
			t.Fatalf("feed projection: %v", err)
		}
		var delta int64
		for _, row := range counts.Items {
			if row.ShedID != nil && *row.ShedID == destShed {
				delta += row.PendingDelta
			}
		}
		return delta
	}

	story.Step("An operator raises a shifting and it lands on the read-only Pending tab",
		"RecordShiftingEvent + CreateApprovalRequest, the same two writes the mobile raise performs. "+
			"By the approve-first gate the movement is reachable ONLY through the Pending bucket, with "+
			"no primary action, and it already counts toward the destination's feed sheet.")
	eventID, requestID := raiseShiftingForDecision(t, ctx, repo, "srl-reject", []string{mover}, sourceShed, destShed, raisedAt)

	story.Assert("the raised movement is on the Pending tab", tabLists("pending", eventID), "event=%s", eventID)
	story.Assert("the raised movement is NOT in the operator work list (all)", !tabLists("all", eventID), "event=%s", eventID)
	pendingPage := listTab("pending")
	story.Assert("the Pending tab offers no action on it", len(pendingPage.Items) == 1 && pendingPage.Items[0].PrimaryActionKey == "none",
		"items=%d action=%q", len(pendingPage.Items), func() string {
			if len(pendingPage.Items) == 0 {
				return ""
			}
			return pendingPage.Items[0].PrimaryActionKey
		}())
	story.Assert("the Pending tab count matches what it lists", pendingPage.StatusCounts.Pending == 1, "pending=%d", pendingPage.StatusCounts.Pending)
	deltaBefore := destFeedDelta()
	story.Assert("the raised movement already feeds the destination shed (+1 head)", deltaBefore == 1, "delta=%d", deltaBefore)

	story.Step("The approver REJECTS the movement",
		"DecideApprovalRequest(rejected) with a reason. The decision transaction must flip the "+
			"shifting_events row itself: authorization_state and event_status both record the refusal.")
	rejectedAt := time.Date(2026, 8, 25, 11, 0, 0, 0, biztime.DefaultLocation())
	decided, replay, err := repo.DecideApprovalRequest(ctx, countsdomain.ApprovalDecision{
		TenantID: fxTenant, ApprovalRequestID: requestID,
		Status: countsdomain.ApprovalStatusRejected, DecidedByUserID: fxParty, DecidedAt: rejectedAt,
		Reason:         "wrong destination pen, raise it again",
		IdempotencyKey: "decide-srl-reject", RequestFingerprint: "decide-fp-srl-reject",
	})
	story.Assert("the rejection succeeded", err == nil, "err=%v", err)
	story.Assert("the rejection is a fresh decision, not a replay", !replay, "replay=%v", replay)
	story.Assert("the approval request records rejected", decided.Status == countsdomain.ApprovalStatusRejected, "status=%s", decided.Status)

	var authState, eventStatus string
	if err := fx.Pool.QueryRow(ctx, `
SELECT authorization_state, event_status FROM shifting_events WHERE tenant_id=$1 AND shifting_event_id=$2`,
		fxTenant, eventID).Scan(&authState, &eventStatus); err != nil {
		t.Fatalf("read shifting event: %v", err)
	}
	story.Assert("the movement itself records the refusal (authorization_state=rejected)", authState == "rejected", "authorization_state=%s", authState)
	story.Assert("the movement itself records the refusal (event_status=rejected)", eventStatus == countsdomain.ShiftingEventStatusRejected, "event_status=%s", eventStatus)

	story.Step("The rejected movement leaves EVERY Actions surface",
		"It is gone from the Pending tab (the reported defect), from the work list, from every status "+
			"tab and its count, from the previous-dates badge — and it stops feeding the destination shed.")
	for _, status := range []string{"pending", "all", "authorized", "rework", "completed"} {
		story.Assert("the "+status+" tab no longer lists it", !tabLists(status, eventID), "event=%s", eventID)
	}
	countsAfter := listTab("all").StatusCounts
	story.Assert("every tab count reads zero for the raise date",
		countsAfter.All == 0 && countsAfter.Pending == 0 && countsAfter.Authorized == 0 &&
			countsAfter.Rework == 0 && countsAfter.Completed == 0,
		"counts=%+v", countsAfter)
	prev := listTab("all").PreviousDates
	story.Assert("the previous-dates badge advertises no outstanding work", len(prev) == 0, "previous_dates=%+v", prev)
	deltaAfter := destFeedDelta()
	story.Assert("rejection stopped the feed clock (destination delta back to 0)", deltaAfter == 0, "delta=%d", deltaAfter)

	story.Step("A rejected movement is terminal",
		"An exact reject replay returns the original decision applying nothing; a contrary approve "+
			"conflicts; and completion is refused writing nothing — the animal never moves.")
	var versionBefore int
	if err := fx.Pool.QueryRow(ctx, `SELECT row_version FROM shifting_events WHERE tenant_id=$1 AND shifting_event_id=$2`,
		fxTenant, eventID).Scan(&versionBefore); err != nil {
		t.Fatalf("read row_version: %v", err)
	}
	_, replay2, errReplay := repo.DecideApprovalRequest(ctx, countsdomain.ApprovalDecision{
		TenantID: fxTenant, ApprovalRequestID: requestID,
		Status: countsdomain.ApprovalStatusRejected, DecidedByUserID: fxParty, DecidedAt: rejectedAt,
		Reason:         "wrong destination pen, raise it again",
		IdempotencyKey: "decide-srl-reject", RequestFingerprint: "decide-fp-srl-reject",
	})
	story.Assert("an exact reject replay succeeds as a replay", errReplay == nil && replay2, "err=%v replay=%v", errReplay, replay2)
	var versionAfter int
	if err := fx.Pool.QueryRow(ctx, `SELECT row_version FROM shifting_events WHERE tenant_id=$1 AND shifting_event_id=$2`,
		fxTenant, eventID).Scan(&versionAfter); err != nil {
		t.Fatalf("read row_version after replay: %v", err)
	}
	story.Assert("the replay wrote nothing to the movement (row_version unchanged)", versionAfter == versionBefore,
		"before=%d after=%d", versionBefore, versionAfter)

	_, _, errFlip := repo.DecideApprovalRequest(ctx, countsdomain.ApprovalDecision{
		TenantID: fxTenant, ApprovalRequestID: requestID,
		Status: countsdomain.ApprovalStatusApproved, DecidedByUserID: fxParty, DecidedAt: rejectedAt,
		IdempotencyKey: "decide-srl-flip", RequestFingerprint: "decide-fp-srl-flip",
		Effect: &countsdomain.ApprovalEffect{Shifting: &countsdomain.ShiftingApprovalEffect{
			ShiftingEventID: eventID, DestinationParkID: fxPark, DestinationShedID: destShed, GoatIDs: []string{mover},
		}},
	})
	story.Assert("approving after the rejection conflicts", errors.Is(errFlip, countsports.ErrApprovalAlreadyDecided), "err=%v", errFlip)

	_, _, errComplete := completeShiftingE2E(repo, ctx, "srl-reject", eventID, "", now)
	story.Assert("completing a rejected movement is refused", errors.Is(errComplete, countsports.ErrShiftingNotAuthorized), "err=%v", errComplete)
	residue := fx.countRows(`
SELECT count(*) FROM shifting_events
WHERE tenant_id=$1 AND shifting_event_id=$2
  AND (proof_ref IS NOT NULL OR completed_at IS NOT NULL OR applied_at IS NOT NULL)`, fxTenant, eventID)
	story.Assert("the refused completion wrote nothing", residue == 0, "rows_with_completion_state=%d", residue)
	shedNow := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("the animal never moved", shedNow == sourceShed, "shed=%s", shedNow)

	story.Step("The rejection strands nothing: the same animals raise again and move normally",
		"A fresh raise of the same movement under a new key flows raise -> approve -> complete; the "+
			"animals relocate and the new movement shows as completed while the rejected one stays gone.")
	secondRaisedAt := time.Date(2026, 8, 21, 9, 0, 0, 0, biztime.DefaultLocation())
	eventID2 := recordAndApproveShifting(t, ctx, repo, "srl-retry", []string{mover}, sourceShed, destShed, secondRaisedAt)
	_, _, errComplete2 := completeShiftingE2E(repo, ctx, "srl-retry", eventID2, "", now)
	story.Assert("the re-raised movement completes", errComplete2 == nil, "err=%v", errComplete2)
	shedFinal := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("the animal reached the destination through the normal path", shedFinal == destShed, "shed=%s", shedFinal)

	windowFrom2 := time.Date(2026, 8, 21, 0, 0, 0, 0, biztime.DefaultLocation())
	windowTo2 := windowFrom2.AddDate(0, 0, 1)
	page2, err := repo.ListShiftingEventsPendingExecution(ctx, countsdomain.ShiftingExecutionQuery{
		TenantID: fxTenant, Status: "completed", Now: now,
		RaisedFrom: &windowFrom2, RaisedBefore: &windowTo2,
	})
	if err != nil {
		t.Fatalf("list completed tab: %v", err)
	}
	completedListed := false
	for _, row := range page2.Items {
		if row.ShiftingEventID == eventID2 {
			completedListed = true
		}
	}
	story.Assert("the re-raised movement shows on the completed tab", completedListed, "event=%s", eventID2)
	story.Assert("the rejected movement is still gone from every tab", !tabLists("pending", eventID) && !tabLists("all", eventID), "event=%s", eventID)
}

// raiseShiftingForDecision records a pending shifting event and submits its approval request —
// the two production writes of a mobile raise — returning both ids so the story can decide the
// request either way.
func raiseShiftingForDecision(
	t *testing.T, ctx context.Context, repo *countspg.Repository, key string, goatIDs []string,
	sourceShed, destShed string, at time.Time,
) (shiftingEventID, approvalRequestID string) {
	t.Helper()
	src := sourceShed
	shiftingEventID, _, err := repo.RecordShiftingEvent(ctx, countsdomain.ShiftingEvent{
		TenantID: fxTenant, LogicalShiftingEventKey: key, Priority: "low", Category: "growth",
		SourceParkID: shParkPtr(), SourceShedID: &src,
		DestinationParkID: fxPark, DestinationShedID: destShed,
		RaisedAt: at, EffectiveAt: at,
		AuthorizationState: "pending", VerificationState: "unverified", EventStatus: "pending",
		SourceSystem: "goatos_canonical", SourceRef: "e2e:" + key,
		PayloadHash: "hash-" + key, IdempotencyKey: "idem-" + key, RequestFingerprint: "fp-" + key,
		Impacts: []countsdomain.ShiftingEventImpact{{
			GrainKey: destShed + ":beetal", BreedKey: "beetal", BreedLabel: "Beetal",
			HeadCount: int32(len(goatIDs)), RiskFlagsJSON: []byte("{}"),
		}},
	})
	if err != nil {
		t.Fatalf("record shifting event %s: %v", key, err)
	}
	payload, err := json.Marshal(map[string]any{
		"shifting_event_id":   shiftingEventID,
		"destination_park_id": fxPark,
		"destination_shed_id": destShed,
		"goat_ids":            goatIDs,
	})
	if err != nil {
		t.Fatalf("marshal shifting approval payload %s: %v", key, err)
	}
	req, _, err := repo.CreateApprovalRequest(ctx, countsdomain.ApprovalRequestSubmission{
		TenantID: fxTenant, RequestType: countsdomain.ApprovalRequestTypeShifting,
		Payload: payload, ShiftingEventID: &shiftingEventID,
		RaisedByUserID: fxParty, RaisedAt: at,
		IdempotencyKey: "submit-" + key, RequestFingerprint: "submit-fp-" + key,
	})
	if err != nil {
		t.Fatalf("submit shifting approval %s: %v", key, err)
	}
	return shiftingEventID, req.ApprovalRequestID
}
