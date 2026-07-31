package domain

import (
	"strings"
	"time"
)

const (
	StatusDraft      = "draft"
	StatusPublished  = "published"
	StatusInProgress = "in_progress"
	StatusDelayed    = "delayed"
	StatusCompleted  = "completed"
	StatusClosed     = "closed"

	// Verification verdict state carried by a weighing observation. Weighing
	// enqueues one generic verification item per observation; these are the
	// answers a verifier can send back.
	VerificationStatusPending  = "pending"
	VerificationStatusVerified = "verified"
	VerificationStatusRework   = "rework"

	CategoryIndividualAnimal     = "individual_animal"
	CategoryPerShedPartition     = "per_shed_partition"
	VerificationVerticalWeighing = "weighing"
	VerificationModuleWeighing   = "weighing"
	VerificationCategoryWeighing = "weighing_proof"
	VerificationRefTypeAnimal    = "weighing_observation"
	VerificationRefTypeShed      = "weighing_shed_observation"
	AvailabilityExpectedShed     = "expected_shed"
	AvailabilityMovedOtherShed   = "moved_other_shed"
	AvailabilityICU              = "icu"
	AvailabilityQuarantine       = "quarantine"
	AvailabilityDead             = "dead"
	AvailabilityCulled           = "culled"
	AvailabilitySoldTransferred  = "sold_transferred"
	AvailabilityExited           = "exited"
)

type Actor struct {
	TenantID string
	UserID   string
	Roles    []string
}

type Campaign struct {
	CampaignID        string         `json:"campaign_id"`
	TenantID          string         `json:"tenant_id"`
	ParkID            string         `json:"park_id"`
	ParkName          string         `json:"park_name"`
	PeriodStartDate   string         `json:"period_start_date"`
	PeriodEndDate     string         `json:"period_end_date"`
	StartBusinessDate string         `json:"start_business_date"`
	Status            string         `json:"status"`
	PlannedCapPerDay  int            `json:"planned_cap_per_day"`
	OperatorUserID    string         `json:"operator_user_id"`
	CreatedBy         string         `json:"created_by"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	RowVersion        int            `json:"row_version"`
	Sheds             []CampaignShed `json:"sheds,omitempty"`
	Progress          Progress       `json:"progress"`
}

type CampaignPage struct {
	Items      []Campaign     `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Counts     CampaignCounts `json:"counts"`
}

// CampaignCounts is the WHOLE-FILTER task tally behind the two task-list tabs.
//
// GRAIN: one weighing_campaigns row = one task = one park on one weigh date.
// The counts range over the ENTIRE scope the caller is allowed to see (mine /
// all / operators), NOT over the returned page and NOT narrowed by the park
// chip — so the tab numbers stay still while the user filters or pages.
//
// The split is server-owned so two clients can never disagree about what
// "completed" means: Completed = status IN (completed, closed); Active =
// every other live status (draft, published, in_progress, delayed). A canceled
// task is in neither: it is retracted work, not work in either tab.
type CampaignCounts struct {
	Active    int `json:"active"`
	Completed int `json:"completed"`
}

// CampaignListScope names WHICH weighing surface a campaign listing is for. The three mobile
// weighing screens are separate destinations with separate authority, so the surface is stated
// by the caller rather than guessed from the actor's roles.
type CampaignListScope string

const (
	// CampaignListScopeMine is the assignee's own executable work list.
	CampaignListScopeMine CampaignListScope = "mine"
	// CampaignListScopeAll is the planner's flat all-tasks list across parks.
	CampaignListScopeAll CampaignListScope = "all"
	// CampaignListScopeOperators is read-only oversight of other people's weighing work.
	CampaignListScopeOperators CampaignListScope = "operators"
)

