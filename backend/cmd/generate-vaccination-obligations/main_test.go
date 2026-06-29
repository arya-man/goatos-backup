package main

import (
	"testing"
	"time"
)

func TestParseFlagsDefaultsAsOfToUTCDayBucket(t *testing.T) {
	now := func() time.Time {
		return time.Date(2026, time.June, 29, 15, 4, 5, 0, time.UTC)
	}

	cfg, err := parseFlags([]string{"-tenant-id", "00000000-0000-4000-8000-000000000001"}, now)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	want := time.Date(2026, time.June, 29, 0, 0, 0, 0, time.UTC)
	if !cfg.AsOf.Equal(want) {
		t.Fatalf("AsOf=%s, want stable UTC day bucket %s", cfg.AsOf, want)
	}
}

func TestParseFlagsExplicitAsOfOverridesDayBucket(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-tenant-id", "00000000-0000-4000-8000-000000000001",
		"-as-of", "2026-06-29T12:34:56Z",
	}, func() time.Time {
		return time.Date(2026, time.June, 29, 15, 4, 5, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	want := time.Date(2026, time.June, 29, 12, 34, 56, 0, time.UTC)
	if !cfg.AsOf.Equal(want) {
		t.Fatalf("AsOf=%s, want explicit value %s", cfg.AsOf, want)
	}
}
