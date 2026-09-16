package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// SOP → verifier parity (maintainer decisions 2026-09-16): the verifier sees every proof under
// its step title with the register's kind and every answer in farm words; death follows the SOP
// (the exactly-two-videos lock is retired); the release path and the re-shoot path build ONE item.

const testOperator = "33333333-3333-4333-8333-333333333333"

// fakeProofKinds is the proof register: ref -> kind.
type fakeProofKinds struct {
	kinds map[string]string
	calls int
}

func (f *fakeProofKinds) ResolveProofKinds(_ context.Context, _ string, refs []string) (map[string]string, error) {
	f.calls++
	out := map[string]string{}
	for _, ref := range refs {
		if k, ok := f.kinds[ref]; ok {
			out[ref] = k
		}
	}
	return out, nil
}

// addDeathStep appends an authored step to the fake's death workflow (the seeded document has
// only the two videos; the SOP may author more).
func addDeathStep(t *testing.T, repo *fakeRepo, workflowID string, a domain.WorkflowAction) domain.WorkflowAction {
	t.Helper()
	a.ActionID = workflowID + ":" + a.ActionKey
	a.TenantID, a.WorkflowID, a.Section, a.Status, a.RowVersion = testTenant, workflowID, domain.SectionMain, domain.ActionStatusPending, 1
	// Keep the internal approval row last so the seq order stays authored steps then sign-off.
	actions := repo.actions[workflowID]
	var out []domain.WorkflowAction
	for _, existing := range actions {
		if existing.ActionType == domain.ActionTypeApproval {
			continue
		}
		out = append(out, existing)
	}
	a.Seq = len(out) + 1
	out = append(out, a)
	for _, existing := range actions {
		if existing.ActionType == domain.ActionTypeApproval {
			existing.Seq = len(out) + 1
			out = append(out, existing)
		}
	}
	repo.actions[workflowID] = out
	repo.workflows[workflowID] = domain.RecomputeCard(repo.workflows[workflowID], out)
	return a
}

func completeDeathStep(t *testing.T, svc *Service, repo *fakeRepo, workflowID, key, idem string, proofs ...domain.ProofItem) domain.ActionWriteResult {
	t.Helper()
	res, err := svc.CompleteAction(context.Background(), CompleteActionInput{
		TenantID: testTenant, WorkflowID: workflowID, ActionID: actionID(t, repo, workflowID, key),
		Proofs: proofs, CompletedBy: testOperator, IdempotencyKey: idem, RequestFingerprint: "fp-" + idem,
	})
	if err != nil {
		t.Fatalf("complete %s: %v", key, err)
	}
	return res
}

