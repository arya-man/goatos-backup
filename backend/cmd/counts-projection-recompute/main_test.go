package main

import (
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)

func TestParseFlagsRequiresTargetDateForProjectedHorizon(t *testing.T) {
	_, err := parseFlags([]string{
		"-tenant-id", "tenant-1",
		"-park-id", "park-1",
		"-horizon", "feed_target_date",
	}, func() time.Time { return fixedNow })
	if err == nil || !strings.Contains(err.Error(), "target-date is required") {
		t.Fatalf("err=%v, want target-date required", err)
	}
}

func TestParseFlagsAllowsCountAsOfWithoutTargetDate(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-tenant-id", "tenant-1",
		"-park-id", "park-1",
		"-horizon", "count_as_of",
		"-as-of", "2026-06-30T13:30:00Z",
	}, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !cfg.TargetDate.IsZero() {
		t.Fatalf("target date=%s, want zero before service normalization", cfg.TargetDate)
	}
	if cfg.AsOf.Format(time.RFC3339) != "2026-06-30T13:30:00Z" {
		t.Fatalf("as_of=%s", cfg.AsOf.Format(time.RFC3339))
	}
}

func TestParseFlagsNormalizesTargetDate(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-tenant-id", "tenant-1",
		"-park-id", "park-1",
		"-target-date", "2026-07-01T15:45:00Z",
	}, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if got := cfg.TargetDate.Format(time.RFC3339); got != "2026-07-01T00:00:00Z" {
		t.Fatalf("target_date=%s, want day boundary", got)
	}
	if got := horizons(cfg.Horizon); len(got) != 2 || got[0] != horizonCountAsOf || got[1] != horizonFeedTargetDate {
		t.Fatalf("horizons=%v, want count_as_of/feed_target_date", got)
	}
}

func TestParseFlagsRejectsInvalidHorizon(t *testing.T) {
	_, err := parseFlags([]string{
		"-tenant-id", "tenant-1",
		"-park-id", "park-1",
		"-horizon", "all_time",
		"-target-date", "2026-07-01",
	}, func() time.Time { return fixedNow })
	if err == nil || !strings.Contains(err.Error(), "horizon must be") {
		t.Fatalf("err=%v, want invalid horizon", err)
	}
}

func TestParseDateOrInstantRejectsInvalidDate(t *testing.T) {
	if _, err := parseDateOrInstant("tomorrow"); err == nil || !strings.Contains(err.Error(), "target-date must be") {
		t.Fatalf("err=%v, want invalid target-date", err)
	}
}
