package domain

import (
	"strings"
	"time"
)

// patternWindowSeconds is the window PatternStateFromHistory's currentDelta is computed over
// (the 15-minute movement-state window; see postgres.updateTagLatest). baselineBucketSeconds is
// the tier the p75 baseline is computed from (the 300s/5-minute activity-window tier; see
// postgres.GetBaselineDeltas). Kept here, next to the one place that compares them, so the two
// numbers cannot drift apart silently the way the un-scaled comparison did (defect 3).
const (
	patternWindowSeconds  = 900
	baselineBucketSeconds = 300
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

// IsGapDelta reports whether a delta between previousSeenAt and the new packet's received_at
// crosses a reception gap (maintainer decision on offline behaviour): the gateway does not
// buffer scan reports through a WAN outage and a tag only broadcasts its current cumulative
// motion_count, so an interval this long means the backend received NOTHING in between, and the
// eventual delta is a TOTAL across the whole gap with NO information about its distribution in
// time. previousSeenAt is nil for a tag's first-ever packet, which is not a "gap" (there is no
// prior sighting to have lost touch with).
//
// received_at (server clock) is the ONLY input, by maintainer decision: gateway_seen_at is the
// gateway's own clock, stored uncorrected and never used to decide whether a gap occurred (one
// observed gateway runs +02:30:00 ahead of real IST, so trusting it would fabricate or hide
// gaps).
func IsGapDelta(previousSeenAt *time.Time, receivedAt time.Time, thresholds Thresholds) bool {
	if previousSeenAt == nil {
		return false
	}
	return receivedAt.Sub(*previousSeenAt) > time.Duration(thresholds.ReceptionGapMinutes)*time.Minute
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
		// Unreachable in practice: domain.MotionDelta floors a counter reset at 0, so delta is
		// never negative here. Falls back to not_moving rather than an "unknown" value the
		// frontend contract (HerdSignalMovementState) does not declare.
		return "not_moving"
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

// BatteryStateFromVoltage determines the ABSOLUTE battery band from a single reading: healthy,
// watch, low, or critical (maintainer decision, replacing a removed remaining-life estimate --
// the tag reports voltage only, so this is the only honest per-reading classification). This is
// the ingest-time value stored on herd_signal_tag_latest; the RELATIVE half of "watch" (falling
// against the tag's own history) and the critical-on-silence escalation are computed at READ
// TIME by BatteryStateWithTrend, because they need this tag's history and its current
// missing-signal state, neither of which is available or meaningful to keep re-deriving on every
// single ingest.
func BatteryStateFromVoltage(batteryMV *int, thresholds Thresholds) string {
	if batteryMV == nil {
		return "unknown"
	}
	switch {
	case *batteryMV >= thresholds.BatteryHealthyMV:
		return "healthy"
	case *batteryMV >= thresholds.BatteryWatchMV:
		return "watch"
	case *batteryMV < thresholds.BatteryCriticalMV:
		return "critical"
	default:
		return "low"
	}
}

// BatteryTrend is a compact voltage trend: a direction plus the two endpoint readings that
// justify it. Returned as nil when there is not enough history to say anything -- coin-cell
// voltage is noisy and temperature-sensitive, so a direction is never invented from two adjacent
// packets (see BatteryTrendFromHistory).
type BatteryTrend struct {
	Direction  string // "stable" | "falling"
	WindowDays int
	FirstMV    int
	FirstAt    time.Time
	LastMV     int
	LastAt     time.Time
}

// BatteryTrendFromHistory builds a trend from the first and last battery_mv readings in the
// configured window. Returns nil (no trend claim at all) unless the readings span at least
// BatteryTrendMinSpanHours -- two packets five minutes apart cannot support a "falling" claim
// against normal coin-cell voltage sag/noise/temperature sensitivity, and a null trend is more
// honest than a direction computed from noise.
func BatteryTrendFromHistory(firstMV, lastMV *int, firstAt, lastAt *time.Time, thresholds Thresholds) *BatteryTrend {
	if firstMV == nil || lastMV == nil || firstAt == nil || lastAt == nil {
		return nil
	}
	span := lastAt.Sub(*firstAt)
	if span < time.Duration(thresholds.BatteryTrendMinSpanHours*float64(time.Hour)) {
		return nil
	}
	direction := "stable"
	if *firstMV-*lastMV >= thresholds.BatteryFallMV {
		direction = "falling"
	}
	return &BatteryTrend{
		Direction:  direction,
		WindowDays: thresholds.BatteryTrendWindowDays,
		FirstMV:    *firstMV,
		FirstAt:    *firstAt,
		LastMV:     *lastMV,
		LastAt:     *lastAt,
	}
}

// BatteryStateWithTrend composes the absolute battery_state (from BatteryStateFromVoltage,
// already stored on the tag) with the voltage trend and the tag's current missing-signal state
// to produce the final four-value battery_state:
//
//   - A relative fall (trend.Direction == "falling") escalates healthy -> watch: the point of
//     this model is that a tag drifting 3.18V -> 3.10V is informative before it crosses any
//     absolute line, so it must not be silently absorbed into "healthy".
//   - Critical-on-silence: a tag that is currently missing signal (read-time pattern_state,
//     computed elsewhere) AND was falling escalates to critical. This is a genuine cross-signal,
//     combining battery history with the missing-signal state, and it is explicitly INFERRED,
//     not measured -- it must never be read as "the tag is dead", only that it went quiet while
//     its voltage was falling. It never fires on its own: a tag that is missing but had a STABLE
//     or unknown trend is left at its absolute battery_state, because silence alone says nothing
//     about the battery.
func BatteryStateWithTrend(absoluteState string, trend *BatteryTrend, patternStateIsMissing bool) string {
	state := absoluteState
	falling := trend != nil && trend.Direction == "falling"

	if falling && state == "healthy" {
		state = "watch"
	}
	if falling && patternStateIsMissing {
		state = "critical"
	}
	return state
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
	currentIsGapDelta bool,
	thresholds Thresholds,
) string {
	if lastPacketAt == nil {
		return string(PatternMissingSignal)
	}

	timeSincePacket := now.Sub(*lastPacketAt).Minutes()
	if timeSincePacket > float64(thresholds.MissingSignalMinutes) {
		return string(PatternMissingSignal)
	}

	// Compute baseline (p75) from 24h non-gap, non-gap-delta windows. The baseline is computed
	// over the 300s (5-minute) activity-window tier -- see the postgres adapter's
	// GetBaselineDeltas -- while currentDelta is a 900s (15-minute) window sum. Comparing a
	// 15-minute value directly against a 5-minute baseline (maintainer correctness review,
	// defect 3: "like grain to like grain") makes spike fire at ~0.83x the animal's normal rate,
	// i.e. constantly. Scale the baseline up by the grain ratio (patternWindowSeconds /
	// baselineBucketSeconds = 3) before applying the spike multiplier, so both sides of the
	// comparison cover the same span.
	baseline := Baseline75(recentWindows)
	grainRatio := float64(patternWindowSeconds) / float64(baselineBucketSeconds)

	// A reconnect lump is NOT a spike (maintainer decision on offline behaviour): a tag that was
	// offline for two hours and comes back +239 is not a burst of activity in this window, it is
	// a total across a gap with an unknown time distribution -- attributing it to "spike" would
	// invent a timeline the data cannot support, the same reason it is excluded from the
	// baseline above. Skip the comparison entirely rather than exempting it after the fact.
	if !currentIsGapDelta && baseline > 0 && float64(currentDelta) > float64(baseline)*grainRatio*thresholds.SpikeThresholdMultiplier {
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
		// GapDelta windows are excluded on purpose (maintainer decision on offline behaviour):
		// a reconnect lump is a total over an unknown span, not a sample of this animal's normal
		// per-bucket movement. Averaging or otherwise folding it into the baseline would treat an
		// artifact of a network outage as if it were the animal's behaviour -- DO NOT "fix" this
		// by including it, weighting it down, or smoothing it across buckets; exclude it, full stop.
		if !w.IsGap && !w.GapDelta && w.MotionDelta >= 0 {
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

// countConsecutiveQuietWindows counts how long the tail of windows is "quiet" (delta < threshold)
// AND CONTINUOUSLY RECEIVING PACKETS. Returns duration by summing bucket_seconds of consecutive
// quiet windows from the end.
//
// windows is the SPARSE result of a SQL range query: a bucket with zero packets has no row at
// all (there is nothing to upsert for it), it is not a row with IsGap=true. Walking the slice by
// INDEX alone (as this function originally did) therefore cannot see a reception gap between two
// stored buckets -- a tag that went quiet for 40 minutes then resumed reads as one unbroken
// quiet run bridging the gap, even though "inactive" is explicitly defined as low movement WHILE
// PACKETS STILL ARRIVE (maintainer correctness review, defect 7). This walks by TIME instead:
// each step must be exactly one bucket_seconds earlier than the one after it, or the run breaks.
func countConsecutiveQuietWindows(windows []ActivityWindow, quietThreshold int64) time.Duration {
	var duration time.Duration
	var expectedStart time.Time
	for i := len(windows) - 1; i >= 0; i-- {
		w := windows[i]
		if w.IsGap || w.MotionDelta >= quietThreshold {
			break // Stop at first non-quiet or gap
		}
		if i != len(windows)-1 && !w.BucketStart.Add(time.Duration(w.BucketSeconds)*time.Second).Equal(expectedStart) {
			break // A reception gap sits between this bucket and the one after it: run ends here.
		}
		duration += time.Duration(w.BucketSeconds) * time.Second
		expectedStart = w.BucketStart
	}
	return duration
}

// NormalizeTagIdentifier mirrors the canonical identifier normalizer
// (backend/internal/identity/app/service.go: strings.ToUpper(strings.TrimSpace(...))) exactly.
// BLE tag_id/tag_mac values must be compared against goat_identifiers.normalized_value using
// this SAME transform, or a lowercase device MAC ("f0:c9:90:...", the capture's own convention)
// silently never matches an uppercase-stored identifier and every tag reads unmapped
// (maintainer scale/correctness review, defect 2).
func NormalizeTagIdentifier(v string) string {
	return strings.ToUpper(strings.TrimSpace(v))
}