func TestDeathReleaseAndReshootPathsEnqueueTheSameItem(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithDeathWorkflow(t)
	question := addDeathStep(t, repo, workflowID, domain.WorkflowAction{
		ActionKey: "likely_cause", ActionType: domain.ActionTypeQuestion, Title: "Likely cause?", AnswerType: domain.AnswerKindText, TaskType: "question",
	})
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyDeathVideo, "k-death", domain.ProofItem{Ref: "v-death", Kind: domain.ProofKindVideo})
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyPostMortemVideo, "k-post", domain.ProofItem{Ref: "v-post", Kind: domain.ProofKindVideo})
	if _, err := svc.AnswerAction(context.Background(), AnswerActionInput{
		TenantID: testTenant, WorkflowID: workflowID, ActionID: question.ActionID, AnswerValue: "bloat",
		AnsweredBy: testOperator, IdempotencyKey: "k-cause", RequestFingerprint: "fp-cause",
	}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	releaseApprovedDeath(t, svc, repo, workflowID)
	if len(enq.calls) != 1 {
		t.Fatalf("release calls = %d, want 1", len(enq.calls))
	}
	release := enq.calls[0]
	wantRefs := []string{"v-death", "v-post"}
	if !reflect.DeepEqual(release.ProofRefs, wantRefs) {
		t.Fatalf("release refs = %v, want %v", release.ProofRefs, wantRefs)
	}
	wantMeta := []domain.MediaMetaItem{{Label: "Record death video", Kind: "video"}, {Label: "Record post-mortem video", Kind: "video"}}
	if !reflect.DeepEqual(release.MediaMeta, wantMeta) {
		t.Fatalf("release meta = %+v, want %+v", release.MediaMeta, wantMeta)
	}
	wantRows := []domain.EvidenceRow{{Label: "Likely cause?", Value: "bloat", Group: "Likely cause?"}}
	if !reflect.DeepEqual(release.ContextRows, wantRows) {
		t.Fatalf("release rows = %+v, want %+v", release.ContextRows, wantRows)
	}
	round := repo.workflows[workflowID].RowVersion
	if release.IdempotencyKey != domain.DeathEvidenceKey(workflowID, round, wantRefs) {
		t.Fatalf("release key = %q", release.IdempotencyKey)
	}

	// Verifier rejects: proof steps reopen with the reason; the answer is kept.
	if err := svc.BounceDeathVideosForRework(context.Background(), ports.DeathVerdictCommand{
		TenantID: testTenant, WorkflowID: workflowID, Reason: "timestamp not visible", VerdictAt: time.Now(),
	}); err != nil {
		t.Fatalf("rework: %v", err)
	}
	for _, key := range []string{domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo} {
		a := actionByKeyT(t, repo, workflowID, key)
		if a.Status != domain.ActionStatusRework || len(a.ProofRefs) != 0 || a.ProofRef != nil || a.ReworkReason == nil || *a.ReworkReason != "timestamp not visible" {
			t.Fatalf("%s after rework = %+v", key, a)
		}
	}
	if a := actionByKeyT(t, repo, workflowID, "likely_cause"); a.Status != domain.ActionStatusCompleted || a.AnswerValue == nil {
		t.Fatalf("answer step must be kept on rework: %+v", a)
	}

	// Re-shoot both: the completion path builds the SAME item shape, a fresh round.
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyDeathVideo, "k-death-2", domain.ProofItem{Ref: "v-death", Kind: domain.ProofKindVideo})
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyPostMortemVideo, "k-post-2", domain.ProofItem{Ref: "v-post", Kind: domain.ProofKindVideo})
	if len(enq.calls) != 2 || enq.created() != 2 {
		t.Fatalf("after re-shoot calls=%d created=%d, want 2/2", len(enq.calls), enq.created())
	}
	reshoot := enq.calls[1]
	if !reflect.DeepEqual(reshoot.ProofRefs, release.ProofRefs) || !reflect.DeepEqual(reshoot.MediaMeta, release.MediaMeta) || !reflect.DeepEqual(reshoot.ContextRows, release.ContextRows) {
		t.Fatalf("re-shoot item differs from the release item:\n%+v\n%+v", reshoot, release)
	}
	if reshoot.SubjectLabel != release.SubjectLabel || reshoot.IdempotencyKey == release.IdempotencyKey {
		t.Fatalf("re-shoot label %q key %q vs release %q key %q", reshoot.SubjectLabel, reshoot.IdempotencyKey, release.SubjectLabel, release.IdempotencyKey)
	}
}

func TestAnsweringTheLastDeathStepAfterReworkReEntersVerify(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithDeathWorkflow(t)
	// An authored question WITH a photo: the SOP asks "which pen?" plus a photo of it.
	question := addDeathStep(t, repo, workflowID, domain.WorkflowAction{
		ActionKey: "found_where", ActionType: domain.ActionTypeQuestion, Title: "Where was it found?", AnswerType: domain.AnswerKindText, TaskType: "question", ProofMinPhotos: 1,
	})
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyDeathVideo, "k-death", domain.ProofItem{Ref: "v-death", Kind: domain.ProofKindVideo})
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyPostMortemVideo, "k-post", domain.ProofItem{Ref: "v-post", Kind: domain.ProofKindVideo})
	answer := func(idem string) {
		t.Helper()
		if _, err := svc.AnswerAction(context.Background(), AnswerActionInput{
			TenantID: testTenant, WorkflowID: workflowID, ActionID: question.ActionID, AnswerValue: "In the pen",
			Proofs: []domain.ProofItem{{Ref: "p-pen", Kind: domain.ProofKindPhoto}}, AnsweredBy: testOperator,
			IdempotencyKey: idem, RequestFingerprint: "fp-" + idem,
		}); err != nil {
			t.Fatalf("answer: %v", err)
		}
	}
	answer("k-where")
	releaseApprovedDeath(t, svc, repo, workflowID)
	if enq.created() != 1 {
		t.Fatalf("release created %d, want 1", enq.created())
	}
	if err := svc.BounceDeathVideosForRework(context.Background(), ports.DeathVerdictCommand{TenantID: testTenant, WorkflowID: workflowID, Reason: "photo dark", VerdictAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if a := actionByKeyT(t, repo, workflowID, "found_where"); a.Status != domain.ActionStatusRework {
		t.Fatalf("a proof-bearing answer step must reopen on rework: %+v", a)
	}
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyDeathVideo, "k-death-2", domain.ProofItem{Ref: "v-death", Kind: domain.ProofKindVideo})
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyPostMortemVideo, "k-post-2", domain.ProofItem{Ref: "v-post", Kind: domain.ProofKindVideo})
	if enq.created() != 1 {
		t.Fatalf("videos alone must not re-enter Verify while the photo step is open: created=%d", enq.created())
	}
	answer("k-where-2")
	if enq.created() != 2 {
		t.Fatalf("answering the LAST reopened step must re-enter Verify: created=%d", enq.created())
	}
	last := enq.calls[len(enq.calls)-1]
	wantRefs := []string{"v-death", "v-post", "p-pen"}
	if !reflect.DeepEqual(last.ProofRefs, wantRefs) {
		t.Fatalf("refs = %v, want %v", last.ProofRefs, wantRefs)
	}
	if last.MediaMeta[2] != (domain.MediaMetaItem{Label: "Where was it found?", Kind: "photo"}) {
		t.Fatalf("photo meta = %+v", last.MediaMeta[2])
	}
	if !reflect.DeepEqual(last.ContextRows, []domain.EvidenceRow{{Label: "Where was it found?", Value: "In the pen", Group: "Where was it found?"}}) {
		t.Fatalf("rows = %+v", last.ContextRows)
	}
}

