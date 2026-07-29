package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func TestGetAppTaskUsesLocaleAwarePresentationAndHidesRawUUIDTitle(t *testing.T) {
	repo := newFakeRepo()
	repo.task.TaskType = "vaccination"
	repo.task.Title = "Vaccination drive " + repo.task.ScopeID

	result, err := NewService(repo).GetAppTask(context.Background(), testTenantID, testTaskID, "hi", "trace-1")
	if err != nil {
		t.Fatalf("GetAppTask: %v", err)
	}
	if result.Task.Presentation == nil || result.Task.Presentation.Eyebrow != "टीकाकरण" || result.Task.Presentation.Title != "शेड रिकॉर्ड" {
		t.Fatalf("presentation=%+v want Hindi task copy", result.Task.Presentation)
	}
	if strings.Contains(result.Task.Title, repo.task.ScopeID) {
		t.Fatalf("app task title exposed raw UUID: %q", result.Task.Title)
	}
}

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
	delete(dsl, "repeat_for_each_goat")
	items = buildSubmissionItems(dsl, answers)
	if len(items) != 2 || items[0].GoatID != "66000000-0000-4000-8000-000000000001" || items[1].GoatID != "66000000-0000-4000-8000-000000000002" {
		t.Fatalf("field-level repeat items = %#v", items)
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

func TestEvaluateRequiresCompletedCameraProofForEveryScannedGoat(t *testing.T) {
	goatA := "66000000-0000-4000-8000-000000000001"
	goatB := "66000000-0000-4000-8000-000000000002"
	dsl := vaccinationDSL()
	dsl["fields"] = dsl["fields"].([]any)[:5]
	dsl["goat_row_proof"] = map[string]any{
		"subject_scope": "goat", "capture_source": "in_app_camera",
		"minimum_clips": float64(1), "maximum_clips": float64(5),
	}
	policy := map[string]any{
		"required": true, "subject_scope": "goat", "types": []any{"video"},
		"minimum_count": float64(1), "minimum_count_per_subject": float64(1),
		"maximum_count_per_subject": float64(5), "expected_subjects": []any{"goat"},
		"verify_before_apply": true,
	}
	answers := map[string]any{
		"vaccine_lot_id": "67000000-0000-4000-8000-000000000001", "cold_chain_verified": true,
		"goat_ids": []any{goatA, goatB}, "dose_ml_given": float64(1),
		"administered_at": "2026-07-19T08:00:00Z",
	}
	refs := []domain.ProofReference{{
		ProofID: "proof-a", ProofType: "video", SubjectType: "goat", SubjectID: &goatA, UploadState: "completed",
	}}
	if result := Evaluate(dsl, policy, answers, refs); result.Valid {
		t.Fatalf("one goat proof satisfied a two-goat submission: %#v", result)
	}
	refs = append(refs, domain.ProofReference{
		ProofID: "proof-b", ProofType: "video", SubjectType: "goat", SubjectID: &goatB, UploadState: "completed",
	})
	result := Evaluate(dsl, policy, answers, refs)
	if !result.Valid || result.FinalState != "needs_review" {
		t.Fatalf("per-goat proof should reach review: %#v", result)
	}

	unrelated := "66000000-0000-4000-8000-000000000099"
	refs[1].SubjectID = &unrelated
	if result := Evaluate(dsl, policy, answers, refs); result.Valid {
		t.Fatalf("unrelated goat proof satisfied selected goat: %#v", result)
	}
}

func TestValidateGoatRowProofPolicyWithoutStandaloneProofField(t *testing.T) {
	dsl := vaccinationDSL()
	dsl["fields"] = dsl["fields"].([]any)[:5]
	dsl["goat_row_proof"] = map[string]any{
		"subject_scope": "goat", "capture_source": "in_app_camera",
	}
	policy := map[string]any{
		"required": true, "subject_scope": "goat", "types": []any{"video"},
		"minimum_count": float64(1), "minimum_count_per_subject": float64(1),
		"maximum_count_per_subject": float64(5),
	}
	if report := ValidateFormDSL(dsl, policy); !report.Valid {
		t.Fatalf("goat-row camera proof contract should validate: %#v", report.Errors)
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

func TestCreateVersionRejectsOldProofPolicyShape(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo)
	_, err := service.CreateVersion(context.Background(), ports.CreateVersionCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		SOPID:    testSOPID,
		Body: domain.CreateSOPVersionRequest{
			VersionLabel: "old-shape",
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

func TestCreateVersionRejectsVaccinationDriveShedProofPolicy(t *testing.T) {
	repo := newFakeRepo()
	repo.sop.Code = "vaccination.drive"
	service := NewService(repo)
	dsl := vaccinationDSL()
	dsl["goat_row_proof"] = map[string]any{
		"subject_scope": "goat", "capture_source": "in_app_camera",
		"minimum_clips": float64(1), "maximum_clips": float64(5),
	}
	_, err := service.CreateVersion(context.Background(), ports.CreateVersionCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		SOPID:    testSOPID,
		Body: domain.CreateSOPVersionRequest{
			VersionLabel: "bad-vaccination-proof",
			FormDSL:      dsl,
			ProofPolicy: map[string]any{
				"required":      true,
				"subject_scope": "shed",
				"types":         []any{"video"},
				"minimum_count": float64(1),
			},
		},
	}, "trace")
	if err == nil {
		t.Fatal("expected vaccination.drive shed proof policy to be rejected")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "invalid_sop_dsl" {
		t.Fatalf("err = %#v", err)
	}
}

func TestCreateVersionAcceptsVaccinationDriveShedLevelProofPolicy(t *testing.T) {
	repo := newFakeRepo()
	repo.sop.Code = "vaccination.drive"
	service := NewService(repo)
	dsl := vaccinationDSL()
	fields := dsl["fields"].([]any)
	fields = append(fields, map[string]any{
		"key":           "shed_video",
		"label":         "Shed vaccination video",
		"type":          "video_proof",
		"required":      true,
		"repeat":        true,
		"proof_subject": "shed",
	})
	dsl["fields"] = fields
	_, err := service.CreateVersion(context.Background(), ports.CreateVersionCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		SOPID:    testSOPID,
		Body: domain.CreateSOPVersionRequest{
			VersionLabel: "shed-level-proof",
			FormDSL:      dsl,
			ProofPolicy: map[string]any{
				"required":                  true,
				"proof_mode":                "shed_level_video",
				"subject_scope":             "shed",
				"types":                     []any{"video"},
				"minimum_count":             float64(1),
				"maximum_count":             float64(5),
				"maximum_count_per_subject": float64(5),
				"expected_subjects":         []any{"shed"},
				"capture_source":            "in_app_camera",
				"allowed_capture_sources":   []any{"in_app_camera", "gallery_picker"},
				"verify_before_apply":       true,
			},
		},
	}, "trace")
	if err != nil {
		t.Fatalf("valid vaccination.drive shed-level proof policy should be accepted: %#v", err)
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
		"goat_ids":            []any{false},
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

func TestSubmitMergesServerDraftScanCapturesOneToManyPageBoundaryStatusMatrix(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.FormDSL = vaccinationDSL()
	repo.version.ProofPolicy = canonicalProofPolicy(false, "video")
	repo.scanCaptures = []domain.ScanCaptureSummary{{
		CaptureID:    "66000000-0000-4000-8000-000000000001",
		TaskID:       testTaskID,
		FieldKey:     rosterScanFieldKey,
		Tag:          "901007000504392",
		GoatID:       "66000000-0000-4000-8000-000000000001",
		ObligationID: "68000000-0000-4000-8000-000000000001",
		CapturedAt:   "2026-07-17T00:00:00Z",
	}, {
		CaptureID:    "66000000-0000-4000-8000-000000000002",
		TaskID:       testTaskID,
		FieldKey:     rosterScanFieldKey,
		Tag:          "901007000504418",
		GoatID:       "66000000-0000-4000-8000-000000000002",
		ObligationID: "68000000-0000-4000-8000-000000000002",
		CapturedAt:   "2026-07-17T00:00:01Z",
	}}
	service := NewService(repo)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-draft-scan",
			Answers: map[string]any{
				"vaccine_lot_id":      "69000000-0000-4000-8000-000000000001",
				"cold_chain_verified": true,
				// Simulate an older client sending one RFID alias while the other item already
				// uses the canonical identity. Durable scan captures must canonicalize both.
				"goat_ids":        []any{"901007000504392", "66000000-0000-4000-8000-000000000002"},
				"dose_ml_given":   float64(1),
				"administered_at": "2026-07-17T00:00:00Z",
			},
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	got, ok := repo.lastSubmit.Body.Answers["goat_ids"].([]any)
	if !ok || len(got) != 2 || got[0] != "66000000-0000-4000-8000-000000000001" || got[1] != "66000000-0000-4000-8000-000000000002" {
		t.Fatalf("merged goat_ids = %#v", repo.lastSubmit.Body.Answers["goat_ids"])
	}
	if len(repo.lastSubmit.SubmissionItems) != 2 ||
		repo.lastSubmit.SubmissionItems[0].GoatID != "66000000-0000-4000-8000-000000000001" ||
		repo.lastSubmit.SubmissionItems[1].GoatID != "66000000-0000-4000-8000-000000000002" {
		t.Fatalf("submission items = %#v", repo.lastSubmit.SubmissionItems)
	}
}

func TestSubmitVaccinationShedCompletionAckSkipsGenericProofRefsWhenReady(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.FormDSL = map[string]any{
		"schema_version": "goatos.sop-form.v1",
		"fields":         []any{},
	}
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	goatID := "66000000-0000-4000-8000-000000000001"
	repo.completedTaskGoatProofRefs = []domain.ProofReference{{
		ProofID:     "67000000-0000-4000-8000-000000000001",
		ProofType:   "video",
		SubjectType: "goat",
		SubjectID:   &goatID,
		UploadState: "completed",
	}}
	service := NewService(repo)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "vaccination-shed-ack-ready",
			Answers:        map[string]any{},
			ProofRefs:      nil,
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if repo.lastSubmit.TaskState != "accepted" {
		t.Fatalf("task state = %q, want accepted", repo.lastSubmit.TaskState)
	}
	if got := repo.lastSubmit.Body.ProofRefs; len(got) != 1 || got[0].ProofID != "67000000-0000-4000-8000-000000000001" || got[0].SubjectID == nil || *got[0].SubjectID != goatID {
		t.Fatalf("proof refs = %#v, want backend-attached completed goat proof", got)
	}
}

func TestSubmitVaccinationPerGoatCompletionAckUsesScopedShedReadiness(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.FormDSL = map[string]any{
		"schema_version": "goatos.sop-form.v1",
		"fields":         []any{},
	}
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	shedID := testScopeID
	goatID := "66000000-0000-4000-8000-000000000001"
	repo.completedTaskGoatProofRefs = []domain.ProofReference{{
		ProofID:     "67000000-0000-4000-8000-000000000001",
		ProofType:   "video",
		SubjectType: "goat",
		SubjectID:   &goatID,
		UploadState: "completed",
	}}
	service := NewService(repo)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "shed-submit:" + testTaskID + ":scope:" + shedID + ":rv:1",
			Answers:        map[string]any{},
			ProofRefs:      nil,
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if got := repo.lastShedReadinessShedID; got != shedID {
		t.Fatalf("readiness shed id = %q, want %q", got, shedID)
	}
	if got := repo.lastCompletedProofRefsShedID; got != shedID {
		t.Fatalf("server-proof recovery shed id = %q, want %q", got, shedID)
	}
	if got := repo.lastSubmit.Body.ProofRefs; len(got) != 1 || got[0].ProofID != "67000000-0000-4000-8000-000000000001" {
		t.Fatalf("proof refs = %#v, want recovered server goat proof", got)
	}
}

func TestSubmitVaccinationShedCompletionAckBlocksWhenSummaryNotReady(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.FormDSL = map[string]any{
		"schema_version": "goatos.sop-form.v1",
		"fields":         []any{},
	}
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	repo.shedReadiness = ports.ShedCompletionReadiness{Enabled: false, Reason: "1 animals still need proof"}
	service := NewService(repo)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "vaccination-shed-ack-blocked",
			Answers:        map[string]any{},
			ProofRefs:      nil,
		},
	}, "trace")
	if err == nil {
		t.Fatal("SubmitTask() error = nil, want shed_completion_not_ready")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "shed_completion_not_ready" {
		t.Fatalf("err = %#v", err)
	}
	if repo.lastSubmit.Body.IdempotencyKey != "" {
		t.Fatalf("submission should not be written when blocked: %#v", repo.lastSubmit)
	}
}

func TestSubmitVaccinationShedCompletionAckRecoversServerShedProofWhenMobileCacheIsEmpty(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.FormDSL = map[string]any{
		"schema_version": "goatos.sop-form.v1",
		"fields":         []any{},
	}
	repo.version.ProofPolicy = map[string]any{
		"required":            true,
		"types":               []any{"video"},
		"subject_scope":       "shed",
		"proof_mode":          "shed_level_video",
		"minimum_count":       1,
		"maximum_count":       5,
		"verify_before_apply": true,
	}
	shedID := testScopeID
	repo.completedTaskGoatProofRefs = []domain.ProofReference{{
		ProofID:     "67000000-0000-4000-8000-000000000010",
		ProofType:   "video",
		SubjectType: "shed",
		SubjectID:   &shedID,
		UploadState: "completed",
	}}
	service := NewService(repo)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   "",
			IdempotencyKey: "shed-submit:" + testTaskID + ":scope:" + shedID + ":rv:1",
			Answers:        map[string]any{},
			ProofRefs:      nil,
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if repo.lastCompletedProofRefsShedID != shedID {
		t.Fatalf("server-proof recovery shed id = %q, want %q", repo.lastCompletedProofRefsShedID, shedID)
	}
	if got := repo.lastShedReadinessShedID; got != shedID {
		t.Fatalf("readiness shed id = %q, want %q", got, shedID)
	}
	if got := repo.lastSubmit.Body.SOPVersionID; got != testVersionID {
		t.Fatalf("submitted SOP version = %q, want task pinned version %q", got, testVersionID)
	}
	if got := repo.lastSubmit.Body.ProofRefs; len(got) != 1 || got[0].ProofID != "67000000-0000-4000-8000-000000000010" {
		t.Fatalf("proof refs = %#v, want recovered server shed proof", got)
	}
	if repo.lastSubmit.TaskState != "needs_review" {
		t.Fatalf("task state = %q, want needs_review", repo.lastSubmit.TaskState)
	}
}

func TestRecordScanCaptureValidatesAndPersistsDraft(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.FormDSL = vaccinationDSL()
	service := NewService(repo)

	result, err := service.RecordScanCapture(context.Background(), ports.RecordScanCaptureCommand{
		TenantID:       testTenantID,
		ActorID:        testActorID,
		TaskID:         testTaskID,
		IdempotencyKey: "scan:test",
		Body: domain.ScanCaptureRequest{
			FieldKey: rosterScanFieldKey,
			Tag:      "901007000504392",
			GoatID:   "66000000-0000-4000-8000-000000000001",
		},
	}, "trace")
	if err != nil {
		t.Fatalf("RecordScanCapture() error = %v", err)
	}
	if result.Capture.Tag != "901007000504392" || repo.lastScanCapture.Body.FieldKey != rosterScanFieldKey {
		t.Fatalf("capture = %#v last = %#v", result.Capture, repo.lastScanCapture)
	}
}

func TestRecordScanAttemptValidatesAndPersistsDuplicateAudit(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.FormDSL = vaccinationDSL()
	service := NewService(repo)

	result, err := service.RecordScanAttempt(context.Background(), ports.RecordScanAttemptCommand{
		TenantID:       testTenantID,
		ActorID:        testActorID,
		TaskID:         testTaskID,
		IdempotencyKey: "scan-attempt:test",
		Body: domain.ScanAttemptRequest{
			FieldKey:     rosterScanFieldKey,
			Tag:          "901007000504419",
			GoatID:       "66000000-0000-4000-8000-000000000001",
			ObligationID: "67000000-0000-4000-8000-000000000001",
			Outcome:      "duplicate",
			TagRole:      "secondary",
			Reason:       "goat_already_scanned",
		},
	}, "trace")
	if err != nil {
		t.Fatalf("RecordScanAttempt() error = %v", err)
	}
	if result.Attempt.Outcome != "duplicate" || result.Attempt.TagRole != "secondary" {
		t.Fatalf("attempt = %#v", result.Attempt)
	}
	if repo.lastScanAttempt.Body.Reason != "goat_already_scanned" {
		t.Fatalf("last attempt = %#v", repo.lastScanAttempt)
	}
}

func TestRecordScanAttemptRejectsInvalidOutcome(t *testing.T) {
	repo := newFakeRepo()
	repo.version.FormDSL = vaccinationDSL()
	service := NewService(repo)

	_, err := service.RecordScanAttempt(context.Background(), ports.RecordScanAttemptCommand{
		TenantID:       testTenantID,
		ActorID:        testActorID,
		TaskID:         testTaskID,
		IdempotencyKey: "scan-attempt:test",
		Body: domain.ScanAttemptRequest{
			FieldKey: rosterScanFieldKey,
			Tag:      "901007000504419",
			Outcome:  "count_it_twice",
		},
	}, "trace")
	if err == nil {
		t.Fatal("RecordScanAttempt() accepted invalid outcome")
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

func TestSubmitRejectsForgedProofRefsBeforeRepoWrite(t *testing.T) {
	repo := newFakeRepo()
	proofs := &fakeProofValidator{err: errors.New("proof does not belong to task")}
	service := NewService(repo).WithProofValidator(proofs)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-forged-proof",
			Answers:        validAnswers(),
			ProofRefs: []domain.ProofReference{{
				ProofID:     "10000000-0000-4000-8000-000000000111",
				ProofType:   "video",
				SubjectType: "batch",
				UploadState: "completed",
			}},
		},
	}, "trace")
	if err == nil {
		t.Fatal("SubmitTask() error = nil, want invalid proof refs")
	}
	if appErr, ok := err.(*Error); !ok || appErr.Code != "invalid_proof_refs" {
		t.Fatalf("err = %#v, want invalid_proof_refs", err)
	}
	if repo.lastSubmit.TaskID != "" {
		t.Fatalf("repo write happened despite rejected proof refs: %#v", repo.lastSubmit)
	}
	if proofs.calls != 1 || proofs.binding.TaskID != testTaskID || proofs.binding.ScopeID != testScopeID {
		t.Fatalf("proof validator calls=%d binding=%#v", proofs.calls, proofs.binding)
	}
}

func TestSubmitNormalizesProofRefsThroughServerValidator(t *testing.T) {
	repo := newFakeRepo()
	normalized := []domain.ProofReference{{
		ProofID:     "10000000-0000-4000-8000-000000000111",
		ProofType:   "video",
		SubjectType: "batch",
		UploadState: "completed",
		Metadata:    map[string]any{"storage_provider": "local"},
	}}
	proofs := &fakeProofValidator{resolved: normalized}
	service := NewService(repo).WithProofValidator(proofs)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-server-proof",
			Answers:        validAnswers(),
			ProofRefs: []domain.ProofReference{{
				ProofID:     "10000000-0000-4000-8000-000000000111",
				ProofType:   "video",
				SubjectType: "client-forged-subject",
				UploadState: "completed",
			}},
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if proofs.calls != 1 {
		t.Fatalf("proof validator calls = %d, want 1", proofs.calls)
	}
	if got := repo.lastSubmit.Body.ProofRefs; len(got) != 1 || got[0].SubjectType != "batch" || got[0].Metadata["storage_provider"] != "local" {
		t.Fatalf("proof refs persisted without server normalization: %#v", got)
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

func TestSubmitReplaySkipsProofRetention(t *testing.T) {
	repo := newFakeRepo()
	repo.submitReplay = true
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	repo.version.ProofPolicy["retention_policy"] = "operational_90d"
	proofs := &fakeProofValidator{resolved: completedProof()}
	service := NewService(repo).WithProofValidator(proofs)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-retention-replay",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if proofs.retentionCalls != 0 {
		t.Fatalf("retention calls = %d, want replay to skip proof mutation", proofs.retentionCalls)
	}
}

func TestSubmitFailureSkipsProofRetention(t *testing.T) {
	repo := newFakeRepo()
	repo.submitErr = errors.New("submit transaction failed")
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	repo.version.ProofPolicy["retention_policy"] = "operational_90d"
	proofs := &fakeProofValidator{resolved: completedProof()}
	service := NewService(repo).WithProofValidator(proofs)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-retention-fail",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err == nil {
		t.Fatal("SubmitTask() error = nil, want submit failure")
	}
	if proofs.retentionCalls != 0 {
		t.Fatalf("retention calls = %d, want failed submit to skip proof mutation", proofs.retentionCalls)
	}
}

func TestSubmitSuccessAppliesProofRetention(t *testing.T) {
	repo := newFakeRepo()
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	repo.version.ProofPolicy["retention_policy"] = "operational_90d"
	proofs := &fakeProofValidator{resolved: completedProof()}
	service := NewService(repo).WithProofValidator(proofs)

	_, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-retention-success",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if proofs.retentionCalls != 1 || proofs.retentionPolicy != "operational_90d" {
		t.Fatalf("retention calls/policy = %d/%q, want one operational_90d call", proofs.retentionCalls, proofs.retentionPolicy)
	}
}

func TestSubmitRetentionFailureDoesNotFailCommittedSubmissionOrBlockFanout(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	repo.version.SOPCode = "vaccination.drive"
	repo.version.ProofPolicy = canonicalProofPolicy(true, "video")
	repo.version.ProofPolicy["retention_policy"] = "operational_90d"
	proofs := &fakeProofValidator{resolved: completedProof(), retentionErr: errors.New("proof retention temporarily unavailable")}
	hook := &fakeSubmissionHook{}
	service := NewService(repo).WithProofValidator(proofs).WithSubmissionHook(hook)

	result, err := service.SubmitTask(context.Background(), ports.SubmitTaskCommand{
		TenantID: testTenantID,
		ActorID:  testActorID,
		TaskID:   testTaskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   testVersionID,
			IdempotencyKey: "retry-retention-post-commit",
			Answers:        validAnswers(),
			ProofRefs:      completedProof(),
		},
	}, "trace")
	if err != nil {
		t.Fatalf("SubmitTask() error = %v, want committed response despite retention failure", err)
	}
	if result == nil || result.Submission.SubmissionID == "" {
		t.Fatalf("result = %#v, want committed submission response", result)
	}
	if proofs.retentionCalls != 1 {
		t.Fatalf("retention calls = %d, want one best-effort attempt", proofs.retentionCalls)
	}
	if hook.submitted != 1 {
		t.Fatalf("submission hook calls = %d, want fanout after retention failure", hook.submitted)
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

func TestRetrySubmissionFanoutsContinuesAfterPoisonRow(t *testing.T) {
	repo := newFakeRepo()
	repo.task.SOPCode = "vaccination.drive"
	repo.task.TaskType = "vaccination"
	failingSubmissionID := "65000000-0000-4000-8000-000000000001"
	successSubmissionID := "65000000-0000-4000-8000-000000000002"
	repo.submissions = []domain.SubmissionSummary{
		{
			SubmissionID: failingSubmissionID,
			TaskID:       testTaskID,
			SubmittedBy:  testActorID,
			State:        "accepted",
		},
		{
			SubmissionID: successSubmissionID,
			TaskID:       testTaskID,
			SubmittedBy:  testActorID,
			State:        "accepted",
		},
	}
	repo.submissionFanouts = []ports.SubmissionFanoutAttempt{
		{
			TenantID:     testTenantID,
			TaskID:       testTaskID,
			SubmissionID: failingSubmissionID,
			ActorID:      testActorID,
		},
		{
			TenantID:     testTenantID,
			TaskID:       testTaskID,
			SubmissionID: successSubmissionID,
			ActorID:      testActorID,
		},
	}
	hook := &fakeSubmissionHook{errBySubmission: map[string]error{
		failingSubmissionID: errors.New("poison submission"),
	}}
	service := NewService(repo).WithSubmissionHook(hook)

	applied, err := service.RetrySubmissionFanouts(context.Background(), testTenantID, 10)
	if err == nil {
		t.Fatal("RetrySubmissionFanouts() expected aggregate error")
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	if hook.submitted != 2 {
		t.Fatalf("submission hook calls = %d, want 2", hook.submitted)
	}
	if len(repo.recordedSubmissionFanouts) != 2 {
		t.Fatalf("recorded submission fanouts = %#v", repo.recordedSubmissionFanouts)
	}
	if got := repo.recordedSubmissionFanouts[0]; got.SubmissionID != failingSubmissionID || got.Status != "failed" {
		t.Fatalf("first recorded fanout = %#v, want failed poison row", got)
	}
	if got := repo.recordedSubmissionFanouts[1]; got.SubmissionID != successSubmissionID || got.Status != "completed" {
		t.Fatalf("second recorded fanout = %#v, want completed repairable row", got)
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

// TestListSOPsLoadsLatestVersionsInOneBatchCall proves the SOP Library list is O(1) in repo calls:
// growing the SOP count does not add per-SOP detail calls (C35-015 — no list-then-N-details N+1). The
// service must batch every listed SOP's latest version into a single LatestVersionsFor call.
func TestListSOPsLoadsLatestVersionsInOneBatchCall(t *testing.T) {
	// The public list contract caps one keyset page at 100 rows. Exercise that
	// maximum here; larger result sets continue through next_cursor rather than
	// widening a single batch response.
	for _, n := range []int{1, 50, 100} {
		repo := newFakeRepo()
		defs := make([]domain.SOPDefinition, n)
		versions := make(map[string]domain.SOPVersion, n)
		for i := range defs {
			id := fmt.Sprintf("70000000-0000-4000-8000-%012d", i)
			defs[i] = domain.SOPDefinition{SOPID: id, TenantID: testTenantID, Code: "vaccination.drive", Name: "Vaccination"}
			versions[id] = domain.SOPVersion{SOPVersionID: "v-" + id, SOPID: id, Version: 1, Status: "published"}
		}
		repo.listSOPsResult = defs
		repo.latestVersionsForResult = versions
		service := NewService(repo)

		resp, err := service.ListSOPs(context.Background(), ports.ListSOPsParams{TenantID: testTenantID, Limit: 100}, "trace")
		if err != nil {
			t.Fatalf("n=%d: ListSOPs error: %v", n, err)
		}
		if repo.latestVersionsForCalls != 1 {
			t.Fatalf("n=%d: LatestVersionsFor called %d times, want exactly 1 (O(1) batch, not per-SOP)", n, repo.latestVersionsForCalls)
		}
		if len(repo.lastLatestVersionsForIDs) != n {
			t.Fatalf("n=%d: batched %d sop ids, want %d", n, len(repo.lastLatestVersionsForIDs), n)
		}
		if len(resp.LatestVersions) != n {
			t.Fatalf("n=%d: response carried %d latest versions, want %d", n, len(resp.LatestVersions), n)
		}
		for _, def := range defs {
			if v := resp.LatestVersions[def.SOPID]; v == nil || v.SOPID != def.SOPID {
				t.Fatalf("n=%d: latest version for %s missing/mismatched", n, def.SOPID)
			}
		}
	}
}

// TestListSOPsWithoutVersionsOmitsLatestVersions confirms the map is nil (JSON-omitted) when no listed
// SOP has a version, so the additive contract field never appears empty.
func TestListSOPsWithoutVersionsOmitsLatestVersions(t *testing.T) {
	repo := newFakeRepo()
	repo.listSOPsResult = []domain.SOPDefinition{{SOPID: "70000000-0000-4000-8000-000000000001", TenantID: testTenantID, Code: "vaccination.drive"}}
	repo.latestVersionsForResult = map[string]domain.SOPVersion{}
	service := NewService(repo)

	resp, err := service.ListSOPs(context.Background(), ports.ListSOPsParams{TenantID: testTenantID, Limit: 200}, "trace")
	if err != nil {
		t.Fatalf("ListSOPs error: %v", err)
	}
	if resp.LatestVersions != nil {
		t.Fatalf("LatestVersions = %#v, want nil when no SOP has a version", resp.LatestVersions)
	}
}

func TestListAppTasksUsesTwentyRowBoundaryAndCompleteMetadata(t *testing.T) {
	repo := newFakeRepo()
	dueAt := "2026-07-21T09:00:00Z"
	assignedTo := testActorID
	for i := 1; i <= AppTaskPageSize+1; i++ {
		repo.listTasksResult = append(repo.listTasksResult, domain.TaskSummary{
			TaskID:     fmt.Sprintf("20000000-0000-4000-8000-%012d", i),
			TenantID:   testTenantID,
			AssignedTo: &assignedTo,
			DueAt:      &dueAt,
		})
	}
	repo.listTasksTotal = int64(len(repo.listTasksResult))
	service := NewService(repo)

	first, err := service.ListAppTasks(context.Background(), ports.ListTasksParams{
		TenantID: testTenantID,
		ActorID:  testActorID,
	}, "trace")
	if err != nil {
		t.Fatal(err)
	}
	if repo.lastListTasks.Limit != AppTaskPageSize+1 {
		t.Fatalf("repository limit=%d want %d lookahead rows", repo.lastListTasks.Limit, AppTaskPageSize+1)
	}
	if len(first.Items) != AppTaskPageSize || first.Total != int64(AppTaskPageSize+1) || !first.HasMore || first.NextCursor == nil {
		t.Fatalf("first page=%#v", first)
	}
	decoded, err := domain.DecodeTaskCursor(*first.NextCursor)
	if err != nil || decoded.TaskID != first.Items[AppTaskPageSize-1].TaskID {
		t.Fatalf("next cursor=%+v err=%v", decoded, err)
	}

	repo.listTasksResult = repo.listTasksResult[AppTaskPageSize:]
	last, err := service.ListAppTasks(context.Background(), ports.ListTasksParams{
		TenantID: testTenantID,
		ActorID:  testActorID,
		Cursor:   &decoded,
	}, "trace-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Items) != 1 || last.Total != int64(AppTaskPageSize+1) || last.HasMore || last.NextCursor != nil {
		t.Fatalf("last page=%#v", last)
	}
}

func TestListSOPsNormalizesFiltersAndReturnsOpaqueNextCursor(t *testing.T) {
	repo := newFakeRepo()
	repo.listSOPsResult = []domain.SOPDefinition{
		{SOPID: "70000000-0000-4000-8000-000000000002", TenantID: testTenantID, Code: "vaccination.drive", UpdatedAt: "2026-07-12T10:00:00Z"},
		{SOPID: "70000000-0000-4000-8000-000000000001", TenantID: testTenantID, Code: "vaccination.booster", UpdatedAt: "2026-07-12T09:00:00Z"},
	}
	repo.latestVersionsForResult = map[string]domain.SOPVersion{
		repo.listSOPsResult[0].SOPID: {SOPID: repo.listSOPsResult[0].SOPID, SOPVersionID: testVersionID},
	}
	service := NewService(repo)

	resp, err := service.ListSOPs(context.Background(), ports.ListSOPsParams{
		TenantID: testTenantID, Status: " active ", CodePrefix: " Vaccination. ", Search: " Drive ", Limit: 1,
	}, "trace")
	if err != nil {
		t.Fatalf("ListSOPs error: %v", err)
	}
	if len(resp.Items) != 1 || resp.NextCursor == nil {
		t.Fatalf("items=%d next=%v, want one item plus cursor", len(resp.Items), resp.NextCursor)
	}
	if repo.lastListSOPs.Status != "active" || repo.lastListSOPs.CodePrefix != "vaccination." || repo.lastListSOPs.Search != "Drive" || repo.lastListSOPs.Limit != 2 {
		t.Fatalf("repo params = %#v", repo.lastListSOPs)
	}
	if len(repo.lastLatestVersionsForIDs) != 1 || repo.lastLatestVersionsForIDs[0] != resp.Items[0].SOPID {
		t.Fatalf("latest version ids = %#v, want only visible page", repo.lastLatestVersionsForIDs)
	}
	decoded, err := domain.DecodeSOPCursor(*resp.NextCursor)
	if err != nil || decoded.SOPID != resp.Items[0].SOPID {
		t.Fatalf("next cursor=%+v err=%v", decoded, err)
	}
}

type fakeRepo struct {
	sop                          domain.SOPDefinition
	task                         domain.TaskSummary
	version                      domain.SOPVersion
	submissions                  []domain.SubmissionSummary
	lastSubmit                   ports.SubmitTaskCommand
	reviewFanouts                []ports.ReviewFanoutAttempt
	recordedFanouts              []ports.ReviewFanoutStatusCommand
	submissionFanouts            []ports.SubmissionFanoutAttempt
	recordedSubmissionFanouts    []ports.SubmissionFanoutStatusCommand
	failedSubmissionFanouts      []domain.FailedSubmissionFanout
	lastFailedSubmissionFanouts  ports.ListAgedFailedSubmissionFanoutsParams
	scanCaptures                 []domain.ScanCaptureSummary
	shedReadiness                ports.ShedCompletionReadiness
	shedReadinessErr             error
	lastShedReadinessShedID      string
	completedTaskGoatProofRefs   []domain.ProofReference
	completedTaskGoatProofErr    error
	lastCompletedProofRefsShedID string
	lastScanCapture              ports.RecordScanCaptureCommand
	scanAttempts                 []domain.ScanAttemptSummary
	lastScanAttempt              ports.RecordScanAttemptCommand
	submitReplay                 bool
	submitErr                    error
	reviewCalls                  int
	listSOPsResult               []domain.SOPDefinition
	lastListSOPs                 ports.ListSOPsParams
	latestVersionsForResult      map[string]domain.SOPVersion
	latestVersionsForCalls       int
	lastLatestVersionsForIDs     []string
	listTasksResult              []domain.TaskSummary
	listTasksTotal               int64
	lastListTasks                ports.ListTasksParams
}

func newFakeRepo() *fakeRepo {
	assigned := testActorID
	return &fakeRepo{
		sop: domain.SOPDefinition{
			SOPID:    testSOPID,
			TenantID: testTenantID,
			Code:     "shifting",
			Name:     "Shifting",
			Status:   "active",
		},
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
		shedReadiness: ports.ShedCompletionReadiness{Enabled: true},
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

func (f *fakeRepo) ListSOPs(_ context.Context, params ports.ListSOPsParams) ([]domain.SOPDefinition, error) {
	f.lastListSOPs = params
	return f.listSOPsResult, nil
}
func (f *fakeRepo) LatestVersionsFor(_ context.Context, _ string, sopIDs []string) (map[string]domain.SOPVersion, error) {
	f.latestVersionsForCalls++
	f.lastLatestVersionsForIDs = sopIDs
	if f.latestVersionsForResult == nil {
		return map[string]domain.SOPVersion{}, nil
	}
	return f.latestVersionsForResult, nil
}
func (f *fakeRepo) CreateSOP(context.Context, ports.CreateSOPCommand) (domain.SOPDefinition, error) {
	return domain.SOPDefinition{}, nil
}
func (f *fakeRepo) GetSOP(context.Context, string, string) (domain.SOPDefinition, *domain.SOPVersion, error) {
	return f.sop, &f.version, nil
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
func (f *fakeRepo) ListTasks(_ context.Context, params ports.ListTasksParams) (ports.TaskListPage, error) {
	f.lastListTasks = params
	if f.listTasksResult != nil {
		return ports.TaskListPage{Items: f.listTasksResult, Total: f.listTasksTotal}, nil
	}
	return ports.TaskListPage{Items: []domain.TaskSummary{f.task}, Total: 1}, nil
}
func (f *fakeRepo) CreateTask(context.Context, ports.CreateTaskCommand) (domain.TaskSummary, error) {
	return f.task, nil
}
func (f *fakeRepo) CreateTasksForBatches(_ context.Context, _ string, _ string, _ string, tasks []domain.BatchTaskRequest) (map[string]string, error) {
	result := make(map[string]string, len(tasks))
	for _, t := range tasks {
		result[t.BatchID] = "task-id-for-" + t.BatchID
	}
	return result, nil
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
func (f *fakeRepo) RecordScanCapture(_ context.Context, cmd ports.RecordScanCaptureCommand) (domain.ScanCaptureSummary, error) {
	f.lastScanCapture = cmd
	capture := domain.ScanCaptureSummary{
		CaptureID:    "66000000-0000-4000-8000-000000000001",
		TaskID:       cmd.TaskID,
		FieldKey:     cmd.Body.FieldKey,
		Tag:          cmd.Body.Tag,
		GoatID:       cmd.Body.GoatID,
		ObligationID: cmd.Body.ObligationID,
		CapturedAt:   "2026-07-17T00:00:00Z",
	}
	f.scanCaptures = append(f.scanCaptures, capture)
	return capture, nil
}
func (f *fakeRepo) ListScanCaptures(context.Context, string, string) ([]domain.ScanCaptureSummary, error) {
	return f.scanCaptures, nil
}
func (f *fakeRepo) RecordScanAttempt(_ context.Context, cmd ports.RecordScanAttemptCommand) (domain.ScanAttemptSummary, error) {
	f.lastScanAttempt = cmd
	attempt := domain.ScanAttemptSummary{
		AttemptID:    "68000000-0000-4000-8000-000000000001",
		TaskID:       cmd.TaskID,
		FieldKey:     cmd.Body.FieldKey,
		Tag:          cmd.Body.Tag,
		GoatID:       cmd.Body.GoatID,
		ObligationID: cmd.Body.ObligationID,
		Outcome:      cmd.Body.Outcome,
		TagRole:      cmd.Body.TagRole,
		Reason:       cmd.Body.Reason,
		CapturedAt:   "2026-07-17T00:00:00Z",
	}
	f.scanAttempts = append(f.scanAttempts, attempt)
	return attempt, nil
}
func (f *fakeRepo) ShedCompletionReadiness(_ context.Context, _, _, _, shedID string, _ int, _ int) (ports.ShedCompletionReadiness, error) {
	f.lastShedReadinessShedID = shedID
	return f.shedReadiness, f.shedReadinessErr
}
func (f *fakeRepo) CompletedTaskProofRefs(_ context.Context, _, _, _, shedID string) ([]domain.ProofReference, error) {
	f.lastCompletedProofRefsShedID = shedID
	return f.completedTaskGoatProofRefs, f.completedTaskGoatProofErr
}
func (f *fakeRepo) SubmitTask(_ context.Context, cmd ports.SubmitTaskCommand) (domain.SubmissionSummary, domain.TaskSummary, bool, error) {
	f.lastSubmit = cmd
	if f.submitErr != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, f.submitErr
	}
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

func (f *fakeRepo) AcceptSubmissionItemVerification(context.Context, string, string, string, string) error {
	return nil
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
	submitted       int
	err             error
	errBySubmission map[string]error
}

func (f *fakeSubmissionHook) OnTaskSubmitted(_ context.Context, _ string, _ domain.TaskSummary, submission domain.SubmissionSummary) error {
	f.submitted++
	if f.errBySubmission != nil {
		if err := f.errBySubmission[submission.SubmissionID]; err != nil {
			return err
		}
	}
	return f.err
}

type fakeProofValidator struct {
	calls           int
	binding         domain.ProofBinding
	refs            []domain.ProofReference
	resolved        []domain.ProofReference
	err             error
	retentionCalls  int
	retentionPolicy string
	retentionErr    error
}

func (f *fakeProofValidator) ResolveProofRefs(_ context.Context, _ string, binding domain.ProofBinding, refs []domain.ProofReference) ([]domain.ProofReference, error) {
	f.calls++
	f.binding = binding
	f.refs = refs
	if f.err != nil {
		return nil, f.err
	}
	return f.resolved, nil
}

func (f *fakeProofValidator) ApplyRetentionPolicy(_ context.Context, _ string, refs []domain.ProofReference, policy string, _ time.Time) error {
	f.retentionCalls++
	f.retentionPolicy = policy
	f.refs = refs
	return f.retentionErr
}
