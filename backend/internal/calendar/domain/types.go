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
	OwnerPC           = "pc"
	OwnerInventory    = "inventory"
	OwnerAdminDataOps = "admin_data_ops"
)

const (
	StatusScheduled           = "scheduled"
	StatusDue                 = "due"
	StatusOverdue             = "overdue"
	StatusMissed              = "missed"
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
	EventVaccinationDoseDue                = "vaccination_dose_due"
	EventVaccinationDrive                  = "vaccination_drive"
	EventVaccinationHistory                = "vaccination_history"
	EventVaccinationCampaign               = "vaccination_campaign"
	EventVaccinationBoosterDue             = "vaccination_booster_due"
	EventVaccinationDeferReview            = "vaccination_defer_review"
	EventVaccinationEvidenceReview         = "vaccination_evidence_review"
	EventVaccinationProofVerification      = "vaccination_proof_verification"
	EventVaccinationReworkDue              = "vaccination_rework_due"
	EventVaccineStockReadiness             = "vaccine_stock_readiness"
	EventVaccineColdChainCheck             = "vaccine_cold_chain_check"
	EventVaccineReorderExpiryGRN           = "vaccine_reorder_expiry_grn"
	EventPCStockAntiMisuse                 = "pc_stock_anti_misuse"
	EventVaccinationConfigActivationReview = "vaccination_config_activation_review"
)

// DriveSummary holds park-level drive progress data for vaccination drive events.
type DriveSummary struct {
	ParkName       string             `json:"park_name"`
	DriveName      string             `json:"drive_name"`
	DriveTotal     int                `json:"drive_total"`
	DueDate        string             `json:"due_date"`
	ShedCount      int                `json:"shed_count"`
	ShedsCompleted int                `json:"sheds_completed"`
	Sheds          []DriveShedSummary `json:"sheds,omitempty"`
	VaccineLabels  []string           `json:"vaccine_labels"`
	TotalCount     int                `json:"total_count"`
	CompletedCount int                `json:"completed_count"`
	SubmittedCount int                `json:"submitted_count"`
	// RemainingCount is WORK STILL OWED BY THE OPERATOR = DueCount + OverdueCount + DeferredCount,
	// identical to TotalCount - CompletedCount - SubmittedCount over the five disjoint buckets. It
	// deliberately EXCLUDES submitted-but-unverified work, exactly like the ProgressCompleted
	// numerator below, so one payload can never carry two contradictory answers to "how much is
	// left": a fully submitted drive reports progress_pct 100 AND remaining_count 0. The
	// outstanding verifier review is carried by SubmittedCount and the verification_pending status.
	// (It was TotalCount - CompletedCount, which read "20 remaining" beside a 100% ring.)
	RemainingCount   int `json:"remaining_count"`
	DueCount         int `json:"due_count"`
	OverdueCount     int `json:"overdue_count"`
	DeferredCount    int `json:"deferred_count"`
	// RejectedCount is INFORMATIONAL ONLY -- a subset already counted inside DueCount/OverdueCount
	// above (status='rejected' is one of the statuses those buckets allow), never an additional
	// partition; it does not change the five-bucket total invariant. It exists so the card can
	// name WHY the completed/progress numerator dropped after a verifier rejects proof, instead of
	// the drop reading as an unexplained mystery.
	RejectedCount    int `json:"rejected_count"`
	TotalAnimals     int `json:"total_animals"`
	CompletedAnimals int `json:"completed_animals"`
	SubmittedAnimals int `json:"submitted_animals"`
	// Backend-owned, single cross-surface progress definition. Both Android and admin-web MUST
	// render these verbatim instead of deriving their own numerator (the cross-surface parity
	// defect: the same drive showed different completion numbers and ring percentages because
	// each client picked its own fields). ProgressBasis is "animals" or "doses" and names the
	// grain the numerator/denominator are counted on; ProgressCompleted is FIELD WORK DONE =
	// completed + submitted (maintainer decision 2026-08-03): an operator who vaccinated every
	// animal and submitted proof sees 100%, and the outstanding video review is carried by the
	// verification_pending status/chip and SubmittedCount, never by holding the ring below 100%.
	ProgressBasis     string `json:"progress_basis"`
	ProgressCompleted int    `json:"progress_completed"`
	ProgressTotal     int    `json:"progress_total"`
	ProgressPct       int    `json:"progress_pct"`
	OwnerLabel        string `json:"owner_label"`
}

