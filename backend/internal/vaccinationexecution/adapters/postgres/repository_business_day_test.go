package postgres

import (
	"testing"
	"time"
)

func TestBusinessDateUTCUsesIndiaCalendarAtUTCBoundary(t *testing.T) {
	instant := time.Date(2026, time.July, 11, 19, 0, 0, 0, time.UTC)
	want := time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC)
	if got := businessDateUTC(instant); !got.Equal(want) {
		t.Fatalf("businessDateUTC(%s) = %s, want %s", instant, got, want)
	}
}
