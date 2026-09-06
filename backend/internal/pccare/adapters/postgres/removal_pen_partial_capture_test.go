package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// PARTIAL CAPTURE IS ALLOWED, AND SUBMIT IS WHERE BOTH VIDEOS ARE DEMANDED.
//
// Reported as a P1 in review on 2026-09-05 — "the pair CHECK refuses the first slot, so the
// round-grain removal flow is unusable" — and closed as working-as-designed. This test is the
// reason it should not be raised a third time: it pins all three behaviours at once, on a real
// database, so a change that actually breaks any of them goes red instead of being argued
// about. See context/repo-audits/pc-care-rounds-do-not-reopen-ledger.md.
func TestRemovalPenPartialCaptureIsAllowedAndSubmitStillDemandsBoth(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)

	round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark,
		Pens:                   []domain.RoundPen{{ShedID: pcShedA}},
		PlannedBusinessDate:    pcBusinessDay(2026, 9, 18),
		AssigneeUserIDs:        []string{pcOperator1},
		FeedRemovalRequired:    true,
		RemovalOperatorUserIDs: []string{pcOperator2},
		IdempotencyKey:         "removal-partial-capture",
		CreatedBy:              pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	var removalTaskID string
	if err := pool.QueryRow(ctx, `
SELECT task_id::text FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND gates_round_id = $2::uuid`, pcTenant, round.RoundID).Scan(&removalTaskID); err != nil {
		t.Fatalf("read removal card: %v", err)
	}
	gatedTaskID := round.Pens[0].TaskID

	// 1. THE FEED VIDEO ALONE SAVES. The operator shoots one, walks the pen, shoots the other;
	//    a table that refused this would make the whole flow unusable, which is what the review
	//    believed. A CHECK passes unless it evaluates to FALSE, and the unset side is NULL.
	if err := repo.RegisterRemovalPenProof(ctx, ports.RegisterRemovalPenProofParams{
		TenantID: pcTenant, RemovalTaskID: removalTaskID, GatedTaskID: gatedTaskID,
		SlotKey: domain.SlotFeedVideo, ProofRef: "proof-feed-1",
		CapturedBy: pcOperator2, IdempotencyKey: "pen-feed-alone", ActorID: pcOperator2,
	}); err != nil {
		t.Fatalf("feed video alone must save, got: %v", err)
	}
	pens, err := repo.ListRemovalPenProofs(ctx, pcTenant, removalTaskID)
	if err != nil {
		t.Fatalf("ListRemovalPenProofs: %v", err)
	}
	if len(pens) != 1 || pens[0].FeedProofRef != "proof-feed-1" || pens[0].WaterProofRef != "" {
		t.Fatalf("after the feed video the row = %+v, want feed set and water empty", pens)
	}

	// 2. SUBMITTING WITH ONE VIDEO IS REFUSED. A pen mid-capture is fine; a pen SUBMITTED with
	//    one video is a lie about the evening's work.
	_, err = repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: removalTaskID, SubmittedBy: pcOperator2,
		IdempotencyKey: "removal-submit-partial", ActorID: pcOperator2,
	})
	if !errors.Is(err, domain.ErrRemovalProofIncomplete) {
		t.Fatalf("submit with one video err = %v, want ErrRemovalProofIncomplete", err)
	}

	// 3. THE CONSTRAINT IS NOT VACUOUS. An EMPTY ref is the abuse it exists to stop — a client
	//    "recording" a slot with a blank reference — and it still fails at the database.
	if _, err := pool.Exec(ctx, `
UPDATE pc_care_removal_pen_proofs SET feed_proof_ref = '', water_proof_ref = ''
WHERE tenant_id = $1::uuid AND removal_task_id = $2::uuid`, pcTenant, removalTaskID); err == nil {
		t.Fatal("an empty proof ref must still violate pc_care_removal_pen_proofs_pair")
	} else if !strings.Contains(err.Error(), "pc_care_removal_pen_proofs_pair") {
		t.Fatalf("empty refs failed for the wrong reason: %v", err)
	}

	// 4. WITH BOTH VIDEOS THE SUBMIT SUCCEEDS, and every pen goes to pending_verification.
	if err := repo.RegisterRemovalPenProof(ctx, ports.RegisterRemovalPenProofParams{
		TenantID: pcTenant, RemovalTaskID: removalTaskID, GatedTaskID: gatedTaskID,
		SlotKey: domain.SlotWaterVideo, ProofRef: "proof-water-1",
		CapturedBy: pcOperator2, IdempotencyKey: "pen-water", ActorID: pcOperator2,
	}); err != nil {
		t.Fatalf("water video: %v", err)
	}
	result, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: removalTaskID, SubmittedBy: pcOperator2,
		IdempotencyKey: "removal-submit-complete", ActorID: pcOperator2,
	})
	if err != nil {
		t.Fatalf("submit with both videos: %v", err)
	}
	if result.Status != domain.StatusPendingVerification {
		t.Fatalf("submitted status = %q, want pending_verification", result.Status)
	}
	pens, _ = repo.ListRemovalPenProofs(ctx, pcTenant, removalTaskID)
	for _, pen := range pens {
		if pen.Status != domain.StatusPendingVerification {
			t.Fatalf("pen %s status = %q, want pending_verification", pen.PenLabel, pen.Status)
		}
	}
}

