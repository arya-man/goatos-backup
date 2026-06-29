package app

import (
	"context"
	"errors"
	"testing"
	"time"

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
	policy := canonicalProofPolicy(true, "video")
	policy["verify_before_apply"] = true
	result := Evaluate(shiftingDSL(), policy, validAnswers(), nil)
	if result.Valid {
		t.Fatal("missing proof should block dry-run")
	}
	if result.FinalState != "blocked" {
		t.Fatalf("final state = %s", result.FinalState)
	}

	result = Evaluate(shiftingDSL(), policy, validAnswers(), []domain.ProofReference{{ProofID: "proof-1", ProofType: "video", SubjectType: "batch", UploadState: "completed"}})
	if !result.Valid {
		t.Fatalf("completed proof should pass dry-run: %#v", result.Errors)
	}
	if result.FinalState != "needs_review" {
		t.Fatalf("proof review final state = %s", result.FinalState)
	}
}

func TestValidateVaccinationDSLAndRepeatItems(t *testing.T) {
	dsl := map[string]any{
		"schema_version":       "goatos.sop-form.v1",
		"sop_code":             "vaccination.drive",
		"title":                "Vaccination Session",
		"repeat_for_each_goat": map[string]any{"source_field": "goat_ids"},
		"fields": []any{
			map[string]any{"key": "vaccine_lot_id", "label": "Vaccine lot", "type": "vaccine_batch_picker", "required": true},
			map[string]any{"key": "cold_chain_verified", "label": "Cold chain", "type": "boolean", "required": true},
			map[string]any{"key": "goat_ids", "label": "Goats", "type": "goat_scan", "required": true, "repeat": true},
			map[string]any{"key": "dose_ml_given", "label": "Dose", "type": "number", "required": true},
			map[string]any{"key": "administration_video", "label": "Video", "type": "video_proof", "required": true},
		},
		"rules": []any{
			map[string]any{"type": "block_submission_if", "when": map[string]any{"field": "cold_chain_verified", "operator": "equals", "value": false}, "message": "cold chain must be verified"},
			map[string]any{"type": "proof_required_if", "field": "administration_video", "when": map[string]any{"field": "goat_ids", "operator": "not_empty"}},
		},
	}
	report := ValidateFormDSL(dsl, map[string]any{"required": true, "subject_scope": "batch", "types": []any{"video"}, "minimum_count": float64(1), "verify_before_apply": true})
	if !report.Valid {
		t.Fatalf("vaccination DSL should validate: %#v", report.Errors)
	}

	answers := map[string]any{
		"vaccine_lot_id":       "lot-1",
		"cold_chain_verified":  true,
		"goat_ids":             []any{"66000000-0000-4000-8000-000000000001", "66000000-0000-4000-8000-000000000002"},
		"dose_ml_given":        float64(1),
		"administration_video": "proof-1",
	}
	items := buildSubmissionItems(dsl, answers)
	if len(items) != 2 || items[0].GoatID != "66000000-0000-4000-8000-000000000001" {
		t.Fatalf("repeat items = %#v", items)
	}
}

func TestEvaluateBlocksDeclarativeRule(t *testing.T) {
	dsl := shiftingDSL()
	dsl["fields"].([]any)[0].(map[string]any)["type"] = "goat_scan"
	dsl["repeat_for_each_goat"] = map[string]any{"source_field": "goat_ids"}
	dsl["rules"] = []any{
		map[string]any{"type": "block_submission_if", "when": map[string]any{"field": "destination_count", "operator": "lt", "value": float64(1)}, "message": "destination count must be positive"},
	}
	answers := validAnswers()
	answers["destination_count"] = float64(0)
	result := Evaluate(dsl, canonicalProofPolicy(false, "video"), answers, nil)
	if result.Valid {
		t.Fatalf("expected rule block, got valid")
	}
	if result.Errors[0].Code != "blocked" {
		t.Fatalf("errors = %#v", result.Errors)
	}
}

