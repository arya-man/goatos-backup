package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// ist builds a business-calendar instant, so a test never accidentally asserts a UTC day.
func ist(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, biztime.DefaultLocation())
}

// day builds a business-day start, the shape a target feed date takes.
func day(year int, month time.Month, d int) time.Time {
	return ist(year, month, d, 0)
}

// TestFeedShiftingEffectiveBusinessDate: an authorized movement is feed-relevant from its
// authorization business day itself -- no lead, no priority branch (maintainer decision 2026-07-27).
func TestFeedShiftingEffectiveBusinessDate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		approvedAt time.Time
		want       time.Time
	}{
		{
			name:       "authorized on the 10th is feed effective on the 10th",
			approvedAt: ist(2026, time.July, 10, 9),
			want:       day(2026, time.July, 10),
		},
		{
			name:       "time of day within the approval day is discarded",
			approvedAt: ist(2026, time.July, 10, 23),
			want:       day(2026, time.July, 10),
		},
		{
			// 20:00 UTC on the 10th is 01:30 IST on the 11th. Deriving the business date from UTC
			// would put the movement on the wrong feed day.
			name:       "a late UTC approval is already the next India business day",
			approvedAt: time.Date(2026, time.July, 10, 20, 0, 0, 0, time.UTC),
			want:       day(2026, time.July, 11),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FeedShiftingEffectiveBusinessDate(tc.approvedAt)
			if !got.Equal(tc.want) {
				t.Fatalf("FeedShiftingEffectiveBusinessDate(%s) = %s, want %s", tc.approvedAt, got, tc.want)
			}
		})
	}
}

// TestFeedShiftingCountsToward is the timing rule proper: which feed days an authorized-but-
// unexecuted movement contributes to. With no lead, it counts from the authorization day onward.
func TestFeedShiftingCountsToward(t *testing.T) {
	t.Parallel()

	approvedOn10th := ist(2026, time.July, 10, 9)

	cases := []struct {
		name       string
		approvedAt time.Time
		target     time.Time
		want       bool
	}{
		{
			name:       "does not count before the authorization day",
			approvedAt: approvedOn10th,
			target:     day(2026, time.July, 9),
			want:       false,
		},
		{
			// The change from the retired 2-day-lead rule: a movement counts on its
			// authorization day, and therefore for tomorrow's feed packed the same day.
			name:       "counts on the authorization day itself",
			approvedAt: approvedOn10th,
			target:     day(2026, time.July, 10),
			want:       true,
		},
		{
			name:       "counts on the day after authorization",
			approvedAt: approvedOn10th,
			target:     day(2026, time.July, 11),
			want:       true,
		},
		// --- the <= half of the rule: an OVERDUE movement never drops out ---
		{
			name:       "an overdue movement still counts five days after authorization",
			approvedAt: approvedOn10th,
			target:     day(2026, time.July, 15),
			want:       true,
		},
		{
			name:       "an overdue movement still counts a month later",
			approvedAt: approvedOn10th,
			target:     day(2026, time.August, 10),
			want:       true,
		},
		// --- target dates carrying a time component still resolve to their business day ---
		{
			name:       "a target instant late on the authorization day still counts",
			approvedAt: approvedOn10th,
			target:     ist(2026, time.July, 10, 23),
			want:       true,
		},
		{
			name:       "a target instant late on the day before authorization does not count",
			approvedAt: approvedOn10th,
			target:     ist(2026, time.July, 9, 23),
			want:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FeedShiftingCountsToward(tc.approvedAt, tc.target)
			if got != tc.want {
				t.Fatalf("FeedShiftingCountsToward(approved=%s, target=%s) = %t, want %t",
					tc.approvedAt, tc.target, got, tc.want)
			}
		})
	}
}

