package app

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

const (
	// maxActivityRangeDays bounds the window a single overlay read may span. The movement
	// history it overlays is itself bucket-capped at 2000 buckets, so a request for a year of
	// markers is asking for something no chart can draw.
	maxActivityRangeDays = 31
	// maxActivityEvents bounds the markers one response may carry. Beyond this the client is
	// told the window is truncated and must narrow the range -- an overlay that silently drops
	// half its markers is worse than one that says it did.
	maxActivityEvents = 500
	// correlationWindowHours is the span of the motion-delta windows to sum around each activity event
	// for correlation analysis (2 hours before and 2 hours after).
	correlationWindowHours = 2
	// correlationBucketSeconds is the grain at which we fetch motion windows for correlation.
	// We use 300-second (5-minute) buckets for the 2h windows around activity events.
	// This provides adequate granularity for correlation analysis while keeping the query efficient.
	// 5-minute buckets: 2h = 120 minutes = 24 buckets. Same grain as the mock uses (motion_h = 24 5-min buckets).
	correlationBucketSeconds = 300
)

// GetTagActivity returns the farm records that may be drawn beside one tag's movement history.
//
// THE BOUNDARY THIS FUNCTION EXISTS TO ENFORCE (migration 000197): an animal's history with a
// tag starts at the instant the tag was mapped to it. `from` is clamped UP to that instant
// before a single record is read, so no farm event from the tag's device-bench period can ever
// reach the response. A tag with no animal behind it returns an empty result with a reason --
// never an error, and never a zero pretending to be an observation.
//
// WHAT THE RESULT MEANS: co-occurrence in time. Nothing here explains a movement change, and no
// movement here confirms anything about a record. See domain.ActivityCorrelationNote, which is
// returned with every response so the boundary travels with the data.
func (s *Service) GetTagActivity(ctx context.Context, actor domain.Actor, tagID, from, to string) (domain.ActivityResponse, error) {
	if actor.TenantID == "" {
		return domain.ActivityResponse{}, fmt.Errorf("actor tenant_id required")
	}

	fromTime, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return domain.ActivityResponse{}, fmt.Errorf("invalid from timestamp: %w", domain.ErrValidation)
	}
	toTime, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return domain.ActivityResponse{}, fmt.Errorf("invalid to timestamp: %w", domain.ErrValidation)
	}
	if !toTime.After(fromTime) {
		return domain.ActivityResponse{}, fmt.Errorf("to must be after from: %w", domain.ErrValidation)
	}
	if toTime.Sub(fromTime) > maxActivityRangeDays*24*time.Hour {
		return domain.ActivityResponse{}, fmt.Errorf("range spans more than %d days: %w", maxActivityRangeDays, domain.ErrValidation)
	}

	scope, err := s.repo.GetTagActivityScope(ctx, actor.TenantID, tagID)
	if err != nil {
		s.log.Error("failed to resolve tag activity scope", "tag_id", tagID, "error", err)
		return domain.ActivityResponse{}, fmt.Errorf("resolve tag activity scope failed: %w", err)
	}
	if scope == nil {
		return domain.ActivityResponse{}, fmt.Errorf("tag %q: %w", tagID, domain.ErrTagNotFound)
	}

	resp := domain.ActivityResponse{
		TagID:            tagID,
		From:             fromTime,
		To:               toTime,
		MonitoringSince:  scope.MonitoringSince,
		Events:           []domain.ActivityEvent{},
		UnavailableKinds: []domain.UnavailableActivityKind{},
		CorrelationNote:  domain.ActivityCorrelationNote,
	}

	// No animal behind the tag: its packets are device telemetry, so there is no farm activity
	// to show -- not "zero events for this animal", but "there is no animal". Empty + reason.
	if scope.GoatID == "" {
		reason := domain.ActivityReasonTagNotMapped
		resp.Reason = &reason
		return resp, nil
	}
	// Mapped, but we cannot say WHEN the mapping happened. Fail closed: attributing a whole
	// record history to a boundary we cannot state is exactly what 000197 forbids.
	if scope.MonitoringSince == nil {
		reason := domain.ActivityReasonMonitoringBoundaryUnknown
		resp.Reason = &reason
		return resp, nil
	}

	// THE CLAMP. Everything before this instant is the tag's device period.
	effectiveFrom := fromTime
	if scope.MonitoringSince.After(effectiveFrom) {
		effectiveFrom = *scope.MonitoringSince
	}
	resp.From = effectiveFrom
	if !toTime.After(effectiveFrom) {
		reason := domain.ActivityReasonWindowBeforeMonitoring
		resp.Reason = &reason
		return resp, nil
	}

	events, err := s.repo.ListFarmActivity(ctx, actor.TenantID, *scope, effectiveFrom, toTime, maxActivityEvents)
	if err != nil {
		s.log.Error("failed to list farm activity", "tag_id", tagID, "error", err)
		return domain.ActivityResponse{}, fmt.Errorf("list farm activity failed: %w", err)
	}
	if len(events) > maxActivityEvents {
		events = events[:maxActivityEvents]
		resp.Truncated = true
	}

	// Compute activity correlations for each event (2h before and 2h after motion deltas)
	if len(events) > 0 {
		for i := range events {
			s.computeEventCorrelation(ctx, actor.TenantID, scope.TagID, &events[i])
		}
		resp.Events = events
	}
	return resp, nil
}

