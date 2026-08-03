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
	CampaignID        string    `json:"campaign_id"`
	TenantID          string    `json:"tenant_id"`
	ParkID            string    `json:"park_id"`
	ParkName          string    `json:"park_name"`
	PeriodStartDate   string    `json:"period_start_date"`
	PeriodEndDate     string    `json:"period_end_date"`
	StartBusinessDate string    `json:"start_business_date"`
	Status            string    `json:"status"`
	PlannedCapPerDay  int       `json:"planned_cap_per_day"`
	OperatorUserID    string    `json:"operator_user_id"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	RowVersion        int       `json:"row_version"`
	// CloseReason is the backend-owned sentence recorded when the task was ended.
	// Empty on a task that is still live. Clients RENDER it; they never author it.
	CloseReason string         `json:"close_reason,omitempty"`
	Sheds       []CampaignShed `json:"sheds,omitempty"`
	Progress    Progress       `json:"progress"`
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

// PlannerCatalog is the PARK-GRAIN planner vocabulary for ONE weigh date: every
// park the planner may pick, plus the operator picker. It carries NO shed rows.
//
// Parks and sheds are two different grains and used to share one flattened
// keyset page. Because a real park holds 76+ sheds and the page was ~20 rows,
// page one was entirely ONE park and the wizard's "Select park" step offered a
// single park — the other parks were unreachable without paging through dozens
// of shed rows. The park step needs ALL parks (there are a handful); the bucket
// step is what pages, per park, through PlannerParkBuckets.
type PlannerCatalog struct {
	Parks     []PlannerPark     `json:"parks"`
	Operators []PlannerOperator `json:"operators"`
}

type PlannerPark struct {
	ParkID   string `json:"park_id"`
	Name     string `json:"name"`
	KidCount int    `json:"kid_count"`
	// ShedCount is a PARK-GRAIN count of the park's active sheds, computed by a
	// scalar aggregate over that park's own children. It is deliberately NOT a
	// count of shed rows returned on any page: the catalog returns no shed rows
	// at all, and a bucket page carries only ~20 of them.
	ShedCount int `json:"shed_count"`
	// ExistingCampaign summarizes the park's MOST RECENT task on the requested
	// week. A park-week may legitimately hold SEVERAL tasks: the capture category
	// is a per-BUCKET property, so one campaign cannot express "weigh these sheds
	// lump-sum now, plan the leftover sheds separately", and leadership plans the
	// remainder as a second task. This field is therefore a summary for display,
	// never proof that the park holds exactly one task, and never a reason to
	// block a create.
	ExistingCampaign *CampaignSummary `json:"existing_campaign,omitempty"`
	// ExistingCampaignCount is how many non-canceled tasks the park holds on the
	// requested week, so a caller can say "2 tasks already scheduled" instead of
	// mistaking the single ExistingCampaign summary for the whole truth.
	// 0 means the park-week is free.
	ExistingCampaignCount int `json:"existing_campaign_count,omitempty"`
}

// PlannerParkBuckets is ONE keyset page of the sheds of ONE park, with the same
// date-scoped availability the planner renders. This is the many-side grain: it
// pages, the park list does not.
type PlannerParkBuckets struct {
	ParkID string        `json:"park_id"`
	Sheds  []PlannerShed `json:"sheds"`
	// NextCursor is the keyset over (shed display_order, shed name, shed
	// location_id) WITHIN this park. Empty means the last page.
	NextCursor string `json:"next_cursor,omitempty"`
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
	// ParkIDs are the parks this person may be assigned weighing work in. EMPTY means every park
	// (a tenant-scoped principal). Operators and park-scoped directors carry exactly their park.
	//
	// The planner used to receive a flat roster with no scope at all, so the wizard offered -- and
	// DEFAULTED to -- someone from another park, and the write accepted it. The picker filters on
	// this; the write re-checks it, because a client is not a permission boundary.
	ParkIDs []string `json:"park_ids,omitempty"`
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
	// OperatorDisplayName is the backend-resolved name of the bucket's assignee
	// (active workforce member only). It travels WITH the bucket so a client never
	// has to join the bucket against a separately paged operator vocabulary — doing
	// that left buckets past the first catalog page rendering without a name.
	// Empty WITH a non-empty OperatorUserID is a roster gap, not "not assigned".
	OperatorDisplayName string `json:"operator_display_name"`
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

// CampaignShedPage is the task-detail (L1) bucket list as a keyset page.
//
// GRAIN: one weighing_campaign_sheds row = one bucket = one shed on this task.
// It exists because the task LIST embeds every bucket of every campaign on the
// page: a park holds 76+ sheds, so a 20-task page carried 1,500+ bucket rows for
// cards that show a handful. The detail screen reads this instead, ~20 at a time.
type CampaignShedPage struct {
	CampaignID string         `json:"campaign_id"`
	Items      []CampaignShed `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	// TotalCount is the WHOLE-TASK bucket count, not the page's length, so the
	// detail header can say how big the task is without draining the pages.
	TotalCount int `json:"total_count"`
}

