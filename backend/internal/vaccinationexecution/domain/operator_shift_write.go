package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// ---- Authoring one operator's vaccination shift for a park ----
//
// Until this write path existed, vaccination_operator_shift_config was filled ONLY by the roster
// seed command. UpdateOperatorAssignmentConfig refuses any default/selected operator without a shift
// row for the park, so a park added on Configuration > Items & settings could never get vaccination
// operators at all. These are the rules the admin screen writes under.
//
// Validate-or-reject: every field is checked and a bad value is refused with a field-specific farm
// message. Nothing is defaulted -- a blank shift label or time is an error, never "am 09:00".
//
// OVERNIGHT SHIFTS ARE NOT MODELLED. The table stores minutes-of-day and nothing in the scheduler
// reads a window that wraps midnight; vaccination drives are day work. So the shift must start
// before it ends on the same day, and "22:00 to 06:00" is refused rather than silently stored as a
// window every reader would interpret backwards.

// OperatorShiftInput is the admin's shift form exactly as typed: times are "HH:MM" strings and the
// week-off is optional (nil or blank = no week-off).
type OperatorShiftInput struct {
	ParkID         string
	OperatorID     string
	ShiftLabel     string
	ShiftStart     string
	ShiftEnd       string
	WeekOffWeekday *string
}

// OperatorShiftFieldError names the request field a refusal is about, so the screen can put the
// message next to that field. Code is the stable machine code; Message is farm-worded copy.
type OperatorShiftFieldError struct {
	Code    string
	Field   string
	Message string
}

// ShiftLabelNames is the human name of each shift label, the copy the admin screen renders instead of
// the stored code.
var ShiftLabelNames = map[string]string{
	"am":    "Morning",
	"pm":    "Afternoon",
	"rover": "Rover",
}

// ParseShiftClock parses a 24-hour "HH:MM" wall-clock time into minutes of the day (0-1439). It
// accepts exactly two-digit-or-one-digit hours and two-digit minutes; anything else is refused.
func ParseShiftClock(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	hh, mm, ok := strings.Cut(raw, ":")
	if !ok || len(hh) < 1 || len(hh) > 2 || len(mm) != 2 {
		return 0, false
	}
	h, err := strconv.Atoi(hh)
	if err != nil || h < 0 || h > 23 {
		return 0, false
	}
	m, err := strconv.Atoi(mm)
	if err != nil || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// FormatShiftClock renders minutes of the day as "HH:MM".
func FormatShiftClock(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// ValidateOperatorShiftInput turns the typed form into the stored shift row, or refuses it. The park
// and operator ids are shape-checked by the HTTP adapter; whether the operator really works in that
// park is a database fact checked inside the write transaction.
func ValidateOperatorShiftInput(in OperatorShiftInput) (OperatorShift, *OperatorShiftFieldError) {
	label := strings.TrimSpace(in.ShiftLabel)
	if label == "" {
		return OperatorShift{}, &OperatorShiftFieldError{Code: "missing_shift_label", Field: "shift_label", Message: "Choose the shift: Morning, Afternoon or Rover."}
	}
	if !contains(ShiftLabels, label) {
		return OperatorShift{}, &OperatorShiftFieldError{Code: "invalid_shift_label", Field: "shift_label", Message: "The shift must be Morning, Afternoon or Rover."}
	}
	if strings.TrimSpace(in.ShiftStart) == "" {
		return OperatorShift{}, &OperatorShiftFieldError{Code: "missing_shift_start", Field: "shift_start", Message: "Enter the time the shift starts, like 08:00."}
	}
	start, ok := ParseShiftClock(in.ShiftStart)
	if !ok {
		return OperatorShift{}, &OperatorShiftFieldError{Code: "invalid_shift_start", Field: "shift_start", Message: "The start time must be a 24-hour time like 08:00."}
	}
	if strings.TrimSpace(in.ShiftEnd) == "" {
		return OperatorShift{}, &OperatorShiftFieldError{Code: "missing_shift_end", Field: "shift_end", Message: "Enter the time the shift ends, like 17:00."}
	}
	end, ok := ParseShiftClock(in.ShiftEnd)
	if !ok {
		return OperatorShift{}, &OperatorShiftFieldError{Code: "invalid_shift_end", Field: "shift_end", Message: "The end time must be a 24-hour time like 17:00."}
	}
	if end <= start {
		return OperatorShift{}, &OperatorShiftFieldError{Code: "shift_ends_before_start", Field: "shift_end", Message: "The shift must end later the same day than it starts. Overnight shifts are not used for vaccination."}
	}
	weekOff := ""
	if in.WeekOffWeekday != nil {
		weekOff = strings.TrimSpace(*in.WeekOffWeekday)
	}
	if weekOff != "" && !contains(weekdays, weekOff) {
		return OperatorShift{}, &OperatorShiftFieldError{Code: "invalid_week_off_weekday", Field: "week_off_weekday", Message: "The week off must be a day of the week, like sunday, or left empty for no week off."}
	}
	shift := OperatorShift{
		OperatorID:       strings.TrimSpace(in.OperatorID),
		ParkID:           strings.TrimSpace(in.ParkID),
		ShiftLabel:       label,
		ShiftStartMinute: start,
		ShiftEndMinute:   end,
		WeekOffWeekday:   weekOff,
	}
	// Belt and braces: the stored-row rules (the same the DB CHECKs enforce) must agree.
	if code, message, ok := shift.Validate(); !ok {
		return OperatorShift{}, &OperatorShiftFieldError{Code: code, Field: "", Message: message}
	}
	return shift, nil
}
