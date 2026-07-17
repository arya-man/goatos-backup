package domain

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	FieldErrors []FieldError `json:"field_errors"`
	TraceID     string       `json:"trace_id"`
	Retryable   bool         `json:"retryable"`
}

type ValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationReport struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationIssue `json:"errors"`
	Warnings []ValidationIssue `json:"warnings"`
}

type SOPDefinition struct {
	SOPID           string  `json:"sop_id"`
	TenantID        string  `json:"tenant_id"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Status          string  `json:"status"`
	ActiveVersionID *string `json:"active_sop_version_id"`
	VersionCount    int     `json:"version_count"`
	RowVersion      int     `json:"row_version"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type SOPVersion struct {
	SOPVersionID     string           `json:"sop_version_id"`
	TenantID         string           `json:"tenant_id"`
	SOPID            string           `json:"sop_id"`
	SOPCode          string           `json:"sop_code"`
	Version          int              `json:"version"`
	VersionLabel     string           `json:"version_label"`
	Status           string           `json:"status"`
	FormDSL          map[string]any   `json:"form_dsl"`
	ProofPolicy      map[string]any   `json:"proof_policy"`
	Compatibility    map[string]any   `json:"compatibility"`
	ValidationReport ValidationReport `json:"validation_report"`
	PublishedAt      *string          `json:"published_at"`
	RetiredAt        *string          `json:"retired_at"`
	RowVersion       int              `json:"row_version"`
	CreatedAt        string           `json:"created_at"`
	UpdatedAt        string           `json:"updated_at"`
}

type SOPListResponse struct {
	Items      []SOPDefinition `json:"items"`
	NextCursor *string         `json:"next_cursor"`
	// LatestVersions embeds each listed SOP's latest version keyed by sop_id, so a list consumer that
	// needs version-derived facets (domain/trigger/steps/gates from form_dsl + proof_policy) does not
	// fan out one GetSOP detail call per row (C35-002/C35-015: list-then-N-details N+1). Populated by
	// ListSOPs in a single batched query; omitted when no listed SOP has a version.
	LatestVersions map[string]*SOPVersion `json:"latest_versions,omitempty"`
	TraceID        string                 `json:"trace_id"`
}

type SOPResponse struct {
	SOP           SOPDefinition `json:"sop"`
	LatestVersion *SOPVersion   `json:"latest_version,omitempty"`
	TraceID       string        `json:"trace_id"`
}

type SOPVersionResponse struct {
	Version SOPVersion `json:"version"`
	TraceID string     `json:"trace_id"`
}

type CreateSOPRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CreateSOPVersionRequest struct {
	VersionLabel  string         `json:"version_label"`
	FormDSL       map[string]any `json:"form_dsl"`
	ProofPolicy   map[string]any `json:"proof_policy"`
	Compatibility map[string]any `json:"compatibility"`
}

type DryRunRequest struct {
	Answers   map[string]any   `json:"answers"`
	ProofRefs []ProofReference `json:"proof_refs"`
	Context   map[string]any   `json:"context"`
}

type DryRunResponse struct {
	Valid        bool              `json:"valid"`
	Errors       []ValidationIssue `json:"errors"`
	Warnings     []ValidationIssue `json:"warnings"`
	FieldStates  []FieldState      `json:"field_states"`
	WorkflowPath []string          `json:"workflow_path"`
	FinalState   string            `json:"final_state"`
	TraceID      string            `json:"trace_id"`
}

type FieldState struct {
	Key      string `json:"key"`
	Visible  bool   `json:"visible"`
	Required bool   `json:"required"`
	Blocked  bool   `json:"blocked"`
	Message  string `json:"message,omitempty"`
}

type TaskSummary struct {
	TaskID       string         `json:"task_id"`
	TenantID     string         `json:"tenant_id"`
	SOPID        string         `json:"sop_id"`
	SOPVersionID string         `json:"sop_version_id"`
	SOPCode      string         `json:"sop_code"`
	TaskType     string         `json:"task_type"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	State        string         `json:"state"`
	AssignedTo   *string        `json:"assigned_to"`
	ScopeType    string         `json:"scope_type"`
	ScopeID      string         `json:"scope_id"`
	Priority     string         `json:"priority"`
	DueAt        *string        `json:"due_at"`
	Context      map[string]any `json:"context"`
	RowVersion   int            `json:"row_version"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

type TaskListResponse struct {
	Items   []TaskSummary `json:"items"`
	TraceID string        `json:"trace_id"`
}

type TaskResponse struct {
	Task        TaskSummary         `json:"task"`
	Version     *SOPVersion         `json:"sop_version,omitempty"`
	Submissions []SubmissionSummary `json:"submissions,omitempty"`
	TraceID     string              `json:"trace_id"`
}

type CreateTaskRequest struct {
	SOPCode      string         `json:"sop_code"`
	SOPVersionID *string        `json:"sop_version_id"`
	TaskType     string         `json:"task_type"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	AssignedTo   *string        `json:"assigned_to"`
	ScopeType    string         `json:"scope_type"`
	ScopeID      string         `json:"scope_id"`
	Priority     string         `json:"priority"`
	DueAt        *string        `json:"due_at"`
	Context      map[string]any `json:"context"`
}

// BatchTaskRequest is one task creation request for bulk operations.
type BatchTaskRequest struct {
	BatchID   string
	TaskType  string
	Title     string
	ScopeType string
	ScopeID   string
}

type AssignTaskRequest struct {
	AssignedTo string `json:"assigned_to"`
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

type ReviewTaskRequest struct {
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

type RetryReviewFanoutsRequest struct {
	Limit int `json:"limit"`
}

type RetryReviewFanoutsResponse struct {
	Applied int    `json:"applied"`
	TraceID string `json:"trace_id"`
}

type RetrySubmissionFanoutsRequest struct {
	Limit int `json:"limit"`
}

type RetrySubmissionFanoutsResponse struct {
	Applied int    `json:"applied"`
	TraceID string `json:"trace_id"`
}

type FailedSubmissionFanout struct {
	TaskID                      string `json:"task_id"`
	SubmissionID                string `json:"submission_id"`
	SOPCode                     string `json:"sop_code"`
	TaskType                    string `json:"task_type"`
	TaskState                   string `json:"task_state"`
	SubmittedAt                 string `json:"submitted_at"`
	FanoutUpdatedAt             string `json:"fanout_updated_at"`
	AgeSeconds                  int64  `json:"age_seconds"`
	AttemptCount                int    `json:"attempt_count"`
	LastError                   string `json:"last_error"`
	EligibleSubmissionItems     int    `json:"eligible_submission_items"`
	MaterializedCompletionCount int    `json:"materialized_completion_count"`
	MissingCompletionCount      int    `json:"missing_completion_count"`
}

type FailedSubmissionFanoutsResponse struct {
	Items   []FailedSubmissionFanout `json:"items"`
	TraceID string                   `json:"trace_id"`
}

type SubmissionSummary struct {
	SubmissionID     string           `json:"submission_id"`
	TaskID           string           `json:"task_id"`
	SOPVersionID     string           `json:"sop_version_id"`
	SubmittedBy      string           `json:"submitted_by"`
	IdempotencyKey   string           `json:"idempotency_key"`
	Answers          map[string]any   `json:"answers"`
	ProofRefs        []ProofReference `json:"proof_refs"`
	State            string           `json:"state"`
	ValidationReport ValidationReport `json:"validation_report"`
	Items            []SubmissionItem `json:"items"`
	SubmittedAt      string           `json:"submitted_at"`
	AcceptedAt       *string          `json:"accepted_at"`
	RowVersion       int              `json:"row_version"`
}

type SubmissionItem struct {
	ItemID  string         `json:"item_id"`
	GoatID  *string        `json:"goat_id"`
	ItemKey string         `json:"item_key"`
	State   string         `json:"state"`
	Result  map[string]any `json:"result"`
}

type ProofReference struct {
	ProofID     string         `json:"proof_id"`
	ProofType   string         `json:"proof_type"`
	SubjectType string         `json:"subject_type"`
	SubjectID   *string        `json:"subject_id"`
	UploadState string         `json:"upload_state"`
	Metadata    map[string]any `json:"metadata"`
}

type ProofBinding struct {
	TaskID    string
	ScopeType string
	ScopeID   string
}

type SubmitTaskRequest struct {
	SOPVersionID   string           `json:"sop_version_id"`
	IdempotencyKey string           `json:"idempotency_key"`
	Answers        map[string]any   `json:"answers"`
	ProofRefs      []ProofReference `json:"proof_refs"`
}

type ScanCaptureRequest struct {
	FieldKey     string `json:"field_key"`
	Tag          string `json:"tag"`
	GoatID       string `json:"goat_id,omitempty"`
	ObligationID string `json:"obligation_id,omitempty"`
	CapturedAtMs *int64 `json:"captured_at_ms,omitempty"`
}

type ScanCaptureSummary struct {
	CaptureID    string `json:"capture_id"`
	TaskID       string `json:"task_id"`
	FieldKey     string `json:"field_key"`
	Tag          string `json:"tag"`
	GoatID       string `json:"goat_id,omitempty"`
	ObligationID string `json:"obligation_id,omitempty"`
	CapturedAt   string `json:"captured_at"`
}

type ScanCaptureResponse struct {
	Capture ScanCaptureSummary `json:"capture"`
	TraceID string             `json:"trace_id"`
}

type SubmissionResponse struct {
	Submission SubmissionSummary `json:"submission"`
	Task       TaskSummary       `json:"task"`
	TraceID    string            `json:"trace_id"`
}
