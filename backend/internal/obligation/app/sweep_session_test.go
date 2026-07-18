package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

func TestSelectIDsWithinVisitShotCapForSessionFiltersUnsafeRows(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	expired := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	open := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

	selected, _, err := selectIDsWithinVisitShotCapForSession([]domain.UnbatchedDue{
		{ObligationID: "expired", TargetID: "goat-1", DueAt: planned, WindowEnd: &expired},
		{ObligationID: "open", TargetID: "goat-2", DueAt: planned, WindowEnd: &open},
	}, &planned, 2, "FMD", 5, NewSweepSession())
	if err != nil {
		t.Fatalf("selectIDsWithinVisitShotCapForSession: %v", err)
	}
	if !reflect.DeepEqual(selected, []string{"open"}) {
		t.Fatalf("selected = %#v, want only row still safe on planned date", selected)
	}
}

func TestSelectParkIDsWithinVisitShotCapForSessionFiltersUnsafeRows(t *testing.T) {
	planned := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	expired := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	open := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

	selected, _, err := selectParkIDsWithinVisitShotCapForSession([]domain.ParkConsolidationCandidate{
		{ObligationID: "expired", RuleID: "rule-fmd", TargetID: "goat-1", DueAt: planned, WindowEnd: &expired},
		{ObligationID: "open", RuleID: "rule-fmd", TargetID: "goat-2", DueAt: planned, WindowEnd: &open},
	}, []string{"expired", "open"}, &planned, 2, func(string) RuleVaccineIdentity {
		return RuleVaccineIdentity{VaccineCode: "FMD", VaccinePriority: 5}
	}, NewSweepSession())
	if err != nil {
		t.Fatalf("selectParkIDsWithinVisitShotCapForSession: %v", err)
	}
	if !reflect.DeepEqual(selected, []string{"open"}) {
		t.Fatalf("selected = %#v, want only park row still safe on planned date", selected)
	}
}