func TestEvaluateRequiresExpectedProofSubjects(t *testing.T) {
	policy := map[string]any{
		"required":          true,
		"subject_scope":     "batch",
		"types":             []any{"video"},
		"minimum_count":     float64(3),
		"expected_subjects": []any{"shed", "vial_lot", "administration"},
	}
	refs := []domain.ProofReference{
		{ProofID: "proof-1", ProofType: "video", SubjectType: "shed", UploadState: "completed"},
		{ProofID: "proof-2", ProofType: "video", SubjectType: "vial_lot", UploadState: "completed"},
		{ProofID: "proof-3", ProofType: "video", SubjectType: "shed", UploadState: "completed"},
	}
	result := Evaluate(shiftingDSL(), policy, validAnswers(), refs)
	if result.Valid {
		t.Fatalf("expected missing administration proof subject")
	}
	if result.Errors[len(result.Errors)-1].Code != "proof_subject_required" {
		t.Fatalf("errors = %#v", result.Errors)
	}

	refs[2].SubjectType = "administration"
	result = Evaluate(shiftingDSL(), policy, validAnswers(), refs)
	if !result.Valid {
		t.Fatalf("expected all proof subjects to pass: %#v", result.Errors)
	}
}

func TestValidateProofPolicyRetentionPolicy(t *testing.T) {
	dsl := vaccinationDSL()
	validPolicy := canonicalProofPolicy(true, "video")
	validPolicy["retention_policy"] = "critical_7y"
	if report := ValidateFormDSL(dsl, validPolicy); !report.Valid {
		t.Fatalf("critical retention policy should validate: %#v", report.Errors)
	}

	invalidPolicy := canonicalProofPolicy(true, "video")
	invalidPolicy["retention_policy"] = "delete_after_review"
	report := ValidateFormDSL(dsl, invalidPolicy)
	if report.Valid {
		t.Fatalf("invalid retention policy should fail")
	}
	if len(report.Errors) == 0 || report.Errors[0].Field != "proof_policy.retention_policy" {
		t.Fatalf("errors = %#v, want proof_policy.retention_policy", report.Errors)
	}
}

func TestCreateVersionRejectsLegacyProofPolicyShape(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo)
	_, err := service.CreateVersion(context.Background(), ports.CreateVersionCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		SOPID:    testSOPID,
		Body: domain.CreateSOPVersionRequest{
			VersionLabel: "legacy",
			FormDSL:      shiftingDSL(),
			ProofPolicy: map[string]any{
				"required":      true,
				"scope":         "batch",
				"types":         []any{"video"},
				"minimum_count": float64(1),
			},
		},
	}, "trace")
	if err == nil {
		t.Fatal("expected legacy proof policy to be rejected")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "invalid_sop_dsl" {
		t.Fatalf("err = %#v", err)
	}
}

func TestValidateRejectsUnsupportedRuleKinds(t *testing.T) {
	for _, ruleType := range []string{"validation_rule", "calculated_value", "branch_to"} {
		t.Run(ruleType, func(t *testing.T) {
			dsl := shiftingDSL()
			dsl["rules"] = []any{
				map[string]any{"type": ruleType, "field": "destination_count", "when": map[string]any{"field": "destination_count", "operator": "gt", "value": float64(0)}},
			}
			report := ValidateFormDSL(dsl, canonicalProofPolicy(true, "video"))
			if report.Valid {
				t.Fatalf("expected unsupported rule type %s to fail", ruleType)
			}
		})
	}
}

func TestValidateRejectsAttachmentProofPolicy(t *testing.T) {
	report := ValidateFormDSL(shiftingDSL(), canonicalProofPolicy(true, "attachment"))
	if report.Valid {
		t.Fatal("expected attachment proof policy to be rejected")
	}
	found := false
	for _, issue := range report.Errors {
		if issue.Field == "proof_policy.types" && issue.Code == "unsupported" {
			found = true
		}
	}
	if !found {
		t.Fatalf("errors = %#v, want unsupported proof_policy.types", report.Errors)
	}
}

