// Package domain defines the Health module's disease-course and treatment-work contracts.
package domain

import "time"

const (
	AgeBandAdult        = "adult"
	AgeBandKid          = "kid"
	SessionMorning      = "morning"
	SessionAfternoon    = "afternoon"
	SessionEvening      = "evening"
	SessionUnscheduled  = "unscheduled"
	DefaultDurationDays = 3
	MaxDurationDays     = 90
	MaxPageSize         = 20
)

// Clinical case-closure outcomes (POST /app/health/cases/{id}/close, permission health.diagnose).
// They map 1:1 onto the health_cases.status values 000098 has always allowed but nothing wrote:
//   - recovered: the course worked; the animal is well.
//   - referred:  handed to external/specialist care; the in-app course stops. The case stays
//     holdable by the death workflow (its existing SQL treats 'referred' as open), and may later
//     be closed 'recovered'.
//   - canceled:  the diagnosis was withdrawn or the course abandoned by clinical decision.
//
// 'continued' (extend the course with more sessions) is deliberately NOT a closure outcome — it
// needs session materialization and is a separate feature.
const (
	CaseOutcomeRecovered = "recovered"
	CaseOutcomeReferred  = "referred"
	CaseOutcomeCanceled  = "canceled"
)

// Verification identity for treatment-evidence review. The producer (CompleteWorkItem's enqueue),
// the verdict consumer's source filter, and the verificationcatalog category registration must all
// spell these identically, so they live here once.
const (
	VerificationVerticalHealth          = "health"
	VerificationModuleHealth            = "health"
	VerificationRefTypeTreatmentSession = "health_treatment_session"
	VerificationCategoryHealthAdults    = "health_adults"
	VerificationCategoryHealthKids      = "health_kids"
)

// VerificationCategoryForAgeBand routes a session's proof to the verifier page for its cohort.
func VerificationCategoryForAgeBand(ageBand string) string {
	if ageBand == AgeBandKid {
		return VerificationCategoryHealthKids
	}
	return VerificationCategoryHealthAdults
}

type ProtocolStep struct {
	StepID             string  `json:"step_id,omitempty"`
	DayNo              int     `json:"day_no"`
	Session            string  `json:"session"`
	Seq                int     `json:"seq"`
	RecordType         string  `json:"record_type"`
	MedicineName       *string `json:"medicine_name"`
	DosageText         *string `json:"dosage_text"`
	DosageDenominator  *string `json:"dosage_denominator"`
	MedicineRoute      *string `json:"medicine_route"`
	Instruction        *string `json:"instruction"`
	CriticalActionType *string `json:"critical_action_type"`
	Status             string  `json:"status,omitempty"`
}

type Protocol struct {
	ProtocolVersionID string         `json:"protocol_version_id"`
	DiseaseKey        string         `json:"disease_key"`
	DisplayName       string         `json:"display_name"`
	AgeBand           string         `json:"age_band"`
	Version           int            `json:"version"`
	DurationDays      int            `json:"duration_days"`
	Steps             []ProtocolStep `json:"steps"`
}

type OpenCaseInput struct {
	TenantID           string
	ActorID            string
	GoatID             string
	DiseaseKey         string
	AgeBand            string
	StartDate          time.Time
	IdempotencyKey     string
	RequestFingerprint string
	TraceID            string
}
type OpenCaseResult struct {
	CaseID           string `json:"case_id"`
	FirstSessionID   string `json:"first_session_id"`
	SessionCount     int    `json:"session_count"`
	DurationDays     int    `json:"duration_days"`
	IdempotentReplay bool   `json:"idempotent_replay"`
}

