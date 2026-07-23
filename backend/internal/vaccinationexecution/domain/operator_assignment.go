package domain

import (
	"fmt"
	"sort"
	"time"
)

// ---- Phase 1 CONFIG-ONLY: vaccination operator shift + N-active-operators-per-day default assignment ----
//
// Business rule (CPT/Channapatna single-operator drive, maintainer-authoritative 2026-07-23):
//   - Operators carry a SHIFT (am/pm/rover) + a week-off weekday.
//   - CEO sets a default operator for the drive; when N=1 (the CPT case) the default operator is
//     scheduled every business day the default is available.
//   - If the default is off (week-off) OR on approved leave that day, the PM/afternoon-shift operator
//     covers; the default resumes automatically the next business day (no persisted "swap" state).
//   - If the CEO changes the default operator, the NEW default continues for the rest of the drive
//     (this is a config write, not a one-day override).
//
// This file is domain-only and pure (no I/O, no clock reads beyond the passed-in businessDate). It is
// consumed by the app-layer config service for the weekly-preview read path. It is NOT yet consumed by
// the drive/obligation scheduler -- see the Phase 5 TODOs in
// backend/internal/obligation/adapters/postgres/sweeper.go and
// backend/internal/vaccinationexecution/app/operator_drive_planner.go.

// OperatorAssignmentConfig is the backend-owned N-active-operators-per-day + default-operator config
// (vaccination_operator_assignment_config). RowVersion is the optimistic-concurrency token for admin
// edits; a stale RowVersion on update is rejected as a conflict.
type OperatorAssignmentConfig struct {
	ParkID                string `json:"parkId"`
	ActiveOperatorsPerDay int    `json:"activeOperatorsPerDay"`
	DefaultOperatorID     string `json:"defaultOperatorId"`
	RowVersion            int64  `json:"rowVersion"`
}

var (
	// MinActiveOperatorsPerDay / MaxActiveOperatorsPerDay bound N (mirrors the DB CHECK constraint in
	// migration 000035). CPT today runs N=1.
	MinActiveOperatorsPerDay = 1
	MaxActiveOperatorsPerDay = 3
)

// Validate checks an admin-submitted assignment config against the same rules the DB enforces, so a bad
// value returns a clean 400 instead of a raw constraint-violation 500. defaultOperatorIsKnownShiftOperator
// lets the caller confirm the default operator id resolves to a shift-config row for this park (validate
// FK existence at the app layer with a friendly error, ahead of the DB FK's raw error).
func (c OperatorAssignmentConfig) Validate(defaultOperatorIsKnownShiftOperator bool) (code, message string, ok bool) {
	if c.ActiveOperatorsPerDay < MinActiveOperatorsPerDay || c.ActiveOperatorsPerDay > MaxActiveOperatorsPerDay {
		return "invalid_active_operators_per_day", fmt.Sprintf("active operators per day must be between %d and %d", MinActiveOperatorsPerDay, MaxActiveOperatorsPerDay), false
	}
	if c.DefaultOperatorID == "" {
		return "missing_default_operator", "default operator is required (config validate-or-reject: never silently defaulted)", false
	}
	if !defaultOperatorIsKnownShiftOperator {
		return "unknown_default_operator", "default operator must have a shift config row for this park", false
	}
	return "", "", true
}

// OperatorShift is one operator's authored shift window + week-off for a park
// (vaccination_operator_shift_config). Start/End are minutes-of-day (0-1439) so a non-hour-aligned start
// like 08:30 is representable exactly.
type OperatorShift struct {
	OperatorID       string `json:"operatorId"`
	DisplayName      string `json:"displayName"`
	ParkID           string `json:"parkId"`
	ShiftLabel       string `json:"shiftLabel"` // am | pm | rover
	ShiftStartMinute int    `json:"shiftStartMinute"`
	ShiftEndMinute   int    `json:"shiftEndMinute"`
	WeekOffWeekday   string `json:"weekOffWeekday,omitempty"` // monday..sunday, empty = no week-off
}

var ShiftLabels = []string{"am", "pm", "rover"}

var weekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

// Validate checks an authored shift row against the same rules the DB enforces.
func (s OperatorShift) Validate() (code, message string, ok bool) {
	if s.OperatorID == "" {
		return "missing_operator_id", "operator id is required", false
	}
	if s.ShiftStartMinute < 0 || s.ShiftStartMinute > 1439 {
		return "invalid_shift_start_minute", "shift start minute must be between 0 and 1439", false
	}
	if s.ShiftEndMinute < 0 || s.ShiftEndMinute > 1439 {
		return "invalid_shift_end_minute", "shift end minute must be between 0 and 1439", false
	}
	if !contains(ShiftLabels, s.ShiftLabel) {
		return "invalid_shift_label", "shift label must be one of: am, pm, rover", false
	}
	if s.WeekOffWeekday != "" && !contains(weekdays, s.WeekOffWeekday) {
		return "invalid_week_off_weekday", "week off weekday must be a lowercase weekday name", false
	}
	return "", "", true
}

// OperatorLeaveWindow is the minimal shape ResolveOperatorsForDriveDay needs from
// workforce_absences: an operator is on leave for businessDate when
// start <= businessDate <= end (both inclusive, Asia/Kolkata business dates).
type OperatorLeaveWindow struct {
	OperatorID string
	Start      time.Time
	End        time.Time
}