type DriveShedSummary struct {
	ShedID       string `json:"shed_id"`
	ShedName     string `json:"shed_name"`
	TotalAnimals int    `json:"total_animals"`
}

// CalendarEvent is the generic hot-list/month payload. It intentionally stays source-agnostic.
type CalendarEvent struct {
	EventID                    string          `json:"event_id"`
	EventType                  string          `json:"event_type"`
	OwnerKey                   string          `json:"owner_key"`
	Title                      string          `json:"title"`
	Subtitle                   string          `json:"subtitle"`
	Aggregated                 bool            `json:"aggregated"`
	AllDay                     bool            `json:"all_day"`
	SummaryPrimary             string          `json:"summary_primary"`
	SummarySecondary           string          `json:"summary_secondary"`
	SummaryTertiary            string          `json:"summary_tertiary"`
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
	ShedCount                  int             `json:"shed_count"`
	VaccineCount               int             `json:"vaccine_count"`
	DriveCount                 int             `json:"drive_count"`
	CatchUpCount               int             `json:"catch_up_count"`
	ScheduledCount             int             `json:"scheduled_count"`
	DeferredCount              int             `json:"deferred_count"`
	ReviewCount                int             `json:"review_count"`
	ShedLabels                 []string        `json:"shed_labels"`
	VaccineLabels              []string        `json:"vaccine_labels"`
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
	DriveSummary               *DriveSummary   `json:"drive_summary,omitempty"`
}

type CalendarEventListResponse struct {
	Source        string                 `json:"source"`
	Presentation  CalendarPresentation   `json:"presentation"`
	Items         []CalendarEvent        `json:"items"`
	DateMarkers   []CalendarDateMarker   `json:"date_markers"`
	FilterOptions *CalendarFilterOptions `json:"filter_options,omitempty"`
	NextCursor    *string                `json:"next_cursor"`
	Projection    ProjectionMetadata     `json:"projection"`
	// HistoryProjection is always nil now (5k-50k envelope, migration 000189): the separate
	// completed-history projection is retired, and completed/history rows are served by the same
	// canonical predicate as everything else in Projection, so there is no separate freshness/
	// coverage metadata left to report. The field is kept (rather than removed) to avoid an API
	// contract break; a future consumer can treat non-nil as "reserved for future use."
	HistoryProjection *ProjectionMetadata `json:"history_projection,omitempty"`
	// ReminderRail (DRV-005) is a backend-computed, whole-filtered-week summary of ACTIVE
	// reminder/escalation events (same owner/park/shed/date scope as Items), computed by a single
	// bounded/indexed query over the canonical source_events reconstruction (calendarReminderRailSQL)
	// -- never derived by the frontend from whatever page of Items happens to be on screen. Populated
	// only when q.IncludeDateMarkers is true (the week/month view request shape); nil otherwise so a
	// plain paged list fetch does not pay for it.
	ReminderRail *CalendarReminderRail `json:"reminder_rail,omitempty"`
}

// CalendarFilterOption is a backend-authorized choice for the mobile monthly
// schedule. ParentValue links a shed to its park without asking the client to
// reconstruct the location hierarchy.
type CalendarFilterOption struct {
	Value       string  `json:"value"`
	Label       string  `json:"label"`
	ParentValue *string `json:"parent_value,omitempty"`
}

// CalendarFilterOptions contains only choices visible inside the caller's
// effective Calendar scope. It is returned on demand with the first page so
// continuation requests do not repeat option discovery work.
type CalendarFilterOptions struct {
	Parks    []CalendarFilterOption `json:"parks"`
	Sheds    []CalendarFilterOption `json:"sheds"`
	Vaccines []CalendarFilterOption `json:"vaccines"`
	Statuses []CalendarKeyLabel     `json:"statuses"`
	Months   []CalendarKeyLabel     `json:"months"`
	Years    []CalendarKeyLabel     `json:"years"`
}

