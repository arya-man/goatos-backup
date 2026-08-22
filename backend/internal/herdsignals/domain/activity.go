package domain

import "time"

// FARM-ACTIVITY OVERLAY -- GET /herd-signals/tags/{tag_id}/activity.
//
// WHAT THIS IS: the farm records that exist for the animal this tag is mapped to, over the same
// window the movement history is drawn for, so the two can be read SIDE BY SIDE.
//
// WHAT THIS IS NOT, and what no client may render it as: an explanation. A marker on this
// response is CORRELATION IN TIME AND NOTHING MORE. A feed record says feed was directed to a
// shed, never that this animal took any of it. A vaccination record next to a movement change
// says the two happened in the same window, never that one caused the other and never that
// anything is wrong. A health case is an open record raised by a person, never a diagnosis and
// never something this module inferred from motion. The tag reports a cumulative motion counter,
// a voltage, a tag-housing temperature and a radio strength -- it classifies no behaviour and no
// condition, so nothing downstream of it may either.
//
// MONITORING BOUNDARY (migration 000196): an animal's history with a tag starts at the moment
// the tag was mapped to it. Records from before that instant belong to the animal but NOT to
// this tag's story, and returning them would put farm events beside device-bench telemetry as if
// they were one timeline. The read therefore clamps `from` up to the monitoring boundary, and a
// tag with no animal behind it has no farm activity at all -- an empty result with a reason,
// never an error.

// ActivityEventKind identifies which farm record produced an overlay marker.
type ActivityEventKind string

const (
	ActivityKindVaccination  ActivityEventKind = "vaccination"
	ActivityKindFeedGiven    ActivityEventKind = "feed_given"
	ActivityKindWeighing     ActivityEventKind = "weighing"
	ActivityKindTreatment    ActivityEventKind = "treatment"
	ActivityKindHoofTrimming ActivityEventKind = "hoof_trimming"
	ActivityKindShedMove     ActivityEventKind = "shed_move"
)

// ActivityGrain records WHAT the underlying record is actually about, so a client can never
// present a shed-grain record as an observation of one animal.
type ActivityGrain string

const (
	// GrainAnimal: the source row names this animal (goat_id).
	GrainAnimal ActivityGrain = "animal"
	// GrainShed: the source row names the SHED this animal was in. Every animal in that shed
	// shares the record; it says nothing about this one individually.
	GrainShed ActivityGrain = "shed"
	// GrainScannedIdentifier: the source row names a RAW SCANNED STRING that matches this tag's
	// id or MAC, with no animal resolution anywhere in the chain. Weighing is free-flow and
	// isolated (migration 000078 dropped weighing_observations.animal_id outright) and PC-care
	// records the same way; this module correlates by the scanned string exactly as those
	// modules store it and never resolves a scan to an animal in either direction.
	GrainScannedIdentifier ActivityGrain = "scanned_identifier"
)

// ActivityEvent is one overlay marker: a kind, an instant, a short label, and the grain of the
// record behind it.
type ActivityEvent struct {
	Kind  ActivityEventKind `json:"kind"`
	At    time.Time         `json:"at"`
	Label string            `json:"label"`
	Grain ActivityGrain     `json:"grain"`
}

// UnavailableActivityKind names a kind this deployment cannot produce, with the reason, so the
// UI hides that chip instead of showing one that can never light up.
type UnavailableActivityKind struct {
	Kind   ActivityEventKind `json:"kind"`
	Reason string            `json:"reason"`
}

// ActivityReason explains an EMPTY result structurally, so "nothing happened" and "this tag can
// have no farm activity at all" are never the same answer.
type ActivityReason string

const (
	// ActivityReasonTagNotMapped: no animal is behind this tag. Its packets are device
	// telemetry (migration 000196), so there is no farm activity to show -- not zero events for
	// an animal, no animal.
	ActivityReasonTagNotMapped ActivityReason = "tag_not_mapped_to_animal"
	// ActivityReasonMonitoringBoundaryUnknown: the tag resolves to an animal but carries no
	// mapping instant, so where the animal's history with this tag begins is unknown. Fail
	// closed rather than attribute a whole record history to a boundary we cannot state.
	ActivityReasonMonitoringBoundaryUnknown ActivityReason = "monitoring_boundary_unknown"
	// ActivityReasonWindowBeforeMonitoring: the whole requested window predates the mapping.
	ActivityReasonWindowBeforeMonitoring ActivityReason = "window_entirely_before_monitoring_start"
)

// ActivityCorrelationNote is the backend-owned copy every client renders verbatim beside the
// overlay (AGENTS.md backend-owns-labels). It exists so the claim boundary travels WITH the
// data instead of depending on each client to remember it.
const ActivityCorrelationNote = "These are farm records from the same period, shown beside the movement history for context. " +
	"A marker means the record and the movement fall in the same window -- nothing more. " +
	"It does not explain the movement, and the movement does not confirm anything about the animal."

// ActivityResponse is the response to GET /herd-signals/tags/{tag_id}/activity.
type ActivityResponse struct {
	TagID string `json:"tag_id"`
	// From/To are the EFFECTIVE window actually read, after clamping to the monitoring
	// boundary -- never the raw request values, so a client can see the clamp happened.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// MonitoringSince is the instant this tag became this animal's tag. Null for an unmapped
	// tag.
	MonitoringSince *time.Time `json:"monitoring_since"`
	// Events is ordered by At ascending. Never null: an empty array plus a Reason.
	Events []ActivityEvent `json:"events"`
	// UnavailableKinds lists kinds with no source in this deployment. Empty today; kept in the
	// contract because a client must hide a chip it can never fill, and discovering that from a
	// silently absent kind is guesswork.
	UnavailableKinds []UnavailableActivityKind `json:"unavailable_kinds"`
	// Reason is set only when Events is empty for a STRUCTURAL reason (see ActivityReason),
	// never merely because the window happened to hold no records.
	Reason *ActivityReason `json:"reason"`
	// Truncated is true when the per-request event cap was hit and the window holds more
	// records than were returned. The client must narrow the range rather than assume it has
	// the whole window.
	Truncated       bool   `json:"truncated"`
	CorrelationNote string `json:"correlation_note"`
}
