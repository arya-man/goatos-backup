package domain

import "time"

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
func PatternStateFromHistory(
	currentDelta int64,
	lastPacketAt *time.Time,
	now time.Time,
	recentWindows []ActivityWindow, // 24h history, all tiers
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
	var deltas []int64
	for _, w := range recentWindows {
		if !w.IsGap && w.MotionDelta >= 0 {
			deltas = append(deltas, w.MotionDelta)
		}
	}

	var baseline int64
	if len(deltas) > 0 {
		baseline = percentile75(deltas)
	}

	// Check for spike (current far above baseline)
	if baseline > 0 && float64(currentDelta) > float64(baseline)*thresholds.SpikeThresholdMultiplier {
		return string(PatternSpike)
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
