package domain

import (
	"fmt"
	"time"
)

// MotionDelta computes the motion delta between current and previous motion count.
// If current < previous (counter reset), returns 0 and sets wasReset.
func MotionDelta(current, previous *int64) (delta int64, wasReset bool) {
	if current == nil || previous == nil {
		return 0, false
	}
	if *current < *previous {
		// Counter reset detected
		return 0, true
	}
	return *current - *previous, false
}

// MovementStateFromDelta determines movement state from motion delta and time window.
func MovementStateFromDelta(delta int64, windowSeconds int, thresholds Thresholds) string {
	switch {
	case delta >= thresholds.MotionActiveDelta:
		return "moving"
	case delta >= thresholds.MotionLowDelta:
		return "low"
	case delta >= thresholds.MotionQuietDelta:
		return "quiet"
	case delta == 0:
		return "not_moving"
	default:
		return "unknown"
	}
}

// SignalStateFromRSSI determines signal state based on RSSI reading.
func SignalStateFromRSSI(rssi *int16, avgRSSI *float64, thresholds Thresholds) string {
	if rssi == nil && avgRSSI == nil {
		return "unknown"
	}

	// Prefer individual RSSI if available
	if rssi != nil {
		if *rssi >= thresholds.SignalStrong {
			return "strong"
		} else if *rssi <= thresholds.SignalWeak {
			return "weak"
		}
		return "ok"
	}

	// Fall back to average
	if avgRSSI != nil {
		if *avgRSSI <= float64(thresholds.SignalAvgWeak) {
			return "weak"
		}
		return "ok"
	}

	return "unknown"
}

// BatteryStateFromMillivolts determines battery state.
func BatteryStateFromMillivolts(batteryMV *int, thresholds Thresholds) string {
	if batteryMV == nil {
		return "unknown"
	}
	if *batteryMV < thresholds.BatteryLowMV {
		return "low"
	}
	return "ok"
}

// PatternStateFromHistory determines pattern state based on recent activity history.
// Requires 24h window of non-gap buckets to compute baseline (p75 of motion_delta).
// Classifies animal movement pattern over time: no_movement, quiet_watch, inactive,
// missing_signal, spike, recovered.
//
// previousPattern is the tag's pattern_state BEFORE this ingest (as currently stored
// in herd_signal_tag_latest). It is the only signal that lets "recovered" be
// distinguished from an ordinary active reading: recovered means activity resumed
// after this tag was previously classified quiet_watch/inactive/missing_signal, not
// merely "currently moving".
func PatternStateFromHistory(
	currentDelta int64,
	lastPacketAt *time.Time,
	now time.Time,
	recentWindows []ActivityWindow, // 24h history, all tiers
	previousPattern string,
	thresholds Thresholds,
) string {
	if lastPacketAt == nil {
		return string(PatternMissingSignal)
	}

	timeSincePacket := now.Sub(*lastPacketAt).Minutes()
	if timeSincePacket > float64(thresholds.MissingSignalMinutes) {
		return string(PatternMissingSignal)
	}

	// Compute baseline (p75) from 24h non-gap windows
	baseline := Baseline75(recentWindows)

	// Check for spike (current far above baseline)
	if baseline > 0 && float64(currentDelta) > float64(baseline)*thresholds.SpikeThresholdMultiplier {
		return string(PatternSpike)
	}

	// Recovered: this tag was previously watched as quiet/inactive/missing and activity has now
	// resumed above the quiet threshold. Must be checked before the sustained-quiet windows below,
	// since a single active packet after a long quiet tail should read as "recovered", not
	// re-classify into quiet_watch/inactive off the stale tail.
	wasWatched := previousPattern == string(PatternQuietWatch) ||
		previousPattern == string(PatternInactive) ||
		previousPattern == string(PatternMissingSignal)
	if wasWatched && currentDelta >= thresholds.MotionLowDelta {
		return string(PatternRecovered)
	}

	// Check sustained quiet/inactive periods
	quietDuration := countConsecutiveQuietWindows(recentWindows, thresholds.MotionLowDelta)
	if quietDuration > time.Duration(thresholds.InactiveDurationMinutes)*time.Minute {
		// Inactive: 3+ hours of low/no delta WITH packets still arriving
		return string(PatternInactive)
	}
	if quietDuration > time.Duration(thresholds.QuietWatchDurationMinutes)*time.Minute {
		// Quiet watch: 1-2 hours of low delta
		return string(PatternQuietWatch)
	}

	// No movement in current 15m window
	if currentDelta == 0 {
		return string(PatternNoMovement)
	}

	return string(PatternUnknown)
}

