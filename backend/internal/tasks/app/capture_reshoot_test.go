package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

type fakeReshootListener struct {
	calls []string
	proof []domain.ProofItem
}

func (f *fakeReshootListener) OnBirthCaptureReshot(_ context.Context, tenantID, birthEventID string, index int, proof domain.ProofItem) error {
	f.calls = append(f.calls, tenantID+"|"+birthEventID+"|"+string(rune('0'+index)))
	f.proof = append(f.proof, proof)
	return nil
}

func reshootStep(t *testing.T, repo *fakeRepo, workflowID string) domain.WorkflowAction {
	t.Helper()
	for _, a := range repo.actions[workflowID] {
		if a.HasHook(domain.EngineHookReshootReport) {
			return a
		}
	}
	t.Fatal("no re-shoot step")
	return domain.WorkflowAction{}
}

func TestBirthCaptureRejectOpensAReshootStepOnTheMotherTrack(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithMotherWorkflow(t)
	listener := &fakeReshootListener{}
	svc.WithCaptureReshootListener(listener)
	w := repo.workflows[workflowID]
	event := "birth-event-1"
	w.BirthEventID = &event
	repo.workflows[workflowID] = w
	capture := authored.Evidence{Media: []authored.EvidenceMedia{{Ref: "old-photo", Kind: "photo", Label: "Newborns with the mother"}}}
	for i := 0; i < 2; i++ { // redelivered verdict: one step
		if err := svc.OpenBirthCaptureReshoot(context.Background(), testTenant, event, capture, []int{0}, "rec-1", "Mother's face not visible"); err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
	}
	steps := 0
	for _, a := range repo.actions[workflowID] {
		if a.HasHook(domain.EngineHookReshootReport) {
			steps++
		}
	}
	if steps != 1 {
		t.Fatalf("re-shoot steps = %d, want 1", steps)
	}
	step := reshootStep(t, repo, workflowID)
	if step.Status != domain.ActionStatusRework || step.ReworkReason == nil || *step.ReworkReason != "Mother's face not visible" {
		t.Fatalf("step = %+v", step)
	}
	if repo.workflows[workflowID].State != domain.WorkflowStateOpen {
		t.Fatal("the card reopens so the operator sees the re-shoot")
	}
	res, err := svc.CompleteAction(context.Background(), CompleteActionInput{
		TenantID: testTenant, WorkflowID: workflowID, ActionID: step.ActionID,
		Proofs: []domain.ProofItem{{Ref: "new-photo", Kind: domain.ProofKindPhoto}}, CompletedBy: testOperator,
		IdempotencyKey: "reshoot-1", RequestFingerprint: "fp-reshoot-1",
	})
	if err != nil {
		t.Fatalf("record re-shoot: %v", err)
	}
	if res.Action.Status != domain.ActionStatusCompleted {
		t.Fatalf("re-shoot status = %q, want completed (reviewed as the report item)", res.Action.Status)
	}
	if len(enq.birthCalls) != 0 {
		t.Fatal("a re-shoot must not open a birth STEP item")
	}
	if !reflect.DeepEqual(listener.calls, []string{testTenant + "|birth-event-1|0"}) || listener.proof[0].Ref != "new-photo" {
		t.Fatalf("listener = %v %+v", listener.calls, listener.proof)
	}
}

func TestDeathCaptureRejectAppendsReshootStepsAndTheBundleCarriesTheNewProof(t *testing.T) {
	repo := newFakeRepo()
	repo.goats[testGoat] = ports.GoatWorkflowFacts{GoatID: testGoat, LifecycleStatus: "alive"}
	enq := &fakeEnqueuer{}
	svc := NewService(repo, nil).WithVerificationEnqueuer(enq)
	capture := authored.Evidence{Media: []authored.EvidenceMedia{{Ref: "old-tag", Kind: "photo", Label: "Animal with tag"}}}
	if err := svc.OpenReportedDeathWorkflow(context.Background(), testTenant, testGoat, "", time.Date(2026, 9, 16, 9, 0, 0, 0, biztime.DefaultLocation()), capture); err != nil {
		t.Fatal(err)
	}
	var workflowID string
	for id := range repo.workflows {
		workflowID = id
	}
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyDeathVideo, "k1", domain.ProofItem{Ref: "v1", Kind: "video"})
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyPostMortemVideo, "k2", domain.ProofItem{Ref: "v2", Kind: "video"})
	releaseApprovedDeath(t, svc, repo, workflowID)
	if err := svc.BounceDeathVideosForRework(context.Background(), ports.DeathVerdictCommand{
		TenantID: testTenant, WorkflowID: workflowID, Reason: "tag not visible", RecordingKey: enq.calls[0].IdempotencyKey, VerdictAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	step := reshootStep(t, repo, workflowID)
	if step.Status != domain.ActionStatusRework || step.ProofMinPhotos != 1 {
		t.Fatalf("step = %+v", step)
	}
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyDeathVideo, "k1b", domain.ProofItem{Ref: "v1", Kind: "video"})
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyPostMortemVideo, "k2b", domain.ProofItem{Ref: "v2", Kind: "video"})
	if enq.created() != 1 {
		t.Fatal("the bundle waits for the re-shoot step too")
	}
	completeDeathStep(t, svc, repo, workflowID, step.ActionKey, "k3", domain.ProofItem{Ref: "new-tag", Kind: "photo"})
	if enq.created() != 2 {
		t.Fatalf("recording the last re-shoot must re-enter Verify, created=%d", enq.created())
	}
	last := enq.calls[len(enq.calls)-1]
	if !reflect.DeepEqual(last.ProofRefs, []string{"new-tag", "v1", "v2"}) {
		t.Fatalf("refs = %v: the re-shot proof replaces the rejected one, once", last.ProofRefs)
	}
	if last.MediaMeta[0] != (domain.MediaMetaItem{Label: "At report · Animal with tag", Kind: "photo"}) {
		t.Fatalf("meta[0] = %+v", last.MediaMeta[0])
	}
}
