// Package domain holds procurement/source-entry API and repository types.
package domain

import (
	"encoding/json"
	"time"
)

const (
	LoadStatusSourceWarmup       = "source_warmup"
	LoadStatusHealthPending      = "health_pending"
	LoadStatusPreDispatchPending = "pre_dispatch_pending"
	LoadStatusDispatchReady      = "dispatch_ready"
	LoadStatusInTransit          = "in_transit"
	LoadStatusArrivalReview      = "arrival_review"
	LoadStatusAcceptedIntake     = "accepted_intake"
	LoadStatusRejected           = "rejected"
	LoadStatusDeferred           = "deferred"
	LoadStatusBlocked            = "blocked"

	GoatStateSourceCandidate      = "source_candidate"
	GoatStateSourceWarmup         = "source_warmup"
	GoatStateSourceHealthPending  = "source_health_pending"
	GoatStateSourceHealthPassed   = "source_health_passed"
	GoatStateSourceHealthFailed   = "source_health_failed"
	GoatStatePreDispatchPending   = "pre_dispatch_pending"
	GoatStatePreDispatchAccepted  = "pre_dispatch_accepted"
	GoatStatePreDispatchRejected  = "pre_dispatch_rejected"
	GoatStatePreDispatchDeferred  = "pre_dispatch_deferred"
	GoatStatePreDispatchBlocked   = "pre_dispatch_blocked"
	GoatStateLoaded               = "loaded"
	GoatStateInTransit            = "in_transit"
	GoatStateArrivalReviewPending = "arrival_review_pending"
	GoatStateArrivalAccepted      = "arrival_accepted"
	GoatStateArrivalRejected      = "arrival_rejected"
	GoatStateAcceptedHerdIntake   = "accepted_herd_intake"
	GoatStateDead                 = "dead"
	GoatStateSold                 = "sold"
	GoatStateLost                 = "lost"

	DecisionAccepted = "accepted"
	DecisionRejected = "rejected"
	DecisionDeferred = "deferred"
	DecisionBlocked  = "blocked"

	HealthPassed   = "passed"
	HealthFailed   = "failed"
	HealthDeferred = "deferred"

	PurposeBreeding    = "breeding"
	PurposeFattening   = "fattening"
	PurposeNonBreeding = "non_breeding"
	PurposeUnspecified = "unspecified"

	HFVaccinationReviewImported    = "imported"
	HFVaccinationReviewTrusted     = "trusted"
	HFVaccinationReviewRejected    = "rejected"
	HFVaccinationReviewConflicting = "conflicting"
	HFVaccinationReviewDuplicate   = "duplicate"
)

type LoadQuery struct {
	TenantID string
	Status   string
	Limit    int
	Cursor   *LoadCursor
}

type LoadCursor struct {
	UpdatedAt time.Time
	LoadID    string
}

type LoadListResult struct {
	Items      []Load  `json:"items"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

type Load struct {
	LoadID             string          `json:"load_id"`
	TenantID           string          `json:"tenant_id"`
	SourcePartyID      string          `json:"source_party_id"`
	SourcePartyName    string          `json:"source_party_name,omitempty"`
	SourceLocationID   *string         `json:"source_location_id,omitempty"`
	SourceLocationCode *string         `json:"source_location_code,omitempty"`
	SourceLocationName *string         `json:"source_location_name,omitempty"`
	ExpectedCount      int             `json:"expected_count"`
	PurchaseDate       *time.Time      `json:"purchase_date,omitempty"`
	PlannedDispatch    *time.Time      `json:"planned_dispatch_at,omitempty"`
	Status             string          `json:"status"`
	Notes              string          `json:"notes"`
	Context            json.RawMessage `json:"context"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	RowVersion         int             `json:"row_version"`
}

