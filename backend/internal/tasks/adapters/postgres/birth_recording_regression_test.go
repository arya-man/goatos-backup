package postgres

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

func TestBirthReshootPreservesFollowingDeadline(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	id := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthMother, wfDam)
	detail, err := repo.GetWorkflow(ctx, wfTenant, id, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	var first domain.WorkflowAction
	for _, a := range detail.Actions {
		if a.Seq > 5 {
			continue
		}
		key := "review-" + a.ActionKey
		var r domain.ActionWriteResult
		if a.ActionType == domain.ActionTypeQuestion {
			r, err = repo.AnswerAction(ctx, domain.AnswerActionCommand{TenantID: wfTenant, WorkflowID: id, ActionID: a.ActionID, AnswerValue: "yes", ProofRef: key, AnsweredBy: wfCustodian, AnsweredAt: wfEventAt, IdempotencyKey: key, RequestFingerprint: key})
		} else {
			r, err = repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: id, ActionID: a.ActionID, ProofRef: key, CompletedBy: wfCustodian, CompletedAt: wfEventAt, IdempotencyKey: key, RequestFingerprint: key})
		}
		if err != nil {
			t.Fatal(err)
		}
		if a.ActionKey == domain.ActionKeyORSWater1 {
			first = r.Action
		}
	}
	err = repo.ApplyBirthStepVerdict(ctx, ports.BirthStepVerdictCommand{TenantID: wfTenant, ActionID: first.ActionID, RecordingKey: domain.BirthStepReviewKey(first), Approved: false, Reason: "blurred video"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: id, ActionID: first.ActionID, ProofRef: "reshoot", CompletedBy: wfCustodian, CompletedAt: wfEventAt.Add(40 * time.Minute), IdempotencyKey: "reshoot", RequestFingerprint: "reshoot"})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = repo.GetWorkflow(ctx, wfTenant, id, wfEventAt.Add(50*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range detail.Actions {
		if a.ActionKey == domain.ActionKeyORSWater2 && (a.DueAt == nil || !a.DueAt.Equal(wfEventAt.Add(50*time.Minute))) {
			t.Fatalf("evidence-only reshoot moved round two deadline: got %v, want %v", a.DueAt, wfEventAt.Add(50*time.Minute))
		}
	}
}

func TestBirthDelayedIdentifierEventPreservesRecordingKey(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	id := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	// Arrange legal per-step state: prior steps recorded, one rejected, tagging still pending.
	_, err := pool.Exec(ctx, `UPDATE workflow_actions SET status='completed' WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid AND action_key <> $3`, wfTenant, id, domain.ActionKeyTagTheKid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE workflow_actions SET status='rework' WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid AND action_key=$3`, wfTenant, id, domain.ActionKeyKidClean)
	if err != nil {
		t.Fatal(err)
	}
	d, err := repo.GetWorkflow(ctx, wfTenant, id, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	tagID := actionIDByKey(t, d, domain.ActionKeyTagTheKid)
	recorded, err := repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: id, ActionID: tagID, ProofRef: "tag-proof", CompletedBy: wfCustodian, CompletedAt: wfEventAt.Add(72 * time.Hour), IdempotencyKey: "tag-record", RequestFingerprint: "tag-record"})
	if err != nil {
		t.Fatal(err)
	}
	key := domain.BirthStepReviewKey(recorded.Action)
	if err := repo.CompleteTagActionForGoat(ctx, wfTenant, wfKid, wfEventAt); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyBirthStepVerdict(ctx, ports.BirthStepVerdictCommand{TenantID: wfTenant, ActionID: tagID, RecordingKey: key, Approved: true}); err != nil {
		t.Fatal(err)
	}
	d, err = repo.GetWorkflow(ctx, wfTenant, id, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range d.Actions {
		if a.ActionID == tagID && a.Status != domain.ActionStatusCompleted {
			t.Fatalf("approved tagging clip stranded after identifier event: status=%s, recording=%s, current=%s", a.Status, key, domain.BirthStepReviewKey(a))
		}
	}
}
