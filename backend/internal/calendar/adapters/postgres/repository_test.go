package postgres

import (
	"testing"
	"time"
)

func TestCalendarBusinessDateInUsesProjectionTimezone(t *testing.T) {
	now := time.Date(2026, time.June, 28, 20, 0, 0, 0, time.UTC)

	if got := calendarBusinessDateIn(now, "UTC"); got != "2026-06-28" {
		t.Fatalf("UTC business date=%s, want 2026-06-28", got)
	}
	if got := calendarBusinessDateIn(now, "Asia/Kolkata"); got != "2026-06-29" {
		t.Fatalf("Asia/Kolkata business date=%s, want 2026-06-29", got)
	}

	pacificBoundary := time.Date(2026, time.June, 29, 6, 30, 0, 0, time.UTC)
	if got := calendarBusinessDateIn(pacificBoundary, "America/Los_Angeles"); got != "2026-06-28" {
		t.Fatalf("America/Los_Angeles business date=%s, want 2026-06-28", got)
	}
}
