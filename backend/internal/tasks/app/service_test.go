package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// ---------------------------------------------------------------------------
// In-memory fake repository. It reuses the SAME pure domain functions the postgres adapter runs
// (ApplyAnswer / ApplyComplete / RecomputeCard / DeathVideosComplete), so the idempotency and
// state-machine contract exercised here is the shared implementation, not a test-only copy.
// ---------------------------------------------------------------------------

type fakeRepo struct {
	seq       int
	workflows map[string]domain.WorkflowInstance // by workflow_id
	actions   map[string][]domain.WorkflowAction // by workflow_id, seq order
	goats     map[string]ports.GoatWorkflowFacts // by goat_id
	dams      map[string]string                  // dam ref -> goat_id
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		workflows: map[string]domain.WorkflowInstance{},
		actions:   map[string][]domain.WorkflowAction{},
		goats:     map[string]ports.GoatWorkflowFacts{},
		dams:      map[string]string{},
	}
}

func (f *fakeRepo) nextID(prefix string) string {
	f.seq++
	return prefix + "-" + time.Time{}.Add(time.Duration(f.seq)).Format("000000001") + string(rune('a'+f.seq%26))
}

func (f *fakeRepo) OpenWorkflow(_ context.Context, cmd ports.OpenWorkflowCommand) (bool, error) {
	template, ok := domain.TemplateByKeyAt(cmd.TemplateKey, cmd.EventAt)
	if !ok {
		return false, domain.ErrUnknownTemplate
	}
	for _, w := range f.workflows {
		if w.TenantID == cmd.TenantID && w.TemplateKey == cmd.TemplateKey && w.SubjectGoatID == cmd.SubjectGoatID {
			return false, nil // natural-key conflict: attach, insert nothing
		}
	}
	f.seq++
	workflowID := cmd.TemplateKey + "-wf-" + strings.Repeat("0", 3) + string(rune('a'+f.seq%26))
	w := domain.WorkflowInstance{
		WorkflowID:    workflowID,
		TenantID:      cmd.TenantID,
		TemplateKey:   cmd.TemplateKey,
		Module:        template.Module,
		SubjectGoatID: cmd.SubjectGoatID,
		DamGoatID:     cmd.DamGoatID,
		EventAt:       cmd.EventAt,
		EventDate:     biztime.BusinessDate(cmd.EventAt),
		ParkID:        cmd.ParkID,
		ShedID:        cmd.ShedID,
		State:         domain.WorkflowStateOpen,
		RowVersion:    1,
	}
	var actions []domain.WorkflowAction
	for _, at := range template.Actions {
		due := at.Schedule.DueAt(cmd.EventAt)
		actions = append(actions, domain.WorkflowAction{
			ActionID:      workflowID + ":" + at.Key,
			TenantID:      cmd.TenantID,
			WorkflowID:    workflowID,
			ActionKey:     at.Key,
			Seq:           at.Seq,
			Section:       at.Section,
			ActionType:    at.Type,
			Title:         at.Title,
			Detail:        at.Detail,
			RequiresVideo: at.RequiresVideo,
			Options:       at.Options,
			DueAt:         &due,
			Status:        domain.ActionStatusPending,
			RowVersion:    1,
		})
	}
	f.actions[workflowID] = actions
	f.workflows[workflowID] = domain.RecomputeCard(w, actions)
	return true, nil
}

func (f *fakeRepo) GoatWorkflowFacts(_ context.Context, _ string, goatID string) (ports.GoatWorkflowFacts, error) {
	facts, ok := f.goats[goatID]
	if !ok {
		return ports.GoatWorkflowFacts{}, domain.ErrNotFound
	}
	return facts, nil
}

func (f *fakeRepo) ResolveDamGoat(_ context.Context, _ string, damRef string) (string, error) {
	if id, ok := f.dams[damRef]; ok {
		return id, nil
	}
	return "", domain.ErrNotFound
}

func (f *fakeRepo) ListWorkflows(_ context.Context, _ domain.WorkflowListQuery) (domain.WorkflowListPage, error) {
	return domain.WorkflowListPage{}, nil
}

func (f *fakeRepo) ListColostrumDay(_ context.Context, _ domain.ColostrumDayQuery) (domain.WorkflowListPage, error) {
	return domain.WorkflowListPage{}, nil
}

func (f *fakeRepo) GetWorkflow(_ context.Context, tenantID, workflowID string, _ time.Time) (domain.WorkflowDetail, error) {
	w, ok := f.workflows[workflowID]
	if !ok || w.TenantID != tenantID {
		return domain.WorkflowDetail{}, domain.ErrNotFound
	}
	return domain.WorkflowDetail{Actions: f.actions[workflowID]}, nil
}

// mutate mirrors the adapter's workflowMutation: run the domain mutation, then recompute the card
// in the same "transaction".
func (f *fakeRepo) mutate(tenantID, workflowID string,
	fn func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) (bool, error),
) (domain.WorkflowInstance, []domain.WorkflowAction, bool, error) {
	w, ok := f.workflows[workflowID]
	if !ok || w.TenantID != tenantID {
		return domain.WorkflowInstance{}, nil, false, domain.ErrNotFound
	}
	actions := f.actions[workflowID]
	replay, err := fn(&w, actions)
	if err != nil {
		return domain.WorkflowInstance{}, nil, false, err
	}
	if !replay {
		w = domain.RecomputeCard(w, actions)
		w.RowVersion++
		f.workflows[workflowID] = w
		f.actions[workflowID] = actions
	}
	return w, actions, replay, nil
}

func (f *fakeRepo) AnswerAction(_ context.Context, cmd domain.AnswerActionCommand) (domain.ActionWriteResult, error) {
	var target domain.WorkflowAction
	w, actions, replay, err := f.mutate(cmd.TenantID, cmd.WorkflowID,
		func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) (bool, error) {
			for i := range actions {
				if actions[i].ActionID != cmd.ActionID {
					continue
				}
				updated, isReplay, err := domain.ApplyAnswer(actions[i], cmd)
				if err != nil {
					return false, err
				}
				actions[i] = updated
				target = updated
				if !isReplay && w.TemplateKey == domain.TemplateKeyBirthKid && domain.BirthWorkflowComplete(actions) {
					w.AwaitingVerification = true
				}
				return isReplay, nil
			}
			return false, domain.ErrNotFound
		})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	return fakeWriteResult(w, actions, target, replay), nil
}