type LoadGoat struct {
	LoadGoatID        string          `json:"load_goat_id"`
	TenantID          string          `json:"tenant_id"`
	LoadID            string          `json:"load_id"`
	GoatID            string          `json:"goat_id"`
	AnimalIdentifier1 *string         `json:"animal_identifier_1,omitempty"`
	AnimalIdentifier2 *string         `json:"animal_identifier_2,omitempty"`
	SelectionState    string          `json:"selection_state"`
	SelectionReason   string          `json:"selection_reason"`
	Purpose           string          `json:"purpose"`
	CurrentState      string          `json:"current_state"`
	SourceEntryState  string          `json:"source_entry_state"`
	SourceEntryRef    *string         `json:"source_entry_ref,omitempty"`
	OwnershipState    string          `json:"ownership_state"`
	HealthState       string          `json:"health_state"`
	WarmupStartedAt   *time.Time      `json:"warmup_started_at,omitempty"`
	WarmupEndedAt     *time.Time      `json:"warmup_ended_at,omitempty"`
	WarmupDays        *int            `json:"warmup_days,omitempty"`
	HoldingLocationID *string         `json:"holding_location_id,omitempty"`
	LoadedAt          *time.Time      `json:"loaded_at,omitempty"`
	ArrivedAt         *time.Time      `json:"arrived_at,omitempty"`
	IntakeAcceptedAt  *time.Time      `json:"intake_accepted_at,omitempty"`
	ExitReason        *string         `json:"exit_reason,omitempty"`
	ProofRefs         json.RawMessage `json:"proof_refs"`
	Metadata          json.RawMessage `json:"metadata"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	RowVersion        int             `json:"row_version"`
}

type HoldingStay struct {
	StayID            string          `json:"stay_id"`
	TenantID          string          `json:"tenant_id"`
	GoatID            string          `json:"goat_id"`
	LoadID            string          `json:"load_id"`
	HoldingLocationID string          `json:"holding_location_id"`
	StartedAt         time.Time       `json:"started_at"`
	EndedAt           *time.Time      `json:"ended_at,omitempty"`
	WarmupState       string          `json:"warmup_state"`
	WarmupDays        *int            `json:"warmup_days,omitempty"`
	Purpose           string          `json:"purpose"`
	HealthState       string          `json:"health_state"`
	OwnershipState    string          `json:"ownership_state"`
	Status            string          `json:"status"`
	ProofRefs         json.RawMessage `json:"proof_refs"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	RowVersion        int             `json:"row_version"`
}

type SourceHealthCheck struct {
	HealthCheckID string    `json:"health_check_id"`
	TenantID      string    `json:"tenant_id"`
	GoatID        string    `json:"goat_id"`
	LoadID        string    `json:"load_id"`
	HealthState   string    `json:"health_state"`
	Reason        string    `json:"reason"`
	CheckedBy     *string   `json:"checked_by,omitempty"`
	CheckedAt     time.Time `json:"checked_at"`
	ProofRefID    *string   `json:"proof_ref_id,omitempty"`
	SOPTaskID     *string   `json:"sop_task_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	// Replayed is a transient response flag (never persisted/scanned/serialized): true when this result was
	// returned by an idempotent replay rather than a fresh write, so the service can skip replay-unsafe side
	// effects (e.g. re-cancelling vaccination obligations).
	Replayed bool `json:"-"`
}

type Decision struct {
	DecisionID      string          `json:"decision_id"`
	TenantID        string          `json:"tenant_id"`
	GoatID          string          `json:"goat_id"`
	LoadID          string          `json:"load_id"`
	DecisionStage   string          `json:"decision_stage"`
	DecisionType    string          `json:"decision_type"`
	Reason          string          `json:"reason"`
	DecidedBy       *string         `json:"decided_by,omitempty"`
	DecidedAt       time.Time       `json:"decided_at"`
	ProofRefID      *string         `json:"proof_ref_id,omitempty"`
	SOPTaskID       *string         `json:"sop_task_id,omitempty"`
	OwnerID         *string         `json:"owner_id,omitempty"`
	ResumeCondition *string         `json:"resume_condition,omitempty"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedAt       time.Time       `json:"created_at"`
	// Replayed is a transient response flag (never persisted/scanned/serialized) — see SourceHealthCheck.
	Replayed bool `json:"-"`
}

type TransitHandoff struct {
	HandoffID         string     `json:"handoff_id"`
	TenantID          string     `json:"tenant_id"`
	LoadID            string     `json:"load_id"`
	FromLocationID    *string    `json:"from_location_id,omitempty"`
	FromLocationLabel string     `json:"from_location_label,omitempty"`
	ToLocationID      string     `json:"to_location_id"`
	ToLocationLabel   string     `json:"to_location_label,omitempty"`
	LoadedCount       int        `json:"loaded_count"`
	DispatchedAt      time.Time  `json:"dispatched_at"`
	ArrivedAt         *time.Time `json:"arrived_at,omitempty"`
	ProofRefID        *string    `json:"proof_ref_id,omitempty"`
	DiscrepancyState  string     `json:"discrepancy_state"`
	Status            string     `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	RowVersion        int        `json:"row_version"`
}

type ArrivalReview struct {
	ReviewID          string          `json:"review_id"`
	TenantID          string          `json:"tenant_id"`
	LoadID            string          `json:"load_id"`
	ParkLocationID    string          `json:"park_location_id"`
	ParkLocationLabel string          `json:"park_location_label,omitempty"`
	ExpectedCount     int             `json:"expected_count"`
	LoadedCount       int             `json:"loaded_count"`
	ArrivedCount      int             `json:"arrived_count"`
	MatchedCount      int             `json:"matched_count"`
	MissingCount      int             `json:"missing_count"`
	ExtraCount        int             `json:"extra_count"`
	RejectedCount     int             `json:"rejected_count"`
	HealthFlags       json.RawMessage `json:"health_flags"`
	WeightFlags       json.RawMessage `json:"weight_flags"`
	MediaProofID      *string         `json:"media_proof_id,omitempty"`
	Status            string          `json:"status"`
	ReviewedBy        *string         `json:"reviewed_by,omitempty"`
	ReviewedAt        time.Time       `json:"reviewed_at"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	RowVersion        int             `json:"row_version"`
	Goats             []ArrivalGoat   `json:"goats,omitempty"`
	// Replayed is a transient response flag (never persisted/scanned/serialized) — see SourceHealthCheck.
	Replayed bool `json:"-"`
}

