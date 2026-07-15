package main

import (
	"strings"
	"testing"
)

// mkFact builds a sourceFact for a cell with the given disposition, mirroring addFact's lineage key.
func mkFact(animalKey, vaccine, dose string, seq int, val, disposition string) sourceFact {
	c := vaccCell{AnimalKey: animalKey, Vaccine: vaccine, DoseCode: dose, Sequence: seq, Value: val}
	return sourceFact{
		lineageKey:    sourceFactLineageKey(c),
		animalKey:     animalKey,
		vaccineHeader: vaccine,
		doseCode:      sourceDoseCode(c),
		sequence:      seq,
		sourceValue:   val,
		disposition:   disposition,
	}
}

// TestSourceFactLineageKeyIsUniquePerCell proves distinct source cells never collide and the same
// cell always yields the same lineage identity (VACC-REV-01 exactly-once foundation).
func TestSourceFactLineageKeyIsUniquePerCell(t *testing.T) {
	base := vaccCell{AnimalKey: "goat-1", Vaccine: "ET+TT", DoseCode: "first", Sequence: 3, Value: "2026-06-21"}
	if got, want := sourceFactLineageKey(base), "goat-1|ET+TT|first|3|2026-06-21"; got != want {
		t.Fatalf("lineage key = %q, want %q", got, want)
	}
	// Any single varying field changes the key -> no two distinct cells share a lineage.
	variants := []vaccCell{
		{AnimalKey: "goat-2", Vaccine: "ET+TT", DoseCode: "first", Sequence: 3, Value: "2026-06-21"},
		{AnimalKey: "goat-1", Vaccine: "FMD", DoseCode: "first", Sequence: 3, Value: "2026-06-21"},
		{AnimalKey: "goat-1", Vaccine: "ET+TT", DoseCode: "booster", Sequence: 3, Value: "2026-06-21"},
		{AnimalKey: "goat-1", Vaccine: "ET+TT", DoseCode: "first", Sequence: 4, Value: "2026-06-21"},
		{AnimalKey: "goat-1", Vaccine: "ET+TT", DoseCode: "first", Sequence: 3, Value: "2026-06-22"},
	}
	seen := map[string]bool{sourceFactLineageKey(base): true}
	for _, v := range variants {
		k := sourceFactLineageKey(v)
		if seen[k] {
			t.Fatalf("lineage collision for variant %+v -> %q", v, k)
		}
		seen[k] = true
	}
}

// TestReconcileSourceFactLineageCountsEachFactExactlyOnce proves the VACC-REV-01 gate accepts a
// ledger where every dated fact appears once, and rejects double-counting / gaps.
func TestReconcileSourceFactLineageCountsEachFactExactlyOnce(t *testing.T) {
	facts := []sourceFact{
		mkFact("goat-1", "ET+TT", "first", 1, "2026-06-21", dispositionImportedCompletion),
		mkFact("goat-1", "FMD", "first", 2, "2026-06-21", dispositionImportedCompletion),
		mkFact("goat-2", "ET+TT", "first", 1, "2030-01-01", dispositionScheduledObligation),
		mkFact("goat-3", "ET+TT", "first", 1, "bad-date", dispositionUnresolved),
		mkFact("goat-4", "ET+TT", "first", 1, "2026-06-21", dispositionExcludedGoatNotPlaced),
	}
	if err := reconcileSourceFactLineage(len(facts), facts); err != nil {
		t.Fatalf("valid ledger rejected: %v", err)
	}

	// dated-count mismatch (a fact vanished before the ledger).
	if err := reconcileSourceFactLineage(len(facts)+1, facts); err == nil {
		t.Fatal("expected failure when dated count exceeds ledger facts")
	}
}

// TestReconcileSourceFactLineageRejectsCollision proves that two ledger entries sharing a lineage
// key (a double-count) fail the gate rather than silently inflating the denominator.
func TestReconcileSourceFactLineageRejectsCollision(t *testing.T) {
	dup := mkFact("goat-1", "ET+TT", "first", 1, "2026-06-21", dispositionImportedCompletion)
	facts := []sourceFact{dup, dup}
	err := reconcileSourceFactLineage(len(facts), facts)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("expected lineage collision error, got %v", err)
	}
}

// TestReconcileSourceFactLineageFailsOnUnknownVaccineHeader proves Fix Plan A1 [P1]: a dated cell
// under an unrecognized vaccine header FAILS the seed instead of vanishing outside the denominator.
func TestReconcileSourceFactLineageFailsOnUnknownVaccineHeader(t *testing.T) {
	facts := []sourceFact{
		mkFact("goat-1", "ET+TT", "first", 1, "2026-06-21", dispositionImportedCompletion),
		mkFact("goat-2", "TYPO_VACCINE", "first", 1, "2026-06-21", dispositionExcludedVaccineUnknown),
	}
	err := reconcileSourceFactLineage(len(facts), facts)
	if err == nil || !strings.Contains(err.Error(), "unknown vaccine header") {
		t.Fatalf("expected unknown-header failure, got %v", err)
	}
}
