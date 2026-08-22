package domain

import (
	"testing"
	"time"
)

func TestMotionDelta(t *testing.T) {
	tests := []struct {
		name      string
		current   *int64
		previous  *int64
		wantDelta int64
		wantReset bool
	}{
		{
			name:      "normal delta",
			current:   int64Ptr(150),
			previous:  int64Ptr(100),
			wantDelta: 50,
			wantReset: false,
		},
		{
			name:      "reset detected",
			current:   int64Ptr(50),
			previous:  int64Ptr(100),
			wantDelta: 0,
			wantReset: true,
		},
		{
			name:      "same value",
			current:   int64Ptr(100),
			previous:  int64Ptr(100),
			wantDelta: 0,
			wantReset: false,
		},
		{
			name:      "no current",
			current:   nil,
			previous:  int64Ptr(100),
			wantDelta: 0,
			wantReset: false,
		},
		{
			name:      "no previous",
			current:   int64Ptr(100),
			previous:  nil,
			wantDelta: 0,
			wantReset: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta, reset := MotionDelta(tt.current, tt.previous)
			if delta != tt.wantDelta {
				t.Errorf("delta = %d, want %d", delta, tt.wantDelta)
			}
			if reset != tt.wantReset {
				t.Errorf("reset = %v, want %v", reset, tt.wantReset)
			}
		})
	}
}

func TestMovementStateFromDelta(t *testing.T) {
	thresholds := DefaultThresholds()

	tests := []struct {
		name   string
		delta  int64
		window int
		want   string
	}{
		{
			name:   "moving",
			delta:  150,
			window: 60,
			want:   "moving",
		},
		{
			name:   "low activity",
			delta:  50,
			window: 60,
			want:   "low",
		},
		{
			name:   "quiet",
			delta:  5,
			window: 60,
			want:   "quiet",
		},
		{
			name:   "not moving",
			delta:  0,
			window: 60,
			want:   "not_moving",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := MovementStateFromDelta(tt.delta, tt.window, thresholds)
			if state != tt.want {
				t.Errorf("state = %s, want %s", state, tt.want)
			}
		})
	}
}

func TestSignalStateFromRSSI(t *testing.T) {
	thresholds := DefaultThresholds()

	tests := []struct {
		name    string
		rssi    *int16
		avgRSSI *float64
		want    string
	}{
		{
			name: "strong signal",
			rssi: int16Ptr(-60),
			want: "strong",
		},
		{
			name: "ok signal",
			rssi: int16Ptr(-70),
			want: "ok",
		},
		{
			name: "weak signal",
			rssi: int16Ptr(-80),
			want: "weak",
		},
		{
			name:    "weak average",
			avgRSSI: float64Ptr(-85),
			want:    "weak",
		},
		{
			name: "unknown",
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := SignalStateFromRSSI(tt.rssi, tt.avgRSSI, thresholds)
			if state != tt.want {
				t.Errorf("state = %s, want %s", state, tt.want)
			}
		})
	}
}

func TestBatteryStateFromMillivolts(t *testing.T) {
	thresholds := DefaultThresholds()

	tests := []struct {
		name string
		mv   *int
		want string
	}{
		{
			name: "ok battery",
			mv:   intPtr(3000),
			want: "ok",
		},
		{
			name: "low battery",
			mv:   intPtr(2500),
			want: "low",
		},
		{
			name: "unknown",
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := BatteryStateFromMillivolts(tt.mv, thresholds)
			if state != tt.want {
				t.Errorf("state = %s, want %s", state, tt.want)
			}
		})
	}
}

func TestPatternStateFromHistory(t *testing.T) {
	thresholds := DefaultThresholds()
	now := time.Now()
	fiveMinutesAgo := now.Add(-5 * time.Minute)

	tests := []struct {
		name            string
		currentDelta    int64
		lastPacketAt    *time.Time
		recentWindows   []ActivityWindow
		previousPattern string
		expectedState   string
	}{
		{
			name:          "missing signal (no recent packets)",
			currentDelta:  0,
			lastPacketAt:  timePtr(now.Add(-time.Hour)),
			recentWindows: []ActivityWindow{},
			expectedState: string(PatternMissingSignal),
		},
		{
			name:         "no movement (delta = 0)",
			currentDelta: 0,
			lastPacketAt: &fiveMinutesAgo,
			recentWindows: []ActivityWindow{
				{BucketSeconds: 60, MotionDelta: 0, PacketCount: 1, IsGap: false},
			},
			expectedState: string(PatternNoMovement),
		},
		{
			name:          "quiet watch (sustained low delta 1-2h)",
			currentDelta:  5,
			lastPacketAt:  &fiveMinutesAgo,
			recentWindows: generateQuietWindows(100, 60), // 100 min > 90 min threshold
			expectedState: string(PatternQuietWatch),
		},
		{
			name:          "inactive (sustained low delta 3+ hours with packets)",
			currentDelta:  5,
			lastPacketAt:  &fiveMinutesAgo,
			recentWindows: generateQuietWindows(190, 60), // 190 min > 180 min threshold
			expectedState: string(PatternInactive),
		},
		{
			name:            "recovered (activity resumes after quiet_watch)",
			currentDelta:    15, // >= MotionLowDelta (10)
			lastPacketAt:    &fiveMinutesAgo,
			recentWindows:   []ActivityWindow{{BucketSeconds: 60, MotionDelta: 15, PacketCount: 1, IsGap: false}},
			previousPattern: string(PatternQuietWatch),
			expectedState:   string(PatternRecovered),
		},
		{
			name:            "recovered (activity resumes after inactive)",
			currentDelta:    50,
			lastPacketAt:    &fiveMinutesAgo,
			recentWindows:   []ActivityWindow{{BucketSeconds: 60, MotionDelta: 50, PacketCount: 1, IsGap: false}},
			previousPattern: string(PatternInactive),
			expectedState:   string(PatternRecovered),
		},
		{
			name:            "not recovered: previous quiet_watch but current delta still below threshold",
			currentDelta:    3, // < MotionLowDelta (10)
			lastPacketAt:    &fiveMinutesAgo,
			recentWindows:   []ActivityWindow{{BucketSeconds: 60, MotionDelta: 3, PacketCount: 1, IsGap: false}},
			previousPattern: string(PatternQuietWatch),
			expectedState:   string(PatternUnknown),
		},
		{
			name:            "not recovered: previously unknown/active, currently active is just unknown",
			currentDelta:    15,
			lastPacketAt:    &fiveMinutesAgo,
			recentWindows:   []ActivityWindow{{BucketSeconds: 60, MotionDelta: 15, PacketCount: 1, IsGap: false}},
			previousPattern: string(PatternUnknown),
			expectedState:   string(PatternUnknown),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := PatternStateFromHistory(tt.currentDelta, tt.lastPacketAt, now, tt.recentWindows, tt.previousPattern, thresholds)
			if state != tt.expectedState {
				t.Errorf("state = %s, want %s", state, tt.expectedState)
			}
		})
	}
}

