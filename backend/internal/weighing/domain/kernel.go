package domain

import "time"

// WEIGHING PHASE 2 — the time-driven kernel contract.
//
// A weighing WORK ITEM is the weighing equivalent of an obligation instance. Its
// grain is ONE `weighing_campaign_sheds` BUCKET — never one animal, never one
// campaign. One bucket has exactly one operator, so a work item has exactly one
// owner.
//
// Time grain is the Asia/Kolkata BUSINESS DAY. Every date on a work item is a
// business date string (`YYYY-MM-DD`) resolved through
// backend/internal/platform/biztime; nothing here is an instant and nothing is
// derived from `now ± N hours`.
const (
	// Disjoint work-item read buckets. Exactly one applies to a row, so
	// Calendar/Control Tower summaries can be added without double counting.
	WorkStateScheduled = "scheduled"
	WorkStateDelayed   = "delayed"
	WorkStateCompleted = "completed"
	WorkStateClosed    = "closed"
	WorkStateCanceled  = "canceled"

	// Kernel cadence events. Each has a registered consumer in
	// context/architecture/domain-event-registry.json; a producer with no consumer
	// is a silent drop.
	//
	//	day_start      -> DOWNWARD to the assigned operator only, their own buckets
	//	rolled_forward -> DOWNWARD to the assigned operator + UPWARD to leadership
	//	delayed        -> UPWARD to growth_director + ceo_internal (escalation)
	EventWorkItemDayStart      = "weighing.work_item.day_start"
	EventWorkItemRolledForward = "weighing.work_item.rolled_forward"
	EventWorkItemDelayed       = "weighing.work_item.delayed"

	// A carry-over that landed on a shed another task already covered that day.
	// NOTHING IS TRANSFERRED: the carried-over item is simply CLOSED, and the task
	// already planned for that day takes over the shed because its operator is the
	// one standing there. No work item is re-assigned and no capture is moved --
	// whatever the closed task weighed stays its own history. Routed DOWNWARD to
	// the operator whose item closed (so it does not just vanish from their day)
	// and UPWARD to leadership.
	EventWorkItemMergedOnCarryOver = "weighing.work_item.merged_on_carry_over"

	// ProcessStateGrain is the declared grain of the Calendar / Control Tower
	// weighing read model, per docs/architecture/operational-read-model-contract.md.
	// It is the WORK ITEM (one per campaign-shed bucket), not the animal and not
	// the campaign.
	ProcessStateGrain = "weighing_work_item"
)

// WorkItem is one durable weighing work item.
type WorkItem struct {
	WorkItemID          string `json:"work_item_id"`
	TenantID            string `json:"tenant_id"`
	CampaignID          string `json:"campaign_id"`
	CampaignShedID      string `json:"campaign_shed_id"`
	ParkID              string `json:"park_id"`
	OperatorUserID      string `json:"operator_user_id"`
	WeighingCategory    string `json:"weighing_category"`
	ShedLabel           string `json:"shed_label"`
	PlannedBusinessDate string `json:"planned_business_date"`
	DueBusinessDate     string `json:"due_business_date"`
	WorkState           string `json:"work_state"`
	RolledForwardCount  int    `json:"rolled_forward_count"`
	DayStartSurfacedOn  string `json:"day_start_surfaced_on,omitempty"`
}

// KernelSweepParams drives one bounded weighing kernel tick.
//
// AsOf is an instant only so the business date can be resolved from it; every
// comparison the sweep performs is on the resolved Asia/Kolkata business DATE.
type KernelSweepParams struct {
	TenantID  string
	AsOf      time.Time
	ChunkSize int
	MaxChunks int
}

// KernelSweepResult reports what one bounded tick did. Counts are exact for the
// rows this tick claimed, not an estimate.
type KernelSweepResult struct {
	BusinessDate       string `json:"business_date"`
	ReconciledTerminal int    `json:"reconciled_terminal"`
	// MergedOnCarryOver counts carry-overs CLOSED because the task already planned
	// for that day covers the same (park, shed, date). Nothing is re-assigned.
	MergedOnCarryOver int `json:"merged_on_carry_over"`
	RolledForward     int `json:"rolled_forward"`
	MarkedDelayed     int `json:"marked_delayed"`
	DayStartSurfaced  int `json:"day_start_surfaced"`
	// FastingGatedWorkItems counts work items pushed to TOMORROW because their
	// campaign's feed & water removal task was never submitted before the
	// midnight deadline (maintainer decision 2026-09-03, domain/fasting.go).
	// FastingTasksRolled counts the fasting rows re-armed for the next evening.
	FastingGatedWorkItems int  `json:"fasting_gated_work_items"`
	FastingTasksRolled    int  `json:"fasting_tasks_rolled"`
	CadenceEvents         int  `json:"cadence_events"`
	Truncated             bool `json:"truncated"`
}

// WorkItemBucket is one bucket named inside a cadence event payload. Every field
// is READ by the notification consumer; nothing is accept-and-discard.
type WorkItemBucket struct {
	CampaignShedID      string `json:"campaign_shed_id"`
	ShedID              string `json:"shed_id"`
	ShedLabel           string `json:"shed_label"`
	PlannedBusinessDate string `json:"planned_business_date"`
	DueBusinessDate     string `json:"due_business_date"`
}

