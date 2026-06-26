// Package domain defines the generic Calendar read contract shared across event sources.
package domain

import (
	"encoding/json"
	"time"
)

const (
	SourceAPI        = "api"
	SliceVaccination = "vaccination"

	DefaultTimezone = "Asia/Kolkata"
)

const (
	OwnerAll          = "all"
	OwnerPHC          = "phc"
	OwnerInventory    = "inventory"
	OwnerAdminDataOps = "admin_data_ops"
)

const (
	StatusScheduled           = "scheduled"
	StatusDue                 = "due"
	StatusOverdue             = "overdue"
	StatusInProgress          = "in_progress"
	StatusProofPending        = "proof_pending"
	StatusVerificationPending = "verification_pending"
	StatusRejected            = "rejected"
	StatusReworkDue           = "rework_due"
	StatusDeferred            = "deferred"
	StatusBlocked             = "blocked"
	StatusCompleted           = "completed"
	StatusCanceled            = "canceled"
)

const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

const (
	EventVaccinationDoseDue              = "vaccination_dose_due"
	EventVaccinationDrive                = "vaccination_drive"
	EventVaccinationCampaign             = "vaccination_campaign"
	EventVaccinationBoosterDue           = "vaccination_booster_due"
	EventVaccinationDeferReview          = "vaccination_defer_review"
	EventVaccinationEvidenceReview       = "vaccination_evidence_review"
	EventVaccinationProofVerification    = "vaccination_proof_verification"
	EventVaccinationReworkDue            = "vaccination_rework_due"
	EventVaccineStockReadiness           = "vaccine_stock_readiness"
	EventVaccineColdChainCheck           = "vaccine_cold_chain_check"
	EventVaccineReorderExpiryGRN         = "vaccine_reorder_expiry_grn"
	EventPHCStockAntiMisuse              = "phc_stock_anti_misuse"
	EventVaccinationConfigSourceApproval = "vaccination_config_source_approval"
)

// CalendarEvent is the generic hot-list/month payload. It intentionally stays source-agnostic.
type CalendarEvent struct {
	EventID                    string          `json:"event_id"`
	EventType                  string          `json:"event_type"`
	OwnerKey                   string          `json:"owner_key"`
	Title                      string          `json:"title"`
	Subtitle                   string          `json:"subtitle"`
	Status                     string          `json:"status"`
	Severity                   string          `json:"severity"`
	DueAt                      time.Time       `json:"due_at"`
	WindowStart                *time.Time      `json:"window_start"`
	WindowEnd                  *time.Time      `json:"window_end"`
	Timezone                   string          `json:"timezone"`
	TimezoneSource             string          `json:"timezone_source"`
	ParkID                     *string         `json:"park_id"`
	ParkCode                   *string         `json:"park_code"`
	ShedID                     *string         `json:"shed_id"`
	ShedName                   *string         `json:"shed_name"`
	CohortID                   *string         `json:"cohort_id"`
	CohortName                 *string         `json:"cohort_name"`
	TargetType                 string          `json:"target_type"`
	TargetCount                int             `json:"target_count"`
	ProtocolID                 *string         `json:"protocol_id"`
	ProtocolVersionID          *string         `json:"protocol_version_id"`
	RuleID                     *string         `json:"rule_id"`
	VaccineName                *string         `json:"vaccine_name"`
	DoseCode                   *string         `json:"dose_code"`
	SourceBacked               bool            `json:"source_backed"`
	SourceLabel                string          `json:"source_label"`
	AssigneeLabel              *string         `json:"assignee_label"`
	ExecutorRole               *string         `json:"executor_role"`
	VerifierLabel              *string         `json:"verifier_label"`
	ReminderState              string          `json:"reminder_state"`
	PrimaryNotificationChannel string          `json:"primary_notification_channel"`
	EscalationState            string          `json:"escalation_state"`
	System                     bool            `json:"system"`
	CrossCutting               bool            `json:"cross_cutting"`
	Links                      json.RawMessage `json:"links"`
}

type CalendarEventListResponse struct {
	Source     string          `json:"source"`
	Items      []CalendarEvent `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

type CalendarEventDetail struct {
	Event                CalendarEvent         `json:"event"`
	Summary              json.RawMessage       `json:"summary"`
	SourceAndRule        json.RawMessage       `json:"source_and_rule"`
	Execution            json.RawMessage       `json:"execution"`
	Stock                json.RawMessage       `json:"stock"`
	Proof                json.RawMessage       `json:"proof"`
	Verification         json.RawMessage       `json:"verification"`
	NotificationChannels []string              `json:"notification_channels"`
	NotificationPolicy   json.RawMessage       `json:"notification_policy"`
	Links                json.RawMessage       `json:"links"`
	RecentActions        []CalendarHistoryItem `json:"recent_actions"`
}

type CalendarHistoryItem struct {
	HistoryID   string          `json:"history_id"`
	EventType   string          `json:"event_type"`
	Status      string          `json:"status"`
	Title       string          `json:"title"`
	ActorLabel  *string         `json:"actor_label"`
	OccurredAt  time.Time       `json:"occurred_at"`
	Channel     *string         `json:"channel"`
	Reason      *string         `json:"reason"`
	TraceID     *string         `json:"trace_id"`
	SourceTable string          `json:"source_table"`
	Details     json.RawMessage `json:"details"`
}

type CalendarHistoryResponse struct {
	Source     string                `json:"source"`
	EventID    string                `json:"event_id"`
	Items      []CalendarHistoryItem `json:"items"`
	NextCursor *string               `json:"next_cursor"`
}

type NudgeRequest struct {
	Channel string `json:"channel,omitempty"`
	Message string `json:"message,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type SnoozeRequest struct {
	SnoozeUntil     time.Time `json:"snooze_until"`
	Reason          string    `json:"reason"`
	ReplaceExisting bool      `json:"replace_existing"`
}

type CalendarActionResponse struct {
	ActionID         string     `json:"action_id"`
	EventID          string     `json:"event_id"`
	ActionType       string     `json:"action_type"`
	Status           string     `json:"status"`
	Channel          *string    `json:"channel"`
	SnoozeUntil      *time.Time `json:"snooze_until"`
	IdempotentReplay bool       `json:"idempotent_replay"`
}

type Query struct {
	TenantID string
	ParkID   *string
	ShedID   *string
	OwnerKey string
	Status   *string
	DateFrom time.Time
	DateTo   time.Time
	Cursor   *CalendarCursor
	Limit    int
}

type HistoryQuery struct {
	TenantID string
	EventID  string
	Cursor   *HistoryCursor
	Limit    int
}

type CalendarCursor struct {
	DueAt   time.Time `json:"due_at"`
	EventID string    `json:"event_id"`
}

type HistoryCursor struct {
	OccurredAt time.Time `json:"occurred_at"`
	HistoryID  string    `json:"history_id"`
}
