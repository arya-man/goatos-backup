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

func TestClampFutureAsOfKeepsHistoricalButCapsFuture(t *testing.T) {
	now := time.Date(2026, 7, 11, 18, 15, 0, 0, DefaultLocation())
	past := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	future := time.Date(2026, 9, 1, 23, 59, 59, 0, time.UTC)

	if got := ClampFutureAsOf(past, now); !got.Equal(past) {
		t.Fatalf("past as_of = %s, want %s", got, past)
	}
	if got := ClampFutureAsOf(future, now); !got.Equal(now) {
		t.Fatalf("future as_of = %s, want clamped now %s", got, now)
	}
}

func TestClampFutureAsOfTreatsZeroAsNow(t *testing.T) {
	now := time.Date(2026, 7, 11, 18, 15, 0, 0, DefaultLocation())
	past := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	future := time.Date(2026, 9, 1, 23, 59, 59, 0, time.UTC)

	if got := ClampFutureAsOf(time.Time{}, now); !got.Equal(now) {
		t.Fatalf("zero as_of = %s, want now %s", got, now)
	}
	if got := ClampFutureAsOf(past, now); !got.Equal(past) {
		t.Fatalf("past as_of = %s, want unchanged %s", got, past)
	}
	if got := ClampFutureAsOf(future, now); !got.Equal(now) {
		t.Fatalf("future as_of = %s, want clamped now %s", got, now)
	}
}

func TestParseLiveAsOfRFC3339ClampsFutureAndRejectsMalformed(t *testing.T) {
	now := time.Date(2026, 7, 11, 18, 15, 0, 0, DefaultLocation())

	got, err := ParseLiveAsOfRFC3339("2026-09-01T23:59:59Z", now)
	if err != nil {
		t.Fatalf("parse future as_of: %v", err)
	}
	if !got.Equal(now) {
		t.Fatalf("future as_of = %s, want clamped now %s", got, now)
	}
	if _, err := ParseLiveAsOfRFC3339("2026-09-01", now); err == nil {
		t.Fatalf("malformed as_of returned nil error")
	}
}
