package domain

import "time"

// ReviewEventType is a fixed, closed vocabulary of client telemetry events proving (or disproving)
// that a verifier actually watched a proof video rather than rubber-stamping the verdict. Scope is
// the verifier role ONLY -- this is not a generic activity log for every role in the product.
type ReviewEventType string

const (
	ReviewEventQueueOpened       ReviewEventType = "queue_opened"
	ReviewEventItemOpened        ReviewEventType = "item_opened"
	ReviewEventVideoPlay         ReviewEventType = "video_play"
	ReviewEventVideoPause        ReviewEventType = "video_pause"
	ReviewEventVideoSeekAttempt  ReviewEventType = "video_seek_attempt"
	ReviewEventVideoEnded        ReviewEventType = "video_ended"
	ReviewEventProofSwitched     ReviewEventType = "proof_switched"
	ReviewEventFullscreenToggled ReviewEventType = "fullscreen_toggled"
	ReviewEventVerdictRecorded   ReviewEventType = "verdict_recorded"
)

// ReviewEventTypes is the closed set the write path validates against. Keep in sync with the
// migration 000116 CHECK constraint -- both exist because Postgres is the last line of defense
// against a bypassed app layer, not because the list is meant to drift between them.
var ReviewEventTypes = map[ReviewEventType]bool{
	ReviewEventQueueOpened:       true,
	ReviewEventItemOpened:        true,
	ReviewEventVideoPlay:         true,
	ReviewEventVideoPause:        true,
	ReviewEventVideoSeekAttempt:  true,
	ReviewEventVideoEnded:        true,
	ReviewEventProofSwitched:     true,
	ReviewEventFullscreenToggled: true,
	ReviewEventVerdictRecorded:   true,
}

// ReviewEventPayload is the client-declared numeric detail for one event. Fields are optional and
// event-type-specific (e.g. VideoPositionMs/VideoDurationMs on play/pause/seek/ended). Everything
// here is untrusted client input -- the derived facts computation treats it as a hint, never as the
// sole source of watch-time truth (see adapters/postgres/review_events.go: watch time is derived from the SEQUENCE of
// play/pause/ended events' occurred_at, not from a client-reported duration).
type ReviewEventPayload struct {
	VideoPositionMs *int64  `json:"video_position_ms,omitempty"`
	VideoDurationMs *int64  `json:"video_duration_ms,omitempty"`
	SeekFromMs      *int64  `json:"seek_from_ms,omitempty"`
	SeekToMs        *int64  `json:"seek_to_ms,omitempty"`
	Verdict         *string `json:"verdict,omitempty"`
	// Category/ParkID/ShedID are the QUEUE-SCOPED attribution fields. A queue_opened event fires
	// before any item exists (landing on the queue screen), so it has no item_id to hang scope on
	// -- see migration 000119. Category is REQUIRED on a queue_opened event so a CEO-facing
	// "queue opened -> item opened -> verdict recorded" funnel can still attribute it to a
	// category/park/shed dimension without a fake item_id. ParkID/ShedID are optional (the queue
	// view may not be park/shed-scoped).
	Category *string `json:"category,omitempty"`
	ParkID   *string `json:"park_id,omitempty"`
	ShedID   *string `json:"shed_id,omitempty"`
	// Status is informational client-declared queue-state context (e.g. "pending"); it carries no
	// server-side meaning and is not validated -- kept only so a queue_opened payload naming it
	// does not need special-case stripping.
	Status *string `json:"status,omitempty"`
}

// ReviewEvent is one client-emitted review-analytics event, as ingested by
// POST /verification/review-events.
type ReviewEvent struct {
	TenantID string
	// ItemID is nil for queue-scoped events (event_type = queue_opened, migration 000119) and
	// required/non-nil for every item-scoped event type. See review_events.go's validation for
	// the enforcement and the DB CHECK constraint for the belt-and-suspenders half.
	ItemID        *string
	ProofID       *string
	ActorID       string
	SessionID     string
	EventType     ReviewEventType
	OccurredAt    time.Time
	Payload       ReviewEventPayload
	ClientEventID string // client-minted UUID; the idempotency key for this one event.
}

// ReviewEventBatch is one flush from the browser.
type ReviewEventBatch struct {
	TenantID string
	ActorID  string
	Events   []ReviewEvent
}

// ItemReviewFacts is the derived per-(item,actor) integrity signal computed from the raw event
// stream -- see adapters/postgres/review_events.go for the computation and why it is read-time (bounded by one item's
// event count, not a whole-table scan).
type ItemReviewFacts struct {
	ItemID               string
	ActorID              string
	ProofDurationMs      int64
	WatchedDistinctMs    int64
	WatchFraction        float64 // WatchedDistinctMs / ProofDurationMs, clamped to [0,1]; 0 when duration is unknown.
	PlayCount            int
	PauseCount           int
	SeekAttemptCount     int
	ItemOpenedAt         *time.Time
	VerdictRecordedAt    *time.Time
	TimeToVerdictSeconds *float64
	WatchedFull          bool // WatchFraction >= the configured threshold (see adapters/postgres/review_events.go WatchedFullThreshold).
}

// ItemWatchState is the LIGHTWEIGHT per-item watch summary shown on the queue table's "Watch"
// column (never per-actor, unlike ItemReviewFacts above, which is deliberately kept off the hot
// list path -- see EvidenceAvailabilityChecker's doc comment on why per-row telemetry stats do not
// belong on a page read). It is a single max(video_position_ms)/max(video_duration_ms) aggregate
// per item_id, not the interval-merge integrity computation ItemReviewFacts does for the verdict
// detail view -- that is deliberately too expensive to run per row on a 20-row page.
type ItemWatchState struct {
	ItemID string
	// Opened is true when at least one item_opened event exists for this item -- "not opened" in
	// the UI when false and no play/duration facts exist either.
	Opened bool
	// PercentWatched is nil when no proof duration was ever reported (telemetry absent or the
	// verifier never played the video), 0-100 clamped when a duration is known.
	PercentWatched *int
}
