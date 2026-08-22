package domain

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