// CampaignShedPageSize / MaxCampaignShedPageSize bound the task-detail bucket page.
const (
	CampaignShedPageSize    = 20
	MaxCampaignShedPageSize = 100
)

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

// MaxShedProofArtifacts is how many group videos ONE lump-sum shed submission may
// carry. It is the proof policy, so it is also the denominator leadership reads
// ("3 of 5"); clients must render it rather than hardcode a number of their own.
const MaxShedProofArtifacts = 5

// LeadershipShedVideos is the read-only, shed-grain weighing proof contract.
// Exactly one observation collection is populated according to WeighingCategory.
type LeadershipShedVideos struct {
	CampaignID     string `json:"campaign_id"`
	CampaignShedID string `json:"campaign_shed_id"`
	ShedName       string `json:"shed_name"`
	// ParkName and WeighDate are the shed's OWN context, carried on the shed-grain
	// read so a cold deep link into this surface renders a real eyebrow. They used
	// to travel as client route args, which meant a link opened without the parent
	// list showed a blank header. WeighDate is the Asia/Kolkata business DATE
	// (YYYY-MM-DD) of the task this bucket belongs to — never a timestamp.
	ParkName  string `json:"park_name"`
	WeighDate string `json:"weigh_date"`
	// OperatorUserID is who owns this bucket. Empty means nobody is assigned yet,
	// which is the ONLY thing that entitles a client to say "not assigned".
	OperatorUserID string `json:"operator_user_id"`
	// OperatorDisplayName is the backend-resolved name of that assignee, resolved
	// the same way the planner catalog resolves it (active workforce member only).
	// Empty WITH a non-empty OperatorUserID means the assignee has no active
	// workforce row — that is a roster gap, not "not assigned", and a client must
	// not render it as unassigned.
	OperatorDisplayName string `json:"operator_display_name"`
	WeighingCategory    string `json:"weighing_category"`
	Status              string `json:"status"`
	// EstimatedAnimalCount is the shed's herd estimate captured when the bucket was
	// planned. It is a coverage hint, NEVER a denominator for completeness: weighing
	// is free-flow and has no expected roster.
	EstimatedAnimalCount int `json:"estimated_animal_count"`
	// MaxShedVideos is the lump-sum group-video allowance, so "N of MaxShedVideos"
	// reads off the same policy the write path enforces.
	MaxShedVideos int           `json:"max_shed_videos"`
	Individual    []Observation `json:"individual"`
	LumpSum       *Observation  `json:"lump_sum,omitempty"`
	// NextIndividualCursor pages Individual on a keyset of
	// (accepted_at, observation_id) scoped to this bucket. Empty means the last
	// page. LumpSum is a single latest-row read and is never paged: a per-shed
	// bucket has exactly one lump-sum submission.
	NextIndividualCursor string `json:"next_individual_cursor,omitempty"`
	// PeriodLabel is the backend-owned sentence for the weigh period this bucket
	// belongs to. It travels with the bucket so a client reading a bucket page does
	// not have to build the label by concatenating dates it happened to have.
	PeriodLabel string `json:"period_label,omitempty"`
}

