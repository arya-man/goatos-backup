package domain

import (
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// ---------------------------------------------------------------------------
// Live-herd feed projection
// ---------------------------------------------------------------------------
//
// MAINTAINER CONTEXT (2026-07-19): this farm does NOT run a physical counting
// workflow. The live `goats` table IS the count; the only thing that changes a
// shed's head count is a shifting. This projection therefore starts from the
// live herd and applies the movements that are APPROVED BUT NOT YET EXECUTED.
//
// It is deliberately PARALLEL to, not a replacement for, the anchor/snapshot
// machinery (count_base_anchors, count_projection_snapshots, ProjectedCountFor,
// CountAsOf). That path replays movements on top of a physically-counted anchor
// and is still used by the counts-source import and parity tooling. Nothing here
// reads or writes those tables.

// Feed shifting projection timing.
//
// MAINTAINER DECISION (2026-07-27, SUPERSEDING the priority-based lead-day rule):
// a shifting is a pending feed input the moment a park head AUTHORIZES it. There
// is NO lead time and NO priority branch. An authorized-but-unexecuted movement --
// normal or high priority alike -- counts toward EVERY feed day from its
// authorization business date onward, and stops counting only once it is APPLIED
// (verifier-approved), at which point the animals already sit in the destination
// shed in the live `goats` table and the projection must not add them a second
// time (see the adapter's event_status filter, which counts 'authorized' and
// 'pending_verification' but never 'applied').
//
// The retired rule gave a normal movement a two-day lead and a high-priority one
// a one-day lead. This farm completes a normal shifting the day after approval and
// a high-priority one same-day, so the feed team wants the destination ration ready
// as soon as the move is authorized rather than a configured number of days later:
// tomorrow's feed, packed today, must reflect every move still pending now.
//
// These helpers are the pure-Go spec of the rule the adapter SQL implements. They
// have no production caller of their own; the SQL is the production path. Keeping
// them in lock-step with the SQL is what a domain unit test can prove without a DB.

// FeedShiftingEffectiveBusinessDate is the business date from which an authorized
// shifting begins contributing to the destination shed's feed: the authorization
// day itself, in Asia/Kolkata. Per AGENTS.md, UTC never defines a Goat OS business
// day -- an approval stamped 2026-07-19T20:00:00Z is already 2026-07-20 in India.
func FeedShiftingEffectiveBusinessDate(approvedAt time.Time) time.Time {
	return biztime.BusinessDayStart(approvedAt)
}

// FeedShiftingCountsToward reports whether an authorized-but-unexecuted shifting
// contributes to the projected counts for feed day targetDate.
//
// The comparison is <=, so a movement counts on its authorization day and every
// day after until it is executed. That <= is load-bearing: an OVERDUE movement --
// one still unexecuted long after it was authorized -- keeps contributing rather
// than silently dropping out and quietly de-feeding a destination shed whose
// animals are still expected. Overdue rows are surfaced, never dropped: see
// FeedShiftingIsOverdue.
func FeedShiftingCountsToward(approvedAt time.Time, targetDate time.Time) bool {
	effective := FeedShiftingEffectiveBusinessDate(approvedAt)
	target := biztime.BusinessDayStart(targetDate)
	return !effective.After(target)
}

// FeedShiftingIsOverdue reports whether an authorized-but-unexecuted shifting has
// been pending SINCE BEFORE the packing day and still has not physically happened.
//
// Feed for day D is packed on D-1, so the packing day is targetDate - 1 business
// day. A movement authorized ON the packing day or later is expected to be executed
// during that same packing day and is NOT overdue; only one authorized on an EARLIER
// day -- pending across at least one full cycle -- is flagged. That keeps the signal
// meaningful instead of lighting up for every freshly authorized move (which under
// a zero lead would otherwise all read as "assumed but not yet physical"). An overdue
// movement is by construction still counting, since its effective date precedes the
// packing day, which precedes the feed day.
func FeedShiftingIsOverdue(approvedAt time.Time, targetDate time.Time) bool {
	effective := FeedShiftingEffectiveBusinessDate(approvedAt)
	packingDay := biztime.BusinessDayStart(targetDate).AddDate(0, 0, -1)
	return effective.Before(packingDay)
}

// FeedProjectedCountQuery filters and pages the live-herd feed projection.
type FeedProjectedCountQuery struct {
	TenantID string
	// TargetDate is the feed day D being planned. Interpreted in the Goat OS
	// business calendar; the time-of-day component is discarded.
	TargetDate time.Time
	// LifecycleStatus defaults to "alive" -- the same live-herd population the
	// Counts Breakdown census pins.
	LifecycleStatus *string
	ParkID          *string
	ShedID          *string
	// Breed narrows the projection to one breed. Matched against BOTH the live
	// herd and the pending/delta legs using the same normalization
	// (feedGrainNormSQL) the grouping key already applies, so "Beetal" and
	// "beetal " match the same grain instead of silently returning every breed.
	Breed *string
	// ShedIDs restricts the projection to an explicit SET of sheds, ANDed with
	// ShedID when both are given. Empty means "no shed-set filter".
	//
	// It exists so a consumer that has already paged a bounded list of sheds can
	// fetch every grain for that page in ONE round trip. The single-valued ShedID
	// filter above cannot serve that caller: calling it once per shed inside a
	// loop is exactly the N+1 fan-out the scale rules ban, and widening the read
	// to the whole park instead would make the consumer's page boundary
	// meaningless. The feed-direction generator is the first such consumer -- it
	// pages by shed precisely so a shed's grains are never split across two
	// pages, which would silently halve that shed's session total.
	ShedIDs []string
	Limit   int32
	Offset  int32
	// StableOrder selects a sort key that never changes between two reads of the same grain
	// (park/shed/stage/breed/sex identity only), instead of the default head-count-DESC order.
	//
	// It exists for multi-page CONSISTENT-SNAPSHOT drains (feeddirection's ProjectedGrainsForSheds):
	// the default order's leading term is GREATEST(current_head_count + pending_delta, 0), which is
	// exactly the value a concurrent shifting-event authorization/completion can change WHILE a
	// caller is mid-drain across several OFFSET pages. A grain whose count moves can cross a page
	// boundary between two reads -- appearing on neither page (omission) or on both (duplication) --
	// while the drain reports success. Sorting by the grain's own identity columns instead removes
	// that dependency: an existing grain's park/shed/stage/breed/sex never change out from under a
	// movement (a movement changes COUNTS via the delta CTE, not which grain rows exist), so the
	// same grain lands at the same offset on every page of the same drain regardless of concurrent
	// writes elsewhere in the projection.
	StableOrder bool
}

// FeedProjectedCountRow is one projected shed grain:
// park x shed x partition x management stage x breed x sex.
type FeedProjectedCountRow struct {
	ParkID          *string `json:"park_id"`
	ParkLabel       string  `json:"park_label"`
	ShedID          *string `json:"shed_id"`
	ShedLabel       string  `json:"shed_label"`
	PartitionLabel  string  `json:"partition_label"`
	ManagementStage string  `json:"management_stage"`
	Breed           string  `json:"breed"`
	Sex             string  `json:"sex"`
	// CurrentHeadCount is the live herd today: canonical goats rows, no replay.
	CurrentHeadCount int64 `json:"current_head_count"`
	// PendingDelta is the RAW signed sum of approved-but-unexecuted movements
	// that are feed-effective on or before the target date. It is reported
	// unclamped so a caller can see the full movement pressure on the grain even
	// when ProjectedHeadCount was floored -- see Clamped.
	PendingDelta int64 `json:"pending_delta"`
	// ProjectedHeadCount is max(CurrentHeadCount+PendingDelta, 0).
	ProjectedHeadCount int64 `json:"projected_head_count"`
	// Clamped is true when CurrentHeadCount+PendingDelta went negative and was
	// floored at zero. A negative projection is never silently rendered as a
	// small positive number: it means the recorded movements claim to remove
	// more animals than the source grain holds, which is a real data problem the
	// operator has to see rather than an arithmetic result to round away.
	Clamped bool `json:"clamped"`
	// OverduePending is true when at least one contributing movement was
	// authorized before the packing day (feed day - 1) and still has not been
	// executed -- i.e. it has been pending across at least one full cycle.
	OverduePending bool `json:"overdue_pending"`
	// OverdueShiftingEventIDs names those movements so the UI can link to them.
	OverdueShiftingEventIDs []string `json:"overdue_shifting_event_ids"`
}

// FeedProjectedCounts is one page of projected shed grains plus the
// page-independent grain-row total.
type FeedProjectedCounts struct {
	Items []FeedProjectedCountRow `json:"items"`
	// TotalRows is the number of grain rows in the WHOLE filtered result,
	// computed by a window function over the full combined set and therefore
	// invariant to Limit/Offset.
	TotalRows  int64     `json:"total_rows"`
	TargetDate time.Time `json:"target_date"`
	// ProjectedAt is when this projection was computed. It is not a freshness
	// gate: this read is served live from canonical SQL, so it is always current.
	ProjectedAt time.Time `json:"projected_at"`
}