// computeEventCorrelation computes motion deltas in the 2h windows before and after an activity event.
//
// CORRELATION WINDOW GRAIN AND GAP HANDLING:
// We fetch herd_signal_activity_windows at 5-minute (300s) granularity for the 2h windows.
// This provides 24 buckets per side (24 × 5 min = 120 min = 2h), matching the grain the mock uses.
// We chose 5-minute buckets because:
// - They provide adequate temporal resolution to see movement response to farm activities
// - 24 buckets per side is statistically meaningful for correlation
// - The 300-second tier is indexed in herd_signal_activity_windows
// - Coarser tiers (hourly/6-hourly) would blur within-window responses to feeding/vaccination
//
// Gap handling is CRITICAL and the reason these fields may be nil:
//   - If ANY bucket in a window carries gap_delta=true (a reconnect after a reception gap),
//     the sum includes a value whose time distribution is unknown. We cannot trust the total.
//   - If ANY bucket in a window is IsGap=true (period with no packets), we cannot know whether
//     motion occurred but was not transmitted.
//   - We mark the window INCOMPLETE if either condition holds, and return nil values rather
//     than presenting a partial truth as fact.
func (s *Service) computeEventCorrelation(ctx context.Context, tenantID, tagID string, event *domain.ActivityEvent) {
	// Fetch 2h windows: 2h before the event and 2h after the event.
	before := event.At.Add(time.Duration(-correlationWindowHours) * time.Hour)
	after := event.At.Add(time.Duration(correlationWindowHours) * time.Hour)

	windows, err := s.repo.ListActivityWindows(ctx, tenantID, tagID, before, after, correlationBucketSeconds)
	if err != nil {
		s.log.Debug("failed to list activity windows for correlation", "tag_id", tagID, "event_at", event.At, "error", err)
		// Leave correlation fields as nil if we can't fetch the data
		return
	}

	// Split windows into before and after halves.
	var beforeWindows, afterWindows []domain.ActivityWindow
	for _, w := range windows {
		if w.BucketStart.Add(time.Duration(w.BucketSeconds)*time.Second).Before(event.At) || w.BucketStart.Equal(event.At) {
			// Bucket ends before or at the event time -> belongs to before window
			beforeWindows = append(beforeWindows, w)
		} else {
			// Bucket starts after the event time -> belongs to after window
			afterWindows = append(afterWindows, w)
		}
	}

	// Compute before correlation.
	beforeDelta, beforeIncomplete := sumMotionDeltas(beforeWindows)

	// Compute after correlation.
	afterDelta, afterIncomplete := sumMotionDeltas(afterWindows)

	// Set the correlation fields.
	if beforeDelta != nil {
		event.MotionDeltaBefore2h = beforeDelta
		event.BeforeWindowIncomplete = beforeIncomplete
	}
	if afterDelta != nil {
		event.MotionDeltaAfter2h = afterDelta
		event.AfterWindowIncomplete = afterIncomplete
	}

	// Compute percentage change only if both windows have valid data.
	if beforeDelta != nil && afterDelta != nil && !beforeIncomplete && !afterIncomplete {
		change := computeMotionChangePercent(*beforeDelta, *afterDelta)
		event.MotionChangePercent = &change
	}
}

// sumMotionDeltas sums the motion_delta values in a set of activity windows.
// Returns (sum, isIncomplete). isIncomplete is true if any bucket has a gap or gap_delta.
// Returns (nil, _) if the window list is empty.
func sumMotionDeltas(windows []domain.ActivityWindow) (*int64, bool) {
	if len(windows) == 0 {
		return nil, false
	}

	var sum int64
	incomplete := false
	for _, w := range windows {
		// A gap means no packets were received in this bucket.
		if w.IsGap {
			incomplete = true
		}
		// A gap_delta means this bucket carries motion from AFTER a reconnect, distributed
		// over an unknown time span. Never smear it into a normal sum.
		if w.GapDelta {
			incomplete = true
		}
		sum += w.MotionDelta
	}

	return &sum, incomplete
}

// computeMotionChangePercent computes the percentage change from before to after.
// If before is 0 and after > 0, returns 100.
// If before is 0 and after == 0, returns 0.
// If before is 0 and after < 0, returns -100.
// Otherwise returns ((after - before) / before) × 100, rounded to nearest int64.
func computeMotionChangePercent(before, after int64) int64 {
	if before == 0 {
		if after > 0 {
			return 100
		} else if after < 0 {
			return -100
		}
		return 0
	}
	// Compute percentage: (after - before) / before × 100
	// Use floating point for the division to avoid loss of precision, then round.
	pct := float64(after-before) / float64(before) * 100
	return int64(pct)
}
