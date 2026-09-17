package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// Birth evidence is reviewed ONE RECORDED STEP AT A TIME (maintainer decision 2026-09-16). These
// tests drive the production service + verdict consumer over the fakes that share the domain
// rules with the postgres adapter. Before this change the same calls produced NO verifier item
// until the whole kid track (three days, ~14 clips) was finished, and one rejection re-opened
// every clip.

func newServiceWithMotherWorkflow(t *testing.T) (*Service, *fakeRepo, *fakeEnqueuer, string) {
	t.Helper()
	repo := newFakeRepo()
	repo.goats[testGoat] = ports.GoatWorkflowFacts{GoatID: testGoat, DisplayID: "CPT-00045", LifecycleStatus: "alive"}
	enq := &fakeEnqueuer{}
	svc := NewService(repo, nil).WithVerificationEnqueuer(enq)
	created, err := repo.OpenWorkflow(context.Background(), ports.OpenWorkflowCommand{
		TenantID:      testTenant,
		TemplateKey:   domain.TemplateKeyBirthMother,
		SubjectGoatID: testGoat,
		EventAt:       time.Date(2026, 9, 16, 6, 30, 0, 0, biztime.DefaultLocation()),
	})
	if err != nil || !created {
		t.Fatalf("open mother workflow: created=%v err=%v", created, err)
	}
	for id := range repo.workflows {
		return svc, repo, enq, id
	}
	t.Fatal("no workflow opened")
	return nil, nil, nil, ""
}

func recordMotherStep(t *testing.T, svc *Service, repo *fakeRepo, workflowID, key, proof, idem string) domain.ActionWriteResult {
	t.Helper()
	a := actionByKeyT(t, repo, workflowID, key)
	var (
		result domain.ActionWriteResult
		err    error
	)
	if a.ActionType == domain.ActionTypeAction {
		result, err = svc.CompleteAction(context.Background(), CompleteActionInput{
			TenantID: testTenant, WorkflowID: workflowID, ActionID: a.ActionID,
			ProofRef: proof, CompletedBy: "33333333-3333-4333-8333-333333333333",
			IdempotencyKey: idem, RequestFingerprint: "fp-" + idem,
		})
	} else {
		result, err = svc.AnswerAction(context.Background(), AnswerActionInput{
			TenantID: testTenant, WorkflowID: workflowID, ActionID: a.ActionID,
			AnswerValue: "yes", ProofRef: proof, AnsweredBy: "33333333-3333-4333-8333-333333333333",
			IdempotencyKey: idem, RequestFingerprint: "fp-" + idem,
		})
	}
	if err != nil {
		t.Fatalf("record %s: %v", key, err)
	}
	return result
}

func birthVerdictEvent(t *testing.T, eventType, refType, refID, reason string, recordingKey ...string) eventbus.Event {
	t.Helper()
	key := ""
	if len(recordingKey) > 0 {
		key = recordingKey[0]
	}
	payload, err := json.Marshal(map[string]any{
		"decision": "x", "verified_by": "44444444-4444-4444-8444-444444444444", "reason": reason,
		"source": map[string]string{"module": domain.VerificationModuleCounts, "ref_type": refType, "ref_id": refID, "recording_key": key},
	})
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Event{Type: eventType, TenantID: testTenant, Payload: payload, OccurredAt: time.Now().UTC()}
}

func TestBirthStepReachesTheVerifierTheMomentItIsRecorded(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithMotherWorkflow(t)

	first := recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyBabiesStillInside, "proof-babies", "k1")
	if first.Action.Status != domain.ActionStatusInReview {
		t.Fatalf("recorded step status = %q, want in_review", first.Action.Status)
	}
	if enq.created() != 1 || len(enq.birthCalls) != 1 {
		t.Fatalf("items after ONE recorded step = %d (calls %d), want 1: the verifier must not wait for the rest of the track",
			enq.created(), len(enq.birthCalls))
	}
	item := enq.birthCalls[0]
	if item.ActionID != first.Action.ActionID || item.WorkflowID != workflowID {
		t.Fatalf("item ref = action %q workflow %q, want this step", item.ActionID, item.WorkflowID)
	}
	if len(item.ProofRefs) != 1 || item.ProofRefs[0] != "proof-babies" {
		t.Fatalf("item proofs = %v, want exactly this step's clip", item.ProofRefs)
	}
	if item.SubjectLabel != first.Action.Title+" · Mother CPT-00045 · 16/09/2026" {
		t.Fatalf("subject label %q must name the step, the animal and the date -- and NOT the pen, which both verifier surfaces render themselves", item.SubjectLabel)
	}
	if item.IdempotencyKey != domain.BirthStepReviewKey(first.Action) {
		t.Fatalf("idempotency key = %q, want the per-recording key", item.IdempotencyKey)
	}
	if first.Workflow.AwaitingVerification {
		t.Fatal("card must stay ordinary open work while the operator still has steps to record")
	}

	// Exact replay after the enqueue: re-reported, de-duplicated, nothing new for the verifier.
	replay := recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyBabiesStillInside, "proof-babies", "k1")
	if !replay.Replayed || enq.created() != 1 {
		t.Fatalf("replay: replayed=%v items=%d, want true/1", replay.Replayed, enq.created())
	}

	// The next step opens without waiting for the verdict on the first.
	second := recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyMotherLicking, "proof-licking", "k2")
	if enq.created() != 2 || second.Action.Status != domain.ActionStatusInReview {
		t.Fatalf("after second step: items=%d status=%q, want 2/in_review", enq.created(), second.Action.Status)
	}
}

