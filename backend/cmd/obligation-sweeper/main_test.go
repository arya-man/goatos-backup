package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseFlagsMissedOptions(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-tenant-id", "00000000-0000-4000-8000-000000000001",
		"-mark-missed=false",
		"-missed-before", "2026-08-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.MarkMissed {
		t.Fatal("MarkMissed = true, want false")
	}
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if !cfg.MissedBefore.Equal(want) {
		t.Fatalf("MissedBefore = %s, want %s", cfg.MissedBefore, want)
	}
}

func TestParseFlagsRejectsNegativeMissedGrace(t *testing.T) {
	_, err := parseFlags([]string{
		"-tenant-id", "00000000-0000-4000-8000-000000000001",
		"-missed-grace", "-1s",
	})
	if err == nil || !strings.Contains(err.Error(), "missed-grace") {
		t.Fatalf("parseFlags error = %v, want missed-grace error", err)
	}
}
