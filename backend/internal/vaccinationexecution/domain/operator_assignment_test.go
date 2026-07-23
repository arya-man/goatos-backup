package domain

import (
	"testing"
	"time"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, kolkata)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

func cptShifts() []OperatorShift {
	return []OperatorShift{
		{OperatorID: "amit", ParkID: "cpt", ShiftLabel: "am", ShiftStartMinute: 7 * 60, ShiftEndMinute: 15 * 60, WeekOffWeekday: "friday"},
		{OperatorID: "sagar", ParkID: "cpt", ShiftLabel: "pm", ShiftStartMinute: 15 * 60, ShiftEndMinute: 24 * 60, WeekOffWeekday: "saturday"},
		{OperatorID: "darshan", ParkID: "cpt", ShiftLabel: "rover", ShiftStartMinute: 8*60 + 30, ShiftEndMinute: 18 * 60, WeekOffWeekday: "sunday"},
	}
}

func TestResolveOperatorsForDriveDay(t *testing.T) {
	cfg := OperatorAssignmentConfig{ParkID: "cpt", ActiveOperatorsPerDay: 1, DefaultOperatorID: "darshan", RowVersion: 1}

	tests := []struct {
		name         string
		date         string
		defaultID    string
		leaves       []OperatorLeaveWindow
		wantOperator string
		wantReason   ResolutionReason
	}{
		{
			name:         "Thursday: default (Darshan) is available",
			date:         "2026-07-23", // Thursday
			defaultID:    "darshan",
			wantOperator: "darshan",
			wantReason:   ReasonDefaultAvailable,
		},
		{
			name:         "Sunday: default (Darshan) week-off -> PM operator (Sagar) covers",
			date:         "2026-07-26", // Sunday
			defaultID:    "darshan",
			wantOperator: "sagar",
			wantReason:   ReasonDefaultWeekOff,
		},
		{
			name:      "Thursday: default (Darshan) on approved leave -> PM operator (Sagar) covers",
			date:      "2026-07-23",
			defaultID: "darshan",
			leaves: []OperatorLeaveWindow{
				{OperatorID: "darshan", Start: mustDate(t, "2026-07-23"), End: mustDate(t, "2026-07-23")},
			},
			wantOperator: "sagar",
			wantReason:   ReasonDefaultOnLeave,
		},
		{
			name:         "Next day after a covered leave: default reverts automatically (no persisted swap state)",
			date:         "2026-07-24", // Friday, Darshan not on leave this day
			defaultID:    "darshan",
			wantOperator: "darshan",
			wantReason:   ReasonDefaultAvailable,
		},
		{
			name:         "CEO changes default to Amit -> new default continues (Thursday, Amit available)",
			date:         "2026-07-23",
			defaultID:    "amit",
			wantOperator: "amit",
			wantReason:   ReasonDefaultAvailable,
		},
		{
			name:         "New default (Amit) week-off Friday -> PM operator (Sagar) covers",
			date:         "2026-07-24",
			defaultID:    "amit",
			wantOperator: "sagar",
			wantReason:   ReasonDefaultWeekOff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cfg
			c.DefaultOperatorID = tt.defaultID
			got, err := ResolveOperatorsForDriveDay(mustDate(t, tt.date), c, cptShifts(), tt.leaves)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.AvailableOperators) == 0 || got.AvailableOperators[0] != tt.wantOperator {
				t.Fatalf("got operators %v, want first=%s", got.AvailableOperators, tt.wantOperator)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("got reason %s, want %s", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestResolveOperatorsForDriveDay_PMAlsoUnavailable_FallsBackToShiftOrder(t *testing.T) {
	cfg := OperatorAssignmentConfig{ParkID: "cpt", ActiveOperatorsPerDay: 1, DefaultOperatorID: "darshan", RowVersion: 1}
	// Sunday: Darshan (default, rover) is week-off; Sagar (pm) is also on leave that day -> falls back to
	// remaining shift order (am = Amit).
	leaves := []OperatorLeaveWindow{
		{OperatorID: "sagar", Start: mustDate(t, "2026-07-26"), End: mustDate(t, "2026-07-26")},
	}
	got, err := ResolveOperatorsForDriveDay(mustDate(t, "2026-07-26"), cfg, cptShifts(), leaves)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AvailableOperators[0] != "amit" {
		t.Fatalf("got %v, want amit", got.AvailableOperators)
	}
	if got.Reason != ReasonFallbackShiftOrder {
		t.Fatalf("got reason %s, want %s", got.Reason, ReasonFallbackShiftOrder)
	}
}

func TestResolveOperatorsForDriveDay_NTwoUsesDefaultThenShiftFill(t *testing.T) {
	cfg := OperatorAssignmentConfig{ParkID: "cpt", ActiveOperatorsPerDay: 2, DefaultOperatorID: "darshan", RowVersion: 1}
	got, err := ResolveOperatorsForDriveDay(mustDate(t, "2026-07-24"), cfg, cptShifts(), nil) // Friday: Amit off, Darshan + Sagar available
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"darshan", "sagar"}
	if len(got.AvailableOperators) != len(want) {
		t.Fatalf("got operators %v, want %v", got.AvailableOperators, want)
	}
	for i := range want {
		if got.AvailableOperators[i] != want[i] {
			t.Fatalf("got operators %v, want %v", got.AvailableOperators, want)
		}
	}
}

func TestResolveOperatorsForDriveDay_NoOperatorAvailable_Errors(t *testing.T) {
	cfg := OperatorAssignmentConfig{ParkID: "cpt", ActiveOperatorsPerDay: 1, DefaultOperatorID: "darshan", RowVersion: 1}
	leaves := []OperatorLeaveWindow{
		{OperatorID: "amit", Start: mustDate(t, "2026-07-26"), End: mustDate(t, "2026-07-26")},
		{OperatorID: "sagar", Start: mustDate(t, "2026-07-26"), End: mustDate(t, "2026-07-26")},
	}
	// Sunday: Darshan week-off, Amit + Sagar on leave -> nobody available.
	_, err := ResolveOperatorsForDriveDay(mustDate(t, "2026-07-26"), cfg, cptShifts(), leaves)
	if err == nil {
		t.Fatal("expected error when no operator is available")
	}
}

func TestOperatorAssignmentConfig_Validate(t *testing.T) {
	tests := []struct {
		name     string
		cfg      OperatorAssignmentConfig
		known    bool
		wantOK   bool
		wantCode string
	}{
		{"valid N=1 with known default", OperatorAssignmentConfig{ActiveOperatorsPerDay: 1, DefaultOperatorID: "darshan"}, true, true, ""},
		{"N=0 rejected", OperatorAssignmentConfig{ActiveOperatorsPerDay: 0, DefaultOperatorID: "darshan"}, true, false, "invalid_active_operators_per_day"},
		{"N=4 rejected", OperatorAssignmentConfig{ActiveOperatorsPerDay: 4, DefaultOperatorID: "darshan"}, true, false, "invalid_active_operators_per_day"},
		{"N=1 with empty default rejected (never silent default)", OperatorAssignmentConfig{ActiveOperatorsPerDay: 1, DefaultOperatorID: ""}, true, false, "missing_default_operator"},
		{"default not a known shift operator rejected", OperatorAssignmentConfig{ActiveOperatorsPerDay: 1, DefaultOperatorID: "ghost"}, false, false, "unknown_default_operator"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, ok := tt.cfg.Validate(tt.known)
			if ok != tt.wantOK {
				t.Fatalf("got ok=%v, want %v", ok, tt.wantOK)
			}
			if !ok && code != tt.wantCode {
				t.Fatalf("got code=%s, want %s", code, tt.wantCode)
			}
		})
	}
}
