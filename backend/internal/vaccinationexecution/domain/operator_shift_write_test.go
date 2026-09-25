package domain

import "testing"

func strPtr(s string) *string { return &s }

func TestValidateOperatorShiftInputAcceptsAFullDayShift(t *testing.T) {
	shift, ferr := ValidateOperatorShiftInput(OperatorShiftInput{
		ParkID: "p", OperatorID: "o", ShiftLabel: "am", ShiftStart: "08:30", ShiftEnd: "17:00", WeekOffWeekday: strPtr("sunday"),
	})
	if ferr != nil {
		t.Fatalf("valid shift refused: %+v", ferr)
	}
	if shift.ShiftStartMinute != 510 || shift.ShiftEndMinute != 1020 || shift.WeekOffWeekday != "sunday" || shift.ShiftLabel != "am" {
		t.Fatalf("unexpected stored shift: %+v", shift)
	}
}

func TestValidateOperatorShiftInputNoWeekOffIsEmptyNotDefaulted(t *testing.T) {
	for _, weekOff := range []*string{nil, strPtr(""), strPtr("  ")} {
		shift, ferr := ValidateOperatorShiftInput(OperatorShiftInput{OperatorID: "o", ShiftLabel: "pm", ShiftStart: "13:00", ShiftEnd: "21:00", WeekOffWeekday: weekOff})
		if ferr != nil {
			t.Fatalf("refused: %+v", ferr)
		}
		if shift.WeekOffWeekday != "" {
			t.Fatalf("week off must stay empty, got %q", shift.WeekOffWeekday)
		}
	}
}

func TestValidateOperatorShiftInputRefusesEachBadFieldByName(t *testing.T) {
	cases := []struct {
		name  string
		in    OperatorShiftInput
		code  string
		field string
	}{
		{"blank label", OperatorShiftInput{ShiftStart: "08:00", ShiftEnd: "17:00"}, "missing_shift_label", "shift_label"},
		{"unknown label", OperatorShiftInput{ShiftLabel: "night", ShiftStart: "08:00", ShiftEnd: "17:00"}, "invalid_shift_label", "shift_label"},
		{"uppercase label", OperatorShiftInput{ShiftLabel: "AM", ShiftStart: "08:00", ShiftEnd: "17:00"}, "invalid_shift_label", "shift_label"},
		{"blank start", OperatorShiftInput{ShiftLabel: "am", ShiftEnd: "17:00"}, "missing_shift_start", "shift_start"},
		{"bad start", OperatorShiftInput{ShiftLabel: "am", ShiftStart: "8am", ShiftEnd: "17:00"}, "invalid_shift_start", "shift_start"},
		{"hour 24", OperatorShiftInput{ShiftLabel: "am", ShiftStart: "24:00", ShiftEnd: "17:00"}, "invalid_shift_start", "shift_start"},
		{"minute 60", OperatorShiftInput{ShiftLabel: "am", ShiftStart: "08:60", ShiftEnd: "17:00"}, "invalid_shift_start", "shift_start"},
		{"blank end", OperatorShiftInput{ShiftLabel: "am", ShiftStart: "08:00"}, "missing_shift_end", "shift_end"},
		{"bad end", OperatorShiftInput{ShiftLabel: "am", ShiftStart: "08:00", ShiftEnd: "5"}, "invalid_shift_end", "shift_end"},
		{"overnight", OperatorShiftInput{ShiftLabel: "pm", ShiftStart: "22:00", ShiftEnd: "06:00"}, "shift_ends_before_start", "shift_end"},
		{"zero length", OperatorShiftInput{ShiftLabel: "pm", ShiftStart: "09:00", ShiftEnd: "09:00"}, "shift_ends_before_start", "shift_end"},
		{"bad weekday", OperatorShiftInput{ShiftLabel: "am", ShiftStart: "08:00", ShiftEnd: "17:00", WeekOffWeekday: strPtr("Sunday")}, "invalid_week_off_weekday", "week_off_weekday"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ferr := ValidateOperatorShiftInput(tc.in)
			if ferr == nil {
				t.Fatalf("accepted a bad shift")
			}
			if ferr.Code != tc.code || ferr.Field != tc.field || ferr.Message == "" {
				t.Fatalf("got %+v, want code=%s field=%s", ferr, tc.code, tc.field)
			}
		})
	}
}

func TestShiftClockRoundTrip(t *testing.T) {
	for _, raw := range []string{"00:00", "08:05", "23:59"} {
		m, ok := ParseShiftClock(raw)
		if !ok || FormatShiftClock(m) != raw {
			t.Fatalf("%s -> %d,%v -> %s", raw, m, ok, FormatShiftClock(m))
		}
	}
}
