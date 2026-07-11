package main

import (
	"testing"
	"time"
)

func TestNextDueAfterLastVaccinationUsesHistoricalCellAsAnchor(t *testing.T) {
	loc := mustKolkata(t)
	asOf := time.Date(2026, time.July, 11, 17, 4, 0, 0, loc)
	lastAdministered := sourceVaccinationDateTime(time.Date(2026, time.June, 21, 0, 0, 0, 0, loc), loc)

	got := nextDueAfterLastVaccination(lastAdministered, "ET+TT", asOf)
	want := time.Date(2026, time.December, 20, 9, 0, 0, 0, loc)

	if !got.Equal(want) {
		t.Fatalf("next due = %s, want %s", got, want)
	}
	if !got.After(asOf) {
		t.Fatalf("next due = %s must be after import as-of %s", got, asOf)
	}
}

func TestNextDueAfterLastVaccinationRollsForwardPastImportAsOf(t *testing.T) {
	loc := mustKolkata(t)
	asOf := time.Date(2026, time.July, 11, 17, 4, 0, 0, loc)
	lastAdministered := sourceVaccinationDateTime(time.Date(2025, time.January, 1, 0, 0, 0, 0, loc), loc)

	got := nextDueAfterLastVaccination(lastAdministered, "ET+TT", asOf)
	want := time.Date(2026, time.December, 30, 9, 0, 0, 0, loc)

	if !got.Equal(want) {
		t.Fatalf("next due = %s, want %s", got, want)
	}
	if !got.After(asOf) {
		t.Fatalf("next due = %s must be after import as-of %s", got, asOf)
	}
}

func TestSourceVaccinationDateIsHistoryUsesBusinessDateNotClockTime(t *testing.T) {
	loc := mustKolkata(t)
	sourceDate := time.Date(2026, time.July, 11, 0, 0, 0, 0, loc)
	asOfBeforeSeedTimestamp := time.Date(2026, time.July, 11, 6, 30, 0, 0, loc)

	if !sourceVaccinationDateIsHistory(sourceDate, asOfBeforeSeedTimestamp, loc) {
		t.Fatalf("same source business date must import as completed history even before 09:00 as_of=%s", asOfBeforeSeedTimestamp)
	}
}

func TestSourceVaccinationDateIsHistoryRejectsFutureBusinessDate(t *testing.T) {
	loc := mustKolkata(t)
	sourceDate := time.Date(2026, time.July, 12, 0, 0, 0, 0, loc)
	asOf := time.Date(2026, time.July, 11, 23, 59, 0, 0, loc)

	if sourceVaccinationDateIsHistory(sourceDate, asOf, loc) {
		t.Fatalf("future source business date must remain scheduled, source=%s as_of=%s", sourceDate, asOf)
	}
}

func mustKolkata(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load Asia/Kolkata: %v", err)
	}
	return loc
}
