package domain

import (
	"testing"
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

// Helper functions for test pointers
func int16Ptr(v int16) *int16       { return &v }
func int64Ptr(v int64) *int64       { return &v }
func intPtr(v int) *int             { return &v }
func float64Ptr(v float64) *float64 { return &v }