func TestRemovalPenProofsStayPartitionGrainedInsideOneRemovalCard(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)

	round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark,
		Pens: []domain.RoundPen{
			{ShedID: pcShedA, PartitionLabel: "Part 1"},
			{ShedID: pcShedA, PartitionLabel: "Part 2"},
		},
		PlannedBusinessDate:    pcBusinessDay(2026, 9, 19),
		AssigneeUserIDs:        []string{pcOperator1},
		FeedRemovalRequired:    true,
		RemovalOperatorUserIDs: []string{pcOperator2},
		IdempotencyKey:         "removal-partition-grain",
		CreatedBy:              pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	var removalTaskID string
	if err := pool.QueryRow(ctx, `
SELECT task_id::text FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND gates_round_id = $2::uuid`, pcTenant, round.RoundID).Scan(&removalTaskID); err != nil {
		t.Fatalf("read removal card: %v", err)
	}

	for i, pen := range round.Pens {
		if err := repo.RegisterRemovalPenProof(ctx, ports.RegisterRemovalPenProofParams{
			TenantID: pcTenant, RemovalTaskID: removalTaskID, GatedTaskID: pen.TaskID,
			SlotKey: domain.SlotFeedVideo, ProofRef: "proof-feed-part-" + pen.PartitionLabel,
			CapturedBy: pcOperator2, IdempotencyKey: "partition-feed-" + pen.TaskID, ActorID: pcOperator2,
		}); err != nil {
			t.Fatalf("feed video pen %d: %v", i, err)
		}
		if err := repo.RegisterRemovalPenProof(ctx, ports.RegisterRemovalPenProofParams{
			TenantID: pcTenant, RemovalTaskID: removalTaskID, GatedTaskID: pen.TaskID,
			SlotKey: domain.SlotWaterVideo, ProofRef: "proof-water-part-" + pen.PartitionLabel,
			CapturedBy: pcOperator2, IdempotencyKey: "partition-water-" + pen.TaskID, ActorID: pcOperator2,
		}); err != nil {
			t.Fatalf("water video pen %d: %v", i, err)
		}
	}

	pens, err := repo.ListRemovalPenProofs(ctx, pcTenant, removalTaskID)
	if err != nil {
		t.Fatalf("ListRemovalPenProofs: %v", err)
	}
	if len(pens) != 2 {
		t.Fatalf("removal pen proofs = %+v, want exactly two partition-grained rows", pens)
	}
	seen := map[string]ports.RemovalPenProofRow{}
	for _, pen := range pens {
		seen[pen.GatedTaskID] = pen
	}
	for _, pen := range round.Pens {
		got := seen[pen.TaskID]
		if got.FeedProofRef != "proof-feed-part-"+pen.PartitionLabel || got.WaterProofRef != "proof-water-part-"+pen.PartitionLabel {
			t.Fatalf("pen %s refs = %+v, want refs scoped to %s", pen.TaskID, got, pen.PartitionLabel)
		}
	}
}