// TestFeedShiftingIsOverdue: overdue only when the movement was authorized BEFORE the packing day
// (feed day - 1) and still is not executed. A move authorized on the packing day (expected to be
// executed that same day) is NOT overdue -- see the maintainer decision on the overdue signal.
func TestFeedShiftingIsOverdue(t *testing.T) {
	t.Parallel()

	approvedOn10th := ist(2026, time.July, 10, 9)

	cases := []struct {
		name       string
		approvedAt time.Time
		target     time.Time
		want       bool
	}{
		{
			// packing day = 9th; authorized on the 10th (after the packing day) -> not overdue.
			name:       "authorized after the packing day is not overdue",
			approvedAt: approvedOn10th,
			target:     day(2026, time.July, 10),
			want:       false,
		},
		{
			// feed day 11 -> packing day 10; authorized ON the packing day is expected to execute
			// that day and is on time, not overdue. An off-by-one here would flag every freshly
			// authorized move and train operators to ignore the flag.
			name:       "authorized on the packing day is on time not overdue",
			approvedAt: approvedOn10th,
			target:     day(2026, time.July, 11),
			want:       false,
		},
		{
			// feed day 12 -> packing day 11; authorized on the 10th (before the packing day) is overdue.
			name:       "authorized before the packing day is overdue",
			approvedAt: approvedOn10th,
			target:     day(2026, time.July, 12),
			want:       true,
		},
		{
			name:       "five days past authorization is overdue",
			approvedAt: approvedOn10th,
			target:     day(2026, time.July, 15),
			want:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FeedShiftingIsOverdue(tc.approvedAt, tc.target)
			if got != tc.want {
				t.Fatalf("FeedShiftingIsOverdue(approved=%s, target=%s) = %t, want %t",
					tc.approvedAt, tc.target, got, tc.want)
			}
		})
	}
}

// TestFeedShiftingOverdueImpliesCounting locks the relationship between the two predicates: an
// overdue movement is by definition still contributing. If these ever disagree, the projection
// would flag a movement as late while excluding its animals from the count.
func TestFeedShiftingOverdueImpliesCounting(t *testing.T) {
	t.Parallel()

	approvedAt := ist(2026, time.July, 10, 9)
	for offset := 0; offset <= 20; offset++ {
		target := day(2026, time.July, 10).AddDate(0, 0, offset)
		overdue := FeedShiftingIsOverdue(approvedAt, target)
		counts := FeedShiftingCountsToward(approvedAt, target)
		if overdue && !counts {
			t.Fatalf("at +%dd movement is overdue but not counted", offset)
		}
	}
}

// ---------------------------------------------------------------------------
// RAISED but NOT YET APPROVED (maintainer decision 2026-08-10)
// ---------------------------------------------------------------------------

// istAt (minute precision, fixed date) already lives in this package -- shifting_visibility_test.go.
// The 13:30 cutoff needs minutes and ist() above carries whole hours only, so these cases reuse it
// rather than declaring a second helper that could drift from the one the lead-time rule is tested with.

