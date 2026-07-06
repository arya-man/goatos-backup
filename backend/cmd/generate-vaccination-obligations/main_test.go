package main

import (
	"errors"
	"testing"
	"time"
)

func TestParseFlagsDefaultsAsOfToIndiaBusinessDayBucket(t *testing.T) {
	now := func() time.Time {
		return time.Date(2026, time.June, 29, 15, 4, 5, 0, time.UTC)
	}

	cfg, err := parseFlags([]string{"-tenant-id", "00000000-0000-4000-8000-000000000001"}, now)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	want := startOfIndiaBusinessDay(time.Date(2026, time.June, 29, 15, 4, 5, 0, time.UTC))
	if !cfg.AsOf.Equal(want) {
		t.Fatalf("AsOf=%s, want stable India business-day bucket %s", cfg.AsOf, want)
	}
	if cfg.AsOf.Location().String() != "Asia/Kolkata" {
		t.Fatalf("AsOf location=%s, want Asia/Kolkata", cfg.AsOf.Location())
	}
	if cfg.RecoveryRepairLimit != 1000 || cfg.RecoveryRepairAge != 7*24*time.Hour {
		t.Fatalf("recovery repair defaults limit=%d age=%s", cfg.RecoveryRepairLimit, cfg.RecoveryRepairAge)
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
	want := time.Date(2026, time.June, 29, 18, 4, 56, 0, cfg.AsOf.Location())
	if !cfg.AsOf.Equal(want) {
		t.Fatalf("AsOf=%s, want explicit value %s", cfg.AsOf, want)
	}
	if cfg.AsOf.Location().String() != "Asia/Kolkata" {
		t.Fatalf("explicit AsOf location=%s, want Asia/Kolkata", cfg.AsOf.Location())
	}
}

func TestParseFlagsRecoveryRepairControls(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-tenant-id", "00000000-0000-4000-8000-000000000001",
		"-recovery-repair-limit", "250",
		"-recovery-repair-age", "72h",
	}, func() time.Time {
		return time.Date(2026, time.June, 29, 15, 4, 5, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.RecoveryRepairLimit != 250 || cfg.RecoveryRepairAge != 72*time.Hour {
		t.Fatalf("recovery repair controls limit=%d age=%s", cfg.RecoveryRepairLimit, cfg.RecoveryRepairAge)
	}
}

func TestExitCodeForPartialFailure(t *testing.T) {
	err := withExitCode(exitCodePartialFailure, errors.New("partial generation failure"))
	if got := exitCodeForError(err); got != exitCodePartialFailure {
		t.Fatalf("exitCodeForError(partial)=%d, want %d", got, exitCodePartialFailure)
	}
	if got := exitCodeForError(errors.New("hard failure")); got != exitCodeHardFailure {
		t.Fatalf("exitCodeForError(hard)=%d, want %d", got, exitCodeHardFailure)
	}
	if got := exitCodeForError(nil); got != 0 {
		t.Fatalf("exitCodeForError(nil)=%d, want 0", got)
	}
}
