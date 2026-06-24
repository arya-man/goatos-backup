package domain

import "time"

const (
	ResolutionSingleMatch    = "single_match"
	ResolutionMultipleMatch  = "multiple_matches"
	ResolutionNoMatch        = "no_match"
	ResolutionNeedsReview    = "needs_review"
	ResolutionMergedRedirect = "merged_redirect"
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

type Warning struct {
	Code           string  `json:"code"`
	Message        string  `json:"message"`
	OriginalGoatID *string `json:"original_goat_id"`
	RedirectGoatID *string `json:"redirect_goat_id"`
}

type LocationPath struct {
	Display  string  `json:"display"`
	FarmID   *string `json:"farm_id"`
	ParkID   *string `json:"park_id"`
	ShedID   *string `json:"shed_id"`
	CohortID *string `json:"cohort_id"`
}

type EvidenceRef struct {
	EvidenceType string  `json:"evidence_type"`
	EvidenceID   string  `json:"evidence_id"`
	SourceSystem *string `json:"source_system"`
	Description  *string `json:"description"`
}

type LocationScope struct {
	FarmID   *string `json:"farm_id"`
	ParkID   *string `json:"park_id"`
	ShedID   *string `json:"shed_id"`
	CohortID *string `json:"cohort_id"`
}

type GoatSummary struct {
	GoatID             string       `json:"goat_id"`
	DisplayID          string       `json:"display_id"`
	PrimaryOldTag      *string      `json:"primary_old_tag"`
	RFID               *string      `json:"rfid"`
	Breed              *string      `json:"breed"`
	Sex                *string      `json:"sex"`
	AgeBand            *string      `json:"age_band"`
	LifecycleStatus    string       `json:"lifecycle_status"`
	ReproductiveStatus *string      `json:"reproductive_status"`
	GrowthCohortTag    *string      `json:"growth_cohort_tag"`
	ManagementStage    *string      `json:"management_stage"`
	HealthStatus       *string      `json:"health_status"`
	IdentityState      string       `json:"identity_state"`
	LocationPath       LocationPath `json:"location_path"`
	Warnings           []Warning    `json:"warnings"`
}

type GoatIdentifier struct {
	IdentifierID     string     `json:"identifier_id"`
	IdentifierType   string     `json:"identifier_type"`
	IdentifierValue  string     `json:"identifier_value"`
	ScopeKey         string     `json:"scope_key"`
	Status           string     `json:"status"`
	IsPrimaryForGoat bool       `json:"is_primary_for_goat"`
	ValidFrom        time.Time  `json:"valid_from"`
	ValidTo          *time.Time `json:"valid_to"`
	SourceSystem     *string    `json:"source_system"`
	SourceRecordID   *string    `json:"source_record_id"`
	Confidence       *float64   `json:"confidence"`
}

type GoatPassport struct {
	GoatID           string           `json:"goat_id"`
	DisplayID        string           `json:"display_id"`
	Species          string           `json:"species"`
	IdentityState    string           `json:"identity_state"`
	Summary          GoatSummary      `json:"summary"`
	Identifiers      []GoatIdentifier `json:"identifiers"`
	EvidenceRefs     []EvidenceRef    `json:"evidence_refs"`
	FamilyRefs       []FamilyRef      `json:"family_refs,omitempty"`
	MergedIntoGoatID *string          `json:"merged_into_goat_id"`
	RowVersion       int              `json:"row_version"`
}

type FamilyRef struct {
	Relation string `json:"relation"`
	GoatID   string `json:"goat_id"`
}

type GoatPassportResult struct {
	Goat     GoatPassport `json:"goat"`
	Warnings []Warning    `json:"warnings"`
	TraceID  string       `json:"trace_id"`
}

type GoatSearchResult struct {
	Items      []GoatSummary `json:"items"`
	NextCursor *string       `json:"next_cursor"`
	TraceID    string        `json:"trace_id"`
}

type ResolveIdentifierResult struct {
	ResolutionState string        `json:"resolution_state"`
	GoatSummary     *GoatSummary  `json:"goat_summary"`
	CandidateGoats  []GoatSummary `json:"candidate_goats"`
	Warnings        []Warning     `json:"warnings"`
	ConflictID      *string       `json:"conflict_id"`
	RedirectGoatID  *string       `json:"redirect_goat_id"`
	TraceID         string        `json:"trace_id"`
}

type IdentifierMatch struct {
	Identifier GoatIdentifier
	Goat       GoatSummary
}

type DecisionRecordSummary struct {
	DecisionID     string    `json:"decision_id"`
	DecisionType   string    `json:"decision_type"`
	DecisionResult string    `json:"decision_result"`
	DecisionState  string    `json:"decision_state"`
	PolicyVersion  string    `json:"policy_version"`
	CreatedAt      time.Time `json:"created_at"`
}

type EventSummary struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
}

type IdempotencyMeta struct {
	IdempotencyKey string  `json:"idempotency_key"`
	Replayed       bool    `json:"replayed"`
	FirstResultID  *string `json:"first_result_id"`
}

type GoatTimelineEvent struct {
	EventID      string        `json:"event_id"`
	EventType    string        `json:"event_type"`
	OccurredAt   time.Time     `json:"occurred_at"`
	RecordedAt   time.Time     `json:"recorded_at"`
	ActorType    string        `json:"actor_type"`
	EvidenceRefs []EvidenceRef `json:"evidence_refs"`
	DecisionID   *string       `json:"decision_id"`
}

type GoatTimelineResponse struct {
	Items      []GoatTimelineEvent `json:"items"`
	NextCursor *string             `json:"next_cursor"`
	TraceID    string              `json:"trace_id"`
}

type AdminGoatResponse struct {
	Goat        GoatSummary           `json:"goat"`
	Identifiers []GoatIdentifier      `json:"identifiers"`
	Decision    DecisionRecordSummary `json:"decision"`
	Events      []EventSummary        `json:"events"`
	Idempotency IdempotencyMeta       `json:"idempotency"`
	TraceID     string                `json:"trace_id"`
}