type ArrivalGoat struct {
	ReviewGoatID      string    `json:"review_goat_id"`
	TenantID          string    `json:"tenant_id"`
	ReviewID          string    `json:"review_id"`
	LoadID            string    `json:"load_id"`
	GoatID            *string   `json:"goat_id,omitempty"`
	AnimalIdentifier1 *string   `json:"animal_identifier_1,omitempty"`
	AnimalIdentifier2 *string   `json:"animal_identifier_2,omitempty"`
	ArrivalState      string    `json:"arrival_state"`
	HealthFlag        *string   `json:"health_flag,omitempty"`
	WeightFlag        *string   `json:"weight_flag,omitempty"`
	ProofRefID        *string   `json:"proof_ref_id,omitempty"`
	Notes             string    `json:"notes"`
	CreatedAt         time.Time `json:"created_at"`
}

type PCHandoff struct {
	HandoffID                 string          `json:"handoff_id"`
	TenantID                  string          `json:"tenant_id"`
	LoadID                    string          `json:"load_id"`
	GoatID                    string          `json:"goat_id"`
	AcceptedAt                time.Time       `json:"accepted_at"`
	ParkLocationID            string          `json:"park_location_id"`
	ParkLocationLabel         string          `json:"park_location_label,omitempty"`
	ShedLocationID            string          `json:"shed_location_id"`
	ShedLocationLabel         string          `json:"shed_location_label,omitempty"`
	EntryDate                 time.Time       `json:"entry_date"`
	TrustedVaccinationHistory json.RawMessage `json:"trusted_vaccination_history"`
	IntakeHealthSignal        *string         `json:"intake_health_signal,omitempty"`
	EventStatus               string          `json:"event_status"`
	CreatedAt                 time.Time       `json:"created_at"`
	UpdatedAt                 time.Time       `json:"updated_at"`
}

type HFVaccinationEvidence struct {
	EvidenceID        string          `json:"evidence_id"`
	TenantID          string          `json:"tenant_id"`
	LoadID            string          `json:"load_id"`
	GoatID            string          `json:"goat_id"`
	ProtocolVersionID string          `json:"protocol_version_id"`
	RuleID            string          `json:"rule_id"`
	DoseCode          string          `json:"dose_code"`
	AdministeredAt    time.Time       `json:"administered_at"`
	VaccineName       string          `json:"vaccine_name"`
	LotNumber         string          `json:"lot_number"`
	ProofRefID        *string         `json:"proof_ref_id,omitempty"`
	SourceRef         string          `json:"source_ref"`
	ReviewStatus      string          `json:"review_status"`
	ReviewedBy        *string         `json:"reviewed_by,omitempty"`
	ReviewedAt        *time.Time      `json:"reviewed_at,omitempty"`
	ReviewReason      string          `json:"review_reason"`
	ImportedBy        *string         `json:"imported_by,omitempty"`
	ImportedAt        time.Time       `json:"imported_at"`
	Metadata          json.RawMessage `json:"metadata"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	RowVersion        int             `json:"row_version"`
}

type LoadDetail struct {
	Load           Load                    `json:"load"`
	Goats          []LoadGoat              `json:"goats"`
	HoldingStays   []HoldingStay           `json:"holding_stays"`
	HFVaccinations []HFVaccinationEvidence `json:"hf_vaccination_evidence"`
	HealthChecks   []SourceHealthCheck     `json:"source_health_checks"`
	Decisions      []Decision              `json:"decisions"`
	Transit        []TransitHandoff        `json:"transit_handoffs"`
	ArrivalReviews []ArrivalReview         `json:"arrival_reviews"`
	PCHandoffs     []PCHandoff             `json:"pc_handoffs"`
	Timeline       []TimelineEvent         `json:"timeline"`
}

type TimelineEvent struct {
	EventType  string    `json:"event_type"`
	OccurredAt time.Time `json:"occurred_at"`
	GoatID     *string   `json:"goat_id,omitempty"`
	RefID      string    `json:"ref_id"`
	State      string    `json:"state"`
	Summary    string    `json:"summary"`
}

const SourceAPI = "api"

type WorkQuery struct {
	TenantID      string
	WorkState     *string
	Severity      *string
	Limit         int
	Cursor        *WorkCursor
	RowID         *string
	ExceptionOnly bool
}

type WorkCursor struct {
	UpdatedAt time.Time
	RowID     string
}

type WorkRow struct {
	TenantID       string     `json:"-"`
	RowID          string     `json:"row_id"`
	LoadID         string     `json:"load_id"`
	LoadStatus     string     `json:"load_status"`
	LoadGoatID     *string    `json:"load_goat_id,omitempty"`
	GoatID         *string    `json:"goat_id,omitempty"`
	SourcePartyID  string     `json:"source_party_id"`
	WorkType       string     `json:"work_type"`
	WorkState      string     `json:"work_state"`
	Severity       string     `json:"severity"`
	Title          string     `json:"title"`
	Detail         string     `json:"detail"`
	NextAction     string     `json:"next_action"`
	BlockerReason  *string    `json:"blocker_reason,omitempty"`
	OwnerID        *string    `json:"owner_id,omitempty"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	WarmupDays     *int       `json:"warmup_days,omitempty"`
	ExpectedCount  int        `json:"expected_count"`
	CompletedCount int        `json:"completed_count"`
}

