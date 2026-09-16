package postgres

// Death FOLLOWS THE SOP (maintainer decision 1, 2026-09-16): the approval gate waits for EVERY
// authored step, the release read carries every proof with its title/kind and every answer, and
// a rework reopens every proof step (answers kept). Opt-in like every DB test.

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// addAuthoredDeathStep stamps one more operator step on an open death workflow, as a published
// SOP with a third step would have at open (the seeded document carries only the two videos).
func addAuthoredDeathStep(t *testing.T, repo *Repository, ctx context.Context, workflowID, key, title, actionType, answerType string, minPhotos int) {
	t.Helper()
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO workflow_actions (
  tenant_id, workflow_id, action_key, seq, section, action_type, title, detail, requires_video, options, due_at,
  task_type, answer_type, engine_hook, proof_min_videos, proof_min_photos, hard_time_gate, wait_for_all,
  requires_keys, after_action_key, after_offset_seconds
) VALUES ($1::uuid, $2::uuid, $3, 3, 'main', $4, $5, '', false, NULL, now(),
  'question', $6, '', 0, $7, false, false, '[]'::jsonb, '', 0)`,
		wfTenant, workflowID, key, actionType, title, answerType, minPhotos); err != nil {
		t.Fatalf("add authored step: %v", err)
	}
	// The internal sign-off stays last.
	if _, err := repo.pool.Exec(ctx, `UPDATE workflow_actions SET seq = 4 WHERE workflow_id = $1::uuid AND action_key = $2`,
		workflowID, domain.ActionKeyParkHeadSignoff); err != nil {
		t.Fatalf("reorder sign-off: %v", err)
	}
}

func completeDeathVideosPg(t *testing.T, repo *Repository, ctx context.Context, workflowID string, round string) {
	t.Helper()
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo} {
		if _, err := repo.CompleteAction(ctx, domain.CompleteActionCommand{
			TenantID: wfTenant, WorkflowID: workflowID, ActionID: actionIDByKey(t, detail, key),
			Proofs: []domain.ProofItem{{Ref: "proof-" + key, Kind: domain.ProofKindVideo}}, CompletedBy: wfCustodian,
			CompletedAt: wfEventAt.UTC(), IdempotencyKey: round + "-" + key, RequestFingerprint: "fp-" + round + "-" + key,
		}); err != nil {
			t.Fatalf("complete %s: %v", key, err)
		}
	}
}

func prepareDeathApproval(t *testing.T, repo *Repository, ctx context.Context) bool {
	t.Helper()
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := repo.PrepareDeathEvidenceForApprovalInTx(ctx, tx, wfTenant, wfDead)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("prepare: %v", err)
	}
	if !ready {
		_ = tx.Rollback(ctx)
		return false
	}
	if _, err := tx.Exec(ctx, `UPDATE goats SET lifecycle_status='dead', exit_reason='died', exited_at=now() WHERE goat_id=$1::uuid`, wfDead); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return true
}

func TestDeathApprovalWaitsForEveryAuthoredStepPg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyDeath, wfDead)
	addAuthoredDeathStep(t, repo, ctx, workflowID, "likely_cause", "Likely cause?", domain.ActionTypeQuestion, domain.AnswerKindText, 0)
	completeDeathVideosPg(t, repo, ctx, workflowID, "r1")
	if prepareDeathApproval(t, repo, ctx) {
		t.Fatal("approval must WAIT for the authored question step: the two videos alone are not the SOP")
	}
	detail, _ := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if _, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID, ActionID: actionIDByKey(t, detail, "likely_cause"),
		AnswerValue: "bloat", AnsweredBy: wfCustodian, AnsweredAt: wfEventAt.UTC(), IdempotencyKey: "a1", RequestFingerprint: "fp-a1",
	}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if !prepareDeathApproval(t, repo, ctx) {
		t.Fatal("every authored step done: approval must proceed")
	}
}

func TestDeathEvidenceForVerificationReadsAllProofsPg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	capture := authored.Evidence{VersionLabel: "v2", Media: []authored.EvidenceMedia{{Ref: "c-tag", Kind: "photo", Label: "Animal with tag"}},
		Rows: []authored.EvidenceRow{{Label: "Found where?", Value: "Trough"}}}
	if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeyDeath, SubjectGoatID: wfDead, EventAt: wfEventAt, CaptureEvidence: capture}); err != nil {
		t.Fatal(err)
	}
	workflowID := findWorkflowID(t, repo, ctx, domain.TemplateKeyDeath, wfDead)
	addAuthoredDeathStep(t, repo, ctx, workflowID, "found_where", "Where was it found?", domain.ActionTypeQuestion, domain.AnswerKindText, 1)
	completeDeathVideosPg(t, repo, ctx, workflowID, "r1")
	detail, _ := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if _, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID, ActionID: actionIDByKey(t, detail, "found_where"),
		AnswerValue: "In the pen", Proofs: []domain.ProofItem{{Ref: "p-pen", Kind: domain.ProofKindPhoto}},
		AnsweredBy: wfCustodian, AnsweredAt: wfEventAt.UTC(), IdempotencyKey: "a1", RequestFingerprint: "fp-a1",
	}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if !prepareDeathApproval(t, repo, ctx) {
		t.Fatal("approval must proceed")
	}
	review, err := repo.DeathEvidenceForVerification(ctx, wfTenant, wfDead)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !reflect.DeepEqual(review.Workflow.CaptureEvidence, capture) {
		t.Fatalf("capture snapshot = %+v, want %+v (round-tripped through jsonb)", review.Workflow.CaptureEvidence, capture)
	}
	if review.OperatorID != wfCustodian {
		t.Fatalf("operator = %q", review.OperatorID)
	}
	bundle := domain.DeathEvidenceBundle(review.Workflow.CaptureEvidence, review.Actions)
	wantRefs := []string{"c-tag", "proof-death_video", "proof-post_mortem_video", "p-pen"}
	if !reflect.DeepEqual(bundle.Refs, wantRefs) {
		t.Fatalf("refs = %v, want %v", bundle.Refs, wantRefs)
	}
	wantMeta := []domain.MediaMetaItem{
		{Label: "At report · Animal with tag", Kind: "photo"},
		{Label: "Record death video", Kind: "video"},
		{Label: "Record post-mortem video", Kind: "video"},
		{Label: "Where was it found?", Kind: "photo"},
	}
	if !reflect.DeepEqual(bundle.Meta, wantMeta) {
		t.Fatalf("meta = %+v, want %+v", bundle.Meta, wantMeta)
	}
	wantRows := []domain.EvidenceRow{
		{Label: "Found where?", Value: "Trough", Group: "At report"},
		{Label: "Where was it found?", Value: "In the pen", Group: "Where was it found?"},
	}
	if !reflect.DeepEqual(bundle.Rows, wantRows) {
		t.Fatalf("rows = %+v, want %+v", bundle.Rows, wantRows)
	}
	// Redelivered counts.death.reported with a different snapshot: the natural key wins.
	created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeyDeath, SubjectGoatID: wfDead, EventAt: wfEventAt, CaptureEvidence: authored.Evidence{VersionLabel: "later"}})
	if err != nil || created {
		t.Fatalf("redelivery: created=%v err=%v", created, err)
	}
	if again, _ := repo.DeathEvidenceForVerification(ctx, wfTenant, wfDead); again.Workflow.CaptureEvidence.VersionLabel != "v2" {
		t.Fatal("redelivery relabelled the snapshot")
	}
}

func TestBounceDeathForReworkClearsProofRefsAndSetsReasonPg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyDeath, wfDead)
	addAuthoredDeathStep(t, repo, ctx, workflowID, "found_where", "Where was it found?", domain.ActionTypeQuestion, domain.AnswerKindText, 1)
	addAuthoredDeathStep(t, repo, ctx, workflowID, "likely_cause", "Likely cause?", domain.ActionTypeQuestion, domain.AnswerKindText, 0)
	completeDeathVideosPg(t, repo, ctx, workflowID, "r1")
	detail, _ := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	answer := func(key, value, idem string, proofs ...domain.ProofItem) {
		t.Helper()
		if _, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{
			TenantID: wfTenant, WorkflowID: workflowID, ActionID: actionIDByKey(t, detail, key), AnswerValue: value, Proofs: proofs,
			AnsweredBy: wfCustodian, AnsweredAt: wfEventAt.UTC(), IdempotencyKey: idem, RequestFingerprint: "fp-" + idem,
		}); err != nil {
			t.Fatalf("answer %s: %v", key, err)
		}
	}
	answer("found_where", "In the pen", "a1", domain.ProofItem{Ref: "p-pen", Kind: domain.ProofKindPhoto})
	answer("likely_cause", "bloat", "a2")
	if !prepareDeathApproval(t, repo, ctx) {
		t.Fatal("approval must proceed")
	}
	rework := ports.DeathVerdictCommand{TenantID: wfTenant, WorkflowID: workflowID, Reason: "photo too dark", VerdictAt: wfEventAt}
	for i := 0; i < 2; i++ {
		if err := repo.BounceDeathVideosForRework(ctx, rework); err != nil {
			t.Fatalf("rework #%d: %v", i, err)
		}
	}
	detail, _ = repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	for _, a := range detail.Actions {
		switch a.ActionKey {
		case domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo, "found_where":
			if a.Status != domain.ActionStatusRework || a.ProofRef != nil || len(a.ProofRefs) != 0 || a.CompletedAt != nil {
				t.Fatalf("%s after rework = %+v, want rework with proof_ref AND proof_refs cleared", a.ActionKey, a)
			}
			if a.ReworkReason == nil || *a.ReworkReason != "photo too dark" {
				t.Fatalf("%s must carry the verifier's reason, got %v", a.ActionKey, a.ReworkReason)
			}
		case "likely_cause":
			if a.Status != domain.ActionStatusCompleted || a.AnswerValue == nil || *a.AnswerValue != "bloat" {
				t.Fatalf("answer-only step must keep its answer: %+v", a)
			}
		}
	}
	if detail.Card.AwaitingVerification {
		t.Fatal("gate must close on rework")
	}
	// The re-answered photo step (last one back) reopens the gate and reports the enqueue.
	completeDeathVideosPg(t, repo, ctx, workflowID, "r2")
	res, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID, ActionID: actionIDByKey(t, detail, "found_where"), AnswerValue: "In the pen",
		Proofs: []domain.ProofItem{{Ref: "p-pen-2", Kind: domain.ProofKindPhoto}}, AnsweredBy: wfCustodian, AnsweredAt: wfEventAt.UTC(),
		IdempotencyKey: "a1-r2", RequestFingerprint: "fp-a1-r2",
	})
	if err != nil {
		t.Fatalf("re-answer: %v", err)
	}
	if !res.NeedsVerificationEnqueue || !res.Workflow.AwaitingVerification {
		t.Fatalf("re-answering the last reopened step must re-enter Verify: %+v", res)
	}
	if !reflect.DeepEqual(res.DeathProofRefs, []string{"proof-death_video", "proof-post_mortem_video", "p-pen-2"}) {
		t.Fatalf("refs = %v", res.DeathProofRefs)
	}
	if _, err := repo.DeathEvidenceForVerification(ctx, wfTenant, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown goat err = %v", err)
	}
}
