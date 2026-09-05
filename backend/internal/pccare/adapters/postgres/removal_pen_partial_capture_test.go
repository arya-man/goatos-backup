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