// ParseCampaignListScope maps the wire value to a scope, using fallback for an absent value.
// The fallback is supplied by the ROUTE (the admin listing defaults to the flat list, the app
// listing defaults to the caller's own work) so that an already-installed app that sends no
// scope keeps the behaviour it had, without the service inferring anything from a role.
func ParseCampaignListScope(raw string, fallback CampaignListScope) (CampaignListScope, bool) {
	switch CampaignListScope(strings.TrimSpace(raw)) {
	case "":
		return fallback, true
	case CampaignListScopeMine:
		return CampaignListScopeMine, true
	case CampaignListScopeAll:
		return CampaignListScopeAll, true
	case CampaignListScopeOperators:
		return CampaignListScopeOperators, true
	default:
		return "", false
	}
}

type PlannerCatalog struct {
	Parks     []PlannerPark     `json:"parks"`
	Operators []PlannerOperator `json:"operators"`
}

type PlannerPark struct {
	ParkID           string           `json:"park_id"`
	Name             string           `json:"name"`
	KidCount         int              `json:"kid_count"`
	Sheds            []PlannerShed    `json:"sheds"`
	ExistingCampaign *CampaignSummary `json:"existing_campaign,omitempty"`
}

type PlannerShed struct {
	LocationID string `json:"location_id"`
	Name       string `json:"name"`
	KidCount   int    `json:"kid_count"`
	// Scheduled and the Scheduled* fields describe whether this shed is ALREADY
	// claimed by an open weighing task on the REQUESTED weigh date, and by whom.
	// They are the availability the planner renders (available vs already
	// scheduled) and the reason text on a blocked bucket. They are date-scoped:
	// a shed taken on another date is not taken here.
	//
	// Absent (Scheduled=false, the rest empty) means the shed is free on that
	// date. Never treat these as a roster or a count of animals.
	Scheduled                    bool   `json:"scheduled,omitempty"`
	ScheduledCampaignID          string `json:"scheduled_campaign_id,omitempty"`
	ScheduledStatus              string `json:"scheduled_status,omitempty"`
	ScheduledOperatorUserID      string `json:"scheduled_operator_user_id,omitempty"`
	ScheduledOperatorDisplayName string `json:"scheduled_operator_display_name,omitempty"`
	ScheduledWeighingCategory    string `json:"scheduled_weighing_category,omitempty"`
}

type CampaignSummary struct {
	CampaignID        string `json:"campaign_id"`
	Status            string `json:"status"`
	PeriodStartDate   string `json:"period_start_date"`
	PeriodEndDate     string `json:"period_end_date"`
	StartBusinessDate string `json:"start_business_date"`
	OperatorUserID    string `json:"operator_user_id"`
	ShedCount         int    `json:"shed_count"`
}

type PlannerOperator struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	DisplayCode string `json:"display_code"`
}

type CampaignShed struct {
	CampaignShedID      string `json:"campaign_shed_id"`
	CampaignID          string `json:"campaign_id"`
	LocationID          string `json:"location_id"`
	LocationType        string `json:"location_type"`
	DisplayName         string `json:"display_name"`
	ExpectedAnimalCount int    `json:"expected_animal_count"`
	WeighingCategory    string `json:"weighing_category"`
	OperatorUserID      string `json:"operator_user_id"`
	Status              string `json:"status"`
	// PendingVerificationCount is the exact count of this bucket's SUBMITTED
	// observations (weighing_observations with submitted_at set, plus any
	// weighing_shed_observations row) whose verification_status is not yet
	// 'verified'. There is NO expected-animal denominator here — only a count of
	// evidence that actually exists and still needs a verifier look.
	PendingVerificationCount int `json:"pending_verification_count"`
	// ReworkCount is the subset of PendingVerificationCount that a verifier
	// actively BOUNCED back ('rework'), i.e. work sitting with the operator
	// again. It is a strict subset, never added to the pending count: an
	// observation is either awaiting a first look or bounced, never both.
	ReworkCount int `json:"rework_count"`
	// ReadyToClose is true only when the bucket is submitted (status='completed'),
	// has at least one submitted observation, and NONE of its observations have a
	// verification_status other than 'verified'. A bucket with an outstanding
	// 'rework' observation counts as NOT ready — a bounced video is unfinished
	// work the operator still owes, so surfacing "ready" on it would bury the
	// rework request from leadership's view.
	ReadyToClose bool `json:"ready_to_close"`
}

