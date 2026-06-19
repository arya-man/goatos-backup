package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

const (
	testTenantID     = "00000000-0000-4000-8000-000000000001"
	testActorID      = "90000000-0000-4000-8000-000000000101"
	testOtherID      = "90000000-0000-4000-8000-000000000202"
	testSOPID        = "61000000-0000-4000-8000-000000000001"
	testVersionID    = "62000000-0000-4000-8000-000000000001"
	testOldVersionID = "62000000-0000-4000-8000-000000000099"
	testTaskID       = "63000000-0000-4000-8000-000000000001"
	testScopeID      = "64000000-0000-4000-8000-000000000001"
)

func TestEvaluateShiftingProofAndWorkflow(t *testing.T) {
	result := Evaluate(shiftingDSL(), map[string]any{"required": true, "verify_before_apply": true}, validAnswers(), nil)
	if result.Valid {
		t.Fatal("missing proof should block dry-run")
	}
	if result.FinalState != "blocked" {
		t.Fatalf("final state = %s", result.FinalState)
	}

	result = Evaluate(shiftingDSL(), map[string]any{"required": true, "verify_before_apply": true}, validAnswers(), []domain.ProofReference{{ProofID: "proof-1", ProofType: "video", SubjectType: "batch", UploadState: "completed"}})
	if !result.Valid {
		t.Fatalf("completed proof should pass dry-run: %#v", result.Errors)
	}
	if result.FinalState != "needs_review" {
		t.Fatalf("proof review final state = %s", result.FinalState)
	}
}

func TestSubmitRejectsStalePinnedVersion(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo)
	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testOldVersionID,
			IdempotencyKey: "retry-1",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err == nil {
		t.Fatal("expected stale version rejection")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "stale_sop_version" {
		t.Fatalf("err = %#v", err)
	}
}

func TestSubmitRejectsWrongAssignee(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo)
	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testOtherID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-2",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err == nil {
		t.Fatal("expected assignment rejection")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "task_not_assigned" {
		t.Fatalf("err = %#v", err)
	}
}

func TestSubmitAcceptedWritesMovementPayload(t *testing.T) {
	repo := newFakeRepo()
	repo.version.ProofPolicy = map[string]any{"required": true, "verify_before_apply": false}
	service := NewService(repo)
	result, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-3",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if result.Task.State != "accepted" || result.Submission.State != "accepted" {
		t.Fatalf("states = task %s submission %s", result.Task.State, result.Submission.State)
	}
	if repo.lastSubmit.MovementPayload["destination_location_id"] == nil {
		t.Fatalf("movement payload missing destination: %#v", repo.lastSubmit.MovementPayload)
	}
}

type fakeRepo struct {
	task       domain.TaskSummary
	version    domain.SOPVersion
	lastSubmit ports.SubmitTaskCommand
}

func newFakeRepo() *fakeRepo {
	assigned := testActorID
	return &fakeRepo{
		task: domain.TaskSummary{
			TaskID:       testTaskID,
			TenantID:     testTenantID,
			SOPID:        testSOPID,
			SOPVersionID: testVersionID,
			SOPCode:      "shifting",
			TaskType:     "shifting_direction",
			Title:        "Shift goats",
			State:        "assigned",
			AssignedTo:   &assigned,
			ScopeType:    "park",
			ScopeID:      testScopeID,
			Priority:     "normal",
			Context:      map[string]any{},
			RowVersion:   1,
		},
		version: domain.SOPVersion{
			SOPVersionID: testVersionID,
			TenantID:     testTenantID,
			SOPID:        testSOPID,
			SOPCode:      "shifting",
			Version:      1,
			VersionLabel: "Shifting v1",
			Status:       "published",
			FormDSL:      shiftingDSL(),
			ProofPolicy:  map[string]any{"required": true, "verify_before_apply": true},
		},
	}
}

