package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// Reconcile SOP parity (bugs 3 and 4, 2026-09-16): the workflow-completion path carries every
// proof's step title and register kind plus every answer, names its own workflow so the
// store accepts it, and the durable recovery re-enqueues the SAME full set.

func TestPenReconciliationWorkflowCompletionCarriesTitlesKindsAndAnswers(t *testing.T) {
	park := "park-1"
	repo := &fakePenReconciliationRepo{cardIDByWorkflow: "card-1", completeResult: domain.PenReconciliationCompletionResult{
		CardID: "card-1", Status: domain.PenReconciliationStatusPendingVerification, ScannedIdentifier: "1420 0001",
		RegisteredShedID: "shed-1", RegisteredShedName: "Mandela 11", RegisteredPartitionLabel: "Part 2", ParkID: &park,
		NeedsVerificationEnqueue: true,
	}}
	enq := &fakePenReconciliationEnqueuer{}
	svc := NewPenReconciliationService(repo, func() time.Time { return time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC) }).WithVerificationEnqueuer(enq)
	op := "op-1"
	answer := "yes"
	video, photo := "v-return", "p-tag"
	actions := []tasksdomain.WorkflowAction{
		{ActionKey: "tag_visible", Seq: 2, ActionType: tasksdomain.ActionTypeQuestion, Title: "Is the tag visible?", AnswerType: tasksdomain.AnswerKindYesNo,
			Status: tasksdomain.ActionStatusCompleted, AnswerValue: &answer, CompletedBy: &op, ProofRefs: []tasksdomain.ProofItem{{Ref: photo, Kind: "photo"}}},
		{ActionKey: "return_to_pen", Seq: 1, ActionType: tasksdomain.ActionTypeAction, Title: "Return the animal to its registered pen",
			Status: tasksdomain.ActionStatusCompleted, CompletedBy: &op, ProofRef: &video, ProofRefs: []tasksdomain.ProofItem{{Ref: video, Kind: "video"}}},
	}
	if err := svc.OnWorkflowCompleted(context.Background(), tasksdomain.WorkflowInstance{TenantID: "tenant-1", WorkflowID: "wf-1"}, actions); err != nil {
		t.Fatalf("OnWorkflowCompleted: %v", err)
	}
	if len(repo.completeCalls) != 1 {
		t.Fatalf("complete calls = %d", len(repo.completeCalls))
	}
	cmd := repo.completeCalls[0]
	if cmd.WorkflowID != "wf-1" {
		t.Fatalf("the completion must name its workflow so a workflow-backed card accepts it, got %q", cmd.WorkflowID)
	}
	if !reflect.DeepEqual(cmd.ProofRefs, []string{video, photo}) || cmd.ProofRef != video {
		t.Fatalf("refs = %q %v, want seq order", cmd.ProofRef, cmd.ProofRefs)
	}
	wantMeta := []domain.PenReconciliationProofMeta{{Label: "Return the animal to its registered pen", Kind: "video"}, {Label: "Is the tag visible?", Kind: "photo"}}
	if !reflect.DeepEqual(cmd.MediaMeta, wantMeta) {
		t.Fatalf("meta = %+v, want %+v", cmd.MediaMeta, wantMeta)
	}
	wantRows := []domain.PenReconciliationContextRow{{Label: "Is the tag visible?", Value: "Yes", Group: "Is the tag visible?"}}
	if !reflect.DeepEqual(cmd.ContextRows, wantRows) {
		t.Fatalf("rows = %+v", cmd.ContextRows)
	}
	if len(enq.calls) != 1 || !reflect.DeepEqual(enq.calls[0].MediaMeta, wantMeta) || !reflect.DeepEqual(enq.calls[0].ContextRows, wantRows) ||
		!reflect.DeepEqual(enq.calls[0].MediaRefs, []string{video, photo}) {
		t.Fatalf("enqueue = %+v", enq.calls)
	}
}

func TestPenReconciliationRecoveryReEnqueuesEveryProofWithMeta(t *testing.T) {
	meta := []domain.PenReconciliationProofMeta{{Label: "Return", Kind: "video"}, {Label: "Tag", Kind: "photo"}}
	rows := []domain.PenReconciliationContextRow{{Label: "Is the tag visible?", Value: "Yes", Group: "Is the tag visible?"}}
	repo := &fakePenReconciliationRepo{debts: []domain.PenReconciliationVerificationEnqueueDebt{{
		CardID: "card-1", ScannedIdentifier: "1420 0001", RegisteredShedID: "shed-1", RegisteredShedName: "Mandela 11",
		ProofRef: "v", ProofRefs: []string{"v", "p"}, MediaMeta: meta, ContextRows: rows,
		CompletedBy: "op", CompletedAt: time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC),
	}}}
	enq := &fakePenReconciliationEnqueuer{}
	svc := NewPenReconciliationService(repo, nil).WithVerificationEnqueuer(enq)
	if n, err := svc.RecoverVerificationEnqueues(context.Background(), "tenant-1", 10); err != nil || n != 1 {
		t.Fatalf("recover: n=%d err=%v", n, err)
	}
	got := enq.calls[0]
	if !reflect.DeepEqual(got.MediaRefs, []string{"v", "p"}) || !reflect.DeepEqual(got.MediaMeta, meta) || !reflect.DeepEqual(got.ContextRows, rows) {
		t.Fatalf("recovery lost the proof set: %+v", got)
	}
	if got.IdempotencyKey != "counts-pen-reconciliation-verification:card-1:v" {
		t.Fatalf("key = %q (must stay the completion's key)", got.IdempotencyKey)
	}
}

// TestSeededReconcileQuestionnaireBehavesAsToday pins the deploy-day rule: the seeded reconcile
// document (one "return to pen" video) enqueues the same item key and the same single proof as
// before; the only differences are the confirmed workflow-completion fix and the step title on the
// proof label.
func TestSeededReconcileQuestionnaireBehavesAsToday(t *testing.T) {
	repo := &fakePenReconciliationRepo{cardIDByWorkflow: "card-1", completeResult: domain.PenReconciliationCompletionResult{
		CardID: "card-1", Status: domain.PenReconciliationStatusPendingVerification, ScannedIdentifier: "1420 0001",
		RegisteredShedID: "shed-1", RegisteredShedName: "Mandela 11", NeedsVerificationEnqueue: true,
	}}
	enq := &fakePenReconciliationEnqueuer{}
	svc := NewPenReconciliationService(repo, nil).WithVerificationEnqueuer(enq)
	op, video := "op-1", "v-return"
	step := tasksdomain.WorkflowAction{ActionKey: "return_to_pen", Seq: 1, ActionType: tasksdomain.ActionTypeAction, Title: "Return the animal to its registered pen",
		Status: tasksdomain.ActionStatusCompleted, CompletedBy: &op, ProofRef: &video, ProofRefs: []tasksdomain.ProofItem{{Ref: video, Kind: "video"}}}
	if err := svc.OnWorkflowCompleted(context.Background(), tasksdomain.WorkflowInstance{TenantID: "t", WorkflowID: "wf-1"}, []tasksdomain.WorkflowAction{step}); err != nil {
		t.Fatal(err)
	}
	got := enq.calls[0]
	if got.IdempotencyKey != "counts-pen-reconciliation-verification:card-1:v-return" || !reflect.DeepEqual(got.MediaRefs, []string{"v-return"}) {
		t.Fatalf("seeded reconcile item = %+v", got)
	}
	if len(got.ContextRows) != 0 {
		t.Fatalf("the seeded step asks nothing: %+v", got.ContextRows)
	}
}