func TestRemovalPenReworkSubmitDoesNotResetApprovedSiblingPen(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)

	round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark,
		Pens: []domain.RoundPen{
			{ShedID: pcShedA, PartitionLabel: "Part 1"},
			{ShedID: pcShedA, PartitionLabel: "Part 2"},
		},
		PlannedBusinessDate:    pcBusinessDay(2026, 9, 20),
		AssigneeUserIDs:        []string{pcOperator1},
		FeedRemovalRequired:    true,
		RemovalOperatorUserIDs: []string{pcOperator2},
		IdempotencyKey:         "removal-rework-sibling-isolation",
		CreatedBy:              pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	var removalTaskID string
	if err := pool.QueryRow(ctx, `
	SELECT task_id::text FROM pc_care_tasks
	WHERE tenant_id = $1::uuid AND gates_round_id = $2::uuid`, pcTenant, round.RoundID).Scan(&removalTaskID); err != nil {
		t.Fatalf("read removal card: %v", err)
	}

	for _, pen := range round.Pens {
		if err := repo.RegisterRemovalPenProof(ctx, ports.RegisterRemovalPenProofParams{
			TenantID: pcTenant, RemovalTaskID: removalTaskID, GatedTaskID: pen.TaskID,
			SlotKey: domain.SlotFeedVideo, ProofRef: "initial-feed-" + pen.PartitionLabel,
			CapturedBy: pcOperator2, IdempotencyKey: "initial-feed-" + pen.TaskID, ActorID: pcOperator2,
		}); err != nil {
			t.Fatalf("feed video pen %s: %v", pen.PartitionLabel, err)
		}
		if err := repo.RegisterRemovalPenProof(ctx, ports.RegisterRemovalPenProofParams{
			TenantID: pcTenant, RemovalTaskID: removalTaskID, GatedTaskID: pen.TaskID,
			SlotKey: domain.SlotWaterVideo, ProofRef: "initial-water-" + pen.PartitionLabel,
			CapturedBy: pcOperator2, IdempotencyKey: "initial-water-" + pen.TaskID, ActorID: pcOperator2,
		}); err != nil {
			t.Fatalf("water video pen %s: %v", pen.PartitionLabel, err)
		}
	}
	if _, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: removalTaskID, SubmittedBy: pcOperator2,
		IdempotencyKey: "removal-submit-before-rework", ActorID: pcOperator2,
	}); err != nil {
		t.Fatalf("initial submit: %v", err)
	}

	pens, err := repo.ListRemovalPenProofs(ctx, pcTenant, removalTaskID)
	if err != nil {
		t.Fatalf("ListRemovalPenProofs: %v", err)
	}
	var approvedPenID, approvedGatedTaskID, reworkPenID, reworkGatedTaskID string
	for _, pen := range pens {
		switch pen.PenLabel {
		case "Shed A - Part 1":
			approvedPenID = pen.RemovalPenID
			approvedGatedTaskID = pen.GatedTaskID
		case "Shed A - Part 2":
			reworkPenID = pen.RemovalPenID
			reworkGatedTaskID = pen.GatedTaskID
		}
	}
	if approvedPenID == "" || approvedGatedTaskID == "" || reworkPenID == "" || reworkGatedTaskID == "" {
		t.Fatalf("missing expected pens: %+v", pens)
	}
	if ok, err := repo.ApplyVerifiedRemovalPen(ctx, ports.ApplyRemovalPenVerdictParams{
		TenantID: pcTenant, RemovalPenID: approvedPenID, VerifiedBy: pcVerifier,
	}); err != nil || !ok {
		t.Fatalf("approve sibling pen ok=%v err=%v", ok, err)
	}
	if ok, err := repo.BounceRemovalPenForRework(ctx, ports.ApplyRemovalPenVerdictParams{
		TenantID: pcTenant, RemovalPenID: reworkPenID, VerifiedBy: pcVerifier, Reason: "reshoot water",
	}); err != nil || !ok {
		t.Fatalf("bounce rework pen ok=%v err=%v", ok, err)
	}

	// A stale client can resend all local clips when a round-grain card is reopened. The
	// already-approved sibling must stay completed; the resend is a no-op, not a reopen.
	if err := repo.RegisterRemovalPenProof(ctx, ports.RegisterRemovalPenProofParams{
		TenantID: pcTenant, RemovalTaskID: removalTaskID, GatedTaskID: approvedGatedTaskID,
		SlotKey: domain.SlotFeedVideo, ProofRef: "stale-feed-for-approved-sibling",
		CapturedBy: pcOperator2, IdempotencyKey: "stale-approved-feed-resend", ActorID: pcOperator2,
	}); err != nil {
		t.Fatalf("stale resend for approved sibling should no-op, got: %v", err)
	}
	var staleStatus string
	if err := pool.QueryRow(ctx, `
SELECT status FROM idempotency_keys
WHERE idempotency_key = $1`, pcTenant+":pc_care.animal.slot:stale-approved-feed-resend").Scan(&staleStatus); err != nil {
		t.Fatalf("read stale resend idempotency: %v", err)
	}
	if staleStatus != "completed" {
		t.Fatalf("stale resend idempotency status = %q, want completed", staleStatus)
	}

	if err := repo.RegisterRemovalPenProof(ctx, ports.RegisterRemovalPenProofParams{
		TenantID: pcTenant, RemovalTaskID: removalTaskID, GatedTaskID: reworkGatedTaskID,
		SlotKey: domain.SlotWaterVideo, ProofRef: "reshoot-water-Part 2",
		CapturedBy: pcOperator2, IdempotencyKey: "reshoot-water-part-2", ActorID: pcOperator2,
	}); err != nil {
		t.Fatalf("reshoot water video: %v", err)
	}
	if _, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: removalTaskID, SubmittedBy: pcOperator2,
		IdempotencyKey: "removal-submit-after-rework", ActorID: pcOperator2,
	}); err != nil {
		t.Fatalf("rework submit: %v", err)
	}

	pens, err = repo.ListRemovalPenProofs(ctx, pcTenant, removalTaskID)
	if err != nil {
		t.Fatalf("ListRemovalPenProofs after rework submit: %v", err)
	}
	for _, pen := range pens {
		switch pen.RemovalPenID {
		case approvedPenID:
			if pen.Status != domain.StatusCompleted {
				t.Fatalf("approved sibling status = %q, want completed", pen.Status)
			}
			if pen.FeedProofRef != "initial-feed-Part 1" {
				t.Fatalf("approved sibling feed ref = %q, want original ref", pen.FeedProofRef)
			}
		case reworkPenID:
			if pen.Status != domain.StatusPendingVerification {
				t.Fatalf("reworked pen status = %q, want pending_verification", pen.Status)
			}
		}
	}
}