func (f *fakeRepo) CompleteAction(_ context.Context, cmd domain.CompleteActionCommand) (domain.ActionWriteResult, error) {
	var target domain.WorkflowAction
	w, actions, replay, err := f.mutate(cmd.TenantID, cmd.WorkflowID,
		func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) (bool, error) {
			for i := range actions {
				if actions[i].ActionID != cmd.ActionID {
					continue
				}
				updated, isReplay, err := domain.ApplyComplete(actions[i], cmd)
				if err != nil {
					return false, err
				}
				actions[i] = updated
				target = updated
				if isReplay {
					return true, nil
				}
				if f.goats[w.SubjectGoatID].LifecycleStatus == "dead" && w.TemplateKey == domain.TemplateKeyDeath && updated.RequiresVideo && domain.DeathVideosComplete(actions) {
					w.AwaitingVerification = true
				}
				if w.TemplateKey == domain.TemplateKeyBirthKid && domain.BirthWorkflowComplete(actions) {
					w.AwaitingVerification = true
				}
				return false, nil
			}
			return false, domain.ErrNotFound
		})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	return fakeWriteResult(w, actions, target, replay), nil
}

func fakeWriteResult(w domain.WorkflowInstance, actions []domain.WorkflowAction, target domain.WorkflowAction, replay bool) domain.ActionWriteResult {
	result := domain.ActionWriteResult{Workflow: w, Action: target, Replayed: replay}
	if w.TemplateKey == domain.TemplateKeyDeath && w.AwaitingVerification && domain.DeathVideosComplete(actions) {
		result.NeedsVerificationEnqueue = true
		result.DeathProofRefs = domain.DeathProofRefs(actions)
		result.DeathReviewRound = w.RowVersion
	}
	return result
}

func (f *fakeRepo) CompleteTagActionForGoat(_ context.Context, tenantID, goatID string, completedAt time.Time) error {
	for id, w := range f.workflows {
		if w.TenantID != tenantID || w.SubjectGoatID != goatID ||
			w.TemplateKey != domain.TemplateKeyBirthKid || w.State != domain.WorkflowStateOpen {
			continue
		}
		_, _, _, err := f.mutate(tenantID, id,
			func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) (bool, error) {
				for i := range actions {
					if actions[i].ActionKey == domain.ActionKeyTagTheKid {
						if actions[i].Status == domain.ActionStatusCompleted {
							return true, nil
						}
						assigned := "permanent-rfid-assigned"
						actions[i].AnswerValue = &assigned
						actions[i].RowVersion++
						return false, nil
					}
				}
				return true, nil
			})
		return err
	}
	return nil
}

func (f *fakeRepo) DeathEvidenceForVerification(_ context.Context, tenantID, goatID string) (ports.DeathEvidenceReview, error) {
	for workflowID, w := range f.workflows {
		if w.TenantID != tenantID || w.SubjectGoatID != goatID || w.TemplateKey != domain.TemplateKeyDeath {
			continue
		}
		var review ports.DeathEvidenceReview
		if !w.AwaitingVerification {
			return ports.DeathEvidenceReview{}, domain.ErrNotFound
		}
		review.WorkflowID, review.EventDate = workflowID, w.EventDate
		review.Round = w.RowVersion
		review.ParkID, review.ShedID = derefOr(w.ParkID), derefOr(w.ShedID)
		for _, a := range f.actions[workflowID] {
			switch a.ActionKey {
			case domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo:
				if a.Status != domain.ActionStatusCompleted || a.ProofRef == nil {
					return ports.DeathEvidenceReview{}, domain.ErrNotFound
				}
			}
		}
		review.ProofRefs = domain.DeathProofRefs(f.actions[workflowID])
		return review, nil
	}
	return ports.DeathEvidenceReview{}, domain.ErrNotFound
}

func (f *fakeRepo) BirthWorkflowEvidenceForVerification(_ context.Context, tenantID, workflowID string) (ports.BirthEvidenceReview, error) {
	w, ok := f.workflows[workflowID]
	if !ok || w.TenantID != tenantID ||
		(w.TemplateKey != domain.TemplateKeyBirthKid && w.TemplateKey != domain.TemplateKeyBirthMother) ||
		!domain.BirthWorkflowComplete(f.actions[workflowID]) {
		return ports.BirthEvidenceReview{}, domain.ErrNotFound
	}
	if !w.AwaitingVerification {
		w.AwaitingVerification = true
		w.RowVersion++
		f.workflows[workflowID] = w
	}
	return ports.BirthEvidenceReview{
		WorkflowID: workflowID, SubjectRole: w.TemplateKey, EventDate: w.EventDate, Round: w.RowVersion,
		ParkID: derefOr(w.ParkID), ShedID: derefOr(w.ShedID),
		ProofRefs:  domain.BirthProofRefs(f.actions[workflowID]),
		OperatorID: domain.BirthProofOperator(f.actions[workflowID]),
	}, nil
}

func (f *fakeRepo) CancelDeathWorkflowForGoat(_ context.Context, tenantID, goatID string, _ time.Time) error {
	for workflowID, w := range f.workflows {
		if w.TenantID == tenantID && w.SubjectGoatID == goatID && w.TemplateKey == domain.TemplateKeyDeath {
			w.State = domain.WorkflowStateCanceled
			w.AwaitingVerification = false
			f.workflows[workflowID] = w
			for i := range f.actions[workflowID] {
				f.actions[workflowID][i].Status = domain.ActionStatusCanceled
				f.actions[workflowID][i].ProofRef = nil
			}
		}
	}
	return nil
}