// CalendarReminderRail is the whole-result reminder/escalation rail for the requested week window.
// Count is the total number of active reminder/escalation events in the filtered window (not just
// len(Items)); Items is a bounded (~20) preview ordered by due_at, so the rail never silently
// empties just because the matching event fell on request page 2+ of the main list.
type CalendarReminderRail struct {
	Count        int                        `json:"count"`
	EmptyMessage string                     `json:"empty_message"`
	Items        []CalendarReminderRailItem `json:"items"`
}

type CalendarReminderRailItem struct {
	EventID         string   `json:"event_id"`
	Title           string   `json:"title"`
	Subtitle        string   `json:"subtitle"`
	ReminderLabel   string   `json:"reminder_label"`
	EscalationLabel string   `json:"escalation_label"`
	Channels        []string `json:"channels"`
}

type ProjectionMetadata struct {
	ProjectionVersion int64     `json:"projection_version"`
	ProjectedAt       time.Time `json:"projected_at"`
	FreshnessStatus   string    `json:"freshness_status"`
	ServingState      string    `json:"serving_state"`
	Stale             bool      `json:"stale"`
	// PartialCoverage is true when the requested date window extends beyond what the projection's
	// own [date_from, date_to) build window covers. It never fails the read (history is served
	// last-known-good) but tells the caller the response may be missing rows outside the covered
	// window.
	PartialCoverage bool `json:"partial_coverage,omitempty"`
}

// CalendarDateMarker is the bounded month-grid summary. It keeps the calendar
// complete at herd scale without paging every goat-level event into the UI.
type CalendarDateMarker struct {
	Date           string `json:"date"`
	EventCount     int    `json:"event_count"`
	CompletedCount int    `json:"completed_count"`
	OpenCount      int    `json:"open_count"`
	DriveCount     int    `json:"drive_count"`
	DueCount       int    `json:"due_count"`
	OverdueCount   int    `json:"overdue_count"`
	DeferredCount  int    `json:"deferred_count"`
}

type CalendarPresentation struct {
	PageTitle              string                         `json:"page_title"`
	PageSubtitle           string                         `json:"page_subtitle"`
	ViewTabs               []CalendarPresentationTab      `json:"view_tabs"`
	OwnerTabs              []CalendarOwnerPresentationTab `json:"owner_tabs"`
	WorkstreamTabs         []CalendarPresentationTab      `json:"workstream_tabs"`
	Rhythm                 CalendarRhythmPresentation     `json:"rhythm"`
	Week                   CalendarViewPresentation       `json:"week"`
	Month                  CalendarViewPresentation       `json:"month"`
	NewEvent               CalendarActionPresentation     `json:"new_event"`
	EmptyState             CalendarEmptyStatePresentation `json:"empty_state"`
	EventTypes             []CalendarKeyLabel             `json:"event_types"`
	ActiveOwnerKey         string                         `json:"active_owner_key"`
	ActiveOwnerLabel       string                         `json:"active_owner_label"`
	ActiveOwnerScopeLabel  string                         `json:"active_owner_scope_label"`
	ActiveOwnerColor       string                         `json:"active_owner_color"`
	AllOwnersSelectedLabel string                         `json:"all_owners_selected_label"`
}

type CalendarPresentationTab struct {
	Key            string            `json:"key"`
	Label          string            `json:"label"`
	Active         bool              `json:"active"`
	Enabled        bool              `json:"enabled"`
	DisabledReason string            `json:"disabled_reason"`
	Query          map[string]string `json:"query"`
}

type CalendarOwnerPresentationTab struct {
	Key            string            `json:"key"`
	Label          string            `json:"label"`
	ScopeLabel     string            `json:"scope_label"`
	Color          string            `json:"color"`
	Active         bool              `json:"active"`
	Enabled        bool              `json:"enabled"`
	DisabledReason string            `json:"disabled_reason"`
	Query          map[string]string `json:"query"`
}

type CalendarRhythmPresentation struct {
	Title string              `json:"title"`
	Note  string              `json:"note"`
	Days  []CalendarRhythmDay `json:"days"`
}

