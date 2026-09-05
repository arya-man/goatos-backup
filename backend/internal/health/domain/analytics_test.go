package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func istDate(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation(HealthAnalyticsDateLayout, value, biztime.DefaultLocation())
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

// The default window opens on a MONTH BOUNDARY, not "today minus N days". The
// flow chart buckets by business month, so an arbitrary start day would put a
// half-empty first column on the default view that reads as a collapse in cases
// rather than the edge of the window.
func TestHealthAnalyticsDefaultWindowOpensOnAMonthBoundary(t *testing.T) {
	// Mid-month, well clear of the history floor.
	now := istDate(t, "2027-03-17").Add(15 * time.Hour)
	from, to := HealthAnalyticsDefaultWindow(now)
	if from != "2026-10-01" {
		t.Fatalf("from = %q, want the first of the month five back", from)
	}
	if to != "2027-03-17" {
		t.Fatalf("to = %q, want today", to)
	}
}

// The floor binds the DEFAULT only. Before the module has six months of history
// a default reaching further back would pad the charts with empty months that
// read as a herd with nothing wrong with it.
func TestHealthAnalyticsDefaultWindowIsClampedToTheHistoryFloor(t *testing.T) {
	now := istDate(t, "2026-09-05").Add(9 * time.Hour)
	from, _ := HealthAnalyticsDefaultWindow(now)
	if from != HealthAnalyticsFloorDate {
		t.Fatalf("from = %q, want the floor %q", from, HealthAnalyticsFloorDate)
	}
}

// A NAMED window is served exactly as named, floor or no floor: the clamp is a
// default, never a rewrite of what a director asked for.
func TestResolveHealthAnalyticsWindowServesANamedWindowBelowTheFloor(t *testing.T) {
	from, to, err := ResolveHealthAnalyticsWindow("2026-05-01", "2026-06-30", istDate(t, "2026-09-05"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if from != "2026-05-01" || to != "2026-06-30" {
		t.Fatalf("window = %q..%q, want it served verbatim", from, to)
	}
}

// Every invalid shape is REJECTED, never quietly rewritten to the default. A
// director who names a window must see that window or an error -- never a
// different window under the label they chose.
func TestResolveHealthAnalyticsWindowRejectsRatherThanRewrites(t *testing.T) {
	now := istDate(t, "2026-09-05")
	cases := []struct {
		name     string
		from, to string
	}{
		{"only from", "2026-08-01", ""},
		{"only to", "", "2026-08-01"},
		{"malformed from", "01-08-2026", "2026-08-31"},
		{"malformed to", "2026-08-01", "31st August"},
		{"reversed", "2026-08-31", "2026-08-01"},
		{"wider than the cap", "2020-01-01", "2026-09-05"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ResolveHealthAnalyticsWindow(tc.from, tc.to, now); err == nil {
				t.Fatal("want an error, got a silently substituted window")
			}
		})
	}
}

func TestResolveHealthAnalyticsWindowDefaultsWhenBothAreAbsent(t *testing.T) {
	now := istDate(t, "2027-03-17")
	from, to, err := ResolveHealthAnalyticsWindow("  ", "", now)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	wantFrom, wantTo := HealthAnalyticsDefaultWindow(now)
	if from != wantFrom || to != wantTo {
		t.Fatalf("window = %q..%q, want the default %q..%q", from, to, wantFrom, wantTo)
	}
}

// The exact cap is allowed; one day past it is not. An off-by-one here would
// either reject a legal three-year window or serve one wider than the read is
// planned for.
func TestResolveHealthAnalyticsWindowBoundaryIsInclusive(t *testing.T) {
	now := istDate(t, "2030-01-01")
	start := istDate(t, "2026-01-01")
	atCap := start.AddDate(0, 0, HealthAnalyticsMaxDays-1).Format(HealthAnalyticsDateLayout)
	if _, _, err := ResolveHealthAnalyticsWindow("2026-01-01", atCap, now); err != nil {
		t.Fatalf("a window of exactly %d days must be served: %v", HealthAnalyticsMaxDays, err)
	}
	overCap := start.AddDate(0, 0, HealthAnalyticsMaxDays).Format(HealthAnalyticsDateLayout)
	if _, _, err := ResolveHealthAnalyticsWindow("2026-01-01", overCap, now); err == nil {
		t.Fatalf("a window of %d days must be rejected", HealthAnalyticsMaxDays+1)
	}
}

// A zero denominator is 0, never NaN: NaN serializes to null and renders as a
// broken cell, and "no cases" is not "no answer".
func TestHealthAnalyticsPctRoundsToOneDecimalAndSurvivesZero(t *testing.T) {
	for _, tc := range []struct {
		part, whole int64
		want        float64
	}{
		{0, 0, 0},
		{5, 0, 0},
		{1, 3, 33.3},
		{2, 3, 66.7},
		{9, 62, 14.5},
		{62, 62, 100},
	} {
		if got := HealthAnalyticsPct(tc.part, tc.whole); got != tc.want {
			t.Errorf("HealthAnalyticsPct(%d,%d) = %v, want %v", tc.part, tc.whole, got, tc.want)
		}
	}
}

func TestAgeBandSummaryNamesBothOnlyWhenBothArePresent(t *testing.T) {
	for _, tc := range []struct {
		adults, kids int64
		want         string
	}{
		{0, 0, ""},
		{3, 0, "adult"},
		{0, 3, "kid"},
		{3, 3, "both"},
	} {
		if got := AgeBandSummary(tc.adults, tc.kids); got != tc.want {
			t.Errorf("AgeBandSummary(%d,%d) = %q, want %q", tc.adults, tc.kids, got, tc.want)
		}
	}
}
