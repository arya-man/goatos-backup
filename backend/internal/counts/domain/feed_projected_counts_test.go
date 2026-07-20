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

func TestFeedShiftingLeadDays(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		priority string
		want     int
	}{
		// Governing-doc taxonomy (migration 000016): Priority is High/Low. 'high' is the fast lane
		// (the retired 'emergency' behaviour) and 'low' takes the standard two-day lead.
		{name: "high moves fast so feed follows one day behind", priority: "high", want: 1},
		{name: "low takes the standard two day lead", priority: "low", want: 2},
		{name: "priority is matched case insensitively", priority: "HIGH", want: 1},
		{name: "surrounding whitespace does not change the lead", priority: "  high  ", want: 1},
		// Falling back to the SHORTER lead would feed the destination shed early, which is the
		// direction that feeds the wrong shed. The longer lead only delays a projection that a
		// later day picks up anyway.
		{name: "unknown priority falls back to the safer standard lead", priority: "unrecognized", want: 2},
		{name: "blank priority falls back to the safer standard lead", priority: "", want: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FeedShiftingLeadDays(tc.priority); got != tc.want {
				t.Fatalf("FeedShiftingLeadDays(%q) = %d, want %d", tc.priority, got, tc.want)
			}
		})
	}
}

func TestFeedEffectiveBusinessDate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		approvedAt time.Time
		priority   string
		want       time.Time
	}{
		{
			name:       "high approved on the 10th is feed effective on the 11th",
			approvedAt: ist(2026, time.July, 10, 9),
			priority:   "high",
			want:       day(2026, time.July, 11),
		},
		{
			name:       "low approved on the 10th is feed effective on the 12th",
			approvedAt: ist(2026, time.July, 10, 9),
			priority:   "low",
			want:       day(2026, time.July, 12),
		},
		{
			name:       "time of day within the approval day is discarded",
			approvedAt: ist(2026, time.July, 10, 23),
			priority:   "low",
			want:       day(2026, time.July, 12),
		},
		{
			// 20:00 UTC on the 10th is 01:30 IST on the 11th. Deriving the business date from UTC
			// would start the lead a day early and feed the destination shed late.
			name:       "a late UTC approval is already the next India business day",
			approvedAt: time.Date(2026, time.July, 10, 20, 0, 0, 0, time.UTC),
			priority:   "high",
			want:       day(2026, time.July, 12),
		},
		{
			name:       "the lead crosses a month boundary",
			approvedAt: ist(2026, time.July, 31, 8),
			priority:   "low",
			want:       day(2026, time.August, 2),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FeedEffectiveBusinessDate(tc.approvedAt, tc.priority)
			if !got.Equal(tc.want) {
				t.Fatalf("FeedEffectiveBusinessDate(%s, %q) = %s, want %s",
					tc.approvedAt, tc.priority, got, tc.want)
			}
		})
	}
}