// WorkItemCadencePayload is the single payload shape for all three cadence
// events. Recipients are resolved by the consumer from role grants and from the
// assignment-row operator id carried here; no name, phone, or token is ever in a
// payload.
type WorkItemCadencePayload struct {
	TenantID     string           `json:"tenant_id"`
	CampaignID   string           `json:"campaign_id"`
	ParkID       string           `json:"park_id"`
	OperatorID   string           `json:"operator_id"`
	BusinessDate string           `json:"business_date"`
	Buckets      []WorkItemBucket `json:"buckets"`
}

// ProcessStateDayMarker is ONE Calendar day marker. It is a dot-grain marker
// (does weighing work exist on this business day, and is any of it delayed) — it
// is deliberately NOT the day's rows, so the Calendar grid never fetches a
// day's work items to draw itself.
type ProcessStateDayMarker struct {
	BusinessDate string `json:"business_date"`
	OpenCount    int    `json:"open_count"`
	DelayedCount int    `json:"delayed_count"`
}

// ProcessStateSummary is a WHOLE-FILTER aggregate over every matching work item,
// never over one page. The five state buckets are DISJOINT and sum to Total.
// OpenTotal is the explicitly-named union of Scheduled + Delayed.
type ProcessStateSummary struct {
	Scheduled int `json:"scheduled"`
	Delayed   int `json:"delayed"`
	Completed int `json:"completed"`
	Closed    int `json:"closed"`
	Canceled  int `json:"canceled"`
	OpenTotal int `json:"open_total"`
	Total     int `json:"total"`
}

// ProcessState is the weighing binding for Calendar (day markers) and Control
// Tower (gap summary). Grain is declared on the wire so a renderer cannot guess.
type ProcessState struct {
	Grain            string                  `json:"grain"`
	FromBusinessDate string                  `json:"from_business_date"`
	ToBusinessDate   string                  `json:"to_business_date"`
	DayMarkers       []ProcessStateDayMarker `json:"day_markers"`
	Summary          ProcessStateSummary     `json:"summary"`
}

// ---------------------------------------------------------------------------
// Per-shed rework digest (maintainer decision 2026-08-03: batch rework pushes).
// ---------------------------------------------------------------------------

// ReworkDigestSweepParams drives one bounded per-shed rework-digest tick.
//
// QuietWindow is the debounce: a bucket is only digested once no NEW bounce has
// landed on it for this long, so a verifier still working through the shed does not
// fire a push after every verdict. MaxAge is the starvation cap: a verifier who
// keeps rejecting steadily would otherwise hold the digest open forever, so a bucket
// whose OLDEST un-notified bounce is older than MaxAge is flushed regardless of how
// recently the newest one landed.
//
// NamedLimit bounds how many animals the push body may name. A shed can carry fifty
// bounced captures; a notification body must never render fifty tags.
type ReworkDigestSweepParams struct {
	TenantID    string
	AsOf        time.Time
	QuietWindow time.Duration
	MaxAge      time.Duration
	NamedLimit  int
	ChunkSize   int
	MaxChunks   int
}

// ReworkDigestSweepResult reports what one bounded tick actually claimed. Counts are
// exact for the rows this tick stamped, never an estimate.
type ReworkDigestSweepResult struct {
	DigestsEmitted    int  `json:"digests_emitted"`
	ObservationsNamed int  `json:"observations_named"`
	Truncated         bool `json:"truncated"`
}

// ReworkDigestItem is one bounced capture named inside a digest payload.
//
// Weighing is free-flow: ScannedIdentifier IS the animal's identity and is never
// resolved to a goat. WeightKg is the weight the verifier rejected, which is what
// lets the operator tell two captures of the same tag apart.
type ReworkDigestItem struct {
	ObservationID     string  `json:"observation_id"`
	ScannedIdentifier string  `json:"scanned_identifier"`
	WeightKg          float64 `json:"weight_kg"`
}

// ReworkDigestPayload is the payload of weighing.observation.rework_digest: ONE
// event per bucket per flush, naming the animals the operator has to re-capture.
//
// Items is CAPPED at ReworkDigestSweepParams.NamedLimit; TotalCount is the true size
// of the flush, so the consumer can say "and N more" honestly without the payload (or
// the push body) growing with the shed.
type ReworkDigestPayload struct {
	TenantID       string             `json:"tenant_id"`
	CampaignID     string             `json:"campaign_id"`
	CampaignShedID string             `json:"campaign_shed_id"`
	ParkID         string             `json:"park_id"`
	ShedID         string             `json:"shed_id"`
	ShedLabel      string             `json:"shed_label"`
	OperatorID     string             `json:"operator_id"`
	Items          []ReworkDigestItem `json:"items"`
	TotalCount     int                `json:"total_count"`
	Reason         string             `json:"reason,omitempty"`
	DecidedAt      string             `json:"decided_at"`
}
