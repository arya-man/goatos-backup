package domain

import (
	"testing"
	"time"
)

// The feed day a sale reduces from is decided by the park's CORRECTION CUTOFF, not by the calendar
// day alone: a sale before 14:00 lands on tomorrow's sheet through the afternoon correction, a sale
// after it waits for the day after. Both sides of the cutoff are pinned on the same business date so
// the test cannot pass by accident of the date arithmetic.
func TestSaleFeedReductionDayFollowsTheParkCorrectionCutoff(t *testing.T) {
	clock := &WorkflowClock{Workflow: WorkflowNormal, DirectionTime: "09:00:00", CorrectionTime: "14:00:00"}
	ist := time.FixedZone("IST", 5*3600+1800)

	before := time.Date(2026, 9, 7, 13, 59, 0, 0, ist)
	if got, err := SaleFeedReductionDay(before, clock); err != nil || got != "2026-09-08" {
		t.Fatalf("sale before the cutoff: got %q err %v, want 2026-09-08", got, err)
	}
	at := time.Date(2026, 9, 7, 14, 0, 0, 0, ist)
	if got, err := SaleFeedReductionDay(at, clock); err != nil || got != "2026-09-09" {
		t.Fatalf("sale at the cutoff: got %q err %v, want 2026-09-09", got, err)
	}
	// The instant is compared in IST regardless of the zone it arrives in.
	utcAfter := time.Date(2026, 9, 7, 9, 30, 0, 0, time.UTC) // 15:00 IST
	if got, err := SaleFeedReductionDay(utcAfter, clock); err != nil || got != "2026-09-09" {
		t.Fatalf("UTC sale after the cutoff: got %q err %v, want 2026-09-09", got, err)
	}
}

// With no clock on record the rule falls back to the NEXT day -- the earlier of the two candidates,
// so a director checks a day early rather than a day late.
func TestSaleFeedReductionDayWithoutAClockIsTheNextDay(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	late := time.Date(2026, 9, 7, 23, 30, 0, 0, ist)
	if got, err := SaleFeedReductionDay(late, nil); err != nil || got != "2026-09-08" {
		t.Fatalf("no clock: got %q err %v, want 2026-09-08", got, err)
	}
	if got, err := SaleFeedReductionDay(late, &WorkflowClock{Workflow: WorkflowNormal}); err != nil || got != "2026-09-08" {
		t.Fatalf("blank correction time: got %q err %v, want 2026-09-08", got, err)
	}
}

// The herd ration's clock governs: an experiment-only clock list yields nil rather than the wrong
// workflow's cutoff.
func TestSaleFeedReductionClockPicksTheNormalWorkflow(t *testing.T) {
	clocks := []WorkflowClock{
		{Workflow: WorkflowExperiment, CorrectionTime: "13:00:00"},
		{Workflow: "Normal", CorrectionTime: "14:00:00"},
	}
	if got := SaleFeedReductionClock(clocks); got == nil || got.CorrectionTime != "14:00:00" {
		t.Fatalf("want the normal clock, got %+v", got)
	}
	if got := SaleFeedReductionClock(clocks[:1]); got != nil {
		t.Fatalf("experiment-only clocks must yield nil, got %+v", got)
	}
}
