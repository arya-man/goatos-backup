package domain

import "time"

const (
	ModeDryRun               = "dry_run"
	ModeExecute              = "execute"
	ModeNightlyDeepReconcile = "nightly_deep_reconcile"

	DomainAll             = "all"
	DomainIdentity        = "identity"
	DomainLifecycle       = "lifecycle"
	DomainCurrentLocation = "current_location"
	DomainActiveCount     = "active_count"

	StatusPlanning  = "planning"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
	StatusBlocked   = "blocked"

	StepPending   = "pending"
	StepRunning   = "running"
	StepCompleted = "completed"
	StepFailed    = "failed"
	StepCanceled  = "canceled"
	StepBlocked   = "blocked"

	FreshnessGreen   = "green"
	FreshnessYellow  = "yellow"
	FreshnessRed     = "red"
	FreshnessUnknown = "unknown"

	CriticalityCritical    = "critical"
	CriticalityNoncritical = "noncritical"

	CounterCheckPending   = "pending"
	CounterCheckSucceeded = "succeeded"
	CounterCheckFailed    = "failed"
	CounterCheckSkipped   = "skipped"

	EvidenceReasonBQGenderSelfConflict          = "bq_gender_self_conflict"
	EvidenceReasonBQBreedSelfConflict           = "bq_breed_self_conflict"
	EvidenceReasonLegacyChangedAfterHumanReview = "legacy_changed_after_human_review"
	EvidenceReasonStatusMismatch                = "status_mismatch"
	EvidenceReasonUnregisteredSource            = "unregistered_source"
)

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

type Source struct {
	SourceID            string     `json:"source_id"`
	SourceName          string     `json:"source_name"`
	Domain              string     `json:"domain"`
	SourceKind          string     `json:"source_kind"`
	SourceRef           *string    `json:"source_ref"`
	CadenceSeconds      int        `json:"cadence_seconds"`
	GreenWithinSeconds  int        `json:"green_within_seconds"`
	YellowWithinSeconds int        `json:"yellow_within_seconds"`
	Criticality         string     `json:"criticality"`
	Enabled             bool       `json:"enabled"`
	KnownDegraded       bool       `json:"known_degraded"`
	Notes               *string    `json:"notes"`
	FreshnessStatus     string     `json:"freshness_status"`
	StatusReason        string     `json:"status_reason"`
	SourceWatermarkAt   *time.Time `json:"source_watermark_at"`
	ObservedAt          *time.Time `json:"observed_at"`
	RowsSeen            int        `json:"rows_seen"`
	IsUnknownSource     bool       `json:"is_unknown_source"`
}

type SourceListResponse struct {
	Items   []Source `json:"items"`
	TraceID string   `json:"trace_id"`
}

type CounterStatus struct {
	FreshnessStatus string     `json:"freshness_status"`
	StatusReason    string     `json:"status_reason"`
	UpdatedAt       *time.Time `json:"updated_at"`
	RebuildRequired bool       `json:"rebuild_required"`
}

type OverallStatusResponse struct {
	OverallFreshness  string        `json:"overall_freshness"`
	CriticalFreshness string        `json:"critical_freshness"`
	CounterFreshness  CounterStatus `json:"counter_freshness"`
	Sources           []Source      `json:"sources"`
	TraceID           string        `json:"trace_id"`
}

type RunSummary struct {
	RowsRead           int `json:"rows_read"`
	RowsPlanned        int `json:"rows_planned"`
	RowsApplied        int `json:"rows_applied"`
	RowsSkipped        int `json:"rows_skipped"`
	GoatsCreated       int `json:"goats_created"`
	GoatsUpdated       int `json:"goats_updated"`
	ConflictsOpened    int `json:"conflicts_opened"`
	ConflictsRefreshed int `json:"conflicts_refreshed"`
}

type Run struct {
	SyncRunID          string     `json:"sync_run_id"`
	Mode               string     `json:"mode"`
	Domain             string     `json:"domain"`
	Status             string     `json:"status"`
	ColdStart          bool       `json:"cold_start"`
	SourceWindowStart  *time.Time `json:"source_window_start"`
	SourceWindowEnd    *time.Time `json:"source_window_end"`
	ETASeconds         *int       `json:"eta_seconds"`
	Summary            RunSummary `json:"summary"`
	CountersRebuilt    bool       `json:"counters_rebuilt"`
	CounterCheckStatus string     `json:"counter_check_status"`
	FreshnessStatus    string     `json:"freshness_status"`
	BlockedReason      *string    `json:"blocked_reason"`
	CancelRequestedAt  *time.Time `json:"cancel_requested_at"`
	StartedAt          time.Time  `json:"started_at"`
	CompletedAt        *time.Time `json:"completed_at"`
}

type RunStep struct {
	SyncStepID  string         `json:"sync_step_id"`
	SourceID    *string        `json:"source_id"`
	StepName    string         `json:"step_name"`
	Status      string         `json:"status"`
	RowsRead    int            `json:"rows_read"`
	RowsPlanned int            `json:"rows_planned"`
	RowsApplied int            `json:"rows_applied"`
	RowsSkipped int            `json:"rows_skipped"`
	Details     map[string]any `json:"details"`
	StartedAt   time.Time      `json:"started_at"`
	CompletedAt *time.Time     `json:"completed_at"`
}

type SourceCorrectionLogItem struct {
	SyncRunConflictID  string     `json:"sync_run_conflict_id"`
	SourceID           string     `json:"source_id"`
	SourceRecordID     string     `json:"source_record_id"`
	ConflictID         *string    `json:"conflict_id"`
	GoatID             *string    `json:"goat_id"`
	EvidenceReason     string     `json:"evidence_reason"`
	Result             string     `json:"result"`
	OldGoatOSValue     *string    `json:"old_goatos_value"`
	NewLegacyValue     *string    `json:"new_legacy_value"`
	PreviousDecisionID *string    `json:"previous_decision_id"`
	PreviousDecisionAt *time.Time `json:"previous_decision_at"`
	AuditID            *string    `json:"audit_id"`
	CreatedAt          time.Time  `json:"created_at"`
}

type RunDetailResponse struct {
	Run                 Run                       `json:"run"`
	Steps               []RunStep                 `json:"steps"`
	Sources             []Source                  `json:"sources"`
	SourceCorrectionLog []SourceCorrectionLogItem `json:"source_correction_log"`
	TraceID             string                    `json:"trace_id"`
}

type RunListResponse struct {
	Items   []Run  `json:"items"`
	TraceID string `json:"trace_id"`
}

type CreateRunRequest struct {
	Mode              string     `json:"mode"`
	Domain            string     `json:"domain"`
	SourceWindowStart *time.Time `json:"source_window_start"`
	SourceWindowEnd   *time.Time `json:"source_window_end"`
}

type CreateRunResponse struct {
	Run     Run    `json:"run"`
	TraceID string `json:"trace_id"`
}

type CancelRunResponse struct {
	Run     Run    `json:"run"`
	TraceID string `json:"trace_id"`
}
