package domain

import (
	"strings"
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

// Feed shifting lead days: how long after a park head APPROVES a movement the
// feed plan should treat the animals as already standing in the destination
// shed. The animals have not moved yet -- this is the operational lead time the
// feed team works to, so the destination shed's ration is ready when they
// arrive rather than a day late.
//
// These two constants are the ONLY definition of the rule. The SQL does not
// re-derive them: Repository.ProjectedShedCountsForFeed binds them as query
// parameters, so the statement branches only on the literal priority string and
// the business numbers cannot drift between Go and SQL.
const (
	// FeedShiftingEmergencyLeadDays applies to priority = 'emergency'. An
	// emergency movement is executed fast, so feed follows one day behind
	// approval.
	FeedShiftingEmergencyLeadDays = 1
	// FeedShiftingStandardLeadDays applies to priority = 'normal' and 'high'.
	// 'high' is a queue-order signal, not an execution-speed signal, so it takes
	// the same two-day lead as 'normal'.
	FeedShiftingStandardLeadDays = 2
)

// FeedShiftingLeadDays returns the feed lead time for a shifting priority.
//
// Unknown/blank priorities fall back to the STANDARD lead rather than the
// emergency one. Falling back to the shorter lead would make an unrecognized
// priority pull animals into the destination shed's ration a day early, which
// is the direction that feeds the wrong shed; the longer lead merely delays a
// projection that a later day picks up anyway.
func FeedShiftingLeadDays(priority string) int {
	if strings.EqualFold(strings.TrimSpace(priority), "emergency") {
		return FeedShiftingEmergencyLeadDays
	}
	return FeedShiftingStandardLeadDays
}

// FeedEffectiveBusinessDate returns the business date on which an approved
// shifting starts counting toward the destination shed's feed.
//
// approvedAt is an absolute instant; the returned value is a business-day start
// in Asia/Kolkata. Per AGENTS.md, UTC never defines a Goat OS business day: an
// approval stamped 2026-07-19T20:00:00Z is already 2026-07-20 in India, and a
// UTC-derived date would feed the destination shed a day late.
func FeedEffectiveBusinessDate(approvedAt time.Time, priority string) time.Time {
	return biztime.BusinessDayStart(approvedAt).AddDate(0, 0, FeedShiftingLeadDays(priority))
}

// FeedShiftingCountsToward reports whether an approved-but-unexecuted shifting
// contributes to the projected counts for feed day targetDate.
//
// The comparison is <=, not ==, and that is load-bearing. An OVERDUE movement --
// one whose feed-effective date has passed without the movement being executed --
// keeps contributing to every later day automatically. Under an == rule it would
// silently drop out of the projection the day after it came due, and the
// destination shed would quietly stop being fed for animals that are still on
// their way. Overdue rows are surfaced, never dropped: see FeedShiftingIsOverdue.
func FeedShiftingCountsToward(approvedAt time.Time, priority string, targetDate time.Time) bool {
	effective := FeedEffectiveBusinessDate(approvedAt, priority)
	target := biztime.BusinessDayStart(targetDate)
	return !effective.After(target)
}

// FeedShiftingIsOverdue reports whether a contributing shifting came due for
// feed BEFORE the target day and still has not been executed. The UI surfaces
// these so an operator can see that a movement the feed plan already assumes
// has not physically happened.
func FeedShiftingIsOverdue(approvedAt time.Time, priority string, targetDate time.Time) bool {
	effective := FeedEffectiveBusinessDate(approvedAt, priority)
	target := biztime.BusinessDayStart(targetDate)
	return effective.Before(target)
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
}

// FeedProjectedCountRow is one projected shed grain:
// park x shed x management stage x breed x sex.
type FeedProjectedCountRow struct {
	ParkID          *string `json:"park_id"`
	ParkLabel       string  `json:"park_label"`
	ShedID          *string `json:"shed_id"`
	ShedLabel       string  `json:"shed_label"`
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
	// OverduePending is true when at least one contributing movement came due
	// before the target date and still has not been executed.
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
