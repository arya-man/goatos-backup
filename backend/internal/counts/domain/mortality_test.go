package domain

import (
	"testing"
	"time"
)

func i64(v int64) *int64 { return &v }

// The season split is a business rule about where the farm is, and it must cover every month
// exactly once: a month in two seasons double-counts a death, a month in none drops it.
func TestMortalitySeasonKeyCoversEveryMonthOnce(t *testing.T) {
	want := map[time.Month]string{
		time.January: "winter", time.February: "winter", time.December: "winter",
		time.March: "summer", time.April: "summer", time.May: "summer",
		time.June: "monsoon", time.July: "monsoon", time.August: "monsoon", time.September: "monsoon",
		time.October: "post_monsoon", time.November: "post_monsoon",
	}
	for m := time.January; m <= time.December; m++ {
		if got := MortalitySeasonKey(m); got != want[m] {
			t.Fatalf("%s: season=%q want %q", m, got, want[m])
		}
		if MortalitySeasonLabel(want[m]) == want[m] {
			t.Fatalf("%s: season key %q has no farm label", m, want[m])
		}
	}
	if len(MortalitySeasonOrder) != 4 {
		t.Fatalf("season order must name the four seasons, got %v", MortalitySeasonOrder)
	}
}

// Age bands at their exact boundaries. Day 7 is neonatal, day 8 is not; a missing or
// negative age is "unknown" and never folded into a band.
func TestMortalityAgeBandKeyBoundaries(t *testing.T) {
	cases := []struct {
		age  *int64
		want string
	}{
		{nil, "unknown"}, {i64(-1), "unknown"},
		{i64(0), "d0_7"}, {i64(7), "d0_7"}, {i64(8), "d8_30"}, {i64(30), "d8_30"},
		{i64(31), "d31_90"}, {i64(90), "d31_90"}, {i64(91), "d91_180"}, {i64(180), "d91_180"},
		{i64(181), "d181_365"}, {i64(365), "d181_365"}, {i64(366), "over_1y"}, {i64(4000), "over_1y"},
	}
	for _, c := range cases {
		if got := MortalityAgeBandKey(c.age); got != c.want {
			t.Fatalf("age %v: band=%q want %q", c.age, got, c.want)
		}
	}
	for _, key := range MortalityAgeBandOrder {
		if MortalityAgeBandLabel(key) == key {
			t.Fatalf("age band %q has no farm label", key)
		}
	}
}

func TestMortalityDaysSinceKeyBoundaries(t *testing.T) {
	cases := []struct {
		days *int64
		want string
	}{
		{nil, "unknown"}, {i64(0), "d0_7"}, {i64(7), "d0_7"}, {i64(8), "d8_30"}, {i64(30), "d8_30"},
		{i64(31), "d31_90"}, {i64(90), "d31_90"}, {i64(91), "over_90"},
	}
	for _, c := range cases {
		if got := MortalityDaysSinceKey(c.days); got != c.want {
			t.Fatalf("days %v: band=%q want %q", c.days, got, c.want)
		}
	}
	// The two days-since series share the bands but say different things about "unknown".
	if MortalityDaysSinceArrivalLabel("unknown") == MortalityDaysSinceVaccineLabel("unknown") {
		t.Fatalf("arrival and vaccination must explain an unknown differently")
	}
}

// A rate with nothing to divide by is ABSENT, never 0%: 0 deaths of 0 animals is not a
// healthy herd, it is an empty bucket. And the rate is one-decimal, so the page prints what
// a farm reads a mortality rate at.
func TestMortalityRatePct(t *testing.T) {
	if MortalityRatePct(0, 0) != nil || MortalityRatePct(3, 0) != nil {
		t.Fatalf("a zero denominator must yield no rate")
	}
	if got := *MortalityRatePct(3, 40); got != 7.5 {
		t.Fatalf("3/40 = %v, want 7.5", got)
	}
	if got := *MortalityRatePct(1, 3); got != 33.3 {
		t.Fatalf("1/3 = %v, want 33.3", got)
	}
	if got := *MortalityRatePct(0, 400); got != 0 {
		t.Fatalf("0/400 = %v, want 0", got)
	}
}

// The deaths list's pager is resolved, never trusted and never refused. A size the pager does not
// offer, a negative offset and an offset past the clamp all land on the first page at the default
// size -- the rest of the payload is whole-window, so failing it over a pager parameter would hide
// every figure the reader came for. A size the pager DOES offer is honoured exactly.
func TestResolveMortalityRecentPageLandsNonsenseOnTheFirstPage(t *testing.T) {
	for _, size := range MortalityRecentPageSizes {
		if limit, offset := ResolveMortalityRecentPage(size, 30); limit != size || offset != 30 {
			t.Fatalf("offered size %d resolved to %d/%d, want %d/30", size, limit, offset, size)
		}
	}
	for _, tc := range []struct {
		name          string
		limit, offset int
		wantLimit     int
		wantOffset    int
	}{
		{"size outside the vocabulary", 7, 10, MortalityRecentLimit, 10},
		{"zero size (absent parameter)", 0, 0, MortalityRecentLimit, 0},
		{"negative offset", 25, -1, 25, 0},
		{"offset past the clamp", 25, MortalityRecentMaxOffset + 1, 25, 0},
		{"offset exactly at the clamp is kept", 25, MortalityRecentMaxOffset, 25, MortalityRecentMaxOffset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limit, offset := ResolveMortalityRecentPage(tc.limit, tc.offset)
			if limit != tc.wantLimit || offset != tc.wantOffset {
				t.Fatalf("resolved to %d/%d, want %d/%d", limit, offset, tc.wantLimit, tc.wantOffset)
			}
		})
	}
}