type WorkItem struct {
	SessionID                  string    `json:"health_session_id"`
	CaseID                     string    `json:"case_id"`
	GoatID                     string    `json:"goat_id"`
	GoatDisplayID              string    `json:"goat_display_id"`
	DiseaseKey                 string    `json:"disease_key"`
	DiseaseName                string    `json:"disease_name"`
	AgeBand                    string    `json:"age_band"`
	DayNo                      int       `json:"day_no"`
	DurationDays               int       `json:"duration_days"`
	BusinessDate               string    `json:"business_date"`
	Session                    string    `json:"session"`
	DueAt                      time.Time `json:"due_at"`
	Status                     string    `json:"status"`
	ParkID                     *string   `json:"park_id"`
	ParkLabel                  string    `json:"park_label"`
	ShedID                     *string   `json:"shed_id"`
	ShedLabel                  string    `json:"shed_label"`
	PartitionLabel             string    `json:"partition_label"`
	OperationalLocationDisplay string    `json:"operational_location_display"`
	StepCount                  int       `json:"step_count"`
	MedicationCount            int       `json:"medication_count"`
	HasCriticalStep            bool      `json:"has_critical_step"`
}
type Summary struct {
	Total         int `json:"total"`
	Due           int `json:"due"`
	Scheduled     int `json:"scheduled"`
	InProgress    int `json:"in_progress"`
	Completed     int `json:"completed"`
	Rework        int `json:"rework"`
	Held          int `json:"held"`
	CanceledDeath int `json:"canceled_death"`
}
type DateMarker struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}
type FilterOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}
type FilterOptions struct {
	Diseases []FilterOption `json:"diseases"`
	Parks    []FilterOption `json:"parks"`
	Sheds    []FilterOption `json:"sheds"`
}
type ListFilter struct {
	TenantID   string
	AgeBand    string
	Date       string
	Status     string
	DiseaseKey string
	ParkID     string
	ShedID     string
	Session    string
	Cursor     string
	Limit      int
	MarkerFrom string
	MarkerTo   string
}
type WorkItemPage struct {
	Items         []WorkItem    `json:"items"`
	Summary       Summary       `json:"summary"`
	DateMarkers   []DateMarker  `json:"date_markers"`
	FilterOptions FilterOptions `json:"filter_options"`
	NextCursor    *string       `json:"next_cursor"`
}
type WorkItemDetail struct {
	WorkItem
	Steps []ProtocolStep `json:"steps"`
	// RegisterRuleID is the DIAGNOSIS RULE this case was opened under, and it is carried here so
	// an operator standing on a treatment screen can record the animal's death against the exact
	// disease it was being treated for, rather than searching a list for what is already on the
	// page in front of them.
	//
	// It is on the DETAIL and deliberately not on WorkItem: the list renders a disease NAME and
	// has no use for a rule id, and widening the list payload for a field only the detail screen
	// reads would put a machine key into every row of a paged read.
	//
	// EMPTY for a PRE-ENGINE case, which is a real state and not an error -- those cases carry
	// only the treatment card they were opened against, and a card is many-to-one across diseases
	// (PPR, POX and UNDIFFERENTIATED all route to 'supportive'), so it cannot say which illness
	// was named. A client seeing an empty value offers the ordinary disease search instead of
	// pre-selecting; it must never fall back to DiseaseKey, which the death write refuses.
	RegisterRuleID string `json:"register_rule_id"`
	// Caller capabilities, computed by the HTTP layer from the caller's own grants (never a role
	// string): CanComplete mirrors health.execute, CanCloseCase mirrors health.diagnose. The
	// route permissions still enforce the write; these exist so a client never renders an action
	// the caller cannot perform (the 2026-08-29 audit found the Complete button 403-dead-lettering
	// for health managers, who deliberately do not hold health.execute).
	CanComplete  bool `json:"can_complete"`
	CanCloseCase bool `json:"can_close_case"`
}
type CompleteInput struct {
	TenantID           string
	ActorID            string
	SessionID          string
	ProofRef           string
	IdempotencyKey     string
	RequestFingerprint string
	TraceID            string
}
type CompleteResult struct {
	SessionID        string    `json:"health_session_id"`
	Status           string    `json:"status"`
	CompletedAt      time.Time `json:"completed_at"`
	MedicationCount  int       `json:"medication_count"`
	IdempotentReplay bool      `json:"idempotent_replay"`

	// Enqueue context for the treatment-evidence verification item, populated by the repository
	// from the completion transaction's own reads and consumed by the app service's enqueue seam.
	// Never serialized: the HTTP completion response contract is the five fields above.
	CaseID         string `json:"-"`
	GoatID         string `json:"-"`
	GoatDisplayID  string `json:"-"`
	DiseaseName    string `json:"-"`
	AgeBand        string `json:"-"`
	DayNo          int    `json:"-"`
	ParkID         string `json:"-"`
	ShedID         string `json:"-"`
	ShedLabel      string `json:"-"`
	PartitionLabel string `json:"-"`
}

// CloseCaseInput is one clinical closure decision (recovered / referred / canceled).
type CloseCaseInput struct {
	TenantID           string
	ActorID            string
	CaseID             string
	Outcome            string
	Note               string
	IdempotencyKey     string
	RequestFingerprint string
	TraceID            string
}
type CloseCaseResult struct {
	CaseID               string    `json:"case_id"`
	Status               string    `json:"status"`
	ClosedAt             time.Time `json:"closed_at"`
	CanceledSessionCount int       `json:"canceled_session_count"`
	IdempotentReplay     bool      `json:"idempotent_replay"`
}
type SourceProtocol struct {
	DiseaseKey             string         `json:"disease_key"`
	DisplayName            string         `json:"display_name"`
	AgeBand                string         `json:"age_band"`
	DurationDays           int            `json:"duration_days"`
	DefaultDurationApplied bool           `json:"default_duration_applied"`
	Steps                  []ProtocolStep `json:"steps"`
}