// TestFeedShiftingRaisedEffectiveBusinessDate pins the rule that fixed the real defect: a
// low-priority movement raised in the MORNING is due tomorrow, but tomorrow's normal sheet was
// issued at 07:00 that same morning and is already being packed. Waiting for a park head's approval
// meant the destination pen was packed for the head count it had at breakfast.
//
// The date is the ACTIONS lead time, deliberately NOT the raise day. The 13:45 case below is the one
// that makes the difference visible: those animals do not walk until the day AFTER tomorrow, so
// feeding their destination from tomorrow would be the same over-feeding bug one day early.
func TestFeedShiftingRaisedEffectiveBusinessDate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		priority string
		raisedAt time.Time
		want     time.Time
	}{
		{
			name:     "low priority raised in the morning is due tomorrow",
			priority: "low",
			raisedAt: istAt(2026, time.August, 10, 9, 0),
			want:     day(2026, time.August, 11),
		},
		{
			name:     "low priority raised one minute before the cutoff is still tomorrow",
			priority: "low",
			raisedAt: istAt(2026, time.August, 10, 13, 29),
			want:     day(2026, time.August, 11),
		},
		{
			name:     "low priority raised exactly at 13:30 slips to the day after",
			priority: "low",
			raisedAt: istAt(2026, time.August, 10, 13, 30),
			want:     day(2026, time.August, 12),
		},
		{
			name:     "low priority raised at 13:45 does NOT reach tomorrow's sheet",
			priority: "low",
			raisedAt: istAt(2026, time.August, 10, 13, 45),
			want:     day(2026, time.August, 12),
		},
		{
			// High priority carries no lead at all: it is executed the same day, so its animals eat
			// at the destination today.
			name:     "high priority is effective the day it is raised",
			priority: "high",
			raisedAt: istAt(2026, time.August, 10, 16, 0),
			want:     day(2026, time.August, 10),
		},
		{
			// UTC never defines a Goat OS business day. 09:00 UTC is 14:30 in India -- AFTER the
			// cutoff -- so a UTC-derived comparison would put this on tomorrow's sheet and feed a
			// destination a day before its animals arrive.
			name:     "the cutoff reads the India wall clock, not UTC",
			priority: "low",
			raisedAt: time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC),
			want:     day(2026, time.August, 12),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FeedShiftingRaisedEffectiveBusinessDate(tc.priority, tc.raisedAt)
			if !got.Equal(tc.want) {
				t.Fatalf("FeedShiftingRaisedEffectiveBusinessDate(%q, %s) = %s, want %s",
					tc.priority, tc.raisedAt.Format(time.RFC3339), got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}

// TestFeedShiftingRaisedCountsToward: once due, an unapproved movement keeps counting until it is
// executed or rejected -- the same <= the authorized rule uses. A movement sitting unapproved in the
// park head's queue for days must not silently stop feeding a destination whose animals are still
// expected to arrive.
func TestFeedShiftingRaisedCountsToward(t *testing.T) {
	t.Parallel()

	raised := istAt(2026, time.August, 10, 9, 0) // low priority -> due 2026-08-11

	cases := []struct {
		name       string
		targetDate time.Time
		want       bool
	}{
		{"the raise day itself is too early -- the animals have not moved", day(2026, time.August, 10), false},
		{"the due day counts", day(2026, time.August, 11), true},
		{"a later day still counts while the movement is unexecuted", day(2026, time.August, 20), true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FeedShiftingRaisedCountsToward("low", raised, tc.targetDate); got != tc.want {
				t.Fatalf("FeedShiftingRaisedCountsToward(low, %s, %s) = %t, want %t",
					raised.Format(time.RFC3339), tc.targetDate.Format("2006-01-02"), got, tc.want)
			}
		})
	}
}

// TestRaisedAndAuthorizedRulesStayDistinct guards the merge this pair keeps inviting. The two rules
// look alike and answer different questions, and collapsing them silently changes which day a shed
// is fed for.
//
// An APPROVED movement counts from its authorization day with NO lead. A RAISED one counts from the
// day its animals are expected to walk. Feeding an unapproved movement's destination from the raise
// day would feed it a day early, every time.
func TestRaisedAndAuthorizedRulesStayDistinct(t *testing.T) {
	t.Parallel()

	at := istAt(2026, time.August, 10, 9, 0)

	authorized := FeedShiftingEffectiveBusinessDate(at)
	raised := FeedShiftingRaisedEffectiveBusinessDate("low", at)

	if !authorized.Equal(day(2026, time.August, 10)) {
		t.Fatalf("authorized effective date = %s, want the authorization day itself", authorized.Format("2006-01-02"))
	}
	if !raised.Equal(day(2026, time.August, 11)) {
		t.Fatalf("raised effective date = %s, want the day the animals are due to move", raised.Format("2006-01-02"))
	}
	if authorized.Equal(raised) {
		t.Fatal("the raised and authorized rules collapsed to one date; they answer different questions and must not be merged")
	}
}