type ExpectedAnimal struct {
	CampaignID             string `json:"campaign_id"`
	CampaignShedID         string `json:"campaign_shed_id"`
	AnimalID               string `json:"animal_id"`
	DisplayAnimalID        string `json:"display_animal_id"`
	PrimaryIdentifier      string `json:"primary_identifier,omitempty"`
	SecondaryIdentifier    string `json:"secondary_identifier,omitempty"`
	ExpectedLocationID     string `json:"expected_location_id"`
	ExpectedLocationLabel  string `json:"expected_location_label"`
	Status                 string `json:"status"`
	AvailabilityStatus     string `json:"availability_status"`
	CurrentLocationID      string `json:"current_location_id,omitempty"`
	CurrentLocationLabel   string `json:"current_location_label,omitempty"`
	CurrentLifecycleStatus string `json:"current_lifecycle_status,omitempty"`
	Seq                    int64  `json:"seq"`
}

type RosterPage struct {
	Items        []ExpectedAnimal `json:"items"`
	Observations []Observation    `json:"observations,omitempty"`
	NextCursor   string           `json:"next_cursor,omitempty"`
	// NextObservationsCursor paginates Observations independently of Items, on
	// a keyset of (accepted_at, observation_id). Empty means no further
	// observations pages for this shed/roster-window request.
	NextObservationsCursor string `json:"next_observations_cursor,omitempty"`
}

// LeadershipShedVideos is the read-only, shed-grain weighing proof contract.
// Exactly one observation collection is populated according to WeighingCategory.
type LeadershipShedVideos struct {
	CampaignID       string        `json:"campaign_id"`
	CampaignShedID   string        `json:"campaign_shed_id"`
	ShedName         string        `json:"shed_name"`
	WeighingCategory string        `json:"weighing_category"`
	Status           string        `json:"status"`
	Individual       []Observation `json:"individual"`
	LumpSum          *Observation  `json:"lump_sum,omitempty"`
}

type ProofMedia struct {
	ProofID     string `json:"proof_id"`
	DownloadURL string `json:"download_url"`
	MimeType    string `json:"mime_type,omitempty"`
}

type Observation struct {
	ObservationID       string       `json:"observation_id"`
	CampaignID          string       `json:"campaign_id"`
	CampaignShedID      string       `json:"campaign_shed_id,omitempty"`
	AnimalID            string       `json:"animal_id,omitempty"`
	WeightKg            float64      `json:"weight_kg"`
	AverageWeightKg     float64      `json:"average_weight_kg,omitempty"`
	AnimalCount         int          `json:"animal_count,omitempty"`
	ProofArtifactID     string       `json:"proof_artifact_id"`
	ProofArtifactIDs    []string     `json:"proof_artifact_ids,omitempty"`
	Media               []ProofMedia `json:"media,omitempty"`
	ExpectedLocationID  string       `json:"expected_location_id,omitempty"`
	ActualLocationID    string       `json:"actual_location_id,omitempty"`
	ActualLocationLabel string       `json:"actual_location_label,omitempty"`
	AcceptedAt          time.Time    `json:"accepted_at"`
}

type Progress struct {
	IndividualExpectedCount  int `json:"individual_expected_count"`
	IndividualCompletedCount int `json:"individual_completed_count"`
	PerScopeExpectedCount    int `json:"per_scope_expected_count"`
	PerScopeCompletedCount   int `json:"per_scope_completed_count"`
	WrongShedCount           int `json:"wrong_shed_count"`
	MissingCount             int `json:"missing_count"`
	RemainingCount           int `json:"remaining_count"`
}

