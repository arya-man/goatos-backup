package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

type Service struct {
	repo         ports.Repository
	proofs       ProofValidator
	submission   SubmissionHook
	reviewFanout TaskReviewFanout
	now          func() time.Time
}

const (
	defaultFailedSubmissionFanoutAgeMinutes = 15
	maxFailedSubmissionFanoutAgeMinutes     = 7 * 24 * 60
	maxFailedSubmissionFanoutLimit          = 100
	rosterScanFieldKey                      = "__scan_roster__"
	AppTaskPageSize                         = 20
)

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

type ProofValidator interface {
	ResolveProofRefs(ctx context.Context, tenantID string, binding domain.ProofBinding, refs []domain.ProofReference) ([]domain.ProofReference, error)
	ApplyRetentionPolicy(ctx context.Context, tenantID string, refs []domain.ProofReference, policy string, acceptedAt time.Time) error
}

type TaskReviewFanout interface {
	ReviewableItemCount(ctx context.Context, tenantID, taskID string) (int, error)
	OnTaskVerified(ctx context.Context, tenantID, taskID, verifiedBy string) (int, error)
	OnTaskReworked(ctx context.Context, tenantID, taskID, verifiedBy, reason string) (int, error)
}

type SubmissionHook interface {
	OnTaskSubmitted(ctx context.Context, tenantID string, task domain.TaskSummary, submission domain.SubmissionSummary) error
}

func (s *Service) WithProofValidator(proofs ProofValidator) *Service {
	s.proofs = proofs
	return s
}

func (s *Service) WithSubmissionHook(hook SubmissionHook) *Service {
	s.submission = hook
	return s
}

func (s *Service) WithTaskReviewFanout(fanout TaskReviewFanout) *Service {
	s.reviewFanout = fanout
	return s
}

// AcceptSubmissionItemVerification projects one closed per-goat verification item back into the
// SOP aggregate. The repository owns the atomic item -> submission -> task roll-up; the
// composition-layer event bridge calls this only after the owning vertical has applied its
// canonical business transition.
func (s *Service) AcceptSubmissionItemVerification(ctx context.Context, tenantID, submissionID, goatID, actorID string) error {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return err
	}
	if !uuidutil.IsUUIDString(strings.TrimSpace(submissionID)) {
		return BadRequest("invalid_submission_id", "submission_id must be a UUID")
	}
	if !uuidutil.IsUUIDString(strings.TrimSpace(goatID)) {
		return BadRequest("invalid_goat_id", "goat_id must be a UUID")
	}
	return mapRepoErr(s.repo.AcceptSubmissionItemVerification(ctx, tenantID, submissionID, goatID, actorID))
}