func TestDeathCaptureEvidenceIsSnapshottedAtOpenAndLeadsTheBundle(t *testing.T) {
	repo := newFakeRepo()
	repo.goats[testGoat] = ports.GoatWorkflowFacts{GoatID: testGoat, LifecycleStatus: "alive"}
	enq := &fakeEnqueuer{}
	svc := NewService(repo, nil).WithVerificationEnqueuer(enq)
	capture := authored.Evidence{
		VersionLabel: "v3",
		Media:        []authored.EvidenceMedia{{Ref: "c-tag", Kind: "photo", Label: "Animal with tag"}},
		Rows:         []authored.EvidenceRow{{Label: "Found where?", Value: "Water trough"}},
		MissingNote:  "Carcass photo",
	}
	if err := svc.OpenReportedDeathWorkflow(context.Background(), testTenant, testGoat, time.Date(2026, 9, 16, 9, 0, 0, 0, biztime.DefaultLocation()), capture); err != nil {
		t.Fatal(err)
	}
	var workflowID string
	for id := range repo.workflows {
		workflowID = id
	}
	if got := repo.workflows[workflowID].CaptureEvidence; !reflect.DeepEqual(got, capture) {
		t.Fatalf("snapshot at open = %+v, want %+v", got, capture)
	}
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyDeathVideo, "k-death", domain.ProofItem{Ref: "v-death", Kind: domain.ProofKindVideo})
	completeDeathStep(t, svc, repo, workflowID, domain.ActionKeyPostMortemVideo, "k-post", domain.ProofItem{Ref: "v-post", Kind: domain.ProofKindVideo})
	releaseApprovedDeath(t, svc, repo, workflowID)
	item := enq.calls[0]
	if !reflect.DeepEqual(item.ProofRefs, []string{"c-tag", "v-death", "v-post"}) {
		t.Fatalf("refs = %v: the report's own proof leads the bundle", item.ProofRefs)
	}
	if item.MediaMeta[0] != (domain.MediaMetaItem{Label: "At report · Animal with tag", Kind: "photo"}) {
		t.Fatalf("meta[0] = %+v", item.MediaMeta[0])
	}
	wantRows := []domain.EvidenceRow{
		{Label: "Found where?", Value: "Water trough", Group: "At report"},
		{Label: authored.MissingNoteOlderApp, Value: "Carcass photo", Group: "At report"},
	}
	if !reflect.DeepEqual(item.ContextRows, wantRows) {
		t.Fatalf("rows = %+v, want %+v", item.ContextRows, wantRows)
	}
	// Redelivered counts.death.reported: the natural key absorbs it and the snapshot is untouched.
	if err := svc.OpenReportedDeathWorkflow(context.Background(), testTenant, testGoat, time.Now(), authored.Evidence{VersionLabel: "later"}); err != nil {
		t.Fatal(err)
	}
	if got := repo.workflows[workflowID].CaptureEvidence.VersionLabel; got != "v3" {
		t.Fatalf("redelivery relabelled the snapshot to %q", got)
	}
}

