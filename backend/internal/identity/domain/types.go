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
	Display    string  `json:"display"`
	FarmID     *string `json:"farm_id"`
	FarmCode   *string `json:"farm_code,omitempty"`
	FarmName   *string `json:"farm_name,omitempty"`
	ParkID     *string `json:"park_id"`
	ParkCode   *string `json:"park_code,omitempty"`
	ParkName   *string `json:"park_name,omitempty"`
	ShedID     *string `json:"shed_id"`
	ShedCode   *string `json:"shed_code,omitempty"`
	ShedName   *string `json:"shed_name,omitempty"`
	CohortID   *string `json:"cohort_id"`
	CohortCode *string `json:"cohort_code,omitempty"`
	CohortName *string `json:"cohort_name,omitempty"`
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
	AnimalIdentifier1  *string      `json:"animal_identifier_1"`
	AnimalIdentifier2  *string      `json:"animal_identifier_2"`
	Breed              *string      `json:"breed"`
	Sex                *string      `json:"sex"`
	AgeBand            *string      `json:"age_band"`
	LifecycleStatus    string       `json:"lifecycle_status"`
	ReproductiveStatus *string      `json:"reproductive_status"`
	GrowthCohortTag    *string      `json:"growth_cohort_tag"`
	ManagementStage    *string      `json:"management_stage"`
	HealthStatus       *string      `json:"health_status"`
	LocationPath       LocationPath `json:"location_path"`
	WeightKg           *float64     `json:"weight_kg,omitempty"`
	// RowVersion is the animal's current optimistic-concurrency token. It travels on the summary so
	// an operator flow that mutates the goat from a scan/search (recording a death) can send it back
	// verbatim -- the token is resolved server-side, never typed by the operator.
	RowVersion       int32     `json:"row_version"`
	Warnings         []Warning `json:"warnings"`
	MergedIntoGoatID *string   `json:"-"`
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

// TemporaryTaggedGoat is one row of the operator "Awaiting RFID" list: a goat that still carries an
// active temporary tag and is waiting to be promoted to a permanent RFID. It is a deliberately
// narrow projection (not a full GoatSummary): the promote screen needs only enough to identify the
// animal, show the temp tag being replaced, and send an optimistic-concurrency-safe promote.
type TemporaryTaggedGoat struct {
	GoatID string `json:"goat_id"`
	// DisplayID doubles as the keyset cursor: the list is ordered by display_id.
	DisplayID string `json:"display_id"`
	// TemporaryIdentifier is the active temporary tag value being retired on promotion.
	TemporaryIdentifier string `json:"temporary_identifier"`
	// LocationDisplay is the animal's own shed (then park) name, matching the search read's shape.
	LocationDisplay string `json:"location_display"`
	// RowVersion is echoed back verbatim in the promote call so a stale in-hand row is rejected.
	RowVersion int32 `json:"row_version"`
}

