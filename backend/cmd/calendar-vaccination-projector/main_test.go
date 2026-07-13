package main

import (
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func TestParseFlagsRequiresTenantID(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "")
	_, err := parseFlags([]string{"-timeout", "1s"})
	if err == nil || !strings.Contains(err.Error(), "tenant-id is required") {
		t.Fatalf("parseFlags err = %v, want tenant-id required", err)
	}
}

func TestParseFlagsEnablesClosedProjectionPruneByDefault(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "00000000-0000-4000-8000-000000000001")
	cfg, err := parseFlags([]string{"-timeout", "1s"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !cfg.PruneClosed {
		t.Fatalf("PruneClosed = false, want true")
	}
	if cfg.PruneLimit != 1000 {
		t.Fatalf("PruneLimit = %d, want 1000", cfg.PruneLimit)
	}
	if cfg.ClosedRetention.String() != "2160h0m0s" {
		t.Fatalf("ClosedRetention = %s, want 2160h", cfg.ClosedRetention)
	}
}

func TestParseFlagsCanDisableClosedProjectionPrune(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "00000000-0000-4000-8000-000000000001")
	cfg, err := parseFlags([]string{"-timeout", "1s", "-prune-closed=false", "-prune-limit=25", "-closed-retention=720h"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.PruneClosed {
		t.Fatalf("PruneClosed = true, want false")
	}
	if cfg.PruneLimit != 25 {
		t.Fatalf("PruneLimit = %d, want 25", cfg.PruneLimit)
	}
	if cfg.ClosedRetention.String() != "720h0m0s" {
		t.Fatalf("ClosedRetention = %s, want 720h", cfg.ClosedRetention)
	}
}

func TestParseFlagsDefaultsCoverWholeBusinessDays(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "00000000-0000-4000-8000-000000000001")
	cfg, err := parseFlags([]string{"-timeout", "1s"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !cfg.DateFrom.Equal(biztime.BusinessDayStart(cfg.DateFrom)) {
		t.Fatalf("DateFrom = %s, want business-day midnight", cfg.DateFrom.Format(time.RFC3339))
	}
	if !cfg.DateTo.Equal(biztime.BusinessDayStart(cfg.DateTo)) {
		t.Fatalf("DateTo = %s, want business-day midnight", cfg.DateTo.Format(time.RFC3339))
	}
	if cfg.DateTo.Sub(cfg.DateFrom) < 47*24*time.Hour {
		t.Fatalf("projection window = %s, want at least 47d", cfg.DateTo.Sub(cfg.DateFrom))
	}
}

func TestParseFlagsDefaultWindowCoversCalendarUIWeekAndMonth(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "00000000-0000-4000-8000-000000000001")
	cases := []struct {
		name       string
		now        string
		windowFrom time.Time
		windowTo   time.Time
	}{
		{
			name:       "wednesday_week_starts_monday",
			now:        "2026-07-15T13:00:00+05:30",
			windowFrom: mustISTDate(t, "2026-07-13"),
			windowTo:   mustISTDate(t, "2026-07-19"),
		},
		{
			name:       "sunday_week_can_start_in_previous_month",
			now:        "2026-08-02T13:00:00+05:30",
			windowFrom: mustISTDate(t, "2026-07-27"),
			windowTo:   mustISTDate(t, "2026-08-02"),
		},
		{
			name:       "mid_month_picker_requests_first_to_last_day",
			now:        "2026-07-15T13:00:00+05:30",
			windowFrom: mustISTDate(t, "2026-07-01"),
			windowTo:   mustISTDate(t, "2026-07-31"),
		},
		{
			name:       "previous_month_picker_requests_first_to_last_day",
			now:        "2026-07-15T13:00:00+05:30",
			windowFrom: mustISTDate(t, "2026-06-01"),
			windowTo:   mustISTDate(t, "2026-06-30"),
		},
		{
			name:       "next_month_picker_requests_inclusive_last_day",
			now:        "2026-07-15T13:00:00+05:30",
			windowFrom: mustISTDate(t, "2026-08-01"),
			windowTo:   mustISTDate(t, "2026-08-31"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			freezeProjectorNow(t, mustISTInstant(t, tc.now))
			cfg, err := parseFlags([]string{"-timeout", "1s"})
			if err != nil {
				t.Fatalf("parseFlags: %v", err)
			}
			if !projectionCoversInclusiveWindow(cfg.DateFrom, cfg.DateTo, tc.windowFrom, tc.windowTo) {
				t.Fatalf("projection [%s,%s) does not cover UI window [%s,%s]",
					cfg.DateFrom.Format(time.RFC3339),
					cfg.DateTo.Format(time.RFC3339),
					tc.windowFrom.Format("2006-01-02"),
					tc.windowTo.Format("2006-01-02"),
				)
			}
		})
	}
}

func TestDefaultUpcomingProjectionWindowExactAdjacentMonthHorizon(t *testing.T) {
	from, to := defaultUpcomingProjectionWindow(mustISTInstant(t, "2026-07-15T13:00:00+05:30"))
	if !from.Equal(mustISTDate(t, "2026-06-01")) {
		t.Fatalf("from=%s, want previous month start", from.Format(time.RFC3339))
	}
	if !to.Equal(mustISTDate(t, "2026-09-01")) {
		t.Fatalf("to=%s, want exclusive bound after next month", to.Format(time.RFC3339))
	}
}

func freezeProjectorNow(t *testing.T, now time.Time) {
	t.Helper()
	previous := nowInBusinessLocation
	nowInBusinessLocation = func() time.Time { return now }
	t.Cleanup(func() { nowInBusinessLocation = previous })
}

func mustISTInstant(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse time %q: %v", raw, err)
	}
	return parsed.In(biztime.DefaultLocation())
}

func mustISTDate(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation())
	if err != nil {
		t.Fatalf("parse date %q: %v", raw, err)
	}
	return parsed
}

func projectionCoversInclusiveWindow(projectedFrom, projectedTo, requestFrom, requestTo time.Time) bool {
	return !requestFrom.Before(projectedFrom) && !requestTo.Add(24*time.Hour).After(projectedTo)
}
