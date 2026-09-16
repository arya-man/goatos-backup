package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

func TestBirthOldRejectionMustNotRejectReshoot(t *testing.T) {
	svc, repo, _, workflowID := newServiceWithMotherWorkflow(t)
	first := recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyBabiesStillInside, "old-proof", "first-recording")
	handler := NewBirthVerificationHandler(svc, nil)
	event := birthVerdictEvent(t, EventVerificationVerdictRework, domain.VerificationRefTypeBirthAction, first.Action.ActionID, "old clip rejected", domain.BirthStepReviewKey(first.Action))
	if err := handler.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyBabiesStillInside, "old-proof", "reshoot")
	if err := handler.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	got := actionByKeyT(t, repo, workflowID, domain.ActionKeyBabiesStillInside)
	if got.Status != domain.ActionStatusInReview || len(got.AllProofRefs()) != 1 || got.AllProofRefs()[0] != "old-proof" {
		t.Fatalf("old rejection changed new recording: status=%s proofs=%v", got.Status, got.AllProofRefs())
	}
}

func TestBirthPhotoMetadataSurvivesProducer(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithMotherWorkflow(t)
	action := actionByKeyT(t, repo, workflowID, domain.ActionKeyBabiesStillInside)
	for i := range repo.actions[workflowID] {
		a := &repo.actions[workflowID][i]
		if a.ActionID == action.ActionID {
			a.RequiresVideo = false
			a.ProofMinVideos = 0
			a.ProofMinPhotos = 1
		}
	}
	_, err := svc.AnswerAction(context.Background(), AnswerActionInput{
		TenantID: testTenant, WorkflowID: workflowID, ActionID: action.ActionID, AnswerValue: "yes",
		Proofs:         []domain.ProofItem{{Ref: "photo-proof", Kind: domain.ProofKindPhoto}},
		IdempotencyKey: "photo-recording", RequestFingerprint: "photo-recording",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.birthCalls) != 1 {
		t.Fatalf("enqueue calls=%d", len(enq.birthCalls))
	}
	got := enq.birthCalls[0]
	if len(got.Proofs) != 1 || got.Proofs[0].Kind != domain.ProofKindPhoto || got.ProofLabel != action.Title {
		t.Fatalf("producer lost proof metadata: %+v", got)
	}
}