func (f *fakeRepo) ApplyDeathSignoffApproved(_ context.Context, cmd ports.DeathVerdictCommand) error {
	_, _, _, err := f.mutate(cmd.TenantID, cmd.WorkflowID,
		func(w *domain.WorkflowInstance, _ []domain.WorkflowAction) (bool, error) {
			if !w.AwaitingVerification {
				return true, nil
			}
			w.AwaitingVerification = false
			return false, nil
		})
	// Propagates ErrNotFound like the real repository, so the app layer's log-and-ack is exercised.
	return err
}

func (f *fakeRepo) BounceDeathVideosForRework(_ context.Context, cmd ports.DeathVerdictCommand) error {
	_, _, _, err := f.mutate(cmd.TenantID, cmd.WorkflowID,
		func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) (bool, error) {
			changed := false
			w.AwaitingVerification = false
			for i := range actions {
				switch actions[i].ActionKey {
				case domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo:
					if actions[i].Status == domain.ActionStatusCompleted {
						actions[i].Status = domain.ActionStatusRework
						actions[i].ProofRef = nil
						actions[i].CompletedAt = nil
						changed = true
					}
				}
			}
			return !changed, nil
		})
	// Propagates ErrNotFound like the real repository (see the approve twin).
	return err
}

func (f *fakeRepo) ApplyBirthSignoffApproved(_ context.Context, cmd ports.DeathVerdictCommand) error {
	return f.ApplyDeathSignoffApproved(context.Background(), cmd)
}

func (f *fakeRepo) BounceBirthVideoForRework(_ context.Context, cmd ports.DeathVerdictCommand) error {
	_, _, _, err := f.mutate(cmd.TenantID, cmd.WorkflowID,
		func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) (bool, error) {
			w.AwaitingVerification = false
			for i := range actions {
				if actions[i].ActionKey == domain.ActionKeyFirstColostrum && actions[i].Status == domain.ActionStatusCompleted {
					actions[i].Status = domain.ActionStatusRework
					actions[i].ProofRef = nil
					actions[i].CompletedAt = nil
					return false, nil
				}
			}
			return true, nil
		})
	return err
}

var _ ports.Repository = (*fakeRepo)(nil)

// fakeEnqueuer models the REAL verification adapter, not just "the seam was called". Verification's
// CreateItem is `INSERT ... ON CONFLICT (tenant_id, idempotency_key) DO NOTHING` (see
// verification/adapters/postgres/repository.go): a repeated key creates NO new review item and hands
// back the pre-existing row. Recording only call counts would let a stranded-workflow bug pass, so
// `items` is keyed exactly like that unique index and is the assertion surface for "did a park head
// actually get something to review".
type fakeEnqueuer struct {
	calls      []DeathVerificationEnqueueRequest
	birthCalls []BirthVerificationEnqueueRequest
	items      map[string]int // (tenant|idempotency_key) -> times an item was actually CREATED
	err        error
}

func (f *fakeEnqueuer) EnqueueBirthEvidenceVerification(_ context.Context, in BirthVerificationEnqueueRequest) error {
	if f.err != nil {
		return f.err
	}
	f.birthCalls = append(f.birthCalls, in)
	if f.items == nil {
		f.items = map[string]int{}
	}
	key := in.TenantID + "|" + in.IdempotencyKey
	if _, exists := f.items[key]; !exists {
		f.items[key] = len(f.items) + 1
	}
	return nil
}

func (f *fakeEnqueuer) EnqueueDeathEvidenceVerification(_ context.Context, in DeathVerificationEnqueueRequest) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, in)
	if f.items == nil {
		f.items = map[string]int{}
	}
	key := in.TenantID + "|" + in.IdempotencyKey
	if _, exists := f.items[key]; exists {
		return nil // ON CONFLICT DO NOTHING: no new pending item enters the verifier queue.
	}
	f.items[key] = len(f.items) + 1
	return nil
}

// created reports how many distinct verification items this enqueuer actually materialized.
func (f *fakeEnqueuer) created() int { return len(f.items) }

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const (
	testTenant = "11111111-1111-1111-1111-111111111111"
	testGoat   = "22222222-2222-2222-2222-222222222222"
)

func newServiceWithDeathWorkflow(t *testing.T) (*Service, *fakeRepo, *fakeEnqueuer, string) {
	t.Helper()
	repo := newFakeRepo()
	repo.goats[testGoat] = ports.GoatWorkflowFacts{GoatID: testGoat, LifecycleStatus: "alive"}
	enq := &fakeEnqueuer{}
	svc := NewService(repo, nil).WithVerificationEnqueuer(enq)
	created, err := repo.OpenWorkflow(context.Background(), ports.OpenWorkflowCommand{
		TenantID:      testTenant,
		TemplateKey:   domain.TemplateKeyDeath,
		SubjectGoatID: testGoat,
		EventAt:       time.Date(2026, 7, 27, 9, 0, 0, 0, biztime.DefaultLocation()),
	})
	if err != nil || !created {
		t.Fatalf("open death workflow: created=%v err=%v", created, err)
	}
	for id := range repo.workflows {
		return svc, repo, enq, id
	}
	t.Fatal("no workflow opened")
	return nil, nil, nil, ""
}

func newServiceWithKidWorkflow(t *testing.T) (*Service, *fakeRepo, string) {
	t.Helper()
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	created, err := repo.OpenWorkflow(context.Background(), ports.OpenWorkflowCommand{
		TenantID:      testTenant,
		TemplateKey:   domain.TemplateKeyBirthKid,
		SubjectGoatID: testGoat,
		EventAt:       time.Date(2026, 7, 27, 6, 30, 0, 0, biztime.DefaultLocation()),
	})
	if err != nil || !created {
		t.Fatalf("open kid workflow: created=%v err=%v", created, err)
	}
	for id := range repo.workflows {
		return svc, repo, id
	}
	t.Fatal("no workflow opened")
	return nil, nil, ""
}