// ResolutionReason is the machine reason code for why an operator ordering was produced, surfaced to the
// admin weekly-preview UI so a CEO can see WHY Sagar (not Darshan) is covering a given day.
type ResolutionReason string

const (
	ReasonDefaultAvailable   ResolutionReason = "default_available"
	ReasonDefaultWeekOff     ResolutionReason = "default_week_off_pm_covers"
	ReasonDefaultOnLeave     ResolutionReason = "default_on_leave_pm_covers"
	ReasonFallbackShiftOrder ResolutionReason = "fallback_shift_order"
	ReasonNoOperatorsAvail   ResolutionReason = "no_operators_available"
)

// DayResolution is one business date's resolved operator ordering for the weekly preview.
type DayResolution struct {
	BusinessDate       string           `json:"businessDate"`
	Weekday            string           `json:"weekday"`
	AvailableOperators []string         `json:"availableOperators"` // ordered operator ids, first = scheduled
	Reason             ResolutionReason `json:"reason"`
}

var kolkata = mustLoadLocation("Asia/Kolkata")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("Asia/Kolkata", 5*3600+30*60)
	}
	return loc
}

// ResolveOperatorsForDriveDay is the PURE resolution function for one business date (Asia/Kolkata):
//   - default operator first if available (not week-off, not on leave)
//   - else the PM/afternoon-shift operator, if available
//   - else remaining shift-config operators (rover, then any other), by shift order, if available
//   - error if no operator is available at all
//
// It performs no I/O and reads no wall clock; businessDate must already be an Asia/Kolkata calendar date.
func ResolveOperatorsForDriveDay(businessDate time.Time, cfg OperatorAssignmentConfig, shifts []OperatorShift, todaysLeaves []OperatorLeaveWindow) (DayResolution, error) {
	businessDate = time.Date(businessDate.Year(), businessDate.Month(), businessDate.Day(), 0, 0, 0, 0, kolkata)
	weekday := weekdays[int(businessDate.Weekday()+6)%7] // time.Sunday==0 -> weekdays index 6

	shiftByID := make(map[string]OperatorShift, len(shifts))
	for _, s := range shifts {
		shiftByID[s.OperatorID] = s
	}
	onLeave := make(map[string]bool, len(todaysLeaves))
	for _, lv := range todaysLeaves {
		start := time.Date(lv.Start.Year(), lv.Start.Month(), lv.Start.Day(), 0, 0, 0, 0, kolkata)
		end := time.Date(lv.End.Year(), lv.End.Month(), lv.End.Day(), 0, 0, 0, 0, kolkata)
		if !businessDate.Before(start) && !businessDate.After(end) {
			onLeave[lv.OperatorID] = true
		}
	}

	available := func(operatorID string) bool {
		s, ok := shiftByID[operatorID]
		if !ok {
			return false
		}
		if s.WeekOffWeekday != "" && s.WeekOffWeekday == weekday {
			return false
		}
		if onLeave[operatorID] {
			return false
		}
		return true
	}

	def := cfg.DefaultOperatorID
	if def != "" && available(def) {
		return DayResolution{
			BusinessDate:       businessDate.Format("2006-01-02"),
			Weekday:            weekday,
			AvailableOperators: []string{def},
			Reason:             ReasonDefaultAvailable,
		}, nil
	}

	// Default unavailable: PM-shift operator covers, if available.
	var pmCoverReason ResolutionReason
	if def != "" {
		if s, ok := shiftByID[def]; ok && s.WeekOffWeekday == weekday {
			pmCoverReason = ReasonDefaultWeekOff
		} else {
			pmCoverReason = ReasonDefaultOnLeave
		}
	} else {
		pmCoverReason = ReasonFallbackShiftOrder
	}
	for _, s := range shifts {
		if s.ShiftLabel == "pm" && available(s.OperatorID) {
			return DayResolution{
				BusinessDate:       businessDate.Format("2006-01-02"),
				Weekday:            weekday,
				AvailableOperators: []string{s.OperatorID},
				Reason:             pmCoverReason,
			}, nil
		}
	}

	// Fall back to remaining operators in deterministic shift order: am, rover, pm-already-tried, then
	// any other label; first available wins.
	order := map[string]int{"am": 0, "rover": 1, "pm": 2}
	ordered := make([]OperatorShift, len(shifts))
	copy(ordered, shifts)
	sort.SliceStable(ordered, func(i, j int) bool {
		oi, oj := order[ordered[i].ShiftLabel], order[ordered[j].ShiftLabel]
		if oi != oj {
			return oi < oj
		}
		return ordered[i].OperatorID < ordered[j].OperatorID
	})
	for _, s := range ordered {
		if available(s.OperatorID) {
			return DayResolution{
				BusinessDate:       businessDate.Format("2006-01-02"),
				Weekday:            weekday,
				AvailableOperators: []string{s.OperatorID},
				Reason:             ReasonFallbackShiftOrder,
			}, nil
		}
	}

	return DayResolution{}, fmt.Errorf("no vaccination operator available for %s (%s): all operators week-off or on leave", businessDate.Format("2006-01-02"), weekday)
}