type CountByWorkState struct {
	WorkState string `json:"work_state"`
	Count     int64  `json:"count"`
}

type WorkListResult struct {
	Rows              []WorkRow          `json:"rows"`
	CountsByWorkState []CountByWorkState `json:"counts_by_work_state"`
	NextCursor        *string            `json:"next_cursor,omitempty"`
}

type ActionCenterResponse struct {
	Source            string             `json:"source"`
	Items             []WorkRow          `json:"items"`
	CountsByWorkState []CountByWorkState `json:"counts_by_work_state"`
	NextCursor        *string            `json:"next_cursor,omitempty"`
}

type AdherenceSummary struct {
	ExpectedCount      int     `json:"expected_count"`
	CompletedCount     int     `json:"completed_count"`
	OpenGapCount       int     `json:"open_gap_count"`
	BlockedCount       int     `json:"blocked_count"`
	DeferredCount      int     `json:"deferred_count"`
	ProcessIntactCount int     `json:"process_intact_count"`
	AdherencePercent   float64 `json:"adherence_percent"`
}

type AdherenceRow struct {
	RowID      string  `json:"row_id"`
	LoadID     string  `json:"load_id"`
	GoatID     *string `json:"goat_id,omitempty"`
	Expected   string  `json:"expected"`
	Actual     string  `json:"actual"`
	Gap        string  `json:"gap"`
	Severity   string  `json:"severity"`
	WorkState  string  `json:"work_state"`
	NextAction string  `json:"next_action"`
}

type ProtocolAdherenceResponse struct {
	Source     string           `json:"source"`
	Summary    AdherenceSummary `json:"summary"`
	Rows       []AdherenceRow   `json:"rows"`
	NextCursor *string          `json:"next_cursor,omitempty"`
}

type ControlTowerSummary struct {
	ProcessIntact           bool `json:"process_intact"`
	CriticalCount           int  `json:"critical_count"`
	WarningCount            int  `json:"warning_count"`
	OpenGapCount            int  `json:"open_gap_count"`
	MissingProofCount       int  `json:"missing_proof_count"`
	ArrivalMismatchCount    int  `json:"arrival_mismatch_count"`
	SourceEntryBlockedCount int  `json:"source_entry_blocked_count"`
}

type ControlTowerAlert struct {
	RowID        string  `json:"row_id"`
	LoadID       string  `json:"load_id"`
	GoatID       *string `json:"goat_id,omitempty"`
	Severity     string  `json:"severity"`
	WorkState    string  `json:"work_state"`
	Title        string  `json:"title"`
	Detail       string  `json:"detail"`
	NextAction   string  `json:"next_action"`
	EvidenceLink string  `json:"evidence_link"`
}

type ControlTowerResponse struct {
	Source  string              `json:"source"`
	Summary ControlTowerSummary `json:"summary"`
	Alerts  []ControlTowerAlert `json:"alerts"`
}

type WorkflowNode struct {
	Key       string     `json:"key"`
	Label     string     `json:"label"`
	State     string     `json:"state"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
	RefID     *string    `json:"ref_id,omitempty"`
}

type WorkflowDrilldownResponse struct {
	Source string         `json:"source"`
	Row    WorkRow        `json:"row"`
	Nodes  []WorkflowNode `json:"nodes"`
}