// CloseCommand drives CloseScope / CloseCampaign. Close is an EXPLICIT
// leadership action that ends work which will never finish on its own. It is
// deliberately allowed while buckets still hold work that was never accepted, so
// the reason and the actor are mandatory context rather than decoration.
type CloseCommand struct {
	TenantID       string
	CampaignID     string
	CampaignShedID string
	Reason         string
	ClosedBy       string
	IdempotencyKey string
}

// CloseResult is the readback of a close. It is persisted as the idempotency
// result snapshot, so an exact replay returns this same value without rerunning
// any side effect.
//
// NotAccepted* describe work that was open at close time and STAYS not accepted:
// closing never marks an unaccepted bucket accepted. NotAccepted is a bounded
// sample (CloseNotAcceptedSampleLimit) while NotAcceptedCount is the exact
// whole-scope total, so the audit trail cannot blow up on a large campaign.
type CloseResult struct {
	CampaignID       string    `json:"campaign_id"`
	CampaignShedID   string    `json:"campaign_shed_id,omitempty"`
	Status           string    `json:"status"`
	Reason           string    `json:"reason,omitempty"`
	ClosedBy         string    `json:"closed_by"`
	ClosedAt         time.Time `json:"closed_at"`
	NotAcceptedCount int       `json:"not_accepted_count"`
	NotAccepted      []string  `json:"not_accepted,omitempty"`
}

// CloseNotAcceptedSampleLimit bounds the identifier list recorded in the close
// audit/idempotency payload and carried in the close event. The count stays exact.
const CloseNotAcceptedSampleLimit = 100

// VerificationVerdict is the weighing-side projection of one generic verifier
// verdict onto one weighing observation. EventID is the verification event id and
// is the idempotency key: the durable bus is at-least-once.
type VerificationVerdict struct {
	TenantID      string
	ObservationID string
	RefType       string
	Status        string
	VerifiedBy    string
	Reason        string
	EventID       string
}

// VerificationVerdictResult reports what the verdict changed so the consumer stays
// idempotent and the notifier can route without a callback into weighing.
type VerificationVerdictResult struct {
	Applied        bool   `json:"applied"`
	CampaignID     string `json:"campaign_id"`
	CampaignShedID string `json:"campaign_shed_id"`
	ObservationID  string `json:"observation_id"`
	OperatorID     string `json:"operator_id"`
	Status         string `json:"status"`
	// DecidedAt is the instant the verdict was PERSISTED (the row's verified_at,
	// returned by the same UPDATE), not a wall clock read afterwards. It is part
	// of the result — and therefore of the idempotency snapshot — so an
	// at-least-once redelivery reports the ORIGINAL decision time instead of
	// minting a new one. Business meaning is India business time per AGENTS.md,
	// so it is carried in Asia/Kolkata.
	DecidedAt time.Time `json:"decided_at"`
}

type CreateCampaign struct {
	TenantID          string
	ParkID            string
	PeriodStartDate   string
	PeriodEndDate     string
	StartBusinessDate string
	PlannedCapPerDay  int
	OperatorUserID    string
	IdempotencyKey    string
	Sheds             []CreateCampaignShed
	CreatedBy         string
}

type UpdateCampaign = CreateCampaign

type CreateCampaignShed struct {
	LocationID       string `json:"location_id"`
	LocationType     string `json:"location_type"`
	DisplayName      string `json:"display_name"`
	WeighingCategory string `json:"weighing_category"`
	OperatorUserID   string `json:"operator_user_id,omitempty"`
}

type RecordAnimalObservation struct {
	TenantID          string
	CampaignID        string
	CampaignShedID    string
	AnimalID          string
	ScannedIdentifier string
	WeightKg          float64
	ProofArtifactID   string
	ActualLocationID  string
	IdempotencyKey    string
	RecordedBy        string
}

type RecordShedObservation struct {
	TenantID         string
	CampaignID       string
	CampaignShedID   string
	WeightKg         float64
	AverageWeightKg  float64
	AnimalCount      int
	ProofArtifactID  string
	ProofArtifactIDs []string
	IdempotencyKey   string
	RecordedBy       string
}
