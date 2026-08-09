package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// istAt builds a fixed Asia/Kolkata wall-clock instant. Every case here is anchored on a FIXED date
// rather than on now±N, because the whole rule is a business-day rule: a now-relative fixture would
// pass or fail depending on what time of day the suite happened to run, which is exactly the
// hour-anchored defect class the vaccination time-grain rule exists to prevent.
func istAt(year int, month time.Month, day, hour, min int) time.Time {
	return time.Date(year, month, day, hour, min, 0, 0, biztime.DefaultLocation())
}

func TestShiftingActionsDueFromHighPriorityHasNoLeadTime(t *testing.T) {
	raised := istAt(2026, time.August, 10, 14, 5)
	if got := ShiftingActionsDueFrom("high", raised); !got.Equal(raised) {
		t.Fatalf("high-priority due=%s, want the raise instant %s -- high priority is the urgent case "+
			"and waits for nothing", got, raised)
	}
	// Case-insensitive and whitespace-tolerant. NOT because stored data can look like this --
	// shifting_events_priority_check constrains the column to exactly 'high'/'low' and the handler
	// lowercases before writing -- but because this is a pure function any caller may reach with a
	// hand-built string, and defaulting a mistyped " High " to the low-priority LEAD TIME would
	// silently delay an urgent movement by a day.
	if got := ShiftingActionsDueFrom(" High ", raised); !got.Equal(raised) {
		t.Fatalf("due=%s for ' High ', want the raise instant %s", got, raised)
	}
	if !ShiftingActionsDue("high", raised, raised) {
		t.Fatalf("a high-priority movement is not due at its own raise instant, want due to the second")
	}
}

func TestShiftingActionsDueFromLowPriorityCrossesTheAfternoonCutoff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		raised time.Time
		want   time.Time
	}{
		{"well before the cutoff", istAt(2026, time.August, 10, 9, 0), istAt(2026, time.August, 11, 0, 0)},
		{"one minute before the cutoff", istAt(2026, time.August, 10, 13, 29), istAt(2026, time.August, 11, 0, 0)},
		// 13:30 exactly is AFTER the cutoff: the boundary belongs to the later day, so a movement
		// raised exactly on the clock cannot claim a plan that had already closed.
		{"exactly on the cutoff", istAt(2026, time.August, 10, 13, 30), istAt(2026, time.August, 12, 0, 0)},
		{"after the cutoff", istAt(2026, time.August, 10, 17, 45), istAt(2026, time.August, 12, 0, 0)},
		{"just before midnight", istAt(2026, time.August, 10, 23, 59), istAt(2026, time.August, 12, 0, 0)},
		{"just after midnight", istAt(2026, time.August, 10, 0, 1), istAt(2026, time.August, 11, 0, 0)},
		// Month and year rollover: +2 days from the 30th of a 31-day month, and across new year.
		{"month rollover", istAt(2026, time.August, 30, 18, 0), istAt(2026, time.September, 1, 0, 0)},
		{"year rollover", istAt(2026, time.December, 31, 18, 0), istAt(2027, time.January, 2, 0, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShiftingActionsDueFrom("low", tc.raised); !got.Equal(tc.want) {
				t.Fatalf("low-priority raised %s -> due %s, want %s", tc.raised, got, tc.want)
			}
		})
	}
}

// TestShiftingActionsDueFromUsesISTWallClockNotUTC is the adversarial timezone case. 09:00 UTC is
// 14:30 IST -- AFTER the cutoff -- so a rule that read the stored UTC instant's clock would put this
// movement one whole day early.
func TestShiftingActionsDueFromUsesISTWallClockNotUTC(t *testing.T) {
	raised := time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC) // 14:30 IST
	want := istAt(2026, time.August, 12, 0, 0)
	if got := ShiftingActionsDueFrom("low", raised); !got.Equal(want) {
		t.Fatalf("due=%s for a 09:00 UTC (14:30 IST) raise, want %s -- the cutoff is an IST wall clock",
			got, want)
	}

	// The other side of midnight: 20:00 UTC on the 10th is 01:30 IST on the 11th, so the business
	// day the lead time counts from is the 11th, not the 10th.
	lateUTC := time.Date(2026, time.August, 10, 20, 0, 0, 0, time.UTC) // 01:30 IST on the 11th
	wantLate := istAt(2026, time.August, 12, 0, 0)
	if got := ShiftingActionsDueFrom("low", lateUTC); !got.Equal(wantLate) {
		t.Fatalf("due=%s for a 20:00 UTC (01:30 IST next day) raise, want %s", got, wantLate)
	}
}

// TestShiftingActionsDueCoversLateApproval pins that the late-approval rule needs no branch: a
// movement whose due instant has already passed is due, however late the park head acted.
func TestShiftingActionsDueCoversLateApproval(t *testing.T) {
	raised := istAt(2026, time.August, 10, 11, 0) // low priority -> due 11 Aug 00:00 IST

	if ShiftingActionsDue("low", raised, istAt(2026, time.August, 10, 23, 59)) {
		t.Fatalf("low-priority movement reads due on its own raise day, want it held until the next day")
	}
	if !ShiftingActionsDue("low", raised, istAt(2026, time.August, 11, 0, 0)) {
		t.Fatalf("low-priority movement is not due at 00:00 IST on its due day, want due")
	}
	// Approved three days late: already past the due instant, so it is visible at once rather than
	// hidden in the past.
	if !ShiftingActionsDue("low", raised, istAt(2026, time.August, 13, 9, 0)) {
		t.Fatalf("a movement approved after its due instant is not due, want it visible immediately")
	}
}