// Baseline75 computes the p75 baseline (per-animal, per AGENTS.md) from 24h of
// non-gap activity windows. p75 is used deliberately instead of median: a resting
// animal's median bucket is 0, so a median-based spike comparison fires on nearly
// every packet.
func Baseline75(recentWindows []ActivityWindow) int64 {
	var deltas []int64
	for _, w := range recentWindows {
		if !w.IsGap && w.MotionDelta >= 0 {
			deltas = append(deltas, w.MotionDelta)
		}
	}
	if len(deltas) == 0 {
		return 0
	}
	return percentile75(deltas)
}

// percentile75 computes the 75th percentile of sorted deltas.
// Assumes deltas are already validated as non-negative.
func percentile75(deltas []int64) int64 {
	if len(deltas) == 0 {
		return 0
	}
	// Simple in-place quickselect-style approach: sort and pick index
	// For now, use simple sort (in production, use heap/quickselect for large N)
	sortedDeltas := make([]int64, len(deltas))
	copy(sortedDeltas, deltas)
	// Bubble sort for simplicity (small dataset)
	for i := 0; i < len(sortedDeltas); i++ {
		for j := i + 1; j < len(sortedDeltas); j++ {
			if sortedDeltas[j] < sortedDeltas[i] {
				sortedDeltas[i], sortedDeltas[j] = sortedDeltas[j], sortedDeltas[i]
			}
		}
	}
	// p75 = value at position 75% through sorted array (0-based indexing)
	// For n elements: index = ceil((n-1) * 0.75)
	idx := ((len(sortedDeltas) - 1) * 3) / 4
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sortedDeltas) {
		idx = len(sortedDeltas) - 1
	}
	return sortedDeltas[idx]
}

// SupportedBucketSeconds are the only tiers activity windows are actually rolled up into
// (see herd_signal_activity_windows / upsertActivityWindows). A timeline request for any
// other bucket_seconds cannot be served and must be rejected rather than silently
// approximated.
var SupportedBucketSeconds = []int{60, 300, 3600}

// SelectBucketTier picks the activity-window tier for a timeline range:
// <=1h -> 60s, <=24h -> 300s, >24h -> 3600s. This is the server-side default; a caller
// may still request an explicit bucket_seconds, which must be one of SupportedBucketSeconds.
func SelectBucketTier(from, to time.Time) int {
	span := to.Sub(from)
	switch {
	case span <= time.Hour:
		return 60
	case span <= 24*time.Hour:
		return 300
	default:
		return 3600
	}
}

// IsSupportedBucketSeconds reports whether bucketSeconds is a tier activity windows are
// actually stored at.
func IsSupportedBucketSeconds(bucketSeconds int) bool {
	for _, b := range SupportedBucketSeconds {
		if b == bucketSeconds {
			return true
		}
	}
	return false
}

// MaxTimelineBuckets bounds the number of buckets a single timeline response may return,
// regardless of range/tier combination, so a caller cannot request an unbounded scan
// (AGENTS.md scale anti-patterns: never return unbounded ranges).
const MaxTimelineBuckets = 2000

// BatteryLifeEstimate returns a coarse, PROVISIONAL remaining-life estimate string for a
// battery reading, or "" when unknown. It linearly maps the range
// [thresholds.BatteryLowMV, thresholds.BatteryNominalFullMV] onto
// [0, thresholds.BatteryNominalLifeDays] days. This is explicitly a placeholder pending
// vendor discharge-curve data -- BLE coin-cell discharge is not linear in reality -- and
// exists only so the live view can show an operator-legible order of magnitude rather than
// a raw millivolt value.
func BatteryLifeEstimate(batteryMV *int, thresholds Thresholds) string {
	if batteryMV == nil || thresholds.BatteryNominalFullMV <= thresholds.BatteryLowMV {
		return ""
	}
	mv := *batteryMV
	if mv <= thresholds.BatteryLowMV {
		return "< 1 day (provisional)"
	}
	span := thresholds.BatteryNominalFullMV - thresholds.BatteryLowMV
	frac := float64(mv-thresholds.BatteryLowMV) / float64(span)
	if frac > 1 {
		frac = 1
	}
	days := int(frac * float64(thresholds.BatteryNominalLifeDays))
	if days < 1 {
		days = 1
	}
	return fmt.Sprintf("~%d days (provisional)", days)
}

// countConsecutiveQuietWindows counts how long the tail of windows is "quiet" (delta < threshold).
// Returns duration by summing bucket_seconds of consecutive quiet windows from the end.
func countConsecutiveQuietWindows(windows []ActivityWindow, quietThreshold int64) time.Duration {
	var duration time.Duration
	for i := len(windows) - 1; i >= 0; i-- {
		w := windows[i]
		if w.IsGap || w.MotionDelta >= quietThreshold {
			break // Stop at first non-quiet or gap
		}
		duration += time.Duration(w.BucketSeconds) * time.Second
	}
	return duration
}
