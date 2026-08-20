package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func istDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := ParseHerdAnalyticsDate(raw)
	if err != nil {
		t.Fatalf("ParseHerdAnalyticsDate(%q): %v", raw, err)
	}
	return parsed
}

// A window a leader NAMED must come back exactly as named. The failure this pins is
// not a crash — it is being shown a different window under the label you chose, which
// looks like working software and reports the wrong number of births.
func TestResolveHerdAnalyticsWindowKeepsANamedWindowExactly(t *testing.T) {
	now := istDay(t, "2026-08-20")
	from, to, err := ResolveHerdAnalyticsWindow("2026-03-15", "2026-08-04", now)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if from != "2026-03-15" || to != "2026-08-04" {
		t.Fatalf("window=%s..%s, want 2026-03-15..2026-08-04", from, to)
	}
}

// Absent is the ONE case that defaults, and it starts on a MONTH BOUNDARY. A rolling
// 365 days would open the default view with a half-empty first column that reads as a
// collapse in births rather than the edge of the window.
func TestResolveHerdAnalyticsWindowDefaultsOnlyWhenBothBoundsAreAbsent(t *testing.T) {
	now := time.Date(2026, 8, 20, 14, 30, 0, 0, biztime.DefaultLocation())
	from, to, err := ResolveHerdAnalyticsWindow("", "", now)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if to != "2026-08-20" {
		t.Fatalf("to=%s, want today", to)
	}
	if from != "2025-09-01" {
		t.Fatalf("from=%s, want the first of the month eleven back", from)
	}
}

// Every invalid shape is REJECTED, never rewritten. Half a window is the one worth
// calling out: silently filling in the missing bound would answer a question the
// caller did not ask.
func TestResolveHerdAnalyticsWindowRejectsRatherThanRewrites(t *testing.T) {
	now := istDay(t, "2026-08-20")
	for _, tc := range []struct{ name, from, to string }{
		{"only from", "2026-03-01", ""},
		{"only to", "", "2026-08-20"},
		{"malformed from", "March", "2026-08-20"},
		{"impossible month", "2026-13-01", "2026-08-20"},
		{"a month, not a day", "2026-03", "2026-08-20"},
		{"reversed", "2026-08-20", "2026-03-01"},
		{"wider than the maximum", "2020-01-01", "2026-08-20"},
	} {
		if _, _, err := ResolveHerdAnalyticsWindow(tc.from, tc.to, now); err == nil {
			t.Fatalf("%s: expected rejection, got none", tc.name)
		}
	}
}

// The span is INCLUSIVE on both ends: one day is a valid window, and the maximum is
// exactly HerdAnalyticsMaxDays wide rather than one short.
func TestResolveHerdAnalyticsWindowSpanCountsBothEnds(t *testing.T) {
	now := istDay(t, "2026-08-20")
	if _, _, err := ResolveHerdAnalyticsWindow("2026-08-20", "2026-08-20", now); err != nil {
		t.Fatalf("single day rejected: %v", err)
	}
	maxFrom := istDay(t, "2026-08-20").AddDate(0, 0, -(HerdAnalyticsMaxDays - 1)).Format(HerdAnalyticsDateLayout)
	if _, _, err := ResolveHerdAnalyticsWindow(maxFrom, "2026-08-20", now); err != nil {
		t.Fatalf("exactly %d days rejected: %v", HerdAnalyticsMaxDays, err)
	}
	overFrom := istDay(t, "2026-08-20").AddDate(0, 0, -HerdAnalyticsMaxDays).Format(HerdAnalyticsDateLayout)
	if _, _, err := ResolveHerdAnalyticsWindow(overFrom, "2026-08-20", now); err == nil {
		t.Fatalf("%d days accepted, want rejection", HerdAnalyticsMaxDays+1)
	}
}