type CalendarRhythmDay struct {
	Day     string            `json:"day"`
	Label   string            `json:"label"`
	Tone    string            `json:"tone"`
	Enabled bool              `json:"enabled"`
	Query   map[string]string `json:"query"`
}

type CalendarViewPresentation struct {
	Title                string `json:"title"`
	ScopeLabel           string `json:"scope_label"`
	ScopeOnlyMessage     string `json:"scope_only_message"`
	ClearScopeLabel      string `json:"clear_scope_label"`
	WholePeriodMessage   string `json:"whole_period_message"`
	AllDaysSelectedLabel string `json:"all_days_selected_label"`
	ClearDayLabel        string `json:"clear_day_label"`
	EmptyMessage         string `json:"empty_message"`
	ReminderTitle        string `json:"reminder_title"`
	ReminderEmptyMessage string `json:"reminder_empty_message"`
	ReminderNote         string `json:"reminder_note"`
	AsOfHint             string `json:"as_of_hint"`
	CellNote             string `json:"cell_note"`
}

type CalendarActionPresentation struct {
	Label          string `json:"label"`
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabled_reason"`
}

type CalendarEmptyStatePresentation struct {
	OkMessage      string `json:"ok_message"`
	ErrorMessage   string `json:"error_message"`
	PrimaryLabel   string `json:"primary_label"`
	SecondaryLabel string `json:"secondary_label"`
}

type CalendarKeyLabel struct {
	Key   string `json:"key"`
	Label string `json:"label"`
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
	// HistoryProjection mirrors CalendarEventListResponse.HistoryProjection: populated only when
	// this detail was served from the completed-history projection fallback (calendarHistoryProjectionDetailSQL).
	HistoryProjection *ProjectionMetadata `json:"history_projection,omitempty"`
}

type CalendarDriveTarget struct {
	ObligationID      string    `json:"obligation_id"`
	AnimalID          string    `json:"animal_id"`
	DisplayID         string    `json:"display_id"`
	AnimalIdentifier1 *string   `json:"animal_identifier_1"`
	AnimalIdentifier2 *string   `json:"animal_identifier_2"`
	ShedName          *string   `json:"shed_name,omitempty"`
	Stage             *string   `json:"stage"`
	LifecycleStatus   *string   `json:"lifecycle_status,omitempty"`
	HealthStatus      *string   `json:"health_status,omitempty"`
	ExitReason        *string   `json:"exit_reason,omitempty"`
	DeferReason       *string   `json:"defer_reason,omitempty"`
	Status            string    `json:"status"`
	ScheduledAt       time.Time `json:"scheduled_at"`
}

type CalendarDriveTargetListResponse struct {
	Source     string                `json:"source"`
	EventID    string                `json:"event_id"`
	Items      []CalendarDriveTarget `json:"items"`
	NextCursor *string               `json:"next_cursor"`
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

type EscalationActionRequest struct {
	Reason string `json:"reason,omitempty"`
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
	TenantID             string
	ParkID               *string
	ShedID               *string
	Vaccine              *string
	OwnerKey             string
	Status               *string
	DateFrom             time.Time
	DateTo               time.Time
	Cursor               *CalendarCursor
	Limit                int
	IncludeDateMarkers   bool
	MarkersOnly          bool
	IncludeFilterOptions bool
	IncludeDriveSummary  bool
	// IncludeReminderRail requests the whole-week reminder/escalation rail summary. It is a
	// SEPARATE trigger from IncludeDateMarkers: the week view needs the rail but not the month
	// date-markers, so gating the rail on IncludeDateMarkers left it null on the week list.
	IncludeReminderRail bool
	Scope               ScopeFilter
}

type EventQuery struct {
	TenantID string
	EventID  string
	Scope    ScopeFilter
}

type HistoryQuery struct {
	TenantID string
	EventID  string
	Cursor   *HistoryCursor
	Limit    int
	Scope    ScopeFilter
}

type ScopeFilter struct {
	TenantWide bool
	ParkIDs    []string
	ShedIDs    []string
}

type CalendarCursor struct {
	DueAt   time.Time `json:"due_at"`
	EventID string    `json:"event_id"`
}

type HistoryCursor struct {
	OccurredAt time.Time `json:"occurred_at"`
	HistoryID  string    `json:"history_id"`
}