func (s *Service) ListSOPs(ctx context.Context, params ports.ListSOPsParams, traceID string) (*domain.SOPListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.Status = strings.TrimSpace(params.Status)
	params.CodePrefix = strings.TrimSpace(strings.ToLower(params.CodePrefix))
	params.Search = strings.TrimSpace(params.Search)
	params.Limit = boundedLimit(params.Limit, 100)
	requestedLimit := params.Limit
	params.Limit++
	items, err := s.repo.ListSOPs(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	var next *string
	if len(items) > requestedLimit {
		items = items[:requestedLimit]
		last := items[len(items)-1]
		updatedAt, parseErr := time.Parse(time.RFC3339Nano, last.UpdatedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("sop: invalid pagination timestamp: %w", parseErr)
		}
		encoded, encodeErr := domain.EncodeSOPCursor(domain.SOPCursor{UpdatedAt: updatedAt, SOPID: last.SOPID})
		if encodeErr != nil {
			return nil, fmt.Errorf("sop: invalid pagination cursor: %w", encodeErr)
		}
		next = &encoded
	}
	// Batch-load each listed SOP's latest version in ONE query (not one GetSOP per row) so list
	// consumers derive version facets without an N+1 detail fanout (C35-015).
	latest := map[string]*domain.SOPVersion{}
	if len(items) > 0 {
		sopIDs := make([]string, len(items))
		for i, it := range items {
			sopIDs[i] = it.SOPID
		}
		versions, err := s.repo.LatestVersionsFor(ctx, params.TenantID, sopIDs)
		if err != nil {
			return nil, mapRepoErr(err)
		}
		for id := range versions {
			v := versions[id]
			latest[id] = &v
		}
	}
	if len(latest) == 0 {
		latest = nil
	}
	return &domain.SOPListResponse{Items: items, NextCursor: next, LatestVersions: latest, TraceID: traceID}, nil
}

func (s *Service) CreateSOP(ctx context.Context, cmd ports.CreateSOPCommand, traceID string) (*domain.SOPResponse, error) {
	if err := validateTenantAndActor(cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	cmd.Body.Code = normalizeCode(cmd.Body.Code)
	cmd.Body.Name = strings.TrimSpace(cmd.Body.Name)
	cmd.Body.Description = strings.TrimSpace(cmd.Body.Description)
	if cmd.Body.Code == "" || cmd.Body.Name == "" {
		return nil, BadRequest("invalid_sop", "code and name are required")
	}
	sop, err := s.repo.CreateSOP(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SOPResponse{SOP: sop, TraceID: traceID}, nil
}

func (s *Service) GetSOP(ctx context.Context, tenantID, sopID, traceID string) (*domain.SOPResponse, error) {
	if err := validateTenantAndID(tenantID, sopID, "sop_id"); err != nil {
		return nil, err
	}
	sop, version, err := s.repo.GetSOP(ctx, tenantID, sopID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SOPResponse{SOP: sop, LatestVersion: version, TraceID: traceID}, nil
}

func (s *Service) CreateVersion(ctx context.Context, cmd ports.CreateVersionCommand, traceID string) (*domain.SOPVersionResponse, error) {
	if err := validateTenantActorID(cmd.TenantID, cmd.ActorID, cmd.SOPID, "sop_id"); err != nil {
		return nil, err
	}
	cmd.Body.VersionLabel = strings.TrimSpace(cmd.Body.VersionLabel)
	if cmd.Body.VersionLabel == "" {
		return nil, BadRequest("invalid_version_label", "version_label is required")
	}
	cmd.Report = ValidateFormDSL(cmd.Body.FormDSL, cmd.Body.ProofPolicy)
	sop, _, err := s.repo.GetSOP(ctx, cmd.TenantID, cmd.SOPID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	validateVaccinationDriveSOPContract(&cmd.Report, sop.Code, cmd.Body.FormDSL, cmd.Body.ProofPolicy)
	if !cmd.Report.Valid {
		return nil, BadRequest("invalid_sop_dsl", cmd.Report.Errors[0].Message)
	}
	version, err := s.repo.CreateVersion(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SOPVersionResponse{Version: version, TraceID: traceID}, nil
}

func (s *Service) GetVersion(ctx context.Context, tenantID, sopID, versionID, traceID string) (*domain.SOPVersionResponse, error) {
	if err := validateTenantAndID(tenantID, sopID, "sop_id"); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(versionID) {
		return nil, BadRequest("invalid_sop_version_id", "sop_version_id must be a UUID")
	}
	version, err := s.repo.GetVersion(ctx, tenantID, sopID, versionID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SOPVersionResponse{Version: version, TraceID: traceID}, nil
}

func (s *Service) GetVersionByID(ctx context.Context, tenantID, versionID, traceID string) (*domain.SOPVersionResponse, error) {
	if err := validateTenantAndID(tenantID, versionID, "sop_version_id"); err != nil {
		return nil, err
	}
	version, err := s.repo.GetVersionByID(ctx, tenantID, versionID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SOPVersionResponse{Version: version, TraceID: traceID}, nil
}

func (s *Service) DryRun(ctx context.Context, tenantID, sopID, versionID string, body domain.DryRunRequest, traceID string) (*domain.DryRunResponse, error) {
	if err := validateTenantAndID(tenantID, sopID, "sop_id"); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(versionID) {
		return nil, BadRequest("invalid_sop_version_id", "sop_version_id must be a UUID")
	}
	version, err := s.repo.GetVersion(ctx, tenantID, sopID, versionID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	result := Evaluate(version.FormDSL, version.ProofPolicy, body.Answers, body.ProofRefs)
	result.TraceID = traceID
	return &result, nil
}

func (s *Service) PublishVersion(ctx context.Context, cmd ports.VersionCommand, traceID string) (*domain.SOPVersionResponse, error) {
	if err := validateVersionCommand(cmd); err != nil {
		return nil, err
	}
	version, err := s.repo.PublishVersion(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SOPVersionResponse{Version: version, TraceID: traceID}, nil
}

func (s *Service) RetireVersion(ctx context.Context, cmd ports.VersionCommand, traceID string) (*domain.SOPVersionResponse, error) {
	if err := validateVersionCommand(cmd); err != nil {
		return nil, err
	}
	version, err := s.repo.RetireVersion(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SOPVersionResponse{Version: version, TraceID: traceID}, nil
}

func (s *Service) ListTasks(ctx context.Context, params ports.ListTasksParams, traceID string) (*domain.TaskListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.State = strings.TrimSpace(params.State)
	params.AssignedTo = strings.TrimSpace(params.AssignedTo)
	params.ScopeType = strings.TrimSpace(params.ScopeType)
	params.ScopeID = strings.TrimSpace(params.ScopeID)
	params.Limit = boundedLimit(params.Limit, 100)
	page, err := s.repo.ListTasks(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.TaskListResponse{Items: page.Items, TraceID: traceID}, nil
}

func (s *Service) ListAppTasks(ctx context.Context, params ports.ListTasksParams, traceID string) (*domain.AppTaskListResponse, error) {
	if err := validateTenantAndActor(params.TenantID, params.ActorID); err != nil {
		return nil, err
	}
	params.State = strings.TrimSpace(params.State)
	params.AssignedTo = params.ActorID
	params.AppView = true
	requestedLimit := params.Limit
	if requestedLimit <= 0 {
		requestedLimit = AppTaskPageSize
	}
	if requestedLimit > AppTaskPageSize {
		requestedLimit = AppTaskPageSize
	}
	params.Limit = requestedLimit + 1
	page, err := s.repo.ListTasks(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	hasMore := len(page.Items) > requestedLimit
	if hasMore {
		page.Items = page.Items[:requestedLimit]
	}
	var next *string
	if hasMore {
		last := page.Items[len(page.Items)-1]
		var dueAt *time.Time
		if last.DueAt != nil {
			parsed, parseErr := time.Parse(time.RFC3339Nano, *last.DueAt)
			if parseErr != nil {
				return nil, fmt.Errorf("sop: invalid task pagination timestamp: %w", parseErr)
			}
			parsed = parsed.UTC()
			dueAt = &parsed
		}
		encoded, encodeErr := domain.EncodeTaskCursor(domain.TaskCursor{DueAt: dueAt, TaskID: last.TaskID})
		if encodeErr != nil {
			return nil, fmt.Errorf("sop: invalid task pagination cursor: %w", encodeErr)
		}
		next = &encoded
	}
	for i := range page.Items {
		page.Items[i] = presentAppTask(page.Items[i], params.LocaleTag)
	}
	return &domain.AppTaskListResponse{
		Items:      page.Items,
		Total:      page.Total,
		HasMore:    hasMore,
		NextCursor: next,
		TraceID:    traceID,
	}, nil
}

func (s *Service) CreateTask(ctx context.Context, cmd ports.CreateTaskCommand, traceID string) (*domain.TaskResponse, error) {
	if err := validateTenantAndActor(cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	normalizeTask(&cmd.Body)
	if err := validateCreateTask(cmd.Body); err != nil {
		return nil, err
	}
	task, err := s.repo.CreateTask(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.TaskResponse{Task: task, TraceID: traceID}, nil
}

// CreateTasksForBatches creates multiple tasks in a single batch operation,
// avoiding N+1 queries. Returns map of batch_id -> task_id.
func (s *Service) CreateTasksForBatches(ctx context.Context, tenantID, sopVersionID, actorID string, tasks []domain.BatchTaskRequest) (map[string]string, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return map[string]string{}, nil
	}
	result, err := s.repo.CreateTasksForBatches(ctx, tenantID, sopVersionID, actorID, tasks)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return result, nil
}

func (s *Service) GetTask(ctx context.Context, tenantID, taskID, traceID string) (*domain.TaskResponse, error) {
	if err := validateTenantAndID(tenantID, taskID, "task_id"); err != nil {
		return nil, err
	}
	task, version, submissions, err := s.repo.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.TaskResponse{Task: task, Version: version, Submissions: submissions, TraceID: traceID}, nil
}

func (s *Service) GetAppTask(ctx context.Context, tenantID, taskID, localeTag, traceID string) (*domain.TaskResponse, error) {
	result, err := s.GetTask(ctx, tenantID, taskID, traceID)
	if err != nil {
		return nil, err
	}
	result.Task = presentAppTask(result.Task, localeTag)
	return result, nil
}

func (s *Service) AssignTask(ctx context.Context, cmd ports.AssignTaskCommand, traceID string) (*domain.TaskResponse, error) {
	if err := validateTenantActorID(cmd.TenantID, cmd.ActorID, cmd.TaskID, "task_id"); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(strings.TrimSpace(cmd.Body.AssignedTo)) {
		return nil, BadRequest("invalid_assigned_to", "assigned_to must be a UUID")
	}
	if cmd.Body.RowVersion <= 0 {
		return nil, BadRequest("invalid_row_version", "row_version is required")
	}
	task, err := s.repo.AssignTask(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.TaskResponse{Task: task, TraceID: traceID}, nil
}

func (s *Service) VerifyTask(ctx context.Context, cmd ports.ReviewTaskCommand, traceID string) (*domain.TaskResponse, error) {
	cmd.State = "accepted"
	return s.reviewTask(ctx, cmd, traceID)
}

func (s *Service) ReworkTask(ctx context.Context, cmd ports.ReviewTaskCommand, traceID string) (*domain.TaskResponse, error) {
	cmd.State = "rework_requested"
	return s.reviewTask(ctx, cmd, traceID)
}

func (s *Service) RetryReviewFanouts(ctx context.Context, tenantID string, limit int) (int, error) {
	if err := validateTenant(tenantID); err != nil {
		return 0, err
	}
	attempts, err := s.repo.ListPendingReviewFanouts(ctx, tenantID, limit)
	if err != nil {
		return 0, mapRepoErr(err)
	}
	applied := 0
	for _, attempt := range attempts {
		task, _, _, err := s.repo.GetTask(ctx, tenantID, attempt.TaskID)
		if err != nil {
			return applied, mapRepoErr(err)
		}
		if task.State != attempt.Outcome || task.RowVersion != attempt.TaskRowVersion {
			if err := s.repo.RecordReviewFanoutStatus(ctx, supersededReviewFanoutStatus(tenantID, attempt, task)); err != nil {
				return applied, mapRepoErr(err)
			}
			continue
		}
		err = s.applyReviewFanout(ctx, ports.ReviewTaskCommand{
			TenantID: tenantID,
			ActorID:  attempt.ActorID,
			TaskID:   attempt.TaskID,
			State:    attempt.Outcome,
			Body: domain.ReviewTaskRequest{
				Reason:     attempt.Reason,
				RowVersion: attempt.TaskRowVersion,
			},
		}, task, attempt.TaskRowVersion)
		if err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func (s *Service) RetrySubmissionFanouts(ctx context.Context, tenantID string, limit int) (int, error) {
	if err := validateTenant(tenantID); err != nil {
		return 0, err
	}
	attempts, err := s.repo.ListPendingSubmissionFanouts(ctx, tenantID, limit)
	if err != nil {
		return 0, mapRepoErr(err)
	}
	applied := 0
	for _, attempt := range attempts {
		task, _, submissions, err := s.repo.GetTask(ctx, tenantID, attempt.TaskID)
		if err != nil {
			return applied, mapRepoErr(err)
		}
		submission, ok := findSubmission(submissions, attempt.SubmissionID)
		if !ok || !submissionFanoutNeeded(task) {
			if err := s.repo.RecordSubmissionFanoutStatus(ctx, skippedSubmissionFanoutStatus(tenantID, attempt, task)); err != nil {
				return applied, mapRepoErr(err)
			}
			continue
		}
		if err := s.applySubmissionFanout(ctx, tenantID, task, submission, true); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func (s *Service) ListAgedFailedSubmissionFanouts(ctx context.Context, tenantID string, olderThanMinutes, limit int, traceID string) (*domain.FailedSubmissionFanoutsResponse, error) {
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	if olderThanMinutes < 0 || olderThanMinutes > maxFailedSubmissionFanoutAgeMinutes {
		return nil, BadRequest("invalid_older_than_minutes", "older_than_minutes must be between 0 and 10080")
	}
	if limit < 0 {
		return nil, BadRequest("invalid_limit", "limit must be a positive integer")
	}
	if olderThanMinutes == 0 {
		olderThanMinutes = defaultFailedSubmissionFanoutAgeMinutes
	}
	now := s.now().UTC()
	items, err := s.repo.ListAgedFailedSubmissionFanouts(ctx, ports.ListAgedFailedSubmissionFanoutsParams{
		TenantID:      tenantID,
		UpdatedBefore: now.Add(-time.Duration(olderThanMinutes) * time.Minute),
		Now:           now,
		Limit:         boundedLimit(limit, maxFailedSubmissionFanoutLimit),
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.FailedSubmissionFanoutsResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) SubmitTask(ctx context.Context, cmd ports.SubmitTaskCommand, traceID string) (*domain.SubmissionResponse, error) {
	if err := validateTenantActorID(cmd.TenantID, cmd.ActorID, cmd.TaskID, "task_id"); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(cmd.Body.SOPVersionID) {
		return nil, BadRequest("invalid_sop_version_id", "sop_version_id must be a UUID")
	}
	cmd.Body.IdempotencyKey = strings.TrimSpace(cmd.Body.IdempotencyKey)
	if cmd.Body.IdempotencyKey == "" {
		return nil, BadRequest("invalid_idempotency_key", "idempotency_key is required")
	}
	task, version, _, err := s.repo.GetTask(ctx, cmd.TenantID, cmd.TaskID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if task.SOPVersionID != cmd.Body.SOPVersionID {
		return nil, Conflict("stale_sop_version", "task requires a different pinned SOP version")
	}
	if version == nil {
		return nil, Conflict("missing_sop_version", "task has no pinned SOP version")
	}
	if version.Status != "published" {
		return nil, Conflict("sop_version_not_executable", "pinned SOP version is not published")
	}
	if task.AssignedTo != nil && *task.AssignedTo != cmd.ActorID {
		return nil, Forbidden("task_not_assigned", "task is not assigned to this actor")
	}
	scanCaptures, err := s.repo.ListScanCaptures(ctx, cmd.TenantID, cmd.TaskID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	cmd.Body.Answers = mergeDraftScanAnswers(version.FormDSL, cmd.Body.Answers, scanCaptures)
	if s.proofs != nil && len(cmd.Body.ProofRefs) > 0 {
		proofRefs, err := s.proofs.ResolveProofRefs(ctx, cmd.TenantID, proofBindingForSubmission(task, cmd.Body.ProofRefs), cmd.Body.ProofRefs)
		if err != nil {
			return nil, BadRequest("invalid_proof_refs", "proof_refs must reference server-issued proof records for this tenant")
		}
		cmd.Body.ProofRefs = proofRefs
	}
	shedCompletionAck := false
	proofPolicy := version.ProofPolicy
	if submissionFanoutNeeded(task) {
		gate := vaccinationCompletionProofGate(version.ProofPolicy)
		readiness, err := s.repo.ShedCompletionReadiness(ctx, cmd.TenantID, cmd.TaskID, gate.SubjectType, submittedShedProofSubjectID(cmd.Body.ProofRefs), gate.MinimumCount, gate.MaximumCount)
		if err != nil {
			return nil, mapRepoErr(err)
		}
		if !readiness.Enabled {
			reason := strings.TrimSpace(readiness.Reason)
			if reason == "" {
				reason = "shed completion is not ready"
			}
			return nil, BadRequest("shed_completion_not_ready", reason)
		}
		// Vaccination shed completion is an acknowledgement. The proof gate is backend-owned and
		// SOP-mode aware: per-goat video gates on every scanned goat; shed-level video gates on
		// the required shed proof count. Do not require a duplicated generic task-level proof
		// attachment here.
		proofPolicy = map[string]any{"required": false, "subject_scope": "task", "types": []any{"video"}, "minimum_count": 0}
		if len(cmd.Body.ProofRefs) == 0 {
			proofRefs, err := s.repo.CompletedTaskProofRefs(ctx, cmd.TenantID, cmd.TaskID, gate.SubjectType)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			cmd.Body.ProofRefs = proofRefs
		}
		shedCompletionAck = true
	}
	evaluation := domain.DryRunResponse{Valid: true, WorkflowPath: []string{"operator_submission"}, FinalState: "accepted"}
	if !shedCompletionAck {
		evaluation = Evaluate(version.FormDSL, proofPolicy, cmd.Body.Answers, cmd.Body.ProofRefs)
	}
	cmd.Report = domain.ValidationReport{Valid: evaluation.Valid, Errors: evaluation.Errors, Warnings: evaluation.Warnings}
	if !evaluation.Valid {
		return nil, BadRequest("submission_failed_validation", evaluation.Errors[0].Message)
	}
	cmd.ItemState = "accepted"
	cmd.TaskState = "accepted"
	requiresProof := evaluationRequiresProof(evaluation)
	if shedCompletionAck && boolValue(version.ProofPolicy, "verify_before_apply") {
		requiresProof = true
	}
	if requiresProof && boolValue(version.ProofPolicy, "verify_before_apply") {
		cmd.ItemState = "needs_review"
		cmd.TaskState = "needs_review"
	}
	cmd.SubmissionItems = buildSubmissionItemsWithDraftScans(version.FormDSL, cmd.Body.Answers, scanCaptures)
	cmd.MovementPayload = buildMovementPayload(task, cmd.Body)
	cmd.SubmissionFanoutRequired = s.submission != nil && submissionFanoutNeeded(task) && cmd.TaskState == "accepted"
	submission, updatedTask, replay, err := s.repo.SubmitTask(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if !replay && s.proofs != nil && len(cmd.Body.ProofRefs) > 0 {
		_ = s.proofs.ApplyRetentionPolicy(ctx, cmd.TenantID, cmd.Body.ProofRefs, stringValue(version.ProofPolicy, "retention_policy"), s.now())
	}
	if cmd.SubmissionFanoutRequired && !replay {
		if err := s.applySubmissionFanout(ctx, cmd.TenantID, updatedTask, submission, true); err != nil {
			return nil, err
		}
	}
	return &domain.SubmissionResponse{Submission: submission, Task: updatedTask, TraceID: traceID}, nil
}

func submittedShedProofSubjectID(refs []domain.ProofReference) string {
	for _, ref := range refs {
		if ref.SubjectType != "shed" || ref.SubjectID == nil {
			continue
		}
		if shedID := strings.TrimSpace(*ref.SubjectID); shedID != "" {
			return shedID
		}
	}
	return ""
}

func proofBindingForSubmission(task domain.TaskSummary, refs []domain.ProofReference) domain.ProofBinding {
	binding := domain.ProofBinding{TaskID: task.TaskID, ScopeType: task.ScopeType, ScopeID: task.ScopeID}
	if shedID := submittedShedProofSubjectID(refs); shedID != "" {
		binding.ScopeType = "shed"
		binding.ScopeID = shedID
	}
	return binding
}

func (s *Service) RecordScanCapture(ctx context.Context, cmd ports.RecordScanCaptureCommand, traceID string) (*domain.ScanCaptureResponse, error) {
	if err := validateTenantActorID(cmd.TenantID, cmd.ActorID, cmd.TaskID, "task_id"); err != nil {
		return nil, err
	}
	cmd.IdempotencyKey = strings.TrimSpace(cmd.IdempotencyKey)
	if cmd.IdempotencyKey == "" {
		return nil, BadRequest("invalid_idempotency_key", "idempotency key is required")
	}
	cmd.Body.FieldKey = strings.TrimSpace(cmd.Body.FieldKey)
	cmd.Body.Tag = strings.TrimSpace(cmd.Body.Tag)
	cmd.Body.GoatID = strings.TrimSpace(cmd.Body.GoatID)
	cmd.Body.ObligationID = strings.TrimSpace(cmd.Body.ObligationID)
	if cmd.Body.FieldKey == "" {
		return nil, BadRequest("invalid_field_key", "field_key is required")
	}
	if cmd.Body.Tag == "" {
		return nil, BadRequest("invalid_tag", "tag is required")
	}
	if cmd.Body.GoatID != "" && !uuidutil.IsUUIDString(cmd.Body.GoatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a UUID")
	}
	if cmd.Body.ObligationID != "" && !uuidutil.IsUUIDString(cmd.Body.ObligationID) {
		return nil, BadRequest("invalid_obligation_id", "obligation_id must be a UUID")
	}
	task, version, _, err := s.repo.GetTask(ctx, cmd.TenantID, cmd.TaskID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if version == nil {
		return nil, Conflict("missing_sop_version", "task has no pinned SOP version")
	}
	if task.AssignedTo != nil && *task.AssignedTo != cmd.ActorID {
		return nil, Forbidden("task_not_assigned", "task is not assigned to this actor")
	}
	if !scanFieldAllowed(version.FormDSL, cmd.Body.FieldKey) {
		return nil, BadRequest("invalid_scan_field", "field_key is not a goat scan field for this task")
	}
	capture, err := s.repo.RecordScanCapture(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.ScanCaptureResponse{Capture: capture, TraceID: traceID}, nil
}

func (s *Service) RecordScanAttempt(ctx context.Context, cmd ports.RecordScanAttemptCommand, traceID string) (*domain.ScanAttemptResponse, error) {
	if err := validateTenantActorID(cmd.TenantID, cmd.ActorID, cmd.TaskID, "task_id"); err != nil {
		return nil, err
	}
	cmd.IdempotencyKey = strings.TrimSpace(cmd.IdempotencyKey)
	if cmd.IdempotencyKey == "" {
		return nil, BadRequest("invalid_idempotency_key", "idempotency key is required")
	}
	cmd.Body.FieldKey = strings.TrimSpace(cmd.Body.FieldKey)
	cmd.Body.Tag = strings.TrimSpace(cmd.Body.Tag)
	cmd.Body.NormalizedTag = normalizeScanTag(cmd.Body.NormalizedTag)
	cmd.Body.GoatID = strings.TrimSpace(cmd.Body.GoatID)
	cmd.Body.ObligationID = strings.TrimSpace(cmd.Body.ObligationID)
	cmd.Body.Outcome = strings.TrimSpace(cmd.Body.Outcome)
	cmd.Body.TagRole = strings.TrimSpace(cmd.Body.TagRole)
	cmd.Body.Reason = strings.TrimSpace(cmd.Body.Reason)
	if cmd.Body.FieldKey == "" {
		return nil, BadRequest("invalid_field_key", "field_key is required")
	}
	if cmd.Body.Tag == "" {
		return nil, BadRequest("invalid_tag", "tag is required")
	}
	if cmd.Body.GoatID != "" && !uuidutil.IsUUIDString(cmd.Body.GoatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a UUID")
	}
	if cmd.Body.ObligationID != "" && !uuidutil.IsUUIDString(cmd.Body.ObligationID) {
		return nil, BadRequest("invalid_obligation_id", "obligation_id must be a UUID")
	}
	if !scanAttemptValueAllowed(cmd.Body.Outcome, "accepted", "duplicate", "not_due", "unknown") {
		return nil, BadRequest("invalid_outcome", "outcome must be accepted, duplicate, not_due, or unknown")
	}
	if cmd.Body.TagRole == "" {
		cmd.Body.TagRole = "unknown"
	}
	if !scanAttemptValueAllowed(cmd.Body.TagRole, "primary", "secondary", "unknown") {
		return nil, BadRequest("invalid_tag_role", "tag_role must be primary, secondary, or unknown")
	}
	task, version, _, err := s.repo.GetTask(ctx, cmd.TenantID, cmd.TaskID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if version == nil {
		return nil, Conflict("missing_sop_version", "task has no pinned SOP version")
	}
	if task.AssignedTo != nil && *task.AssignedTo != cmd.ActorID {
		return nil, Forbidden("task_not_assigned", "task is not assigned to this actor")
	}
	if !scanFieldAllowed(version.FormDSL, cmd.Body.FieldKey) {
		return nil, BadRequest("invalid_scan_field", "field_key is not a goat scan field for this task")
	}
	attempt, err := s.repo.RecordScanAttempt(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.ScanAttemptResponse{Attempt: attempt, TraceID: traceID}, nil
}

func (s *Service) reviewTask(ctx context.Context, cmd ports.ReviewTaskCommand, traceID string) (*domain.TaskResponse, error) {
	if err := validateTenantActorID(cmd.TenantID, cmd.ActorID, cmd.TaskID, "task_id"); err != nil {
		return nil, err
	}
	if cmd.Body.RowVersion <= 0 {
		return nil, BadRequest("invalid_row_version", "row_version is required")
	}
	current, version, submissions, err := s.repo.GetTask(ctx, cmd.TenantID, cmd.TaskID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if err := validateReviewReady(current, version, submissions); err != nil {
		return nil, err
	}
	if err := s.ensureReviewFanoutReady(ctx, cmd, current); err != nil {
		return nil, err
	}
	task, err := s.repo.ReviewTask(ctx, cmd)
	if err != nil {
		if errors.Is(err, ports.ErrConflict) {
			current, _, _, getErr := s.repo.GetTask(ctx, cmd.TenantID, cmd.TaskID)
			if getErr != nil || current.State != cmd.State {
				return nil, mapRepoErr(err)
			}
			task = current
		} else {
			return nil, mapRepoErr(err)
		}
	}
	if err := s.applyReviewFanout(ctx, cmd, task, task.RowVersion); err != nil {
		return nil, err
	}
	return &domain.TaskResponse{Task: task, TraceID: traceID}, nil
}

func validateReviewReady(task domain.TaskSummary, version *domain.SOPVersion, submissions []domain.SubmissionSummary) error {
	switch task.State {
	case "submitted", "needs_review":
	default:
		return Conflict("task_not_reviewable", "task must be submitted for review before it can be verified or reworked")
	}
	submission, ok := latestReviewableSubmission(task.TaskID, submissions)
	if !ok {
		return Conflict("missing_review_submission", "task review requires a submitted SOP response")
	}
	if version == nil {
		return Conflict("missing_sop_version", "task has no pinned SOP version")
	}
	if proofRequired(version.ProofPolicy) {
		if completedProofCount(submission.ProofRefs, version.ProofPolicy) < proofMinimumCount(version.ProofPolicy) {
			return Conflict("review_proof_required", "task review requires completed proof from the submitted SOP response")
		}
		if missing := missingExpectedProofSubjects(submission.ProofRefs, version.ProofPolicy); len(missing) > 0 {
			return Conflict("review_proof_required", "task review requires completed proof for all expected subjects")
		}
	}
	return nil
}

func latestReviewableSubmission(taskID string, submissions []domain.SubmissionSummary) (domain.SubmissionSummary, bool) {
	for _, submission := range submissions {
		if submission.TaskID != "" && submission.TaskID != taskID {
			continue
		}
		switch submission.State {
		case "submitted", "needs_review", "accepted":
			return submission, true
		}
	}
	return domain.SubmissionSummary{}, false
}

func (s *Service) ensureReviewFanoutReady(ctx context.Context, cmd ports.ReviewTaskCommand, task domain.TaskSummary) error {
	if !submissionFanoutNeeded(task) {
		return nil
	}
	if s.reviewFanout == nil {
		return Conflict("review_fanout_not_configured", "task review requires completion fanout wiring to verify or rework")
	}
	count, err := s.reviewFanout.ReviewableItemCount(ctx, cmd.TenantID, cmd.TaskID)
	if err != nil {
		return err
	}
	if count == 0 {
		return Conflict("review_fanout_empty", "task review requires at least one recorded completion to verify or rework")
	}
	return nil
}

func (s *Service) applyReviewFanout(ctx context.Context, cmd ports.ReviewTaskCommand, task domain.TaskSummary, statusTaskRowVersion int) error {
	if cmd.State != "accepted" && cmd.State != "rework_requested" {
		return nil
	}
	if s.reviewFanout == nil {
		if submissionFanoutNeeded(task) {
			return Conflict("review_fanout_not_configured", "task review requires completion fanout wiring to verify or rework")
		}
		return nil
	}
	if statusTaskRowVersion <= 0 {
		statusTaskRowVersion = task.RowVersion
	}
	status := ports.ReviewFanoutStatusCommand{
		TenantID:       cmd.TenantID,
		TaskID:         cmd.TaskID,
		TaskRowVersion: statusTaskRowVersion,
		Outcome:        cmd.State,
		ActorID:        cmd.ActorID,
		Reason:         cmd.Body.Reason,
	}
	var err error
	count := 0
	switch cmd.State {
	case "accepted":
		count, err = s.reviewFanout.OnTaskVerified(ctx, cmd.TenantID, cmd.TaskID, cmd.ActorID)
	case "rework_requested":
		count, err = s.reviewFanout.OnTaskReworked(ctx, cmd.TenantID, cmd.TaskID, cmd.ActorID, cmd.Body.Reason)
	}
	if err == nil && submissionFanoutNeeded(task) && count == 0 {
		err = Conflict("review_fanout_empty", "task review requires at least one recorded completion to verify or rework")
	}
	if err != nil {
		status.Status = "failed"
		status.LastError = sanitizeFanoutError(err)
		if markErr := s.repo.RecordReviewFanoutStatus(ctx, status); markErr != nil {
			return errors.Join(err, markErr)
		}
		return err
	}
	status.Status = "completed"
	if err := s.repo.RecordReviewFanoutStatus(ctx, status); err != nil {
		return err
	}
	return nil
}

func (s *Service) applySubmissionFanout(ctx context.Context, tenantID string, task domain.TaskSummary, submission domain.SubmissionSummary, failClosed bool) error {
	if s.submission == nil || !submissionFanoutNeeded(task) {
		return nil
	}
	status := ports.SubmissionFanoutStatusCommand{
		TenantID:     tenantID,
		TaskID:       task.TaskID,
		SubmissionID: submission.SubmissionID,
		ActorID:      submission.SubmittedBy,
	}
	if err := s.submission.OnTaskSubmitted(ctx, tenantID, task, submission); err != nil {
		status.Status = "failed"
		status.LastError = sanitizeFanoutError(err)
		if markErr := s.repo.RecordSubmissionFanoutStatus(ctx, status); markErr != nil {
			return errors.Join(err, markErr)
		}
		if failClosed {
			return err
		}
		return nil
	}
	status.Status = "completed"
	return s.repo.RecordSubmissionFanoutStatus(ctx, status)
}

func supersededReviewFanoutStatus(tenantID string, attempt ports.ReviewFanoutAttempt, task domain.TaskSummary) ports.ReviewFanoutStatusCommand {
	return ports.ReviewFanoutStatusCommand{
		TenantID:       tenantID,
		TaskID:         attempt.TaskID,
		TaskRowVersion: attempt.TaskRowVersion,
		Outcome:        attempt.Outcome,
		ActorID:        attempt.ActorID,
		Reason:         attempt.Reason,
		Status:         "superseded",
		LastError:      fmt.Sprintf("task is now %s at row_version %d", task.State, task.RowVersion),
	}
}

func skippedSubmissionFanoutStatus(tenantID string, attempt ports.SubmissionFanoutAttempt, task domain.TaskSummary) ports.SubmissionFanoutStatusCommand {
	return ports.SubmissionFanoutStatusCommand{
		TenantID:     tenantID,
		TaskID:       attempt.TaskID,
		SubmissionID: attempt.SubmissionID,
		ActorID:      attempt.ActorID,
		Status:       "skipped",
		LastError:    fmt.Sprintf("task %s is no longer vaccination fanout eligible", task.TaskID),
	}
}

func findSubmission(submissions []domain.SubmissionSummary, submissionID string) (domain.SubmissionSummary, bool) {
	for _, submission := range submissions {
		if submission.SubmissionID == submissionID {
			return submission, true
		}
	}
	return domain.SubmissionSummary{}, false
}

func submissionFanoutNeeded(task domain.TaskSummary) bool {
	switch task.SOPCode {
	case "vaccination.drive", "vaccination.session":
		return true
	}
	switch task.TaskType {
	case "vaccination", "vaccination_drive", "vaccination_session":
		return true
	default:
		return false
	}
}

func evaluationRequiresProof(evaluation domain.DryRunResponse) bool {
	for _, step := range evaluation.WorkflowPath {
		if step == "proof_verification" {
			return true
		}
	}
	for _, issue := range evaluation.Errors {
		if issue.Code == "proof_required" || issue.Code == "proof_subject_required" {
			return true
		}
	}
	return false
}

func ValidateFormDSL(formDSL, proofPolicy map[string]any) domain.ValidationReport {
	report := domain.ValidationReport{Valid: true, Errors: []domain.ValidationIssue{}, Warnings: []domain.ValidationIssue{}}
	if formDSL == nil {
		addError(&report, "form_dsl", "required", "form_dsl is required")
		return report
	}
	if stringValue(formDSL, "schema_version") == "" {
		addError(&report, "form_dsl.schema_version", "required", "schema_version is required")
	}
	fields, ok := formDSL["fields"].([]any)
	if !ok || len(fields) == 0 {
		addError(&report, "form_dsl.fields", "required", "at least one field is required")
		return report
	}
	seen := map[string]struct{}{}
	for idx, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			addError(&report, fmt.Sprintf("form_dsl.fields.%d", idx), "invalid", "field must be an object")
			continue
		}
		key := stringValue(field, "key")
		fieldType := stringValue(field, "type")
		normalizedType, aliased := normalizeFieldType(fieldType)
		if key == "" {
			addError(&report, fmt.Sprintf("form_dsl.fields.%d.key", idx), "required", "field key is required")
		}
		if _, exists := seen[key]; key != "" && exists {
			addError(&report, "form_dsl.fields", "duplicate", "field keys must be unique")
		}
		seen[key] = struct{}{}
		if !supportedFieldType(normalizedType) {
			addError(&report, fmt.Sprintf("form_dsl.fields.%s.type", key), "unsupported", "field type is not supported")
		}
		if aliased {
			addWarning(&report, fmt.Sprintf("form_dsl.fields.%s.type", key), "field_type_alias", "field type alias normalized to "+normalizedType)
		}
	}
	validateRepeatForEachGoat(&report, formDSL["repeat_for_each_goat"], seen)
	validateRules(&report, formDSL["rules"], seen)
	validateProofPolicy(&report, proofPolicy, formDSL, fields)
	return report
}

func Evaluate(formDSL, proofPolicy map[string]any, answers map[string]any, proofRefs []domain.ProofReference) domain.DryRunResponse {
	report := ValidateFormDSL(formDSL, proofPolicy)
	out := domain.DryRunResponse{
		Valid:        report.Valid,
		Errors:       append([]domain.ValidationIssue{}, report.Errors...),
		Warnings:     append([]domain.ValidationIssue{}, report.Warnings...),
		FieldStates:  []domain.FieldState{},
		WorkflowPath: []string{"operator_submission"},
		FinalState:   "accepted",
	}
	fields, _ := formDSL["fields"].([]any)
	answers = nonNilMap(answers)
	fieldStateIndex := map[string]int{}
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := stringValue(field, "key")
		required := boolValue(field, "required")
		state := domain.FieldState{Key: key, Visible: true, Required: required}
		out.FieldStates = append(out.FieldStates, state)
		fieldStateIndex[key] = len(out.FieldStates) - 1
	}
	proofRequiredByRule := false
	supervisorRequired := false
	if rules, ok := formDSL["rules"].([]any); ok {
		for idx, raw := range rules {
			rule, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			ruleType := stringValue(rule, "type")
			matches, evaluable := conditionMatches(rule["when"], answers)
			if !evaluable {
				out.Warnings = append(out.Warnings, domain.ValidationIssue{Field: fmt.Sprintf("rules.%d.when", idx), Code: "not_evaluable", Message: "rule condition could not be evaluated"})
				continue
			}
			switch ruleType {
			case "visible_if":
				if idx, ok := fieldStateIndex[stringValue(rule, "field")]; ok {
					out.FieldStates[idx].Visible = matches
					if !matches {
						out.FieldStates[idx].Required = false
					}
				}
			case "required_if":
				if matches {
					if idx, ok := fieldStateIndex[stringValue(rule, "field")]; ok {
						out.FieldStates[idx].Required = true
					}
				}
			case "enabled_if":
				if !matches {
					if idx, ok := fieldStateIndex[stringValue(rule, "field")]; ok {
						out.FieldStates[idx].Blocked = true
						out.FieldStates[idx].Message = ruleMessage(rule, "Field is disabled by rule.")
					}
				}
			case "proof_required_if":
				if matches {
					proofRequiredByRule = true
				}
			case "block_submission_if":
				if matches {
					addErrorToList(&out.Errors, "rules", "blocked", ruleMessage(rule, "submission blocked by SOP rule"))
				}
			case "requires_supervisor_if":
				if matches {
					supervisorRequired = true
					out.Warnings = append(out.Warnings, domain.ValidationIssue{Field: "workflow", Code: "supervisor_required", Message: ruleMessage(rule, "supervisor approval is required")})
				}
			}
		}
	}
	for i := range out.FieldStates {
		state := &out.FieldStates[i]
		if state.Key == "" || !state.Visible {
			continue
		}
		if state.Required && isEmptyAnswer(answers[state.Key]) {
			state.Blocked = true
			state.Message = "Required answer missing."
			addErrorToList(&out.Errors, state.Key, "required", "required answer missing")
			continue
		}
		if !isEmptyAnswer(answers[state.Key]) {
			validateAnswerValue(&out.Errors, fieldByKey(fields, state.Key), answers[state.Key])
		}
	}
	requiresProof := proofRequired(proofPolicy) || proofRequiredByRule
	proofBlocked := false
	if requiresProof && completedProofCount(proofRefs, proofPolicy) < proofMinimumCount(proofPolicy) {
		addErrorToList(&out.Errors, "proof_refs", "proof_required", "completed proof is required")
		proofBlocked = true
	}
	for _, subject := range missingExpectedProofSubjects(proofRefs, proofPolicy) {
		addErrorToList(&out.Errors, "proof_refs", "proof_subject_required", "completed proof is required for subject "+subject)
		proofBlocked = true
	}
	if validatePerGoatProofRefs(&out.Errors, answers, proofRefs, proofPolicy) {
		proofBlocked = true
	}
	if proofBlocked {
		out.WorkflowPath = append(out.WorkflowPath, "proof_verification")
		out.FinalState = "blocked"
	} else if requiresProof && boolValue(proofPolicy, "verify_before_apply") {
		out.WorkflowPath = append(out.WorkflowPath, "proof_verification")
		out.FinalState = "needs_review"
	}
	if supervisorRequired && out.FinalState == "accepted" {
		out.WorkflowPath = append(out.WorkflowPath, "supervisor_approval")
		out.FinalState = "needs_review"
	}
	if len(out.Errors) > 0 {
		out.Valid = false
	}
	return out
}

func buildMovementPayload(task domain.TaskSummary, body domain.SubmitTaskRequest) map[string]any {
	return map[string]any{
		"task_id":                 task.TaskID,
		"sop_version_id":          body.SOPVersionID,
		"goat_ids":                body.Answers["goat_ids"],
		"source_location_id":      body.Answers["source_location_id"],
		"destination_location_id": body.Answers["destination_location_id"],
		"destination_count":       body.Answers["destination_count"],
		"category":                body.Answers["category"],
		"priority":                body.Answers["priority"],
	}
}

func buildSubmissionItems(formDSL map[string]any, answers map[string]any) []ports.SubmissionItemInput {
	sourceField := repeatSourceField(formDSL)
	if sourceField == "" {
		return []ports.SubmissionItemInput{{ItemKey: "batch"}}
	}
	goatIDs := goatIDsFromAnswer(answers[sourceField])
	if len(goatIDs) == 0 {
		return []ports.SubmissionItemInput{{ItemKey: "batch"}}
	}
	out := make([]ports.SubmissionItemInput, 0, len(goatIDs))
	for _, goatID := range goatIDs {
		if goatID == "" {
			continue
		}
		out = append(out, ports.SubmissionItemInput{GoatID: goatID, ItemKey: goatID})
		if len(out) >= 1000 {
			break
		}
	}
	if len(out) == 0 {
		return []ports.SubmissionItemInput{{ItemKey: "batch"}}
	}
	return out
}

func buildSubmissionItemsWithDraftScans(formDSL map[string]any, answers map[string]any, captures []domain.ScanCaptureSummary) []ports.SubmissionItemInput {
	sourceField := repeatSourceField(formDSL)
	if sourceField == "" {
		return buildSubmissionItems(formDSL, answers)
	}
	target := rosterScanTargetFieldKey(formDSL)
	out := make([]ports.SubmissionItemInput, 0, len(captures))
	seen := map[string]struct{}{}
	for _, capture := range captures {
		if !captureMatchesScanField(capture.FieldKey, sourceField, target) {
			continue
		}
		itemKey := strings.TrimSpace(capture.GoatID)
		if itemKey == "" {
			itemKey = strings.TrimSpace(capture.Tag)
		}
		if itemKey == "" {
			continue
		}
		if _, ok := seen[itemKey]; ok {
			continue
		}
		seen[itemKey] = struct{}{}
		goatID := strings.TrimSpace(capture.GoatID)
		out = append(out, ports.SubmissionItemInput{
			GoatID:         goatID,
			ItemKey:        itemKey,
			AdministeredAt: strings.TrimSpace(capture.CapturedAt),
		})
		if len(out) >= 1000 {
			break
		}
	}
	if len(out) > 0 {
		return out
	}
	return buildSubmissionItems(formDSL, answers)
}

func mergeDraftScanAnswers(formDSL map[string]any, answers map[string]any, captures []domain.ScanCaptureSummary) map[string]any {
	merged := nonNilMap(answers)
	if len(captures) == 0 {
		return merged
	}
	target := rosterScanTargetFieldKey(formDSL)
	if target == "" {
		return merged
	}
	existing := goatIDsFromAnswer(merged[target])
	// RFID/tag text is lookup input, never the canonical medical-record identity. Older app
	// versions may still submit the tag, so use the durable server capture to canonicalize that
	// alias to goat_id before proof validation and submission fan-out.
	goatIDByNormalizedTag := make(map[string]string, len(captures))
	for _, capture := range captures {
		if goatID := strings.TrimSpace(capture.GoatID); goatID != "" {
			goatIDByNormalizedTag[normalizeScanTag(capture.Tag)] = goatID
		}
	}
	seen := map[string]struct{}{}
	values := make([]any, 0, len(existing)+len(captures))
	for _, raw := range existing {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if goatID := goatIDByNormalizedTag[normalizeScanTag(value)]; goatID != "" {
			value = goatID
		}
		key := normalizeScanTag(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, value)
	}
	for _, capture := range captures {
		if !captureMatchesScanField(capture.FieldKey, target, target) {
			continue
		}
		value := strings.TrimSpace(capture.GoatID)
		if value == "" {
			value = strings.TrimSpace(capture.Tag)
		}
		if value == "" {
			continue
		}
		key := normalizeScanTag(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, value)
	}
	if len(values) > 0 {
		merged[target] = values
	}
	return merged
}

func scanFieldAllowed(formDSL map[string]any, fieldKey string) bool {
	fieldKey = strings.TrimSpace(fieldKey)
	if fieldKey == "" {
		return false
	}
	target := rosterScanTargetFieldKey(formDSL)
	if fieldKey == rosterScanFieldKey {
		return target != ""
	}
	fields, _ := formDSL["fields"].([]any)
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok || stringValue(field, "key") != fieldKey {
			continue
		}
		fieldType, _ := normalizeFieldType(stringValue(field, "type"))
		return fieldType == "goat_scan" || fieldType == "goat_lookup" || fieldType == "animal_id_scan"
	}
	return false
}

func scanAttemptValueAllowed(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func rosterScanTargetFieldKey(formDSL map[string]any) string {
	fields, _ := formDSL["fields"].([]any)
	keys := []string{}
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fieldType, _ := normalizeFieldType(stringValue(field, "type"))
		if fieldType == "goat_scan" || fieldType == "animal_id_scan" {
			keys = append(keys, stringValue(field, "key"))
		}
	}
	if len(keys) == 1 {
		return keys[0]
	}
	return ""
}

func captureMatchesScanField(captureField, targetField, rosterTarget string) bool {
	captureField = strings.TrimSpace(captureField)
	targetField = strings.TrimSpace(targetField)
	return captureField == targetField || (captureField == rosterScanFieldKey && rosterTarget != "" && targetField == rosterTarget)
}

func normalizeScanTag(tag string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(tag) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func repeatSourceField(formDSL map[string]any) string {
	switch repeat := formDSL["repeat_for_each_goat"].(type) {
	case bool:
		if !repeat {
			return ""
		}
	case string:
		return strings.TrimSpace(repeat)
	case map[string]any:
		return stringValue(repeat, "source_field")
	case nil:
		return ""
	}
	fields, _ := formDSL["fields"].([]any)
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok || !boolValue(field, "repeat") {
			continue
		}
		fieldType, _ := normalizeFieldType(stringValue(field, "type"))
		switch fieldType {
		case "goat_scan", "goat_lookup", "animal_id_scan":
			return stringValue(field, "key")
		}
	}
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := stringValue(field, "key")
		if key == "goat_ids" || key == "goats" {
			return key
		}
	}
	return ""
}

func goatIDsFromAnswer(raw any) []string {
	switch typed := raw.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = appendGoatID(out, item)
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = appendGoatID(out, item)
		}
		return out
	default:
		return appendGoatID(nil, typed)
	}
}

func appendGoatID(out []string, raw any) []string {
	switch typed := raw.(type) {
	case string:
		if v := strings.TrimSpace(typed); v != "" {
			return append(out, v)
		}
	case map[string]any:
		for _, key := range []string{"goat_id", "id", "value"} {
			if v := strings.TrimSpace(fmt.Sprint(typed[key])); v != "" && v != "<nil>" {
				return append(out, v)
			}
		}
	}
	return out
}

func validateVersionCommand(cmd ports.VersionCommand) error {
	if err := validateTenantActorID(cmd.TenantID, cmd.ActorID, cmd.SOPID, "sop_id"); err != nil {
		return err
	}
	if !uuidutil.IsUUIDString(cmd.SOPVersionID) {
		return BadRequest("invalid_sop_version_id", "sop_version_id must be a UUID")
	}
	if cmd.RowVersion <= 0 {
		return BadRequest("invalid_row_version", "row_version is required")
	}
	return nil
}

func validateCreateTask(body domain.CreateTaskRequest) error {
	if body.SOPCode == "" && body.SOPVersionID == nil {
		return BadRequest("invalid_sop_ref", "sop_code or sop_version_id is required")
	}
	if body.SOPVersionID != nil && !uuidutil.IsUUIDString(*body.SOPVersionID) {
		return BadRequest("invalid_sop_version_id", "sop_version_id must be a UUID")
	}
	if body.TaskType == "" || body.Title == "" {
		return BadRequest("invalid_task", "task_type and title are required")
	}
	if !validScope(body.ScopeType, body.ScopeID) {
		return BadRequest("invalid_scope", "scope_type and scope_id are required")
	}
	if body.AssignedTo != nil && *body.AssignedTo != "" && !uuidutil.IsUUIDString(*body.AssignedTo) {
		return BadRequest("invalid_assigned_to", "assigned_to must be a UUID")
	}
	if body.DueAt != nil && *body.DueAt != "" {
		if _, err := time.Parse(time.RFC3339, *body.DueAt); err != nil {
			return BadRequest("invalid_due_at", "due_at must be RFC3339")
		}
	}
	return nil
}

func normalizeTask(body *domain.CreateTaskRequest) {
	body.SOPCode = normalizeCode(body.SOPCode)
	body.TaskType = strings.TrimSpace(body.TaskType)
	body.Title = strings.TrimSpace(body.Title)
	body.Description = strings.TrimSpace(body.Description)
	body.ScopeType = strings.TrimSpace(body.ScopeType)
	body.ScopeID = strings.TrimSpace(body.ScopeID)
	body.Priority = strings.TrimSpace(body.Priority)
	if body.Priority == "" {
		body.Priority = "normal"
	}
	if body.AssignedTo != nil {
		v := strings.TrimSpace(*body.AssignedTo)
		body.AssignedTo = &v
	}
	if body.DueAt != nil {
		v := strings.TrimSpace(*body.DueAt)
		body.DueAt = &v
	}
	if body.Context == nil {
		body.Context = map[string]any{}
	}
}

func validateTenant(id string) error {
	if !uuidutil.IsUUIDString(id) {
		return BadRequest("invalid_tenant", "tenant_id is required")
	}
	return nil
}

func validateTenantAndActor(tenantID, actorID string) error {
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	if !uuidutil.IsUUIDString(actorID) {
		return BadRequest("invalid_actor", "actor_id is required")
	}
	return nil
}

func validateTenantAndID(tenantID, id, field string) error {
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	if !uuidutil.IsUUIDString(id) {
		return BadRequest("invalid_"+field, field+" must be a UUID")
	}
	return nil
}

func validateTenantActorID(tenantID, actorID, id, field string) error {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return err
	}
	if !uuidutil.IsUUIDString(id) {
		return BadRequest("invalid_"+field, field+" must be a UUID")
	}
	return nil
}

func validScope(scopeType, scopeID string) bool {
	switch scopeType {
	case "tenant", "custodian_party", "farm", "park", "shed", "cohort":
	default:
		return false
	}
	return uuidutil.IsUUIDString(scopeID)
}

func boundedLimit(limit, max int) int {
	if limit <= 0 {
		return 50
	}
	if limit > max {
		return max
	}
	return limit
}

func normalizeCode(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.ReplaceAll(v, "-", "_")
	return v
}

func sanitizeFanoutError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "fanout_failed"
	}
	if len(msg) > 240 {
		return msg[:240]
	}
	return msg
}

func supportedFieldType(v string) bool {
	switch v {
	case "text", "number", "date_time", "boolean", "select", "multiselect",
		"goat_scan", "animal_id_scan", "goat_lookup",
		"shed_picker", "cohort_picker", "location_picker",
		"vaccine_batch_picker", "medicine_picker", "session_picker",
		"photo_proof", "video_proof":
		return true
	default:
		return false
	}
}

func normalizeFieldType(v string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(v))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "yesno", "yes_no", "yes/no":
		return "boolean", true
	case "datetime", "date_time_picker":
		return "date_time", true
	case "animal_id", "animal_id_reader":
		return "animal_id_scan", true
	case "shed", "shed_location_picker":
		return "shed_picker", true
	case "cohort", "cohort_location_picker":
		return "cohort_picker", true
	default:
		return normalized, normalized != v
	}
}

func validateRepeatForEachGoat(report *domain.ValidationReport, raw any, fields map[string]struct{}) {
	if raw == nil {
		return
	}
	switch typed := raw.(type) {
	case bool:
		return
	case string:
		if typed == "" {
			addError(report, "form_dsl.repeat_for_each_goat", "invalid", "repeat_for_each_goat source field is empty")
			return
		}
		if _, ok := fields[typed]; !ok {
			addError(report, "form_dsl.repeat_for_each_goat", "unknown_field", "repeat_for_each_goat source field must exist")
		}
	case map[string]any:
		source := stringValue(typed, "source_field")
		if source == "" {
			addError(report, "form_dsl.repeat_for_each_goat.source_field", "required", "source_field is required")
			return
		}
		if _, ok := fields[source]; !ok {
			addError(report, "form_dsl.repeat_for_each_goat.source_field", "unknown_field", "source_field must exist")
		}
	default:
		addError(report, "form_dsl.repeat_for_each_goat", "invalid", "repeat_for_each_goat must be boolean, string, or object")
	}
}

func validateRules(report *domain.ValidationReport, raw any, fields map[string]struct{}) {
	if raw == nil {
		return
	}
	rules, ok := raw.([]any)
	if !ok {
		addError(report, "form_dsl.rules", "invalid", "rules must be an array")
		return
	}
	for idx, rawRule := range rules {
		path := fmt.Sprintf("form_dsl.rules.%d", idx)
		rule, ok := rawRule.(map[string]any)
		if !ok {
			addError(report, path, "invalid", "rule must be an object")
			continue
		}
		ruleType := stringValue(rule, "type")
		if !supportedRuleType(ruleType) {
			addError(report, path+".type", "unsupported", "rule type is not supported")
			continue
		}
		field := stringValue(rule, "field")
		if ruleTargetsField(ruleType) {
			if field == "" {
				addError(report, path+".field", "required", "rule field is required")
			} else if _, ok := fields[field]; !ok {
				addError(report, path+".field", "unknown_field", "rule field must exist")
			}
		}
		if ruleNeedsCondition(ruleType) {
			validateCondition(report, path+".when", rule["when"], fields)
		}
	}
}

func supportedRuleType(v string) bool {
	switch strings.TrimSpace(v) {
	case "visible_if", "required_if", "enabled_if", "proof_required_if",
		"block_submission_if", "requires_supervisor_if":
		return true
	default:
		return false
	}
}

func ruleTargetsField(v string) bool {
	switch v {
	case "visible_if", "required_if", "enabled_if", "proof_required_if":
		return true
	default:
		return false
	}
}

func ruleNeedsCondition(v string) bool {
	switch v {
	case "visible_if", "required_if", "enabled_if", "proof_required_if", "block_submission_if", "requires_supervisor_if":
		return true
	default:
		return false
	}
}

func validateCondition(report *domain.ValidationReport, path string, raw any, fields map[string]struct{}) {
	if raw == nil {
		addError(report, path, "required", "condition is required")
		return
	}
	condition, ok := raw.(map[string]any)
	if !ok {
		addError(report, path, "invalid", "condition must be an object")
		return
	}
	if rawAll, ok := condition["all"]; ok {
		validateConditionList(report, path+".all", rawAll, fields)
		return
	}
	if rawAny, ok := condition["any"]; ok {
		validateConditionList(report, path+".any", rawAny, fields)
		return
	}
	field := stringValue(condition, "field")
	if field == "" {
		addError(report, path+".field", "required", "condition field is required")
	} else if _, ok := fields[field]; !ok {
		addError(report, path+".field", "unknown_field", "condition field must exist")
	}
	if !supportedConditionOperator(stringValue(condition, "operator")) {
		addError(report, path+".operator", "unsupported", "condition operator is not supported")
	}
}

func validateConditionList(report *domain.ValidationReport, path string, raw any, fields map[string]struct{}) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		addError(report, path, "invalid", "condition list must be a non-empty array")
		return
	}
	for idx, item := range items {
		validateCondition(report, fmt.Sprintf("%s.%d", path, idx), item, fields)
	}
}

func supportedConditionOperator(v string) bool {
	switch strings.TrimSpace(v) {
	case "equals", "not_equals", "empty", "not_empty", "in", "not_in", "gt", "gte", "lt", "lte":
		return true
	default:
		return false
	}
}

func validateProofPolicy(report *domain.ValidationReport, policy, formDSL map[string]any, fields []any) {
	if policy == nil {
		addError(report, "proof_policy", "required", "proof_policy is required")
		return
	}
	if _, ok := policy["scope"]; ok {
		addError(report, "proof_policy.scope", "unsupported", "scope is not supported; use subject_scope")
	}
	subjectScope := stringValue(policy, "subject_scope")
	if !validProofSubjectScope(subjectScope) {
		addError(report, "proof_policy.subject_scope", "required", "subject_scope must be batch, goat, shed, or task")
	}
	types, ok := proofPolicyTypes(policy)
	if !ok {
		addError(report, "proof_policy.types", "required", "types must be a non-empty array")
	} else {
		for _, proofType := range types {
			if !supportedProofType(proofType) {
				addError(report, "proof_policy.types", "unsupported", "proof type is not supported")
			}
		}
	}
	minimum, ok := proofPolicyMinimum(policy)
	if !ok {
		addError(report, "proof_policy.minimum_count", "required", "minimum_count must be a non-negative integer")
	}
	if proofRequired(policy) {
		if ok && minimum < 1 {
			addError(report, "proof_policy.minimum_count", "invalid", "minimum_count must be at least 1 when proof is required")
		}
		if !hasProofFieldForTypes(fields, types) && !hasGoatRowProofCapture(formDSL, subjectScope, types) {
			addError(report, "proof_policy", "missing_proof_field", "proof policy requires a matching photo_proof or video_proof field")
		}
	}
	minimumPerSubject, hasMinimumPerSubject := proofPolicyInteger(policy, "minimum_count_per_subject")
	if _, exists := policy["minimum_count_per_subject"]; exists && !hasMinimumPerSubject {
		addError(report, "proof_policy.minimum_count_per_subject", "invalid", "minimum_count_per_subject must be a non-negative integer")
	}
	maximumPerSubject, hasMaximumPerSubject := proofPolicyInteger(policy, "maximum_count_per_subject")
	if _, exists := policy["maximum_count_per_subject"]; exists && !hasMaximumPerSubject {
		addError(report, "proof_policy.maximum_count_per_subject", "invalid", "maximum_count_per_subject must be a non-negative integer")
	}
	if hasMinimumPerSubject && hasMaximumPerSubject && maximumPerSubject < minimumPerSubject {
		addError(report, "proof_policy.maximum_count_per_subject", "invalid", "maximum_count_per_subject must be greater than or equal to minimum_count_per_subject")
	}
	if retention := stringValue(policy, "retention_policy"); retention != "" && !validProofRetentionPolicy(retention) {
		addError(report, "proof_policy.retention_policy", "unsupported", "retention_policy must be operational_90d, standard_1y, critical_7y, or legal_hold")
	}
}

func validateVaccinationDriveSOPContract(report *domain.ValidationReport, sopCode string, formDSL, proofPolicy map[string]any) {
	switch strings.TrimSpace(sopCode) {
	case "vaccination.drive", "vaccination.session":
	default:
		return
	}
	types, ok := proofPolicyTypes(proofPolicy)
	if !ok || len(types) != 1 || types[0] != "video" {
		addError(report, "proof_policy.types", "invalid", "vaccination SOP proof must be video")
	}
	if !boolValue(proofPolicy, "verify_before_apply") {
		addError(report, "proof_policy.verify_before_apply", "invalid", "vaccination SOP proof must be verified before completion")
	}
	switch vaccinationProofMode(proofPolicy) {
	case "per_goat_video":
		if stringValue(proofPolicy, "subject_scope") != "goat" {
			addError(report, "proof_policy.subject_scope", "invalid", "per-goat vaccination proof must use subject_scope=goat")
		}
		if minimum, ok := proofPolicyInteger(proofPolicy, "minimum_count_per_subject"); !ok || minimum < 1 {
			addError(report, "proof_policy.minimum_count_per_subject", "invalid", "per-goat vaccination proof requires at least one completed proof per goat")
		}
		if maximum, ok := proofPolicyInteger(proofPolicy, "maximum_count_per_subject"); !ok || maximum < 1 || maximum > 5 {
			addError(report, "proof_policy.maximum_count_per_subject", "invalid", "per-goat vaccination proof allows at most five completed proofs per goat")
		}
		config, ok := formDSL["goat_row_proof"].(map[string]any)
		if !ok || stringValue(config, "subject_scope") != "goat" || stringValue(config, "capture_source") != "in_app_camera" {
			addError(report, "form_dsl.goat_row_proof", "required", "per-goat vaccination proof requires in-app camera proof for each goat")
		}
	case "shed_level_video":
		if stringValue(proofPolicy, "subject_scope") != "shed" {
			addError(report, "proof_policy.subject_scope", "invalid", "shed-level vaccination proof must use subject_scope=shed")
		}
		minimum, ok := proofPolicyInteger(proofPolicy, "minimum_count")
		if !ok || minimum < 1 {
			addError(report, "proof_policy.minimum_count", "invalid", "shed-level vaccination proof requires at least one completed shed video")
		}
		maximum := 5
		if value, ok := proofPolicyInteger(proofPolicy, "maximum_count"); ok {
			maximum = value
		} else if value, ok := proofPolicyInteger(proofPolicy, "maximum_count_per_subject"); ok {
			maximum = value
		}
		if maximum < 1 || maximum > 5 || (ok && maximum < minimum) {
			addError(report, "proof_policy.maximum_count", "invalid", "shed-level vaccination proof allows one to five completed shed videos")
		}
		if !stringSliceContains(proofPolicyStringSlice(proofPolicy, "allowed_capture_sources"), "in_app_camera") ||
			!stringSliceContains(proofPolicyStringSlice(proofPolicy, "allowed_capture_sources"), "gallery_picker") {
			addError(report, "proof_policy.allowed_capture_sources", "invalid", "shed-level vaccination proof must allow camera and gallery picker")
		}
		if !formHasShedVideoProofField(formDSL) {
			addError(report, "form_dsl.shed_video", "required", "shed-level vaccination proof requires a required shed video field")
		}
	default:
		addError(report, "proof_policy.proof_mode", "invalid", "vaccination proof_mode must be per_goat_video or shed_level_video")
	}
}

type vaccinationProofGate struct {
	SubjectType  string
	MinimumCount int
	MaximumCount int
}

func vaccinationCompletionProofGate(policy map[string]any) vaccinationProofGate {
	if vaccinationProofMode(policy) == "shed_level_video" {
		minimum := 1
		if value, ok := proofPolicyInteger(policy, "minimum_count"); ok && value > 0 {
			minimum = value
		}
		maximum := 5
		if value, ok := proofPolicyInteger(policy, "maximum_count"); ok && value > 0 {
			maximum = value
		} else if value, ok := proofPolicyInteger(policy, "maximum_count_per_subject"); ok && value > 0 {
			maximum = value
		}
		return vaccinationProofGate{SubjectType: "shed", MinimumCount: minimum, MaximumCount: maximum}
	}
	return vaccinationProofGate{SubjectType: "goat", MinimumCount: 1, MaximumCount: 5}
}

func vaccinationProofMode(policy map[string]any) string {
	mode := strings.TrimSpace(stringValue(policy, "proof_mode"))
	if mode != "" {
		return mode
	}
	switch stringValue(policy, "subject_scope") {
	case "shed":
		return "shed_level_video"
	default:
		return "per_goat_video"
	}
}

func proofPolicyStringSlice(policy map[string]any, key string) []string {
	raw, ok := policy[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		return typed
	default:
		return nil
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func formHasShedVideoProofField(formDSL map[string]any) bool {
	if config, ok := formDSL["shed_video"].(map[string]any); ok &&
		stringValue(config, "subject_scope") == "shed" &&
		stringValue(config, "capture_source") != "" {
		return true
	}
	fields, _ := formDSL["fields"].([]any)
	for _, field := range fields {
		item, ok := field.(map[string]any)
		if !ok {
			continue
		}
		fieldType, _ := normalizeFieldType(stringValue(item, "type"))
		if fieldType == "video_proof" && stringValue(item, "proof_subject") == "shed" && boolValue(item, "required") {
			return true
		}
	}
	return false
}

func validProofSubjectScope(v string) bool {
	switch v {
	case "batch", "goat", "shed", "task":
		return true
	default:
		return false
	}
}

func proofPolicyTypes(policy map[string]any) ([]string, bool) {
	raw, ok := policy["types"]
	if !ok {
		return nil, false
	}
	var values []string
	switch typed := raw.(type) {
	case []any:
		values = make([]string, 0, len(typed))
		for _, value := range typed {
			s, ok := value.(string)
			if !ok {
				return nil, false
			}
			values = append(values, s)
		}
	case []string:
		values = typed
	default:
		return nil, false
	}
	if len(values) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		s := strings.ToLower(strings.TrimSpace(value))
		if s == "" {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func supportedProofType(v string) bool {
	switch v {
	case "photo", "video":
		return true
	default:
		return false
	}
}

func validProofRetentionPolicy(v string) bool {
	switch strings.TrimSpace(v) {
	case "", "operational_90d", "standard_1y", "critical_7y", "legal_hold":
		return true
	default:
		return false
	}
}

func proofPolicyMinimum(policy map[string]any) (int, bool) {
	return proofPolicyInteger(policy, "minimum_count")
}

func proofPolicyInteger(policy map[string]any, key string) (int, bool) {
	switch v := policy[key].(type) {
	case float64:
		i := int(v)
		return i, v >= 0 && float64(i) == v
	case int:
		return v, v >= 0
	case int32:
		return int(v), v >= 0
	case int64:
		return int(v), v >= 0
	default:
		return 0, false
	}
}

func hasGoatRowProofCapture(formDSL map[string]any, subjectScope string, proofTypes []string) bool {
	if subjectScope != "goat" || formDSL == nil {
		return false
	}
	config, ok := formDSL["goat_row_proof"].(map[string]any)
	if !ok || stringValue(config, "subject_scope") != "goat" || stringValue(config, "capture_source") != "in_app_camera" {
		return false
	}
	for _, proofType := range proofTypes {
		if proofType != "video" {
			return false
		}
	}
	return len(proofTypes) > 0
}

func validateAnswerValue(errors *[]domain.ValidationIssue, field map[string]any, value any) {
	if field == nil {
		return
	}
	key := stringValue(field, "key")
	fieldType, _ := normalizeFieldType(stringValue(field, "type"))
	switch fieldType {
	case "number":
		if _, ok := numericAnswer(value); !ok {
			addErrorToList(errors, key, "invalid_answer_type", "answer must be numeric")
		}
	case "boolean":
		if !boolAnswer(value) {
			addErrorToList(errors, key, "invalid_answer_type", "answer must be boolean")
		}
	case "date_time":
		if !dateTimeAnswer(value) {
			addErrorToList(errors, key, "invalid_answer_type", "answer must be an RFC3339 timestamp")
		}
	case "goat_scan":
		if !stringListAnswer(value) {
			addErrorToList(errors, key, "invalid_answer_type", "answer must contain scanned RFID tags")
		}
	case "goat_lookup":
		if !uuidListAnswer(value) {
			addErrorToList(errors, key, "invalid_answer_type", "answer must contain goat UUIDs")
		}
	case "vaccine_batch_picker", "medicine_picker", "shed_picker", "cohort_picker", "location_picker", "session_picker":
		if !uuidStringAnswer(value) {
			addErrorToList(errors, key, "invalid_answer_type", "answer must be a UUID")
		}
	case "multiselect":
		if !stringListAnswer(value) {
			addErrorToList(errors, key, "invalid_answer_type", "answer must be an array of strings")
		}
	case "text", "select", "animal_id_scan", "photo_proof", "video_proof":
		if !stringAnswer(value) {
			addErrorToList(errors, key, "invalid_answer_type", "answer must be a string")
		}
	}
}

func fieldByKey(fields []any, key string) map[string]any {
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if ok && stringValue(field, "key") == key {
			return field
		}
	}
	return nil
}

func boolAnswer(v any) bool {
	switch typed := v.(type) {
	case bool:
		return true
	case string:
		_, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil
	default:
		return false
	}
}

func dateTimeAnswer(v any) bool {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return false
	}
	_, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	return err == nil
}

func uuidStringAnswer(v any) bool {
	s, ok := v.(string)
	return ok && uuidutil.IsUUIDString(strings.TrimSpace(s))
}

func uuidListAnswer(v any) bool {
	items := goatIDsFromAnswer(v)
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if !uuidutil.IsUUIDString(item) {
			return false
		}
	}
	return true
}

func stringListAnswer(v any) bool {
	var items []string
	switch typed := v.(type) {
	case []any:
		items = make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return false
			}
			items = append(items, s)
		}
	case []string:
		items = typed
	default:
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			return false
		}
	}
	return true
}

func stringAnswer(v any) bool {
	_, ok := v.(string)
	return ok
}

func proofRequired(policy map[string]any) bool {
	return boolValue(policy, "required")
}

func hasProofField(fields []any) bool {
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fieldType, _ := normalizeFieldType(stringValue(field, "type"))
		switch fieldType {
		case "photo_proof", "video_proof":
			return true
		}
	}
	return false
}

func hasProofFieldForTypes(fields []any, proofTypes []string) bool {
	if len(proofTypes) == 0 {
		return hasProofField(fields)
	}
	available := map[string]bool{}
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fieldType, _ := normalizeFieldType(stringValue(field, "type"))
		switch fieldType {
		case "photo_proof":
			available["photo"] = true
		case "video_proof":
			available["video"] = true
		}
	}
	for _, proofType := range proofTypes {
		if !available[proofType] {
			return false
		}
	}
	return true
}

func completedProofCount(refs []domain.ProofReference, policy map[string]any) int {
	allowedTypes := proofTypeSet(policy)
	count := 0
	for _, ref := range refs {
		if ref.ProofID == "" || (ref.UploadState != "" && ref.UploadState != "completed") {
			continue
		}
		if len(allowedTypes) > 0 && !allowedTypes[ref.ProofType] {
			continue
		}
		count++
	}
	return count
}

func proofMinimumCount(policy map[string]any) int {
	if policy == nil {
		return 1
	}
	if minimum, ok := proofPolicyMinimum(policy); ok && minimum > 1 {
		return minimum
	}
	return 1
}

func proofTypeSet(policy map[string]any) map[string]bool {
	if policy == nil {
		return nil
	}
	raw, ok := proofPolicyTypes(policy)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, value := range raw {
		out[value] = true
	}
	return out
}

func missingExpectedProofSubjects(refs []domain.ProofReference, policy map[string]any) []string {
	expected := expectedProofSubjects(policy)
	if len(expected) == 0 {
		return nil
	}
	allowedTypes := proofTypeSet(policy)
	covered := map[string]bool{}
	for _, ref := range refs {
		if ref.ProofID == "" || (ref.UploadState != "" && ref.UploadState != "completed") {
			continue
		}
		if len(allowedTypes) > 0 && !allowedTypes[ref.ProofType] {
			continue
		}
		covered[ref.SubjectType] = true
	}
	var missing []string
	for _, subject := range expected {
		if !covered[subject] {
			missing = append(missing, subject)
		}
	}
	return missing
}

func expectedProofSubjects(policy map[string]any) []string {
	if policy == nil {
		return nil
	}
	raw, ok := policy["expected_subjects"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if s := strings.TrimSpace(fmt.Sprint(value)); s != "" && s != "<nil>" {
			out = append(out, s)
		}
	}
	return out
}

// validatePerGoatProofRefs enforces the vaccination camera contract: every goat selected in the
// submission has its own completed, server-resolved proof reference. A clip may cover multiple
// vaccines administered to that same goat during one handling, but it must never cover a different
// goat or silently satisfy the whole drive.
func validatePerGoatProofRefs(errors *[]domain.ValidationIssue, answers map[string]any, refs []domain.ProofReference, policy map[string]any) bool {
	if stringValue(policy, "subject_scope") != "goat" {
		return false
	}
	minimum, ok := proofPolicyInteger(policy, "minimum_count_per_subject")
	if !ok || minimum < 1 {
		return false
	}
	maximum, hasMaximum := proofPolicyInteger(policy, "maximum_count_per_subject")
	goatIDs := proofSubjectIDs(answers["goat_ids"])
	if len(goatIDs) == 0 {
		return false
	}
	wanted := make(map[string]bool, len(goatIDs))
	for _, goatID := range goatIDs {
		wanted[goatID] = true
	}
	allowedTypes := proofTypeSet(policy)
	counts := make(map[string]int, len(goatIDs))
	blocked := false
	for _, ref := range refs {
		if ref.ProofID == "" || (ref.UploadState != "" && ref.UploadState != "completed") {
			continue
		}
		if len(allowedTypes) > 0 && !allowedTypes[ref.ProofType] {
			continue
		}
		if ref.SubjectType != "goat" || ref.SubjectID == nil || strings.TrimSpace(*ref.SubjectID) == "" {
			continue
		}
		goatID := strings.TrimSpace(*ref.SubjectID)
		if !wanted[goatID] {
			addErrorToList(errors, "proof_refs", "proof_subject_invalid", "completed goat proof does not belong to a scanned goat")
			blocked = true
			continue
		}
		counts[goatID]++
	}
	for _, goatID := range goatIDs {
		if counts[goatID] < minimum {
			addErrorToList(errors, "proof_refs", "proof_subject_required", "completed proof is required for each scanned goat")
			blocked = true
		}
		if hasMaximum && maximum >= 0 && counts[goatID] > maximum {
			addErrorToList(errors, "proof_refs", "proof_subject_limit", "completed proof count exceeds the per-goat limit")
			blocked = true
		}
	}
	return blocked
}

func proofSubjectIDs(raw any) []string {
	var values []string
	switch typed := raw.(type) {
	case []any:
		values = make([]string, 0, len(typed))
		for _, value := range typed {
			values = append(values, strings.TrimSpace(fmt.Sprint(value)))
		}
	case []string:
		values = append(values, typed...)
	default:
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "<nil>" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func conditionMatches(raw any, answers map[string]any) (bool, bool) {
	condition, ok := raw.(map[string]any)
	if !ok {
		return false, false
	}
	if all, ok := condition["all"].([]any); ok {
		for _, item := range all {
			matches, evaluable := conditionMatches(item, answers)
			if !evaluable || !matches {
				return false, evaluable
			}
		}
		return true, true
	}
	if any, ok := condition["any"].([]any); ok {
		evaluableAny := false
		for _, item := range any {
			matches, evaluable := conditionMatches(item, answers)
			evaluableAny = evaluableAny || evaluable
			if evaluable && matches {
				return true, true
			}
		}
		return false, evaluableAny
	}
	field := stringValue(condition, "field")
	operator := stringValue(condition, "operator")
	if field == "" || operator == "" {
		return false, false
	}
	answer := answers[field]
	switch operator {
	case "empty":
		return isEmptyAnswer(answer), true
	case "not_empty":
		return !isEmptyAnswer(answer), true
	case "equals":
		return fmt.Sprint(answer) == fmt.Sprint(condition["value"]), true
	case "not_equals":
		return fmt.Sprint(answer) != fmt.Sprint(condition["value"]), true
	case "in", "not_in":
		found := valueInList(answer, condition["value"])
		if operator == "not_in" {
			return !found, true
		}
		return found, true
	case "gt", "gte", "lt", "lte":
		left, lok := numericAnswer(answer)
		right, rok := numericAnswer(condition["value"])
		if !lok || !rok {
			return false, false
		}
		switch operator {
		case "gt":
			return left > right, true
		case "gte":
			return left >= right, true
		case "lt":
			return left < right, true
		case "lte":
			return left <= right, true
		}
	}
	return false, false
}

func valueInList(answer, rawList any) bool {
	values, ok := rawList.([]any)
	if !ok {
		return false
	}
	answerValue := fmt.Sprint(answer)
	for _, value := range values {
		if answerValue == fmt.Sprint(value) {
			return true
		}
	}
	return false
}

func numericAnswer(v any) (float64, bool) {
	switch typed := v.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func ruleMessage(rule map[string]any, fallback string) string {
	if msg := stringValue(rule, "message"); msg != "" {
		return msg
	}
	return fallback
}

func isEmptyAnswer(v any) bool {
	if v == nil {
		return true
	}
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	default:
		return false
	}
}

func addError(report *domain.ValidationReport, field, code, message string) {
	report.Valid = false
	report.Errors = append(report.Errors, domain.ValidationIssue{Field: field, Code: code, Message: message})
}

func addWarning(report *domain.ValidationReport, field, code, message string) {
	report.Warnings = append(report.Warnings, domain.ValidationIssue{Field: field, Code: code, Message: message})
}

func addErrorToList(list *[]domain.ValidationIssue, field, code, message string) {
	*list = append(*list, domain.ValidationIssue{Field: field, Code: code, Message: message})
}

func stringValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func boolValue(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return NotFound("not_found", "resource not found")
	case errors.Is(err, ports.ErrConflict):
		return Conflict("write_conflict", "resource changed or violates constraints")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "idempotency key was reused for a different submission")
	case errors.Is(err, ports.ErrDenied):
		return Forbidden("permission_denied", "operation is not allowed")
	case errors.Is(err, ports.ErrInvalidFilter):
		return BadRequest("invalid_filter", "filter is invalid")
	default:
		return err
	}
}