// LeadershipShedPage is ONE keyset page of shed buckets across tasks, each with
// its own first page of captured evidence.
//
// GRAIN: one weighing_campaign_sheds row = one bucket = one shed on one task.
// There is no denominator here: the page carries the buckets it returned, and
// each bucket carries its own evidence cursor.
type LeadershipShedPage struct {
	Items      []LeadershipShedVideos `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

// LeadershipShedPageSize / MaxLeadershipShedPageSize bound the leadership gallery
// bucket page — the same ~20-row page every mobile list uses.
const (
	LeadershipShedPageSize    = 20
	MaxLeadershipShedPageSize = 100
)

// LeadershipShedVideosPageSize / MaxLeadershipShedVideosPageSize bound the
// leadership shed evidence read. The default is the ~20-row page every mobile
// list uses; the cap stops a client asking for the whole bucket in one request,
// which is what this read used to do (it selected EVERY observation row for the
// shed and let the screen page the display).
const (
	LeadershipShedVideosPageSize    = 20
	MaxLeadershipShedVideosPageSize = 100
)

// The planner's two grains are bounded separately.
//
// MaxPlannerParks caps the PARK picker. Parks are few (a handful in the real
// data), and the park step must show them ALL, so this is a sanity ceiling
// rather than a page size — there is no park cursor.
//
// PlannerBucketPageSize / MaxPlannerBucketPageSize bound the per-park SHED page.
// A real park holds 76+ sheds, so that list is a keyset page like any other.
//
// PlannerOperatorLimit caps the operator picker. The field-operator roster is
// small, but "small today" is not a bound.
const (
	MaxPlannerParks          = 100
	PlannerBucketPageSize    = 20
	MaxPlannerBucketPageSize = 100
	PlannerOperatorLimit     = 100
)

// WeighingAssignableRoles is WHO may be assigned a weighing shed bucket.
//
// Assignability follows the weighing.execute capability, not the word "operator": the growth
// director weighs his own sheds alongside the field operators. Picking the picker's membership by
// primary_role_hint='operator' hid him from it, so a director bucket could not be created through
// the wizard and a bucket he already held rendered as a nameless "Operator".
var WeighingAssignableRoles = []string{"operator", "growth_director"}

// CloseReasonCode values are what a CLIENT may send instead of authoring the
// sentence that is recorded forever. The backend owns the recorded copy.
const (
	CloseReasonCodeAllAccepted = "all_buckets_accepted"
	CloseReasonCodeOpenBuckets = "open_buckets_closed"
	closeReasonAllAcceptedText = "Every shed bucket was accepted."
	closeReasonOpenBucketsText = "Closed while shed buckets were still not accepted."
)

// ResolveCloseReason maps a known client-sent reason CODE to the backend-owned
// sentence. Anything else is passed through unchanged, so an operator/admin who
// types a real reason still has their own words recorded.
func ResolveCloseReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case CloseReasonCodeAllAccepted:
		return closeReasonAllAcceptedText
	case CloseReasonCodeOpenBuckets:
		return closeReasonOpenBucketsText
	default:
		return reason
	}
}

type ProofMedia struct {
	ProofID     string `json:"proof_id"`
	DownloadURL string `json:"download_url"`
	MimeType    string `json:"mime_type,omitempty"`
}

type Observation struct {
	ObservationID  string `json:"observation_id"`
	CampaignID     string `json:"campaign_id"`
	CampaignShedID string `json:"campaign_shed_id,omitempty"`
	// ScannedIdentifier is the raw tag/RFID the scanner read. Weighing is
	// free-flow: this is the ONLY identity an observation carries. There is
	// deliberately no animal_id here (removed by
	// 000078_weighing_observations_drop_animal_id.sql) -- weighing never
	// resolves a scan to herd identity.
	ScannedIdentifier   string       `json:"scanned_identifier,omitempty"`
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
	// Superseded is true when this capture UPDATED an existing, not-yet-submitted
	// (or verifier-reworked) evidence row in place, rather than inserting a fresh
	// one. It is the signal the service layer uses to advance the observation's
	// verification round: the previous verification item (which may already carry
	// a stale 'verified' decision against the OLD weight/proof) must be withdrawn
	// before a new one is raised for the edited evidence. See
	// app.Service.enqueueVerification and the B06 root-cause note there.
	Superseded bool `json:"-"`
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
	// EvidenceProofID is the proof/video id the verifier reviewed BEFORE
	// deciding. Without it a verdict carries no reference to WHICH evidence
	// was approved: approving a stale queue item approves whatever proof
	// happens to be attached to the observation NOW, not the video the
	// reviewer actually watched (e.g. after a rework re-shoot swapped the
	// proof out from under an in-flight review). Empty is a deliberately
	// backward-compatible value for verdicts minted before this field
	// existed (in-flight events on the durable bus at deploy time); the
	// postgres adapter treats empty as "stale-check-skipped", not a crash
	// or a rejection, and logs it so the gap is visible without breaking
	// delivery.
	EvidenceProofID string
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

// RecordAnimalObservation is the free-flow scan write command. It carries no
// animal_id: weighing never resolves a scanned identifier to herd identity, so
// there is no field here for a caller to (mis)supply one. ScannedIdentifier is
// the required identity.
type RecordAnimalObservation struct {
	TenantID          string
	CampaignID        string
	CampaignShedID    string
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
