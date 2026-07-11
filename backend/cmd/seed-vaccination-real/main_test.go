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

func mustKolkata(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load Asia/Kolkata: %v", err)
	}
	return loc
}