func TestPermanentRfidLeavesTagVideoPendingAndDoesNotQueueVerification(t *testing.T) {
	repo := newFakeRepo()
	enq := &fakeEnqueuer{}
	svc := NewService(repo, nil).WithVerificationEnqueuer(enq)
	created, err := repo.OpenWorkflow(context.Background(), ports.OpenWorkflowCommand{
		TenantID: testTenant, TemplateKey: domain.TemplateKeyBirthKid, SubjectGoatID: testGoat,
		EventAt: time.Date(2026, 7, 28, 9, 0, 0, 0, biztime.DefaultLocation()),
	})
	if err != nil || !created {
		t.Fatalf("open birth workflow: created=%v err=%v", created, err)
	}
	var workflowID string
	for id := range repo.workflows {
		workflowID = id
	}
	operator := "33333333-3333-4333-8333-333333333333"
	proof := "proof-birth-first-colostrum"
	completedAt := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	for i := range repo.actions[workflowID] {
		action := &repo.actions[workflowID][i]
		if action.ActionKey == domain.ActionKeyTagTheKid {
			continue
		}
		action.Status = domain.ActionStatusCompleted
		action.CompletedAt = &completedAt
		action.CompletedBy = &operator
		if action.ActionKey == domain.ActionKeyFirstColostrum {
			action.ProofRef = &proof
		}
	}
	repo.workflows[workflowID] = domain.RecomputeCard(repo.workflows[workflowID], repo.actions[workflowID])

	if err := svc.CompleteTagAction(context.Background(), testTenant, testGoat, completedAt); err != nil {
		t.Fatalf("complete permanent RFID action: %v", err)
	}
	if len(enq.birthCalls) != 0 || enq.created() != 0 {
		t.Fatalf("RFID assignment alone queued verification: calls=%d created=%d", len(enq.birthCalls), enq.created())
	}
	if got := actionByKeyT(t, repo, workflowID, domain.ActionKeyTagTheKid); got.Status != domain.ActionStatusPending || got.AnswerValue == nil {
		t.Fatalf("tag action = %+v, want assigned RFID recorded but mandatory video still pending", got)
	}
}

func TestReportedDeathOpensUploadWorkflowBeforeApproval(t *testing.T) {
	repo := newFakeRepo()
	repo.goats[testGoat] = ports.GoatWorkflowFacts{GoatID: testGoat, LifecycleStatus: "alive"}
	svc := NewService(repo, nil)
	if err := svc.OpenReportedDeathWorkflow(context.Background(), testTenant, testGoat,
		time.Date(2026, 7, 28, 9, 0, 0, 0, biztime.DefaultLocation())); err != nil {
		t.Fatalf("open reported death: %v", err)
	}
	if len(repo.workflows) != 1 {
		t.Fatalf("death workflows = %d, want 1 immediately after submit", len(repo.workflows))
	}
	for workflowID, w := range repo.workflows {
		if w.State != domain.WorkflowStateOpen || w.SubjectGoatID != testGoat {
			t.Fatalf("workflow = %+v", w)
		}
		if got := len(repo.actions[workflowID]); got != 2 {
			t.Fatalf("actions = %d, want exactly the two operator video actions", got)
		}
	}
}

func TestAdminDeathRejectionCancelsUploadsAndLeavesGoatAlive(t *testing.T) {
	svc, repo, _, workflowID := newServiceWithDeathWorkflow(t)
	completeBothDeathVideos(t, svc, repo, workflowID)
	if err := svc.CancelRejectedDeathWorkflow(context.Background(), testTenant, testGoat, time.Now()); err != nil {
		t.Fatalf("cancel rejected death: %v", err)
	}
	if got := repo.goats[testGoat].LifecycleStatus; got != "alive" {
		t.Fatalf("goat lifecycle = %q, want alive after rejection", got)
	}
	if got := repo.workflows[workflowID].State; got != domain.WorkflowStateCanceled {
		t.Fatalf("workflow state = %q, want canceled", got)
	}
}

func actionID(t *testing.T, repo *fakeRepo, workflowID, key string) string {
	t.Helper()
	for _, a := range repo.actions[workflowID] {
		if a.ActionKey == key {
			return a.ActionID
		}
	}
	t.Fatalf("action %q not found", key)
	return ""
}

func actionByKeyT(t *testing.T, repo *fakeRepo, workflowID, key string) domain.WorkflowAction {
	t.Helper()
	for _, a := range repo.actions[workflowID] {
		if a.ActionKey == key {
			return a
		}
	}
	t.Fatalf("action %q not found", key)
	return domain.WorkflowAction{}
}

// ---------------------------------------------------------------------------
// Answer idempotency
// ---------------------------------------------------------------------------

func TestAnswerActionIdempotency(t *testing.T) {
	svc, repo, workflowID := newServiceWithKidWorkflow(t)
	in := AnswerActionInput{
		TenantID:           testTenant,
		WorkflowID:         workflowID,
		ActionID:           actionID(t, repo, workflowID, domain.ActionKeyKidClean),
		AnswerValue:        "yes",
		ProofRef:           "proof-kid-clean",
		IdempotencyKey:     "answer-key-1",
		RequestFingerprint: "fp-1",
	}

	// First call applies.
	result, err := svc.AnswerAction(context.Background(), in)
	if err != nil {
		t.Fatalf("first answer: %v", err)
	}
	if result.Replayed || result.Action.Status != domain.ActionStatusCompleted {
		t.Fatalf("first answer result = %+v", result)
	}
	if result.Workflow.ActionsDone != 1 {
		t.Fatalf("actions_done = %d, want 1 (card maintained on write)", result.Workflow.ActionsDone)
	}

	// Exact replay returns the original result with no side effects.
	replayed, err := svc.AnswerAction(context.Background(), in)
	if err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if !replayed.Replayed {
		t.Fatal("exact replay must report Replayed")
	}
	if got := actionByKeyT(t, repo, workflowID, domain.ActionKeyKidClean).RowVersion; got != 2 {
		t.Fatalf("row_version after replay = %d, want 2 (no second mutation)", got)
	}

	// Same key, different payload -> conflict.
	conflicting := in
	conflicting.AnswerValue = "no"
	conflicting.RequestFingerprint = "fp-2"
	if _, err := svc.AnswerAction(context.Background(), conflicting); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay err = %v, want ErrIdempotencyConflict", err)
	}

	// New key on the already-completed action -> already completed.
	fresh := in
	fresh.IdempotencyKey = "answer-key-2"
	fresh.RequestFingerprint = "fp-3"
	if _, err := svc.AnswerAction(context.Background(), fresh); !errors.Is(err, domain.ErrActionAlreadyCompleted) {
		t.Fatalf("new-key-on-completed err = %v, want ErrActionAlreadyCompleted", err)
	}
}