func TestPercentile75(t *testing.T) {
	tests := []struct {
		name     string
		deltas   []int64
		expected int64
	}{
		{
			name:     "empty",
			deltas:   []int64{},
			expected: 0,
		},
		{
			name:     "single value",
			deltas:   []int64{10},
			expected: 10,
		},
		{
			name:     "four values (p75 = 3rd)",
			deltas:   []int64{1, 2, 3, 4},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := percentile75(tt.deltas)
			if result != tt.expected {
				t.Errorf("percentile75 = %d, want %d", result, tt.expected)
			}
		})
	}
}

// Helper functions for test pointers
func int16Ptr(v int16) *int16        { return &v }
func int64Ptr(v int64) *int64        { return &v }
func intPtr(v int) *int              { return &v }
func float64Ptr(v float64) *float64  { return &v }
func timePtr(v time.Time) *time.Time { return &v }

func TestSelectBucketTier(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		span time.Duration
		want int
	}{
		{"30 minutes -> 60s", 30 * time.Minute, 60},
		{"exactly 1 hour -> 60s", time.Hour, 60},
		{"2 hours -> 300s", 2 * time.Hour, 300},
		{"exactly 24 hours -> 300s", 24 * time.Hour, 300},
		{"25 hours -> 3600s", 25 * time.Hour, 3600},
		{"7 days -> 3600s", 7 * 24 * time.Hour, 3600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectBucketTier(base, base.Add(tt.span))
			if got != tt.want {
				t.Errorf("SelectBucketTier(span=%v) = %d, want %d", tt.span, got, tt.want)
			}
		})
	}
}

func TestIsSupportedBucketSeconds(t *testing.T) {
	for _, b := range []int{60, 300, 3600} {
		if !IsSupportedBucketSeconds(b) {
			t.Errorf("IsSupportedBucketSeconds(%d) = false, want true", b)
		}
	}
	for _, b := range []int{0, 30, 120, 900, 7200} {
		if IsSupportedBucketSeconds(b) {
			t.Errorf("IsSupportedBucketSeconds(%d) = true, want false", b)
		}
	}
}

func TestBatteryLifeEstimate(t *testing.T) {
	thresholds := DefaultThresholds()

	if got := BatteryLifeEstimate(nil, thresholds); got != "" {
		t.Errorf("nil battery = %q, want empty", got)
	}
	if got := BatteryLifeEstimate(intPtr(2700), thresholds); got != "< 1 day (provisional)" {
		t.Errorf("below-low battery = %q, want '< 1 day (provisional)'", got)
	}
	if got := BatteryLifeEstimate(intPtr(3000), thresholds); got == "" {
		t.Errorf("full battery should produce a non-empty estimate")
	}
}

func TestBaseline75(t *testing.T) {
	windows := []ActivityWindow{
		{MotionDelta: 0, IsGap: false},
		{MotionDelta: 5, IsGap: false},
		{MotionDelta: 10, IsGap: false},
		{MotionDelta: 100, IsGap: true}, // gap must be excluded even though it has a delta value
	}
	got := Baseline75(windows)
	want := percentile75([]int64{0, 5, 10})
	if got != want {
		t.Errorf("Baseline75 = %d, want %d (gap window must be excluded)", got, want)
	}
}

// generateQuietWindows creates N minutes worth of quiet windows (delta < 10).
func generateQuietWindows(durationMinutes int, bucketSeconds int) []ActivityWindow {
	var windows []ActivityWindow
	bucketsNeeded := (durationMinutes * 60) / bucketSeconds
	for i := 0; i < bucketsNeeded; i++ {
		windows = append(windows, ActivityWindow{
			BucketSeconds: bucketSeconds,
			MotionDelta:   3, // Low delta (below MotionLowDelta threshold of 10)
			PacketCount:   1,
			IsGap:         false,
		})
	}
	return windows
}