func (f *fakeRepo) ListSOPs(context.Context, ports.ListSOPsParams) ([]domain.SOPDefinition, error) {
	return nil, nil
}
func (f *fakeRepo) CreateSOP(context.Context, ports.CreateSOPCommand) (domain.SOPDefinition, error) {
	return domain.SOPDefinition{}, nil
}
func (f *fakeRepo) GetSOP(context.Context, string, string) (domain.SOPDefinition, *domain.SOPVersion, error) {
	return domain.SOPDefinition{}, &f.version, nil
}
func (f *fakeRepo) CreateVersion(context.Context, ports.CreateVersionCommand) (domain.SOPVersion, error) {
	return f.version, nil
}
func (f *fakeRepo) GetVersion(context.Context, string, string, string) (domain.SOPVersion, error) {
	return f.version, nil
}
func (f *fakeRepo) GetVersionByID(context.Context, string, string) (domain.SOPVersion, error) {
	return f.version, nil
}
func (f *fakeRepo) GetPublishedVersionByCode(context.Context, string, string) (domain.SOPVersion, error) {
	return f.version, nil
}
func (f *fakeRepo) PublishVersion(context.Context, ports.VersionCommand) (domain.SOPVersion, error) {
	return f.version, nil
}
func (f *fakeRepo) RetireVersion(context.Context, ports.VersionCommand) (domain.SOPVersion, error) {
	return f.version, nil
}
func (f *fakeRepo) ListTasks(context.Context, ports.ListTasksParams) ([]domain.TaskSummary, error) {
	return []domain.TaskSummary{f.task}, nil
}
func (f *fakeRepo) CreateTask(context.Context, ports.CreateTaskCommand) (domain.TaskSummary, error) {
	return f.task, nil
}
func (f *fakeRepo) GetTask(context.Context, string, string) (domain.TaskSummary, *domain.SOPVersion, []domain.SubmissionSummary, error) {
	return f.task, &f.version, nil, nil
}
func (f *fakeRepo) AssignTask(context.Context, ports.AssignTaskCommand) (domain.TaskSummary, error) {
	return f.task, nil
}
func (f *fakeRepo) ReviewTask(context.Context, ports.ReviewTaskCommand) (domain.TaskSummary, error) {
	return f.task, nil
}
func (f *fakeRepo) SubmitTask(_ context.Context, cmd ports.SubmitTaskCommand) (domain.SubmissionSummary, domain.TaskSummary, bool, error) {
	f.lastSubmit = cmd
	f.task.State = cmd.TaskState
	return domain.SubmissionSummary{
		SubmissionID:   "65000000-0000-4000-8000-000000000001",
		TaskID:         cmd.TaskID,
		SOPVersionID:   cmd.Body.SOPVersionID,
		SubmittedBy:    cmd.ActorID,
		IdempotencyKey: cmd.Body.IdempotencyKey,
		Answers:        cmd.Body.Answers,
		ProofRefs:      cmd.Body.ProofRefs,
		State:          cmd.TaskState,
	}, f.task, false, nil
}

func shiftingDSL() map[string]any {
	return map[string]any{
		"schema_version": "goatos.sop-form.v1",
		"sop_code":       "shifting",
		"title":          "Shifting",
		"fields": []any{
			map[string]any{"key": "goat_ids", "label": "Goats", "type": "goat_lookup", "required": true},
			map[string]any{"key": "source_location_id", "label": "Source", "type": "location_picker", "required": true},
			map[string]any{"key": "destination_location_id", "label": "Destination", "type": "location_picker", "required": true},
			map[string]any{"key": "destination_count", "label": "Destination Count", "type": "number", "required": true},
			map[string]any{"key": "proof_video", "label": "Proof", "type": "video_proof", "required": true},
		},
	}
}

func validAnswers() map[string]any {
	return map[string]any{
		"goat_ids":                []any{"66000000-0000-4000-8000-000000000001"},
		"source_location_id":      "67000000-0000-4000-8000-000000000001",
		"destination_location_id": "67000000-0000-4000-8000-000000000002",
		"destination_count":       float64(1),
		"proof_video":             "proof-1",
	}
}

func completedProof() []domain.ProofReference {
	return []domain.ProofReference{{ProofID: "proof-1", ProofType: "video", SubjectType: "batch", UploadState: "completed"}}
}