func TestAnswerKidWeightRequiresPositiveNumericKilograms(t *testing.T) {
	svc, repo, workflowID := newServiceWithKidWorkflow(t)
	in := AnswerActionInput{
		TenantID:           testTenant,
		WorkflowID:         workflowID,
		ActionID:           actionID(t, repo, workflowID, domain.ActionKeyTakeWeight),
		AnswerValue:        "12 tonnes",
		ProofRef:           "proof-kid-weight",
		IdempotencyKey:     "weight-key-1",
		RequestFingerprint: "fp-w1",
	}
	if _, err := svc.AnswerAction(context.Background(), in); !errors.Is(err, domain.ErrInvalidAnswer) {
		t.Fatalf("non-numeric weight err = %v, want ErrInvalidAnswer", err)
	}
	in.AnswerValue = "0"
	in.RequestFingerprint = "fp-w2"
	if _, err := svc.AnswerAction(context.Background(), in); !errors.Is(err, domain.ErrInvalidAnswer) {
		t.Fatalf("zero weight err = %v, want ErrInvalidAnswer", err)
	}
	in.AnswerValue = "2.35"
	in.RequestFingerprint = "fp-w3"
	if _, err := svc.AnswerAction(context.Background(), in); err != nil {
		t.Fatalf("valid kilogram answer: %v", err)
	}
}

func TestAnswerRejectsNonQuestionAction(t *testing.T) {
	svc, repo, workflowID := newServiceWithKidWorkflow(t)
	in := AnswerActionInput{
		TenantID:           testTenant,
		WorkflowID:         workflowID,
		ActionID:           actionID(t, repo, workflowID, domain.ActionKeyIodineDipping),
		AnswerValue:        "yes",
		IdempotencyKey:     "answer-key-x",
		RequestFingerprint: "fp-x",
	}
	if _, err := svc.AnswerAction(context.Background(), in); !errors.Is(err, domain.ErrActionNotAnswerable) {
		t.Fatalf("err = %v, want ErrActionNotAnswerable", err)
	}
}

// ---------------------------------------------------------------------------
// Complete: proof rule + idempotency
// ---------------------------------------------------------------------------

func TestCompleteRequiresVideoProof(t *testing.T) {
	svc, repo, _, workflowID := newServiceWithDeathWorkflow(t)
	in := CompleteActionInput{
		TenantID:           testTenant,
		WorkflowID:         workflowID,
		ActionID:           actionID(t, repo, workflowID, domain.ActionKeyDeathVideo),
		IdempotencyKey:     "complete-key-1",
		RequestFingerprint: "fp-c1",
	}
	if _, err := svc.CompleteAction(context.Background(), in); !errors.Is(err, domain.ErrProofRequired) {
		t.Fatalf("proofless completion err = %v, want ErrProofRequired", err)
	}
	if got := actionByKeyT(t, repo, workflowID, domain.ActionKeyDeathVideo).Status; got != domain.ActionStatusPending {
		t.Fatalf("action status after rejected completion = %q, want pending", got)
	}
}

// Regression: operator uploads prepare evidence; they do not authorize Verify to see an
// unapproved death. Both proofs must remain staged until the admin decision applies the death.
func TestSecondDeathVideoWaitsForAdminApprovalBeforeVerifierEnqueue(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithDeathWorkflow(t)

	// First video: no enqueue yet.
	first := CompleteActionInput{
		TenantID:           testTenant,
		WorkflowID:         workflowID,
		ActionID:           actionID(t, repo, workflowID, domain.ActionKeyDeathVideo),
		ProofRef:           "proof-death",
		IdempotencyKey:     "complete-key-1",
		RequestFingerprint: "fp-c1",
	}
	result, err := svc.CompleteAction(context.Background(), first)
	if err != nil {
		t.Fatalf("first video: %v", err)
	}
	if result.NeedsVerificationEnqueue || len(enq.calls) != 0 {
		t.Fatalf("first video must not enqueue: result=%+v calls=%d", result, len(enq.calls))
	}

	// Second video: both proofs are durable, but approval is still pending and Verify sees nothing.
	second := CompleteActionInput{
		TenantID:           testTenant,
		WorkflowID:         workflowID,
		ActionID:           actionID(t, repo, workflowID, domain.ActionKeyPostMortemVideo),
		ProofRef:           "proof-postmortem",
		IdempotencyKey:     "complete-key-2",
		RequestFingerprint: "fp-c2",
	}
	result, err = svc.CompleteAction(context.Background(), second)
	if err != nil {
		t.Fatalf("second video: %v", err)
	}
	if result.NeedsVerificationEnqueue || result.Workflow.AwaitingVerification {
		t.Fatalf("second upload released unapproved death evidence: result=%+v", result)
	}
	if len(enq.calls) != 0 {
		t.Fatalf("unapproved death created %d verifier item(s), want 0", len(enq.calls))
	}
	if repo.workflows[workflowID].AwaitingVerification {
		t.Fatal("workflow must not await verifier until admin approval")
	}
}

