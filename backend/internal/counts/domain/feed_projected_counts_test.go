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
