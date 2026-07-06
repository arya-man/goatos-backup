package biztime

import (
	"testing"
	"time"
)

func TestBusinessDayStartUsesDefaultISTCalendar(t *testing.T) {
	input := time.Date(2026, 6, 30, 20, 0, 0, 0, time.UTC)

	got := BusinessDayStart(input)

	if got.Location().String() != DefaultTimezone {
		t.Fatalf("location=%s, want %s", got.Location(), DefaultTimezone)
	}
	if got.Format(time.RFC3339) != "2026-07-01T00:00:00+05:30" {
		t.Fatalf("business day start=%s, want IST midnight for July 1", got.Format(time.RFC3339))
	}
	if date := BusinessDate(input); date != "2026-07-01" {
		t.Fatalf("business date=%s, want 2026-07-01", date)
	}
}

func TestBusinessDayStartInIgnoresRequestedTimezone(t *testing.T) {
	input := time.Date(2026, 6, 30, 20, 0, 0, 0, time.UTC)

	got := BusinessDayStartIn(input, "America/Los_Angeles")

	if got.Location().String() != DefaultTimezone {
		t.Fatalf("location=%s, want %s", got.Location(), DefaultTimezone)
	}
	if got.Format(time.RFC3339) != "2026-07-01T00:00:00+05:30" {
		t.Fatalf("business day start=%s, want IST-only business day", got.Format(time.RFC3339))
	}
}