func TestBirthStepRejectionSendsBackOnlyThatStep(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithMotherWorkflow(t)
	first := recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyBabiesStillInside, "proof-babies", "k1")
	second := recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyMotherLicking, "proof-licking", "k2")
	handler := NewBirthVerificationHandler(svc, nil)

	// Reject clip 1 through the production verdict consumer.
	if err := handler.HandleEvent(context.Background(), birthVerdictEvent(t, EventVerificationVerdictRework,
		domain.VerificationRefTypeBirthAction, first.Action.ActionID, "Mother's face not visible", domain.BirthStepReviewKey(first.Action))); err != nil {
		t.Fatalf("rework verdict: %v", err)
	}
	bounced := actionByKeyT(t, repo, workflowID, domain.ActionKeyBabiesStillInside)
	if bounced.Status != domain.ActionStatusRework || bounced.ProofRef != nil || len(bounced.ProofRefs) != 0 {
		t.Fatalf("rejected step = %+v, want rework with proofs cleared", bounced)
	}
	if bounced.ReworkReason == nil || *bounced.ReworkReason != "Mother's face not visible" {
		t.Fatalf("rejected step must carry the verifier's words, got %v", bounced.ReworkReason)
	}
	if got := actionByKeyT(t, repo, workflowID, domain.ActionKeyMotherLicking); got.Status != domain.ActionStatusInReview {
		t.Fatalf("sibling step = %q, want still in_review: a rejection reopens exactly one clip", got.Status)
	}
	// Later steps carry on: step 3 is not held by the re-shoot of step 1.
	third := actionByKeyT(t, repo, workflowID, domain.ActionKeyMothersMedicine)
	if domain.OperatorActionBlocked(domain.TemplateKeyBirthMother, third, repo.actions[workflowID]) {
		t.Fatal("step 3 must stay open while step 1 is re-shot")
	}
	w := repo.workflows[workflowID]
	if w.State != domain.WorkflowStateOpen || w.AwaitingVerification || w.ActionsDone != 1 {
		t.Fatalf("card after rejection = state %q awaiting %v done %d, want open/false/1", w.State, w.AwaitingVerification, w.ActionsDone)
	}

	// Re-shoot ONLY that clip: a fresh item, distinct from the rejected recording's.
	reshoot := recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyBabiesStillInside, "proof-babies", "k1-reshoot")
	if enq.created() != 3 {
		t.Fatalf("items after re-shoot = %d, want 3 (byte-identical proof still opens a fresh review)", enq.created())
	}
	if reshoot.Action.ReworkReason != nil {
		t.Fatal("re-recording clears the old rejection reason")
	}
	if enq.birthCalls[2].IdempotencyKey == enq.birthCalls[0].IdempotencyKey {
		t.Fatal("re-shoot must not collide with the rejected recording's item key")
	}

	// Approve clip 2: only clip 2 completes.
	if err := handler.HandleEvent(context.Background(), birthVerdictEvent(t, EventVerificationVerdictApproved,
		domain.VerificationRefTypeBirthAction, second.Action.ActionID, "", domain.BirthStepReviewKey(second.Action))); err != nil {
		t.Fatalf("approve verdict: %v", err)
	}
	if got := actionByKeyT(t, repo, workflowID, domain.ActionKeyMotherLicking); got.Status != domain.ActionStatusCompleted {
		t.Fatalf("approved step = %q, want completed", got.Status)
	}
	if got := actionByKeyT(t, repo, workflowID, domain.ActionKeyBabiesStillInside); got.Status != domain.ActionStatusInReview {
		t.Fatalf("re-shot step = %q, want in_review (its own verdict is still outstanding)", got.Status)
	}
	// A redelivered approve is a no-op, never an error.
	if err := handler.HandleEvent(context.Background(), birthVerdictEvent(t, EventVerificationVerdictApproved,
		domain.VerificationRefTypeBirthAction, second.Action.ActionID, "", domain.BirthStepReviewKey(second.Action))); err != nil {
		t.Fatalf("redelivered approve: %v", err)
	}
	// A verdict for a step that does not exist is acked, not retried.
	if err := handler.HandleEvent(context.Background(), birthVerdictEvent(t, EventVerificationVerdictApproved,
		domain.VerificationRefTypeBirthAction, "no-such-action", "")); err != nil {
		t.Fatalf("unroutable verdict must be acked, got %v", err)
	}
}