func TestApprovedDeathWithoutEnqueuerFailsClosed(t *testing.T) {
	svc, repo, _, workflowID := newServiceWithDeathWorkflow(t)
	svc.enqueuer = nil

	first := CompleteActionInput{
		TenantID: testTenant, WorkflowID: workflowID,
		ActionID: actionID(t, repo, workflowID, domain.ActionKeyDeathVideo),
		ProofRef: "proof-death", IdempotencyKey: "complete-key-1", RequestFingerprint: "fp-c1",
	}
	if _, err := svc.CompleteAction(context.Background(), first); err != nil {
		t.Fatalf("first video: %v", err)
	}
	second := CompleteActionInput{
		TenantID: testTenant, WorkflowID: workflowID,
		ActionID: actionID(t, repo, workflowID, domain.ActionKeyPostMortemVideo),
		ProofRef: "proof-postmortem", IdempotencyKey: "complete-key-2", RequestFingerprint: "fp-c2",
	}
	if _, err := svc.CompleteAction(context.Background(), second); err != nil {
		t.Fatalf("second staged upload: %v", err)
	}
	applyAdminDeathApproval(t, repo, workflowID)
	if err := svc.ReleaseApprovedDeathEvidence(context.Background(), testTenant, testGoat, time.Now()); !errors.Is(err, domain.ErrVerificationEnqueuerNotWired) {
		t.Fatalf("approval release err = %v, want ErrVerificationEnqueuerNotWired", err)
	}
	// Both uploads and the applied approval remain durable; redelivering goat.exited after wiring
	// the enqueuer heals the handoff.
	if got := actionByKeyT(t, repo, workflowID, domain.ActionKeyPostMortemVideo).Status; got != domain.ActionStatusCompleted {
		t.Fatalf("completion must be durable across enqueue failure; status = %q", got)
	}
}

// ---------------------------------------------------------------------------
// Verdicts
// ---------------------------------------------------------------------------

func completeBothDeathVideos(t *testing.T, svc *Service, repo *fakeRepo, workflowID string) {
	t.Helper()
	for i, key := range []string{domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo} {
		in := CompleteActionInput{
			TenantID: testTenant, WorkflowID: workflowID,
			ActionID: actionID(t, repo, workflowID, key),
			ProofRef: "proof-" + key, IdempotencyKey: "complete-" + key, RequestFingerprint: "fp-" + key,
		}
		if _, err := svc.CompleteAction(context.Background(), in); err != nil {
			t.Fatalf("video %d: %v", i, err)
		}
	}
}

// applyAdminDeathApproval mirrors the production approval transaction's tasks-side effect: the
// canonical goat becomes dead and the workflow verification gate opens only after both uploads
// exist.
func applyAdminDeathApproval(t *testing.T, repo *fakeRepo, workflowID string) {
	t.Helper()
	w := repo.workflows[workflowID]
	facts := repo.goats[w.SubjectGoatID]
	facts.LifecycleStatus = "dead"
	repo.goats[w.SubjectGoatID] = facts
	_, _, _, err := repo.mutate(w.TenantID, workflowID,
		func(w *domain.WorkflowInstance, actions []domain.WorkflowAction) (bool, error) {
			if !domain.DeathVideosComplete(actions) {
				return false, errors.New("death evidence incomplete")
			}
			w.AwaitingVerification = true
			return false, nil
		})
	if err != nil {
		t.Fatalf("apply admin death approval: %v", err)
	}
}

func releaseApprovedDeath(t *testing.T, svc *Service, repo *fakeRepo, workflowID string) {
	t.Helper()
	applyAdminDeathApproval(t, repo, workflowID)
	w := repo.workflows[workflowID]
	if err := svc.ReleaseApprovedDeathEvidence(context.Background(), w.TenantID, w.SubjectGoatID, time.Now()); err != nil {
		t.Fatalf("release approved death evidence: %v", err)
	}
}

func TestAdminApprovalReleasesBothVideosToVerifier(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithDeathWorkflow(t)
	completeBothDeathVideos(t, svc, repo, workflowID)
	if len(enq.calls) != 0 {
		t.Fatalf("uploads created %d verifier items before approval", len(enq.calls))
	}
	releaseApprovedDeath(t, svc, repo, workflowID)
	if len(enq.calls) != 1 {
		t.Fatalf("approval release calls = %d, want 1", len(enq.calls))
	}
	if got := enq.calls[0].ProofRefs; len(got) != 2 || got[0] != "proof-death_video" || got[1] != "proof-post_mortem_video" {
		t.Fatalf("released proofs = %v, want both operator videos", got)
	}
}