func TestEvaluateRejectsInvalidVaccinationAnswerTypes(t *testing.T) {
	dsl := vaccinationDSL()
	answers := map[string]any{
		"vaccine_lot_id":      "not-a-uuid",
		"cold_chain_verified": "not-a-bool",
		"goat_ids":            []any{"not-a-uuid"},
		"dose_ml_given":       "not-a-number",
		"administered_at":     "yesterday",
	}
	result := Evaluate(dsl, canonicalProofPolicy(false, "video"), answers, nil)
	if result.Valid {
		t.Fatalf("expected invalid answer types")
	}
	want := map[string]bool{
		"vaccine_lot_id":      false,
		"cold_chain_verified": false,
		"goat_ids":            false,
		"dose_ml_given":       false,
		"administered_at":     false,
	}
	for _, issue := range result.Errors {
		if issue.Code == "invalid_answer_type" {
			want[issue.Field] = true
		}
	}
	for field, found := range want {
		if !found {
			t.Fatalf("missing invalid_answer_type for %s in %#v", field, result.Errors)
		}
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
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
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

func TestSubmitVaccinationRecordsSubmissionFanoutStatus(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	hook := &fakeSubmissionHook{}
	service := NewService(repo).WithSubmissionHook(hook)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-vaccination",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if !repo.lastSubmit.SubmissionFanoutRequired {
		t.Fatal("expected durable submission fanout row to be requested")
	}
	if hook.submitted != 1 {
		t.Fatalf("submission hook calls = %d, want 1", hook.submitted)
	}
	if len(repo.recordedSubmissionFanouts) != 1 || repo.recordedSubmissionFanouts[0].Status != "completed" {
		t.Fatalf("recorded submission fanouts = %#v", repo.recordedSubmissionFanouts)
	}
}

func TestSubmitVaccinationFanoutFailureFailsRequestAndRecordsRetry(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	hook := &fakeSubmissionHook{err: errors.New("vaccination fanout materialized no completions")}
	service := NewService(repo).WithSubmissionHook(hook)

	result, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-vaccination-fail",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err == nil {
		t.Fatalf("SubmitTask() error = nil, want fanout failure")
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil on fanout failure", result)
	}
	if hook.submitted != 1 {
		t.Fatalf("submission hook calls = %d, want 1", hook.submitted)
	}
	if len(repo.recordedSubmissionFanouts) != 1 {
		t.Fatalf("recorded submission fanouts = %#v", repo.recordedSubmissionFanouts)
	}
	got := repo.recordedSubmissionFanouts[0]
	if got.Status != "failed" || got.LastError == "" {
		t.Fatalf("fanout status = %#v, want failed retryable row", got)
	}
}

func TestSubmitVaccinationReplaySkipsSubmissionFanout(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	repo.submitReplay = true
	hook := &fakeSubmissionHook{}
	service := NewService(repo).WithSubmissionHook(hook)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-vaccination-replay",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if hook.submitted != 0 {
		t.Fatalf("submission hook calls = %d, want replay to skip fanout", hook.submitted)
	}
	if len(repo.recordedSubmissionFanouts) != 0 {
		t.Fatalf("recorded submission fanouts = %#v, want none on replay", repo.recordedSubmissionFanouts)
	}
}

func TestVerifyTaskRejectsTaskWithoutReviewSubmission(t *testing.T) {
	repo := newFakeRepo()
	repo.task.State = "assigned"
	service := NewService(repo)

	_, err := service.VerifyTask(context.Background(), ports.ReviewTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body:     domain.ReviewTaskRequest{RowVersion: 1},
	}, "trace")
	if err == nil {
		t.Fatal("VerifyTask() error = nil, want fail-closed review rejection")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "task_not_reviewable" {
		t.Fatalf("err = %#v, want task_not_reviewable", err)
	}
	if repo.reviewCalls != 0 {
		t.Fatalf("ReviewTask calls = %d, want 0 before preflight passes", repo.reviewCalls)
	}
}

func TestVerifyTaskRejectsReviewWithoutRequiredProof(t *testing.T) {
	repo := newFakeRepo()
	repo.task.State = "needs_review"
	repo.submissions = []domain.SubmissionSummary{{
		SubmissionID: "65000000-0000-4000-8000-000000000001",
		TaskID:       testTaskID,
		SOPVersionID: testVersionID,
		SubmittedBy:  testActorID,
		State:        "needs_review",
	}}
	service := NewService(repo)

	_, err := service.VerifyTask(context.Background(), ports.ReviewTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body:     domain.ReviewTaskRequest{RowVersion: 1},
	}, "trace")
	if err == nil {
		t.Fatal("VerifyTask() error = nil, want proof-required rejection")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "review_proof_required" {
		t.Fatalf("err = %#v, want review_proof_required", err)
	}
	if repo.reviewCalls != 0 {
		t.Fatalf("ReviewTask calls = %d, want 0 before proof passes", repo.reviewCalls)
	}
}

