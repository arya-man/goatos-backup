package domain

import (
	"regexp"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// LiveTrackerStatus is the ROW-STATE filter for the live drive tracker.
//
// Scope is deliberately narrower than the other four filters. Park / vaccine / operator / shed are
// membership predicates and are pushed into the single scoped CTE, so they narrow every section
// including the combo card and the live feed. Status is a DERIVED row state (computed in Go from
// counts plus an elapsed clock), so it can only narrow the sections that are folded from those
// rows: the tiles, the operator board, the shed board and the Attention list. The combo card and the
// activity feed always describe the whole drive day, and filter.apply_note says so on screen.
type LiveTrackerStatus string

const (
	LiveTrackerStatusActive  LiveTrackerStatus = "active"
	LiveTrackerStatusDone    LiveTrackerStatus = "done"
	LiveTrackerStatusPending LiveTrackerStatus = "pending"
	LiveTrackerStatusReview  LiveTrackerStatus = "review"
)

// LiveTrackerOperatorState is the per-operator live state on the drive day.
const (
	LiveTrackerOperatorActive     = "active"
	LiveTrackerOperatorDone       = "done"
	LiveTrackerOperatorNotStarted = "not_started"
	LiveTrackerOperatorIdle       = "idle"
)

// LiveTrackerShedState is the per-shed×partition live state on the drive day. The mock's legend
// declares four; `review` is the fifth the mock omitted but whose rows it renders
// ("2 extra attempts") — it is emitted here so the legend and the rows agree.
const (
	LiveTrackerShedReceiving  = "receiving"
	LiveTrackerShedSlow       = "slow"
	LiveTrackerShedDone       = "done"
	LiveTrackerShedNotStarted = "not_started"
	LiveTrackerShedReview     = "review"
)

// Proof state of ONE animal's video proof on the drive day.
const (
	LiveTrackerProofVideo     = "video"
	LiveTrackerProofUploading = "uploading"
	LiveTrackerProofNone      = "none"
)

// Per-dose (per obligation) state inside a combo row.
//
// `closed` means the OBLIGATION reached status='completed'. It used to be returned whenever the
// animal had a completed proof, whatever the obligation actually said — so a `scheduled` obligation
// rendered as "closed by the animal's proof" while obligation_instances.status was still scheduled
// and completed_at was null. `verification_pending` is the state that situation really is, and it is
// the same word the sibling execution read model already uses for it
// (domain.WorkStateVerificationPending: "Mobile proof submitted; awaiting verifier review").
const (
	LiveTrackerDoseClosed              = "closed"
	LiveTrackerDoseVerificationPending = "verification_pending"
	LiveTrackerDoseAwaitingProof       = "awaiting_proof"
	LiveTrackerDoseScheduled           = "scheduled"
)

// Activity feed event kinds. Each maps 1:1 to one UNION arm of the feed query.
const (
	LiveTrackerActivityProofVideo    = "proof_video"
	LiveTrackerActivityScanCapture   = "scan_capture"
	LiveTrackerActivityScanDuplicate = "scan_duplicate"
	LiveTrackerActivityScanUnknown   = "scan_unknown"
	LiveTrackerActivityAdministered  = "administration"
	// One row per obligation_status_events row with event_type='completed' — that is PER-ANIMAL
	// obligation closure. It was called "shed submitted", a shed-level name on an animal-grain count;
	// a real shed submission lives in sop_submissions and is not read by this feed at all.
	LiveTrackerActivityObligationClosed = "obligation_closed"
)

// Attention row kinds.
const (
	LiveTrackerAttentionExtraAttempts = "extra_attempts"
	LiveTrackerAttentionIdleOperator  = "idle_operator"
	LiveTrackerAttentionSlowShed      = "slow_shed"
)

// LiveTrackerIdleMinutes is the inactivity threshold after which an operator with remaining work is
// reported idle. The mock's "idle 2h+" label is a copy string; this is the machine threshold.
const LiveTrackerIdleMinutes = 90

// LiveTrackerSlowShedMinutes is how long a shed's drive must have been open before a low completion
// ratio is reported as a slow start rather than a normal ramp-up.
const LiveTrackerSlowShedMinutes = 40

// LiveTrackerSlowShedRatio is the completion ratio below which an open shed is reported slow.
const LiveTrackerSlowShedRatio = 0.25

// Bounded array sizes. Every list the tracker returns is capped server-side; nothing on this page
// can fan out with herd size.
const (
	LiveTrackerMaxOperators    = 100
	LiveTrackerMaxSheds        = 200
	LiveTrackerMaxComboRows    = 200
	LiveTrackerDefaultActivity = 40
	LiveTrackerMaxActivity     = 100
	// LiveTrackerMaxActors caps the per-person evidence rollup. It is a DECLARED constant rather than
	// a bare literal because the operator board reads its Videos/Scans/last-activity out of that
	// rollup by member id: an operator missing from it is rendered with zero evidence and state
	// not_started, which is indistinguishable from someone who genuinely has not started.
	LiveTrackerMaxActors = 500
	// LiveTrackerMaxAttention caps the attention rail. It is derived from the PRE-cap cell rollup and
	// can emit two rows per shed cell plus one per idle operator, so without a cap of its own it is
	// the one array in the response bounded only by (2 x LiveTrackerMaxCells + operators).
	LiveTrackerMaxAttention = 200
	// Filter-bar vocabulary caps. Each kind carries its OWN budget: a single shared cap across the
	// union was spent in kind-name order, so the last kind alphabetically (vaccine) was starved to
	// zero before any other list lost a single row.
	LiveTrackerMaxParkOptions     = 200
	LiveTrackerMaxVaccineOptions  = 100
	LiveTrackerMaxOperatorOptions = 500
	LiveTrackerMaxShedOptions     = 1000
	// LiveTrackerMaxCells caps the park × shed × partition × vaccine × operator rollup. EVERY KPI tile
	// is folded from these rows in Go, so hitting this cap does not merely shorten a table — it makes
	// the headline number itself under-report the day. The response therefore carries
	// CellsTruncated so the page can say so out loud instead of showing a quietly wrong total.
	LiveTrackerMaxCells = 2000
	// LiveTrackerBusinessDateLookbackDays bounds how far back a drive day may be requested. This is a
	// BUSINESS DATE, not a projection as_of: the tracker always reconstructs from canonical rows, so a
	// past drive day is a legitimate read and must not emit historical_as_of_unsupported.
	LiveTrackerBusinessDateLookbackDays = 7
)

// LiveTrackerQuery is one filter set applied to every section of the tracker. The mock promises
// "Filters apply to tiles, tables and the live feed together"; that promise is only keepable because
// all six sections derive from a single membership CTE narrowed by this one query.
type LiveTrackerQuery struct {
	TenantID       string
	BusinessDate   time.Time
	ParkID         *string
	ShedID         *string
	PartitionLabel *string
	OperatorID     *string
	VaccineCode    *string
	Status         *LiveTrackerStatus
	ActivityLimit  int
	ActivityBefore *time.Time
	// ActivityBeforeID is the event_id half of the feed's keyset cursor. occurred_at ALONE is not a
	// key: scan captures and scan attempts written in a burst routinely share a timestamp to the
	// microsecond, and a strict `occurred_at < cursor` predicate skips every one of the tied events
	// on the next page — losing real events rather than merely repeating them.
	ActivityBeforeID *string
}

// LiveTrackerParkCount is one park's share of the day's scheduled administrations.
// ParkCode is the compact location code ("CBE"/"CPT") the mock's detail line uses; ParkName is the
// full name. Carrying both lets the tile stay inside its 150px minimum instead of overflowing with
// "153 Coimbatore + 145 Channapatna".
type LiveTrackerParkCount struct {
	ParkID   string `json:"park_id"`
	ParkName string `json:"park_name"`
	ParkCode string `json:"park_code"`
	Count    int    `json:"count"`
}

// LiveTrackerKPIs are the six headline tiles, all at ADMINISTRATION grain except ComboAnimals which
// is explicitly an ANIMAL count. The two grains are never mixed inside one number.
type LiveTrackerKPIs struct {
	ScheduledAdministrations int                    `json:"scheduled_administrations"`
	ScheduledByPark          []LiveTrackerParkCount `json:"scheduled_by_park"`
	ProofVideosReceived      int                    `json:"proof_videos_received"`
	// ClosedAdministrations is obligation CLOSURE — obligation_instances.status = 'completed'. Proof
	// arrival and closure are different facts and this page reports both: in stg on 2026-08-12 all
	// 298 proof videos had landed while 9 of 298 obligations were completed, so a board that derived
	// Remaining from proof arrival read as a finished drive on a drive that was still open.
	ClosedAdministrations int `json:"closed_administrations"`
	// AwaitingClose is proofed-but-not-closed: the field work landed, the obligation has not.
	AwaitingClose int `json:"awaiting_close"`
	ScanCaptures  int `json:"scan_captures"`
	// Remaining is scheduled minus CLOSED, floored at zero — administrations the drive still owes.
	Remaining      int `json:"remaining"`
	ComboAnimals   int `json:"combo_animals"`
	AttentionCount int `json:"attention_count"`
	ActiveParks    int `json:"active_parks"`
}

// LiveTrackerOperatorRow is one operator's live board row.
type LiveTrackerOperatorRow struct {
	OperatorID            string `json:"operator_id"`
	OperatorName          string `json:"operator_name"`
	OperatorDisplayCode   string `json:"operator_display_code"`
	IdentityResolved      bool   `json:"identity_resolved"`
	ParkID                string `json:"park_id"`
	ParkName              string `json:"park_name"`
	ParkCode              string `json:"park_code"`
	CurrentShedID         string `json:"current_shed_id"`
	CurrentShedLabel      string `json:"current_shed_label"`
	CurrentPartitionLabel string `json:"current_partition_label"`
	CurrentVaccineLabel   string `json:"current_vaccine_label"`
	ScheduledAdmins       int    `json:"scheduled_administrations"`
	// ProofVideos is a PHYSICAL count of proof_artifacts rows this person uploaded — display only.
	// Subtracting it from ScheduledAdmins mixed two grains: on a combo day one video closes two
	// obligations, so a finished operator read Remaining = half their workload, never reached `done`,
	// was dropped by the status=done filter and was emitted as a false idle-operator attention row.
	ProofVideos  int `json:"proof_videos"`
	ScanCaptures int `json:"scan_captures"`
	// ClosedAdmins is this operator's assigned administrations that actually closed — the same
	// OBLIGATION grain as ScheduledAdmins, so Remaining is a subtraction of like from like.
	ClosedAdmins   int        `json:"closed_administrations"`
	Remaining      int        `json:"remaining"`
	LastActivityAt *time.Time `json:"last_activity_at"`
	IdleMinutes    *int       `json:"idle_minutes"`
	State          string     `json:"state"`
}

// LiveTrackerShedRow is one shed×partition proof-progress row.
type LiveTrackerShedRow struct {
	ShedID                     string `json:"shed_id"`
	ShedName                   string `json:"shed_name"`
	PhysicalShed               string `json:"physical_shed"`
	PartitionLabel             string `json:"partition_label"`
	ShedLabel                  string `json:"shed_label"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	ParkID                     string `json:"park_id"`
	ParkName                   string `json:"park_name"`
	VaccineCode                string `json:"vaccine_code"`
	VaccineLabel               string `json:"vaccine_label"`
	OperatorID                 string `json:"operator_id"`
	OperatorName               string `json:"operator_name"`
	ScheduledAdmins            int    `json:"scheduled_administrations"`
	// ProofVideosReceived is proof ARRIVAL; ClosedAdmins is obligation CLOSURE. Remaining is derived
	// from closure, so a shed only reads `done` once its obligations are actually closed.
	ClosedAdmins        int        `json:"closed_administrations"`
	ProofVideosReceived int        `json:"proof_videos_received"`
	Remaining           int        `json:"remaining"`
	LastProofAt         *time.Time `json:"last_proof_at"`
	ExtraAttemptCount   int        `json:"extra_attempt_count"`
	State               string     `json:"state"`
}

// LiveTrackerComboDose is one obligation inside a combo animal's row.
type LiveTrackerComboDose struct {
	ObligationID string `json:"obligation_id"`
	VaccineCode  string `json:"vaccine_code"`
	VaccineLabel string `json:"vaccine_label"`
	State        string `json:"state"`
}

// LiveTrackerComboRow is one animal carrying two or more distinct same-day vaccination obligations.
// PrimaryTag / SecondaryTag are the animal's REAL scanned identifiers; no synthetic display id is
// ever generated here.
type LiveTrackerComboRow struct {
	GoatID       string                 `json:"goat_id"`
	DisplayID    string                 `json:"display_id"`
	PrimaryTag   string                 `json:"primary_tag"`
	SecondaryTag string                 `json:"secondary_tag"`
	ShedLabel    string                 `json:"shed_label"`
	ProofState   string                 `json:"proof_state"`
	ProofCount   int                    `json:"proof_count"`
	Doses        []LiveTrackerComboDose `json:"doses"`
}

// LiveTrackerCombo is the combo-dose card: one proof, two obligations.
type LiveTrackerCombo struct {
	AnimalCount   int                   `json:"animal_count"`
	VaccineLabels []string              `json:"vaccine_labels"`
	Rows          []LiveTrackerComboRow `json:"rows"`
	RowsTruncated bool                  `json:"rows_truncated"`
}

// LiveTrackerActivityItem is one row of the live feed. EventID is "<kind>:<pk>" so a polling client
// can dedupe across refreshes without guessing at identity.
type LiveTrackerActivityItem struct {
	EventID           string    `json:"event_id"`
	OccurredAt        time.Time `json:"occurred_at"`
	Kind              string    `json:"kind"`
	ActorID           string    `json:"actor_id"`
	ActorName         string    `json:"actor_name"`
	ParkName          string    `json:"park_name"`
	ShedLabel         string    `json:"shed_label"`
	VaccineLabel      string    `json:"vaccine_label"`
	GoatID            string    `json:"goat_id"`
	GoatDisplayID     string    `json:"goat_display_id"`
	ScannedIdentifier string    `json:"scanned_identifier"`
	DetailCode        string    `json:"detail_code"`
}

// LiveTrackerActivity is the keyset-paginated live feed plus the OBSERVED event rate over the
// returned window. ObservedPerMin is nil when fewer than two events were returned — the mock's
// hardcoded "~3/min" is not reproduced.
type LiveTrackerActivity struct {
	Items      []LiveTrackerActivityItem `json:"items"`
	NextCursor *time.Time                `json:"next_cursor"`
	// NextCursorEventID is the tiebreaker half of the cursor. A caller MUST send both back
	// (activity_before + activity_before_id) or events sharing the page boundary's timestamp are
	// dropped. The feed's ORDER BY is (occurred_at DESC, event_id DESC), so this is its exact key.
	NextCursorEventID *string  `json:"next_cursor_event_id"`
	ObservedPerMin    *float64 `json:"observed_per_min"`
	WindowMinutes     int      `json:"window_minutes"`
}

// LiveTrackerAttentionRow is one attention item. Every row is derived from the same CTEs the tiles
// and tables use, so the Attention KPI equals len(Attention) by construction.
type LiveTrackerAttentionRow struct {
	Kind                       string     `json:"kind"`
	SubjectLabel               string     `json:"subject_label"`
	OperatorID                 string     `json:"operator_id"`
	ShedID                     string     `json:"shed_id"`
	PartitionLabel             string     `json:"partition_label"`
	OperationalLocationDisplay string     `json:"operational_location_display"`
	MetricCount                int        `json:"metric_count"`
	TotalCount                 int        `json:"total_count"`
	ElapsedMin                 int        `json:"elapsed_minutes"`
	SinceAt                    *time.Time `json:"since_at"`
	Severity                   string     `json:"severity"`
}

// LiveTrackerVerification is the post-drive verification queue block.
type LiveTrackerVerification struct {
	AwaitingReviewItems int `json:"awaiting_review_items"`
	AwaitingReviewSheds int `json:"awaiting_review_sheds"`
	VerifiedTodayItems  int `json:"verified_today_items"`
	VerifiedTodaySheds  int `json:"verified_today_sheds"`
	ReworkRequested     int `json:"rework_requested"`
}

// LiveTrackerFilterOption is one server-authored filter choice. The frontend never assembles a
// filter vocabulary itself, so an option that matches zero rows cannot be offered.
type LiveTrackerFilterOption struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	PartitionLabel string `json:"partition_label"`
	Label          string `json:"label"`
}

// LiveTrackerFilterOptions is the whole filter bar's vocabulary, compiled from the day's own rows.
type LiveTrackerFilterOptions struct {
	Parks     []LiveTrackerFilterOption `json:"parks"`
	Vaccines  []LiveTrackerFilterOption `json:"vaccines"`
	Operators []LiveTrackerFilterOption `json:"operators"`
	Sheds     []LiveTrackerFilterOption `json:"sheds"`
	// Truncated is true when ANY kind hit its own cap. Every other bounded list in this response
	// declares its truncation; the filter vocabulary was the only one that could lose whole option
	// groups with nothing on screen to say so.
	Truncated bool `json:"truncated"`
}

// LiveTrackerResponse is the whole live drive tracker page in one read.
type LiveTrackerResponse struct {
	BusinessDate string    `json:"business_date"`
	GeneratedAt  time.Time `json:"generated_at"`
	// IsLiveDay is true only when BusinessDate is today in business time. The handler accepts a drive
	// day up to LiveTrackerBusinessDateLookbackDays back, and every elapsed figure on the page is
	// already clock-corrected for that; the header's "N parks running" LIVE chip and the client's
	// poller were not, so a drive that closed days ago rendered as running and kept polling.
	IsLiveDay bool                     `json:"is_live_day"`
	Freshness *ProjectionFreshness     `json:"freshness,omitempty"`
	KPIs      LiveTrackerKPIs          `json:"kpis"`
	Operators []LiveTrackerOperatorRow `json:"operators"`
	Sheds     []LiveTrackerShedRow     `json:"sheds"`
	Combo     LiveTrackerCombo         `json:"combo"`
	// Truncation is REPORTED, never silent. The combo card already carried rows_truncated; the two
	// boards did not, so past the caps the Scheduled tile stopped equalling the sum of the visible
	// table's Scheduled column with nothing on screen to explain the gap.
	OperatorsTotal     int  `json:"operators_total"`
	OperatorsTruncated bool `json:"operators_truncated"`
	ShedsTotal         int  `json:"sheds_total"`
	ShedsTruncated     bool `json:"sheds_truncated"`
	// CellsTruncated means the underlying rollup itself hit LiveTrackerMaxCells, so the KPI tiles —
	// which are folded from that rollup — under-report the day by an unbounded amount.
	CellsTruncated bool `json:"cells_truncated"`
	// UnassignedAdministrations is the day's scheduled work that resolved to NO drive assignment. It
	// is counted into the Scheduled tile and rendered in the shed board, but has no operator to be
	// attributed to and therefore appears in no operator row — so without this figure the Operators
	// table's Scheduled column simply summed short of the tile above it, with nothing on screen to
	// explain the gap. On a partially-planned stg day (2026-08-14) that gap is 95 of 199.
	UnassignedAdministrations int                       `json:"unassigned_administrations"`
	Activity                  LiveTrackerActivity       `json:"activity"`
	Attention                 []LiveTrackerAttentionRow `json:"attention"`
	AttentionTotal            int                       `json:"attention_total"`
	AttentionTruncated        bool                      `json:"attention_truncated"`
	Verification              LiveTrackerVerification   `json:"verification"`
	FilterOptions             LiveTrackerFilterOptions  `json:"filter_options"`
}

var partitionPartPrefix = regexp.MustCompile(`^part[[:space:]]*`)

// NormalizePartitionLabel is the ONE partition-identity rule for this read model.
//
// goat_shed_partitions.partition_label and vaccination_drive_assignments.partition_label disagree in
// form for the same physical partition: Old Yashoda is stored as "1".."4" on the goat side and
// "Part 1".."Part 4" on the assignment side, while Gandhi uses bare "2"/"3" on both and Godel 1 uses
// "Part N" on both. Joining the raw labels silently drops every Old Yashoda partition into
// "unassigned" — an operator's whole park vanishes from the board with no error. Both sides of every
// partition join in this file go through here.
func NormalizePartitionLabel(label string) string {
	trimmed := strings.ToLower(strings.TrimSpace(label))
	if trimmed == "" {
		return "whole"
	}
	return partitionPartPrefix.ReplaceAllString(trimmed, "")
}

// ShedDisplayLabel composes the human shed label ("Gandhi 3", "Sumathi 2 - Part 4") the way the
// operational location display does, so the tracker's shed column matches every other vaccination
// surface instead of inventing a second format.
func ShedDisplayLabel(shedName, partitionLabel string) string {
	return oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display()
}

// VaccineFamilyCode reduces a dose code to its antigen family ("goat_pox_adult_w1" -> "goat_pox"),
// which is the grain the tracker's vaccine filter works at. public.vaccines is empty in every
// environment, so the family prefix plus DoseDisplayLabel is the only label source.
func VaccineFamilyCode(doseCode string) string {
	code := strings.ToLower(strings.TrimSpace(doseCode))
	if code == "" {
		return ""
	}
	for _, family := range []string{"blue_tongue", "sheep_pox", "goat_pox", "et_tt", "ppr", "fmd", "hs"} {
		if code == family || strings.HasPrefix(code, family+"_") {
			return family
		}
	}
	if idx := strings.Index(code, "_"); idx > 0 {
		return code[:idx]
	}
	return code
}

// IsLiveTrackerStatus reports whether raw is a supported cross-section status filter.
func IsLiveTrackerStatus(raw string) bool {
	switch LiveTrackerStatus(raw) {
	case LiveTrackerStatusActive, LiveTrackerStatusDone, LiveTrackerStatusPending, LiveTrackerStatusReview:
		return true
	}
	return false
}

// OperatorMatchesStatus maps an operator row state onto the shared status filter vocabulary.
func OperatorMatchesStatus(state string, status LiveTrackerStatus) bool {
	switch status {
	case LiveTrackerStatusActive:
		return state == LiveTrackerOperatorActive
	case LiveTrackerStatusDone:
		return state == LiveTrackerOperatorDone
	case LiveTrackerStatusPending:
		return state == LiveTrackerOperatorNotStarted
	case LiveTrackerStatusReview:
		return state == LiveTrackerOperatorIdle
	}
	return true
}

// ShedMatchesStatus maps a shed row state onto the shared status filter vocabulary.
func ShedMatchesStatus(state string, status LiveTrackerStatus) bool {
	switch status {
	case LiveTrackerStatusActive:
		return state == LiveTrackerShedReceiving || state == LiveTrackerShedSlow
	case LiveTrackerStatusDone:
		return state == LiveTrackerShedDone
	case LiveTrackerStatusPending:
		return state == LiveTrackerShedNotStarted
	case LiveTrackerStatusReview:
		return state == LiveTrackerShedReview
	}
	return true
}