func TestVerdictApprovedCompletesWorkflow(t *testing.T) {
	svc, repo, _, workflowID := newServiceWithDeathWorkflow(t)
	completeBothDeathVideos(t, svc, repo, workflowID)
	applyAdminDeathApproval(t, repo, workflowID)

	err := svc.ApplyDeathSignoffApproved(context.Background(), ports.DeathVerdictCommand{
		TenantID: testTenant, WorkflowID: workflowID, VerifiedBy: "verifier", VerdictAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	w := repo.workflows[workflowID]
	if w.State != domain.WorkflowStateCompleted {
		t.Fatalf("workflow state = %q, want completed", w.State)
	}
	if w.ActionsDone != w.ActionsTotal || w.ActionsTotal != 2 {
		t.Fatalf("operator card counters = %d/%d, want 2/2", w.ActionsDone, w.ActionsTotal)
	}
	if w.AwaitingVerification {
		t.Fatal("approved workflow must not be awaiting verification")
	}
	// Idempotent under redelivery.
	if err := svc.ApplyDeathSignoffApproved(context.Background(), ports.DeathVerdictCommand{
		TenantID: testTenant, WorkflowID: workflowID, VerdictAt: time.Now(),
	}); err != nil {
		t.Fatalf("redelivered approve: %v", err)
	}
}

// TestReworkedDeathEvidenceReEntersVerifierQueue is the stranded-workflow regression.
//
// Production lifecycle: operator uploads both videos -> ONE verification item -> verifier REJECTS
// -> both videos bounce to rework -> operator RE-SHOOTS both -> the gate must put a FRESH item in
// the verifier queue. Verification's CreateItem is ON CONFLICT (tenant_id, idempotency_key) DO
// NOTHING, so an idempotency key derived from the workflow ALONE silently creates nothing on the
// second round: the re-shot evidence never reaches Verify and the workflow sits awaiting
// verification forever with no operator recourse. The key must therefore vary with the PROOFS
// (the counts-shifting precedent), so a re-shoot is a new item while a retry of the same
// completion still de-duplicates.
//
// One enqueuer spans the whole lifecycle here on purpose — production has exactly one, and swapping
// in a fresh one mid-test is what hides the bug.
func TestReworkedDeathEvidenceReEntersVerifierQueue(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithDeathWorkflow(t)

	completeBothDeathVideos(t, svc, repo, workflowID)
	releaseApprovedDeath(t, svc, repo, workflowID)
	if enq.created() != 1 {
		t.Fatalf("first round created %d verification items, want 1", enq.created())
	}

	// Verifier rejects: both videos bounce for a re-shoot and their proofs are cleared.
	if err := svc.BounceDeathVideosForRework(context.Background(), ports.DeathVerdictCommand{
		TenantID: testTenant, WorkflowID: workflowID, Reason: "timestamp not visible", VerdictAt: time.Now(),
	}); err != nil {
		t.Fatalf("rework: %v", err)
	}

	// Operator re-shoots both videos with NEW proof artifacts.
	for _, key := range []string{domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo} {
		in := CompleteActionInput{
			TenantID: testTenant, WorkflowID: workflowID,
			ActionID: actionID(t, repo, workflowID, key),
			ProofRef: "reshoot-" + key, IdempotencyKey: "reshoot-" + key, RequestFingerprint: "fp2-" + key,
		}
		if _, err := svc.CompleteAction(context.Background(), in); err != nil {
			t.Fatalf("re-shoot %s: %v", key, err)
		}
	}

	if got := enq.created(); got != 2 {
		t.Fatalf("after rework + re-shoot the verifier queue holds %d item(s), want 2: "+
			"the re-shot evidence never reached Verify and the workflow is stranded", got)
	}

	// A plain retry of the SAME completion must still de-duplicate (no duplicate review work).
	replay := CompleteActionInput{
		TenantID: testTenant, WorkflowID: workflowID,
		ActionID:       actionID(t, repo, workflowID, domain.ActionKeyPostMortemVideo),
		ProofRef:       "reshoot-" + domain.ActionKeyPostMortemVideo,
		IdempotencyKey: "reshoot-" + domain.ActionKeyPostMortemVideo, RequestFingerprint: "fp2-" + domain.ActionKeyPostMortemVideo,
	}
	if _, err := svc.CompleteAction(context.Background(), replay); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if got := enq.created(); got != 2 {
		t.Fatalf("exact replay created a duplicate review item: %d, want 2", got)
	}
}

// A re-shoot that reuses byte-identical proof refs must STILL open a new review item. Proof ids
// alone cannot carry this: only the review round (the workflow row_version)
// distinguishes round 2 from the rejected round 1. Without it this is the same stranding bug one
// step further along.
func TestReworkedDeathEvidenceWithIdenticalProofsStillReEnters(t *testing.T) {
	svc, repo, enq, workflowID := newServiceWithDeathWorkflow(t)
	completeBothDeathVideos(t, svc, repo, workflowID)
	releaseApprovedDeath(t, svc, repo, workflowID)
	if enq.created() != 1 {
		t.Fatalf("first round created %d items, want 1", enq.created())
	}

	if err := svc.BounceDeathVideosForRework(context.Background(), ports.DeathVerdictCommand{
		TenantID: testTenant, WorkflowID: workflowID, Reason: "re-shoot", VerdictAt: time.Now(),
	}); err != nil {
		t.Fatalf("rework: %v", err)
	}

	// Same proof refs as round 1 (a client that re-sends the old artifacts), new request keys.
	for _, key := range []string{domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo} {
		in := CompleteActionInput{
			TenantID: testTenant, WorkflowID: workflowID,
			ActionID: actionID(t, repo, workflowID, key),
			ProofRef: "proof-" + key, IdempotencyKey: "round2-" + key, RequestFingerprint: "fp-r2-" + key,
		}
		if _, err := svc.CompleteAction(context.Background(), in); err != nil {
			t.Fatalf("re-shoot %s: %v", key, err)
		}
	}
	if got := enq.created(); got != 2 {
		t.Fatalf("identical-proof re-shoot created %d items, want 2: the workflow is stranded", got)
	}
}

// A verdict whose ref_id resolves to no death workflow must be ACKed (never retried forever) but
// must not vanish silently — the app layer logs it. Redelivery of a real verdict is a no-op
// mutation, not ErrNotFound, so the two cases stay distinguishable.
func TestUnroutableDeathVerdictIsAckedNotRetried(t *testing.T) {
	svc, _, _, _ := newServiceWithDeathWorkflow(t)

	if err := svc.ApplyDeathSignoffApproved(context.Background(), ports.DeathVerdictCommand{
		TenantID: testTenant, WorkflowID: "no-such-workflow", VerdictAt: time.Now(),
	}); err != nil {
		t.Fatalf("unroutable approve must ack, got %v", err)
	}
	if err := svc.BounceDeathVideosForRework(context.Background(), ports.DeathVerdictCommand{
		TenantID: testTenant, WorkflowID: "no-such-workflow", VerdictAt: time.Now(),
	}); err != nil {
		t.Fatalf("unroutable rework must ack, got %v", err)
	}
}

func TestVerdictReworkResetsVideos(t *testing.T) {
	svc, repo, _, workflowID := newServiceWithDeathWorkflow(t)
	completeBothDeathVideos(t, svc, repo, workflowID)
	applyAdminDeathApproval(t, repo, workflowID)

	err := svc.BounceDeathVideosForRework(context.Background(), ports.DeathVerdictCommand{
		TenantID: testTenant, WorkflowID: workflowID, Reason: "tag not visible", VerdictAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("rework: %v", err)
	}
	for _, key := range []string{domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo} {
		a := actionByKeyT(t, repo, workflowID, key)
		if a.Status != domain.ActionStatusRework {
			t.Fatalf("%s status = %q, want rework", key, a.Status)
		}
		if a.ProofRef != nil {
			t.Fatalf("%s proof must be cleared for the re-shoot", key)
		}
	}
	w := repo.workflows[workflowID]
	if w.State != domain.WorkflowStateOpen || w.AwaitingVerification {
		t.Fatalf("workflow after rework = %+v, want open and not awaiting", w)
	}

	// The operator re-shoots with NEW keys and the gate re-fires.
	enq := &fakeEnqueuer{}
	svc.enqueuer = enq
	for _, key := range []string{domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo} {
		in := CompleteActionInput{
			TenantID: testTenant, WorkflowID: workflowID,
			ActionID: actionID(t, repo, workflowID, key),
			ProofRef: "reshoot-" + key, IdempotencyKey: "reshoot-" + key, RequestFingerprint: "fp2-" + key,
		}
		if _, err := svc.CompleteAction(context.Background(), in); err != nil {
			t.Fatalf("re-shoot %s: %v", key, err)
		}
	}
	if len(enq.calls) != 1 {
		t.Fatalf("re-shoot enqueues = %d, want 1", len(enq.calls))
	}
}

// ---------------------------------------------------------------------------
// Openers
// ---------------------------------------------------------------------------

func TestOpenBirthWorkflowsOpensKidAndResolvedMother(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	dob := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	tob := "14:30"
	damID := "33333333-3333-3333-3333-333333333333"
	repo.goats[testGoat] = ports.GoatWorkflowFacts{GoatID: testGoat, DOB: &dob, TimeOfBirth: &tob}
	repo.goats[damID] = ports.GoatWorkflowFacts{GoatID: damID}
	repo.dams["DAM-42"] = damID

	if err := svc.OpenBirthWorkflows(context.Background(), OpenBirthWorkflowsInput{
		TenantID: testTenant, GoatID: testGoat, DamRef: "DAM-42", OccurredAt: time.Now(),
	}); err != nil {
		t.Fatalf("open: %v", err)
	}
	var kid, mother *domain.WorkflowInstance
	for id := range repo.workflows {
		w := repo.workflows[id]
		switch w.TemplateKey {
		case domain.TemplateKeyBirthKid:
			kid = &w
		case domain.TemplateKeyBirthMother:
			mother = &w
		}
	}
	if kid == nil || mother == nil {
		t.Fatalf("kid=%v mother=%v, want both opened", kid, mother)
	}
	// The kid's event moment is DOB at time_of_birth IST.
	want := time.Date(2026, 7, 27, 14, 30, 0, 0, biztime.DefaultLocation())
	if !kid.EventAt.Equal(want) {
		t.Fatalf("kid event_at = %v, want %v", kid.EventAt, want)
	}
	if mother.SubjectGoatID != damID {
		t.Fatalf("mother subject = %q, want dam %q", mother.SubjectGoatID, damID)
	}
	if kid.DamGoatID == nil || *kid.DamGoatID != damID {
		t.Fatalf("kid mother link = %v, want dam %q", kid.DamGoatID, damID)
	}

	// A TWIN (second kid, same dam) opens its own kid workflow but attaches to the SAME mother
	// workflow (natural key + ON CONFLICT semantics).
	twin := "44444444-4444-4444-4444-444444444444"
	repo.goats[twin] = ports.GoatWorkflowFacts{GoatID: twin, DOB: &dob}
	if err := svc.OpenBirthWorkflows(context.Background(), OpenBirthWorkflowsInput{
		TenantID: testTenant, GoatID: twin, DamRef: "DAM-42", OccurredAt: time.Now(),
	}); err != nil {
		t.Fatalf("twin open: %v", err)
	}
	mothers := 0
	for _, w := range repo.workflows {
		if w.TemplateKey == domain.TemplateKeyBirthMother {
			mothers++
		}
	}
	if mothers != 1 {
		t.Fatalf("mother workflows = %d, want 1 (twins share)", mothers)
	}
}

func TestOpenBirthWorkflowsSkipsUnresolvableDam(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	dob := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	repo.goats[testGoat] = ports.GoatWorkflowFacts{GoatID: testGoat, DOB: &dob}

	if err := svc.OpenBirthWorkflows(context.Background(), OpenBirthWorkflowsInput{
		TenantID: testTenant, GoatID: testGoat, DamRef: "no-such-dam", OccurredAt: time.Now(),
	}); err != nil {
		t.Fatalf("open with unresolvable dam must not fail: %v", err)
	}
	for _, w := range repo.workflows {
		if w.TemplateKey == domain.TemplateKeyBirthMother {
			t.Fatal("mother workflow must not open for an unresolvable dam")
		}
	}
}

func TestBirthMomentFallsBackTo0700(t *testing.T) {
	dob := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	got := birthMoment(&dob, nil, "", time.Now())
	want := time.Date(2026, 7, 25, 7, 0, 0, 0, biztime.DefaultLocation())
	if !got.Equal(want) {
		t.Fatalf("birthMoment fallback = %v, want %v (07:00 IST)", got, want)
	}
}

func TestIdentifierAddedRecordsTagPrerequisiteWithoutCompletingVideoTask(t *testing.T) {
	svc, repo, workflowID := newServiceWithKidWorkflow(t)
	if err := svc.CompleteTagAction(context.Background(), testTenant, testGoat, time.Now()); err != nil {
		t.Fatalf("tag completion: %v", err)
	}
	if got := actionByKeyT(t, repo, workflowID, domain.ActionKeyTagTheKid); got.Status != domain.ActionStatusPending || got.AnswerValue == nil {
		t.Fatalf("tag_the_kid = %+v, want RFID recorded and video still pending", got)
	}
	// Redelivery is a no-op.
	if err := svc.CompleteTagAction(context.Background(), testTenant, testGoat, time.Now()); err != nil {
		t.Fatalf("redelivered tag completion: %v", err)
	}
}