func TestBirthStepItemCarriesNumberedMediaMetaAndAnswerRows(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithMotherWorkflow(t)
	step := actionByKeyT(t, repo, workflowID, domain.ActionKeyBabiesStillInside)
	for i := range repo.actions[workflowID] {
		if repo.actions[workflowID][i].ActionID == step.ActionID {
			repo.actions[workflowID][i].ProofMinPhotos = 2
			repo.actions[workflowID][i].AnswerType = domain.AnswerKindYesNo
		}
	}
	if _, err := svc.AnswerAction(context.Background(), AnswerActionInput{
		TenantID: testTenant, WorkflowID: workflowID, ActionID: step.ActionID, AnswerValue: "yes",
		Proofs:     []domain.ProofItem{{Ref: "p1", Kind: domain.ProofKindPhoto}, {Ref: "v1", Kind: domain.ProofKindVideo}, {Ref: "p2", Kind: domain.ProofKindPhoto}},
		AnsweredBy: testOperator, IdempotencyKey: "k1", RequestFingerprint: "fp1",
	}); err != nil {
		t.Fatal(err)
	}
	if len(enq.birthCalls) != 1 {
		t.Fatalf("calls = %d", len(enq.birthCalls))
	}
	item := enq.birthCalls[0]
	if !reflect.DeepEqual(item.ProofRefs, []string{"v1", "p1", "p2"}) {
		t.Fatalf("refs = %v", item.ProofRefs)
	}
	wantMeta := []domain.MediaMetaItem{
		{Label: step.Title, Kind: "video"},
		{Label: step.Title + " · photo 1 of 2", Kind: "photo"},
		{Label: step.Title + " · photo 2 of 2", Kind: "photo"},
	}
	if !reflect.DeepEqual(item.MediaMeta, wantMeta) {
		t.Fatalf("meta = %+v, want %+v", item.MediaMeta, wantMeta)
	}
	if !reflect.DeepEqual(item.ContextRows, []domain.EvidenceRow{{Label: step.Title, Value: "Yes", Group: step.Title}}) {
		t.Fatalf("rows = %+v", item.ContextRows)
	}
}

func TestProofKindsAreTakenFromTheRegisterNotTheClient(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithMotherWorkflow(t)
	register := &fakeProofKinds{kinds: map[string]string{"p-register": "photo"}}
	svc.WithProofKindResolver(register)
	step := actionByKeyT(t, repo, workflowID, domain.ActionKeyBabiesStillInside)
	for i := range repo.actions[workflowID] {
		if repo.actions[workflowID][i].ActionID == step.ActionID {
			repo.actions[workflowID][i].RequiresVideo = false
			repo.actions[workflowID][i].ProofMinVideos = 0
			repo.actions[workflowID][i].ProofMinPhotos = 1
		}
	}
	// The client mislabels its photo as a video. The register says photo; the step's photo
	// minimum is met and the verifier item names a photo.
	if _, err := svc.AnswerAction(context.Background(), AnswerActionInput{
		TenantID: testTenant, WorkflowID: workflowID, ActionID: step.ActionID, AnswerValue: "yes",
		Proofs:     []domain.ProofItem{{Ref: "p-register", Kind: domain.ProofKindVideo}},
		AnsweredBy: testOperator, IdempotencyKey: "k1", RequestFingerprint: "fp1",
	}); err != nil {
		t.Fatalf("answer with a register-resolved photo: %v", err)
	}
	if register.calls == 0 {
		t.Fatal("the register was never consulted")
	}
	got := enq.birthCalls[0]
	if len(got.MediaMeta) != 1 || got.MediaMeta[0].Kind != domain.ProofKindPhoto {
		t.Fatalf("meta = %+v, want the register's kind", got.MediaMeta)
	}
	if a := actionByKeyT(t, repo, workflowID, domain.ActionKeyBabiesStillInside); len(a.ProofRefs) != 1 || a.ProofRefs[0].Kind != domain.ProofKindPhoto {
		t.Fatalf("stored proof kind = %+v, want the register's", a.ProofRefs)
	}
	// A ref the register does not know keeps the client's kind (never blocks a capture).
	other := actionByKeyT(t, repo, workflowID, domain.ActionKeyMotherLicking)
	if _, err := svc.AnswerAction(context.Background(), AnswerActionInput{
		TenantID: testTenant, WorkflowID: workflowID, ActionID: other.ActionID, AnswerValue: "yes",
		ProofRef: "unknown-ref", AnsweredBy: testOperator, IdempotencyKey: "k2", RequestFingerprint: "fp2",
	}); err != nil {
		t.Fatalf("unknown ref: %v", err)
	}
	// A register outage is not a capture outage: the client's kind is kept and the write lands.
	svc.WithProofKindResolver(&failingProofKinds{})
	third := actionByKeyT(t, repo, workflowID, domain.ActionKeyMothersMedicine)
	if _, err := svc.CompleteAction(context.Background(), CompleteActionInput{
		TenantID: testTenant, WorkflowID: workflowID, ActionID: third.ActionID,
		ProofRef: "v3", CompletedBy: testOperator, IdempotencyKey: "k3", RequestFingerprint: "fp3",
	}); err != nil {
		t.Fatalf("register outage must not block the capture: %v", err)
	}
}

type failingProofKinds struct{}

func (failingProofKinds) ResolveProofKinds(context.Context, string, []string) (map[string]string, error) {
	return nil, errors.New("register down")
}