// TestFeedShiftingCountsToward is the timing rule proper: which feed days an approved-but-
// unexecuted movement contributes to.
func TestFeedShiftingCountsToward(t *testing.T) {
	t.Parallel()

	approvedOn10th := ist(2026, time.July, 10, 9)

	cases := []struct {
		name       string
		approvedAt time.Time
		priority   string
		target     time.Time
		want       bool
	}{
		// --- high: approval day X, effective X+1 ---
		{
			name:       "high does not count on the approval day itself",
			approvedAt: approvedOn10th,
			priority:   "high",
			target:     day(2026, time.July, 10),
			want:       false,
		},
		{
			name:       "high counts on the day after approval",
			approvedAt: approvedOn10th,
			priority:   "high",
			target:     day(2026, time.July, 11),
			want:       true,
		},

		// --- low: approval day X, effective X+2 ---
		{
			name:       "low does not count on the approval day itself",
			approvedAt: approvedOn10th,
			priority:   "low",
			target:     day(2026, time.July, 10),
			want:       false,
		},
		{
			name:       "low does not count one day after approval",
			approvedAt: approvedOn10th,
			priority:   "low",
			target:     day(2026, time.July, 11),
			want:       false,
		},
		{
			name:       "low counts two days after approval",
			approvedAt: approvedOn10th,
			priority:   "low",
			target:     day(2026, time.July, 12),
			want:       true,
		},

		// --- the <= half of the rule: an OVERDUE movement never drops out ---
		//
		// This is the case an == rule gets wrong. A movement that came due for feed days ago and
		// still has not been executed must keep counting on every later day; otherwise the
		// destination shed silently stops being fed for animals still expected to arrive.
		{
			name:       "an overdue high still counts five days after approval",
			approvedAt: approvedOn10th,
			priority:   "high",
			target:     day(2026, time.July, 15),
			want:       true,
		},
		{
			name:       "an overdue low still counts five days after approval",
			approvedAt: approvedOn10th,
			priority:   "low",
			target:     day(2026, time.July, 15),
			want:       true,
		},
		{
			name:       "an overdue movement still counts a month later",
			approvedAt: approvedOn10th,
			priority:   "low",
			target:     day(2026, time.August, 10),
			want:       true,
		},

		// --- target dates carrying a time component still resolve to their business day ---
		{
			name:       "a target instant late in the day is still that business day",
			approvedAt: approvedOn10th,
			priority:   "high",
			target:     ist(2026, time.July, 10, 23),
			want:       false,
		},
		{
			name:       "a target instant late on the effective day counts",
			approvedAt: approvedOn10th,
			priority:   "high",
			target:     ist(2026, time.July, 11, 23),
			want:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FeedShiftingCountsToward(tc.approvedAt, tc.priority, tc.target)
			if got != tc.want {
				t.Fatalf("FeedShiftingCountsToward(approved=%s, priority=%q, target=%s) = %t, want %t",
					tc.approvedAt, tc.priority, tc.target, got, tc.want)
			}
		})
	}
}

func TestFeedShiftingIsOverdue(t *testing.T) {
	t.Parallel()

	approvedOn10th := ist(2026, time.July, 10, 9)

	cases := []struct {
		name       string
		approvedAt time.Time
		priority   string
		target     time.Time
		want       bool
	}{
		{
			name:       "not yet effective is not overdue",
			approvedAt: approvedOn10th,
			priority:   "high",
			target:     day(2026, time.July, 10),
			want:       false,
		},
		{
			// Due exactly today is on time, not late. An off-by-one here would flag every
			// correctly-timed movement as a problem and train operators to ignore the flag.
			name:       "effective exactly on the target day is on time not overdue",
			approvedAt: approvedOn10th,
			priority:   "high",
			target:     day(2026, time.July, 11),
			want:       false,
		},
		{
			name:       "effective before the target day is overdue",
			approvedAt: approvedOn10th,
			priority:   "high",
			target:     day(2026, time.July, 12),
			want:       true,
		},
		{
			name:       "low effective exactly on the target day is not overdue",
			approvedAt: approvedOn10th,
			priority:   "low",
			target:     day(2026, time.July, 12),
			want:       false,
		},
		{
			name:       "low five days past approval is overdue",
			approvedAt: approvedOn10th,
			priority:   "low",
			target:     day(2026, time.July, 15),
			want:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FeedShiftingIsOverdue(tc.approvedAt, tc.priority, tc.target)
			if got != tc.want {
				t.Fatalf("FeedShiftingIsOverdue(approved=%s, priority=%q, target=%s) = %t, want %t",
					tc.approvedAt, tc.priority, tc.target, got, tc.want)
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
	for _, priority := range []string{"high", "low", "unknown"} {
		for offset := 0; offset <= 20; offset++ {
			target := day(2026, time.July, 10).AddDate(0, 0, offset)
			overdue := FeedShiftingIsOverdue(approvedAt, priority, target)
			counts := FeedShiftingCountsToward(approvedAt, priority, target)
			if overdue && !counts {
				t.Fatalf("priority %q at +%dd is overdue but not counted", priority, offset)
			}
		}
	}
}