func TestBirthTrackCompletesOnlyWhenEveryClipIsApproved(t *testing.T) {
	svc, repo, _, workflowID := newServiceWithMotherWorkflow(t)
	handler := NewBirthVerificationHandler(svc, nil)
	keys := []string{domain.ActionKeyBabiesStillInside, domain.ActionKeyMotherLicking, domain.ActionKeyMothersMedicine,
		domain.ActionKeyORSWater1, domain.ActionKeyMotherEating, domain.ActionKeyORSWater2}
	for _, key := range keys {
		// ORS round 2 is due 50 minutes after round 1; the fake clock is real time, so lift its gate.
		if key == domain.ActionKeyORSWater2 {
			for i := range repo.actions[workflowID] {
				if repo.actions[workflowID][i].ActionKey == key {
					past := time.Now().Add(-time.Hour)
					repo.actions[workflowID][i].DueAt = &past
				}
			}
		}
		recordMotherStep(t, svc, repo, workflowID, key, "proof-"+key, "k-"+key)
	}
	w := repo.workflows[workflowID]
	if !w.AwaitingVerification || w.State != domain.WorkflowStateOpen || w.ActionsDone != len(keys) {
		t.Fatalf("all recorded: awaiting=%v state=%q done=%d; want true/open/%d", w.AwaitingVerification, w.State, w.ActionsDone, len(keys))
	}
	for _, key := range keys {
		a := actionByKeyT(t, repo, workflowID, key)
		if err := handler.HandleEvent(context.Background(), birthVerdictEvent(t, EventVerificationVerdictApproved,
			domain.VerificationRefTypeBirthAction, a.ActionID, "", domain.BirthStepReviewKey(a))); err != nil {
			t.Fatalf("approve %s: %v", key, err)
		}
	}
	w = repo.workflows[workflowID]
	if w.AwaitingVerification || w.State != domain.WorkflowStateCompleted {
		t.Fatalf("all approved: awaiting=%v state=%q; want false/completed", w.AwaitingVerification, w.State)
	}
}

func TestRecordedBirthStepFailsClosedWithoutTheVerifierSeam(t *testing.T) {
	svc, repo, _, workflowID := newServiceWithMotherWorkflow(t)
	svc.enqueuer = nil
	a := actionByKeyT(t, repo, workflowID, domain.ActionKeyBabiesStillInside)
	_, err := svc.AnswerAction(context.Background(), AnswerActionInput{
		TenantID: testTenant, WorkflowID: workflowID, ActionID: a.ActionID, AnswerValue: "yes", ProofRef: "p",
		IdempotencyKey: "k", RequestFingerprint: "fp",
	})
	if !errors.Is(err, domain.ErrVerificationEnqueuerNotWired) {
		t.Fatalf("err = %v, want ErrVerificationEnqueuerNotWired (a recorded clip nobody can review is a composition bug)", err)
	}
}

func TestRetiredBirthBundleVerdictStillLandsOnAPreCutoverItem(t *testing.T) {
	svc, repo, _, workflowID := newServiceWithMotherWorkflow(t)
	// A track finished under the old rule: every clip completed, the whole-workflow gate open.
	for i := range repo.actions[workflowID] {
		proof := "old-" + repo.actions[workflowID][i].ActionKey
		repo.actions[workflowID][i].Status = domain.ActionStatusCompleted
		repo.actions[workflowID][i].ProofRef = &proof
	}
	w := repo.workflows[workflowID]
	w.AwaitingVerification = true
	repo.workflows[workflowID] = w

	handler := NewBirthVerificationHandler(svc, nil)
	if err := handler.HandleEvent(context.Background(), birthVerdictEvent(t, EventVerificationVerdictRework,
		domain.VerificationRefTypeBirthSignoff, workflowID, "redo")); err != nil {
		t.Fatalf("legacy rework: %v", err)
	}
	if got := actionByKeyT(t, repo, workflowID, domain.ActionKeyBabiesStillInside); got.Status != domain.ActionStatusRework {
		t.Fatalf("legacy bundle rework must still reopen the track, got %q", got.Status)
	}
	if repo.workflows[workflowID].AwaitingVerification {
		t.Fatal("legacy gate must close on the verdict")
	}
}
