package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

type Service struct {
	repo ports.Repository
	now  func() time.Time
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func (s *Service) ListSOPs(ctx context.Context, params ports.ListSOPsParams, traceID string) (*domain.SOPListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.Status = strings.TrimSpace(params.Status)
	params.Limit = boundedLimit(params.Limit, 100)
	items, err := s.repo.ListSOPs(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SOPListResponse{Items: items, TraceID: traceID}, nil
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
	items, err := s.repo.ListTasks(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.TaskListResponse{Items: items, TraceID: traceID}, nil
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
	evaluation := Evaluate(version.FormDSL, version.ProofPolicy, cmd.Body.Answers, cmd.Body.ProofRefs)
	cmd.Report = domain.ValidationReport{Valid: evaluation.Valid, Errors: evaluation.Errors, Warnings: evaluation.Warnings}
	if !evaluation.Valid {
		return nil, BadRequest("submission_failed_validation", evaluation.Errors[0].Message)
	}
	cmd.ItemState = "accepted"
	cmd.TaskState = "accepted"
	if proofRequiresReview(version.ProofPolicy) {
		cmd.ItemState = "needs_review"
		cmd.TaskState = "needs_review"
	}
	cmd.MovementPayload = buildMovementPayload(task, cmd.Body)
	submission, updatedTask, _, err := s.repo.SubmitTask(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SubmissionResponse{Submission: submission, Task: updatedTask, TraceID: traceID}, nil
}

func (s *Service) reviewTask(ctx context.Context, cmd ports.ReviewTaskCommand, traceID string) (*domain.TaskResponse, error) {
	if err := validateTenantActorID(cmd.TenantID, cmd.ActorID, cmd.TaskID, "task_id"); err != nil {
		return nil, err
	}
	if cmd.Body.RowVersion <= 0 {
		return nil, BadRequest("invalid_row_version", "row_version is required")
	}
	task, err := s.repo.ReviewTask(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.TaskResponse{Task: task, TraceID: traceID}, nil
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
		if key == "" {
			addError(&report, fmt.Sprintf("form_dsl.fields.%d.key", idx), "required", "field key is required")
		}
		if _, exists := seen[key]; key != "" && exists {
			addError(&report, "form_dsl.fields", "duplicate", "field keys must be unique")
		}
		seen[key] = struct{}{}
		if !supportedFieldType(fieldType) {
			addError(&report, fmt.Sprintf("form_dsl.fields.%s.type", key), "unsupported", "field type is not supported")
		}
	}
	if proofRequired(proofPolicy) && !hasProofField(fields) {
		addError(&report, "proof_policy", "missing_proof_field", "proof policy requires a photo_proof or video_proof field")
	}
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
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := stringValue(field, "key")
		required := boolValue(field, "required")
		state := domain.FieldState{Key: key, Visible: true, Required: required}
		if required && isEmptyAnswer(answers[key]) {
			state.Blocked = true
			state.Message = "Required answer missing."
			addErrorToList(&out.Errors, key, "required", "required answer missing")
		}
		out.FieldStates = append(out.FieldStates, state)
	}
	if proofRequired(proofPolicy) && !hasCompletedProof(proofRefs) {
		addErrorToList(&out.Errors, "proof_refs", "proof_required", "completed proof is required")
		out.WorkflowPath = append(out.WorkflowPath, "proof_verification")
		out.FinalState = "blocked"
	} else if proofRequiresReview(proofPolicy) {
		out.WorkflowPath = append(out.WorkflowPath, "proof_verification")
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

func supportedFieldType(v string) bool {
	switch v {
	case "text", "number", "date_time", "select", "multiselect", "goat_lookup", "rfid_scan", "location_picker", "photo_proof", "video_proof":
		return true
	default:
		return false
	}
}

func proofRequired(policy map[string]any) bool {
	return boolValue(policy, "required")
}

func proofRequiresReview(policy map[string]any) bool {
	return proofRequired(policy) && boolValue(policy, "verify_before_apply")
}

func hasProofField(fields []any) bool {
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch stringValue(field, "type") {
		case "photo_proof", "video_proof":
			return true
		}
	}
	return false
}

func hasCompletedProof(refs []domain.ProofReference) bool {
	for _, ref := range refs {
		if ref.ProofID != "" && (ref.UploadState == "" || ref.UploadState == "completed") {
			return true
		}
	}
	return false
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