type TemporaryTaggedGoatsResult struct {
	Items      []TemporaryTaggedGoat `json:"items"`
	NextCursor *string               `json:"next_cursor"`
	TraceID    string                `json:"trace_id"`
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

type AdminGoatCreateRequest struct {
	AnimalIdentifier1 *string `json:"animal_identifier_1,omitempty"`
	AnimalIdentifier2 *string `json:"animal_identifier_2,omitempty"`
	// TemporaryIdentifier is a provisional tag for a newborn created before its permanent RFID is
	// available. Exactly one of animal_identifier_1 or temporary_identifier must be present; a
	// temp-only goat carries no active animal_identifier_1 and is promoted later.
	TemporaryIdentifier *string `json:"temporary_identifier,omitempty"`
	Species             string  `json:"species"`
	FarmID              *string `json:"farm_id,omitempty"`
	FarmCode            *string `json:"farm_code,omitempty"`
	ParkID              *string `json:"park_id,omitempty"`
	ParkCode            *string `json:"park_code,omitempty"`
	ShedID              *string `json:"shed_id,omitempty"`
	ShedCode            *string `json:"shed_code,omitempty"`
	Breed               *string `json:"breed,omitempty"`
	Sex                 string  `json:"sex"`
	DOB                 *string `json:"dob,omitempty"`
	DOBEstimated        *bool   `json:"dob_estimated,omitempty"`
	OriginType          string  `json:"origin_type"`
	EntryDate           string  `json:"entry_date"`
	ManagementStage     *string `json:"management_stage,omitempty"`
	HealthStatus        *string `json:"health_status,omitempty"`
	// ReproductiveStatus is optional. When present on a row whose identifiers
	// match an existing goat, the bulk import applies it via the event-emitting
	// ReproductiveGoat transition instead of treating the row as a create conflict.
	ReproductiveStatus *string       `json:"reproductive_status,omitempty"`
	WeightKg           *float64      `json:"weight_kg,omitempty"`
	DamID              *string       `json:"dam_id,omitempty"`
	SireOrLot          *string       `json:"sire_or_lot,omitempty"`
	PhotoURL           *string       `json:"photo_url,omitempty"`
	SourceRecordID     *string       `json:"source_record_id,omitempty"`
	EvidenceRefs       []EvidenceRef `json:"evidence_refs"`
	VaccinationHistory []EvidenceRef `json:"vaccination_history,omitempty"`
}

type AdminGoatBulkPreviewRequest struct {
	CSV      string `json:"csv"`
	FileHash string `json:"file_hash,omitempty"`
}

type AdminGoatBulkCommitRequest struct {
	Rows         []AdminGoatBulkCommitRow `json:"rows"`
	FileHash     string                   `json:"file_hash,omitempty"`
	PreviewToken string                   `json:"preview_token,omitempty"`
}

type AdminGoatBulkCommitRow struct {
	RowNumber  int                     `json:"row_number,omitempty"`
	Normalized *AdminGoatCreateRequest `json:"normalized,omitempty"`
	AdminGoatCreateRequest
}

type MoveGoatRequest struct {
	ParkID       string        `json:"park_id"`
	ShedID       string        `json:"shed_id"`
	Reason       string        `json:"reason"`
	OccurredAt   *time.Time    `json:"occurred_at,omitempty"`
	EvidenceRefs []EvidenceRef `json:"evidence_refs"`
	RowVersion   int           `json:"row_version"`
}

type ExitGoatRequest struct {
	LifecycleStatus string        `json:"lifecycle_status"`
	ExitReason      string        `json:"exit_reason"`
	Reason          string        `json:"reason"`
	OccurredAt      *time.Time    `json:"occurred_at,omitempty"`
	EvidenceRefs    []EvidenceRef `json:"evidence_refs"`
	RowVersion      int           `json:"row_version"`
}

type StageGoatRequest struct {
	ManagementStage string        `json:"management_stage"`
	Reason          string        `json:"reason"`
	OccurredAt      *time.Time    `json:"occurred_at,omitempty"`
	EvidenceRefs    []EvidenceRef `json:"evidence_refs"`
	RowVersion      int           `json:"row_version"`
}

type HealthGoatRequest struct {
	HealthStatus string        `json:"health_status"`
	Reason       string        `json:"reason"`
	OccurredAt   *time.Time    `json:"occurred_at,omitempty"`
	EvidenceRefs []EvidenceRef `json:"evidence_refs"`
	RowVersion   int           `json:"row_version"`
}

type ReproductiveGoatRequest struct {
	ReproductiveStatus string `json:"reproductive_status"`
	// BreedingDate and LastDeliveryDate are optional YYYY-MM-DD pregnancy-timing facts. Absent means
	// leave the stored value untouched.
	BreedingDate     *string       `json:"breeding_date,omitempty"`
	LastDeliveryDate *string       `json:"last_delivery_date,omitempty"`
	Reason           string        `json:"reason"`
	OccurredAt       *time.Time    `json:"occurred_at,omitempty"`
	EvidenceRefs     []EvidenceRef `json:"evidence_refs"`
	RowVersion       int           `json:"row_version"`
}

// IdentityGoatRequest corrects a goat's DOB and/or entry_date (later data entry). At least one of
// DOB / EntryDate must be present; an absent field leaves the stored value untouched.
type IdentityGoatRequest struct {
	DOB          *string       `json:"dob,omitempty"`
	EntryDate    *string       `json:"entry_date,omitempty"`
	Reason       string        `json:"reason"`
	OccurredAt   *time.Time    `json:"occurred_at,omitempty"`
	EvidenceRefs []EvidenceRef `json:"evidence_refs"`
	RowVersion   int           `json:"row_version"`
}

type AdminGoatBulkRowResult struct {
	RowNumber        int                     `json:"row_number"`
	Decision         string                  `json:"decision"`
	Errors           []FieldError            `json:"errors"`
	Warnings         []Warning               `json:"warnings"`
	Normalized       *AdminGoatCreateRequest `json:"normalized,omitempty"`
	Result           *AdminGoatResponse      `json:"result,omitempty"`
	GenerationStatus string                  `json:"generation_status,omitempty"`
	// Matched-existing reproductive update fields (populated when a CSV row's
	// identifiers resolve to an existing goat and reproductive_status differs).
	MatchedGoatID   *string  `json:"matched_goat_id,omitempty"`
	MatchConfidence *float64 `json:"match_confidence,omitempty"`
	SourceRef       *string  `json:"source_ref,omitempty"`
}

type AdminGoatBulkSummary struct {
	Total          int `json:"total"`
	CreateReady    int `json:"create_ready"`
	RequiresReview int `json:"requires_review"`
	Skipped        int `json:"skipped"`
	Created        int `json:"created"`
	Failed         int `json:"failed"`
	// UpdateReady counts preview rows that will apply a reproductive transition
	// to an existing goat; Updated counts committed reproductive updates.
	UpdateReady int `json:"update_ready"`
	Updated     int `json:"updated"`
}

type AdminGoatBulkResponse struct {
	Summary      AdminGoatBulkSummary     `json:"summary"`
	Rows         []AdminGoatBulkRowResult `json:"rows"`
	PreviewToken string                   `json:"preview_token,omitempty"`
	TraceID      string                   `json:"trace_id"`
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
	Goat             GoatSummary           `json:"goat"`
	Identifiers      []GoatIdentifier      `json:"identifiers"`
	Decision         DecisionRecordSummary `json:"decision"`
	Events           []EventSummary        `json:"events"`
	Idempotency      IdempotencyMeta       `json:"idempotency"`
	GenerationStatus string                `json:"generation_status"`
	TraceID          string                `json:"trace_id"`
}