func TestVerifyTaskRejectsVaccinationReviewWithNoRecordedCompletions(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.task.State = "needs_review"
	repo.version.SOPCode = "vaccination.drive"
	repo.submissions = []domain.SubmissionSummary{{
		SubmissionID: "65000000-0000-4000-8000-000000000001",
		TaskID:       testTaskID,
		SOPVersionID: testVersionID,
		SubmittedBy:  testActorID,
		ProofRefs:    completedProof(),
		State:        "needs_review",
	}}
	zero := 0
	fanout := &fakeReviewFanout{reviewable: &zero}
	service := NewService(repo).WithTaskReviewFanout(fanout)

	_, err := service.VerifyTask(context.Background(), ports.ReviewTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body:     domain.ReviewTaskRequest{RowVersion: 1},
	}, "trace")
	if err == nil {
		t.Fatal("VerifyTask() error = nil, want empty-fanout rejection")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "review_fanout_empty" {
		t.Fatalf("err = %#v, want review_fanout_empty", err)
	}
	if repo.reviewCalls != 0 {
		t.Fatalf("ReviewTask calls = %d, want 0 before completion fanout passes", repo.reviewCalls)
	}
	if fanout.verified != 0 {
		t.Fatalf("verified fanout calls = %d, want 0 before task review write", fanout.verified)
	}
}

func TestVerifyTaskRejectsVaccinationReviewWithoutFanoutWiring(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.task.State = "needs_review"
	repo.version.SOPCode = "vaccination.drive"
	repo.submissions = []domain.SubmissionSummary{{
		SubmissionID: "65000000-0000-4000-8000-000000000001",
		TaskID:       testTaskID,
		SOPVersionID: testVersionID,
		SubmittedBy:  testActorID,
		ProofRefs:    completedProof(),
		State:        "needs_review",
	}}
	service := NewService(repo)

	_, err := service.VerifyTask(context.Background(), ports.ReviewTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body:     domain.ReviewTaskRequest{RowVersion: 1},
	}, "trace")
	if err == nil {
		t.Fatal("VerifyTask() error = nil, want missing-fanout rejection")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "review_fanout_not_configured" {
		t.Fatalf("err = %#v, want review_fanout_not_configured", err)
	}
	if repo.reviewCalls != 0 {
		t.Fatalf("ReviewTask calls = %d, want 0 before fanout wiring passes", repo.reviewCalls)
	}
}

func TestVerifyTaskAcceptsSubmittedVaccinationReview(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.task.State = "needs_review"
	repo.version.SOPCode = "vaccination.drive"
	repo.submissions = []domain.SubmissionSummary{{
		SubmissionID: "65000000-0000-4000-8000-000000000001",
		TaskID:       testTaskID,
		SOPVersionID: testVersionID,
		SubmittedBy:  testActorID,
		ProofRefs:    completedProof(),
		State:        "needs_review",
	}}
	one := 1
	fanout := &fakeReviewFanout{reviewable: &one}
	service := NewService(repo).WithTaskReviewFanout(fanout)

	result, err := service.VerifyTask(context.Background(), ports.ReviewTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body:     domain.ReviewTaskRequest{RowVersion: 1},
	}, "trace")
	if err != nil {
		t.Fatalf("VerifyTask() error = %v", err)
	}
	if result.Task.State != "accepted" {
		t.Fatalf("task state = %s, want accepted", result.Task.State)
	}
	if repo.reviewCalls != 1 {
		t.Fatalf("ReviewTask calls = %d, want 1", repo.reviewCalls)
	}
	if fanout.verified != 1 {
		t.Fatalf("verified fanout calls = %d, want 1", fanout.verified)
	}
	if len(repo.recordedFanouts) != 1 || repo.recordedFanouts[0].Status != "completed" {
		t.Fatalf("recorded review fanouts = %#v", repo.recordedFanouts)
	}
}

func TestRetrySubmissionFanoutsFailsClosedAfterRecordingFailure(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.submissions = []domain.SubmissionSummary{{
		SubmissionID: "65000000-0000-4000-8000-000000000001",
		TaskID:       testTaskID,
		SubmittedBy:  testActorID,
		State:        "accepted",
	}}
	repo.submissionFanouts = []ports.SubmissionFanoutAttempt{{
		TenantID:     testTenantID,
		TaskID:       testTaskID,
		SubmissionID: "65000000-0000-4000-8000-000000000001",
		ActorID:      testActorID,
	}}
	hook := &fakeSubmissionHook{err: errors.New("still missing obligation")}
	service := NewService(repo).WithSubmissionHook(hook)

	applied, err := service.RetrySubmissionFanouts(context.Background(), testTenantID, 10)
	if err == nil {
		t.Fatal("RetrySubmissionFanouts() expected error")
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0", applied)
	}
	if len(repo.recordedSubmissionFanouts) != 1 || repo.recordedSubmissionFanouts[0].Status != "failed" {
		t.Fatalf("recorded submission fanouts = %#v", repo.recordedSubmissionFanouts)
	}
}

func TestRetryReviewFanoutsSupersedesStaleRows(t *testing.T) {
	repo := newFakeRepo()
	repo.task.State = "needs_review"
	repo.task.RowVersion = 3
	repo.reviewFanouts = []ports.ReviewFanoutAttempt{{
		TenantID:       testTenantID,
		TaskID:         testTaskID,
		TaskRowVersion: 2,
		Outcome:        "accepted",
		ActorID:        testActorID,
		Reason:         "old review",
	}}
	service := NewService(repo).WithTaskReviewFanout(&fakeReviewFanout{})

	applied, err := service.RetryReviewFanouts(context.Background(), testTenantID, 10)
	if err != nil {
		t.Fatalf("RetryReviewFanouts() error = %v", err)
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0", applied)
	}
	if len(repo.recordedFanouts) != 1 {
		t.Fatalf("recorded fanouts = %#v", repo.recordedFanouts)
	}
	got := repo.recordedFanouts[0]
	if got.Status != "superseded" || got.TaskRowVersion != 2 || got.Outcome != "accepted" {
		t.Fatalf("status = %#v, want superseded original row key", got)
	}
	if got.LastError == "" {
		t.Fatalf("expected superseded reason: %#v", got)
	}
}

func TestRetryReviewFanoutsRecordsAttemptRowVersion(t *testing.T) {
	repo := newFakeRepo()
	repo.task.State = "accepted"
	repo.task.RowVersion = 2
	repo.reviewFanouts = []ports.ReviewFanoutAttempt{{
		TenantID:       testTenantID,
		TaskID:         testTaskID,
		TaskRowVersion: 2,
		Outcome:        "accepted",
		ActorID:        testActorID,
		Reason:         "looks good",
	}}
	fanout := &fakeReviewFanout{}
	service := NewService(repo).WithTaskReviewFanout(fanout)

	applied, err := service.RetryReviewFanouts(context.Background(), testTenantID, 10)
	if err != nil {
		t.Fatalf("RetryReviewFanouts() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	if fanout.verified != 1 {
		t.Fatalf("verified fanout calls = %d, want 1", fanout.verified)
	}
	if len(repo.recordedFanouts) != 1 {
		t.Fatalf("recorded fanouts = %#v", repo.recordedFanouts)
	}
	got := repo.recordedFanouts[0]
	if got.Status != "completed" || got.TaskRowVersion != 2 || got.Outcome != "accepted" {
		t.Fatalf("status = %#v, want completed original row key", got)
	}
}

func TestRetrySubmissionFanoutsAppliesPendingRows(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.submissions = []domain.SubmissionSummary{{
		SubmissionID: "65000000-0000-4000-8000-000000000001",
		TaskID:       testTaskID,
		SubmittedBy:  testActorID,
		State:        "accepted",
	}}
	repo.submissionFanouts = []ports.SubmissionFanoutAttempt{{
		TenantID:     testTenantID,
		TaskID:       testTaskID,
		SubmissionID: "65000000-0000-4000-8000-000000000001",
		ActorID:      testActorID,
	}}
	hook := &fakeSubmissionHook{}
	service := NewService(repo).WithSubmissionHook(hook)

	applied, err := service.RetrySubmissionFanouts(context.Background(), testTenantID, 10)
	if err != nil {
		t.Fatalf("RetrySubmissionFanouts() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	if hook.submitted != 1 {
		t.Fatalf("submission hook calls = %d, want 1", hook.submitted)
	}
	if len(repo.recordedSubmissionFanouts) != 1 || repo.recordedSubmissionFanouts[0].Status != "completed" {
		t.Fatalf("recorded submission fanouts = %#v", repo.recordedSubmissionFanouts)
	}
}

func TestListAgedFailedSubmissionFanoutsUsesBoundedAgedQuery(t *testing.T) {
	repo := newFakeRepo()
	repo.failedSubmissionFanouts = []domain.FailedSubmissionFanout{{
		TaskID:                      testTaskID,
		SubmissionID:                "65000000-0000-4000-8000-000000000001",
		SOPCode:                     "vaccination.drive",
		TaskType:                    "vaccination_drive",
		TaskState:                   "needs_review",
		AttemptCount:                2,
		LastError:                   "vaccination: materialized 0 of 1 eligible submission items",
		EligibleSubmissionItems:     1,
		MaterializedCompletionCount: 0,
		MissingCompletionCount:      1,
	}}
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	service := NewService(repo).WithClock(func() time.Time { return now })

	got, err := service.ListAgedFailedSubmissionFanouts(context.Background(), testTenantID, 0, 900, "trace")
	if err != nil {
		t.Fatalf("ListAgedFailedSubmissionFanouts() error = %v", err)
	}
	if got.TraceID != "trace" || len(got.Items) != 1 || got.Items[0].MissingCompletionCount != 1 {
		t.Fatalf("response = %#v", got)
	}
	if repo.lastFailedSubmissionFanouts.TenantID != testTenantID {
		t.Fatalf("tenant = %q", repo.lastFailedSubmissionFanouts.TenantID)
	}
	if !repo.lastFailedSubmissionFanouts.UpdatedBefore.Equal(now.Add(-15 * time.Minute)) {
		t.Fatalf("updated_before = %s", repo.lastFailedSubmissionFanouts.UpdatedBefore)
	}
	if repo.lastFailedSubmissionFanouts.Limit != 100 {
		t.Fatalf("limit = %d want 100", repo.lastFailedSubmissionFanouts.Limit)
	}
}

func TestListAgedFailedSubmissionFanoutsRejectsInvalidAge(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo)

	if _, err := service.ListAgedFailedSubmissionFanouts(context.Background(), testTenantID, -1, 10, "trace"); err == nil {
		t.Fatal("negative older_than_minutes accepted")
	}
	if _, err := service.ListAgedFailedSubmissionFanouts(context.Background(), testTenantID, maxFailedSubmissionFanoutAgeMinutes+1, 10, "trace"); err == nil {
		t.Fatal("oversized older_than_minutes accepted")
	}
	if _, err := service.ListAgedFailedSubmissionFanouts(context.Background(), testTenantID, 15, -1, "trace"); err == nil {
		t.Fatal("negative limit accepted")
	}
	if repo.lastFailedSubmissionFanouts.TenantID != "" {
		t.Fatalf("repo called for invalid age: %#v", repo.lastFailedSubmissionFanouts)
	}
}

type fakeRepo struct {
	task                        domain.TaskSummary
	version                     domain.SOPVersion
	submissions                 []domain.SubmissionSummary
	lastSubmit                  ports.SubmitTaskCommand
	reviewFanouts               []ports.ReviewFanoutAttempt
	recordedFanouts             []ports.ReviewFanoutStatusCommand
	submissionFanouts           []ports.SubmissionFanoutAttempt
	recordedSubmissionFanouts   []ports.SubmissionFanoutStatusCommand
	failedSubmissionFanouts     []domain.FailedSubmissionFanout
	lastFailedSubmissionFanouts ports.ListAgedFailedSubmissionFanoutsParams
	submitReplay                bool
	reviewCalls                 int
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
			ProofPolicy:  canonicalProofPolicy(true, "video"),
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
	return f.task, &f.version, f.submissions, nil
}
func (f *fakeRepo) AssignTask(context.Context, ports.AssignTaskCommand) (domain.TaskSummary, error) {
	return f.task, nil
}
func (f *fakeRepo) ReviewTask(_ context.Context, cmd ports.ReviewTaskCommand) (domain.TaskSummary, error) {
	f.reviewCalls++
	f.task.State = cmd.State
	f.task.RowVersion++
	return f.task, nil
}
func (f *fakeRepo) ListPendingReviewFanouts(context.Context, string, int) ([]ports.ReviewFanoutAttempt, error) {
	return f.reviewFanouts, nil
}
func (f *fakeRepo) RecordReviewFanoutStatus(_ context.Context, cmd ports.ReviewFanoutStatusCommand) error {
	f.recordedFanouts = append(f.recordedFanouts, cmd)
	return nil
}
func (f *fakeRepo) ListPendingSubmissionFanouts(context.Context, string, int) ([]ports.SubmissionFanoutAttempt, error) {
	return f.submissionFanouts, nil
}
func (f *fakeRepo) RecordSubmissionFanoutStatus(_ context.Context, cmd ports.SubmissionFanoutStatusCommand) error {
	f.recordedSubmissionFanouts = append(f.recordedSubmissionFanouts, cmd)
	return nil
}
func (f *fakeRepo) ListAgedFailedSubmissionFanouts(_ context.Context, params ports.ListAgedFailedSubmissionFanoutsParams) ([]domain.FailedSubmissionFanout, error) {
	f.lastFailedSubmissionFanouts = params
	return f.failedSubmissionFanouts, nil
}
func (f *fakeRepo) SubmitTask(_ context.Context, cmd ports.SubmitTaskCommand) (domain.SubmissionSummary, domain.TaskSummary, bool, error) {
	f.lastSubmit = cmd
	if f.submitReplay {
		submission := domain.SubmissionSummary{
			SubmissionID:   "65000000-0000-4000-8000-000000000001",
			TaskID:         cmd.TaskID,
			SOPVersionID:   cmd.Body.SOPVersionID,
			SubmittedBy:    cmd.ActorID,
			IdempotencyKey: cmd.Body.IdempotencyKey,
			Answers:        cmd.Body.Answers,
			ProofRefs:      cmd.Body.ProofRefs,
			State:          f.task.State,
		}
		return submission, f.task, true, nil
	}
	f.task.State = cmd.TaskState
	submission := domain.SubmissionSummary{
		SubmissionID:   "65000000-0000-4000-8000-000000000001",
		TaskID:         cmd.TaskID,
		SOPVersionID:   cmd.Body.SOPVersionID,
		SubmittedBy:    cmd.ActorID,
		IdempotencyKey: cmd.Body.IdempotencyKey,
		Answers:        cmd.Body.Answers,
		ProofRefs:      cmd.Body.ProofRefs,
		State:          cmd.TaskState,
	}
	f.submissions = append(f.submissions, submission)
	return submission, f.task, false, nil
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

func vaccinationDSL() map[string]any {
	return map[string]any{
		"schema_version":       "goatos.sop-form.v1",
		"sop_code":             "vaccination.drive",
		"title":                "Vaccination Session",
		"repeat_for_each_goat": map[string]any{"source_field": "goat_ids"},
		"fields": []any{
			map[string]any{"key": "vaccine_lot_id", "label": "Vaccine lot", "type": "vaccine_batch_picker", "required": true},
			map[string]any{"key": "cold_chain_verified", "label": "Cold chain", "type": "boolean", "required": true},
			map[string]any{"key": "goat_ids", "label": "Goats", "type": "goat_scan", "required": true, "repeat": true},
			map[string]any{"key": "dose_ml_given", "label": "Dose", "type": "number", "required": true},
			map[string]any{"key": "administered_at", "label": "Administered at", "type": "date_time", "required": true},
			map[string]any{"key": "administration_video", "label": "Video", "type": "video_proof", "required": false},
		},
	}
}

func canonicalProofPolicy(required bool, proofType string) map[string]any {
	minimum := float64(0)
	if required {
		minimum = 1
	}
	return map[string]any{
		"required":            required,
		"subject_scope":       "batch",
		"types":               []any{proofType},
		"minimum_count":       minimum,
		"verify_before_apply": false,
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

type fakeReviewFanout struct {
	verified   int
	reworked   int
	reviewable *int
}

func (f *fakeReviewFanout) count() int {
	if f.reviewable != nil {
		return *f.reviewable
	}
	return 1
}

func (f *fakeReviewFanout) ReviewableItemCount(context.Context, string, string) (int, error) {
	return f.count(), nil
}

func (f *fakeReviewFanout) OnTaskVerified(context.Context, string, string, string) (int, error) {
	f.verified++
	return f.count(), nil
}

func (f *fakeReviewFanout) OnTaskReworked(context.Context, string, string, string, string) (int, error) {
	f.reworked++
	return f.count(), nil
}

type fakeSubmissionHook struct {
	submitted int
	err       error
}

func (f *fakeSubmissionHook) OnTaskSubmitted(context.Context, string, domain.TaskSummary, domain.SubmissionSummary) error {
	f.submitted++
	return f.err
}
