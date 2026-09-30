package ports

import (
	"context"
	"errors"
)

// ErrTimetableVersionConflict: the shift or the person's shift was changed by someone else after
// the screen loaded it.
var ErrTimetableVersionConflict = errors.New("the timetable was changed by someone else")

// ErrUnknownShift: the shift code is not in the farm's shift list.
var ErrUnknownShift = errors.New("unknown shift")

// ShiftTimingRow is one catalog shift joined to one park's stored timing (absent timing =
// nil minutes, RowVersion 0).
type ShiftTimingRow struct {
	ShiftCode   string
	Label       string
	StartMinute *int
	EndMinute   *int
	RowVersion  int
}

// TimetablePersonRow is one person at a park with their raw designation inputs and shift.
type TimetablePersonRow struct {
	PersonID         string
	DisplayName      string
	DesignationLabel string
	RoleHint         string
	DesignationGrade string
	Department       string
	ParkID           string
	ShiftCode        string
	// WeekOffs are ISO weekdays (Monday = 1) the person is off every week.
	WeekOffs   []int
	RowVersion int
}

// HolidayRow is one active holiday HR entered. ParkID "" = every park.
type HolidayRow struct {
	HolidayID  string
	HolidayOn  string // YYYY-MM-DD
	ParkID     string
	ParkLabel  string
	Label      string
	RowVersion int
}

// ErrNoShift: weekly offs belong to a person's shift, and they have none yet.
var ErrNoShift = errors.New("the person has no shift")

// ListTimetablePeopleParams pages the people of ONE park on a keyset (lower(name), id).
type ListTimetablePeopleParams struct {
	TenantID string
	ParkID   string
	// Shift is "" (everyone), a shift code, or "unassigned".
	Shift  string
	Cursor string
	Limit  int
}

// TimetableRepository is the HRMS timetable port, implemented by the workforce postgres
// Repository.
type TimetableRepository interface {
	// TimetableParks lists the tenant's active parks in park-code order (CBE before CPT).
	TimetableParks(ctx context.Context, tenantID string) ([]TimetableParkRow, error)
	// ParkShiftTimings returns every active catalog shift with this park's timing, in catalog
	// order.
	ParkShiftTimings(ctx context.Context, tenantID, parkID string) ([]ShiftTimingRow, error)
	// ParkShiftHeadcount counts the park's active people per shift code ("" = unassigned).
	ParkShiftHeadcount(ctx context.Context, tenantID, parkID string) (map[string]int, error)
	ListTimetablePeople(ctx context.Context, params ListTimetablePeopleParams) ([]TimetablePersonRow, string, error)
	// SetParkShiftTiming writes one park's hours for one shift, fenced on rowVersion, with an
	// audit row in the same transaction. A write whose values already match the stored ones is
	// an idempotent success and writes nothing.
	SetParkShiftTiming(ctx context.Context, tenantID, actorUserID, parkID, shiftCode string, start, end *int, rowVersion int) (ShiftTimingRow, error)
	// SetMemberShift puts a person on a shift ("" = off every shift), fenced and audited the
	// same way.
	SetMemberShift(ctx context.Context, tenantID, actorUserID, personID, shiftCode string, rowVersion int) (TimetablePersonRow, error)
	// SetMemberWeekOffs sets the weekdays a person is off every week, fenced on the shift row's
	// version. It does NOT move the shift's set date (a new off day is not a new shift).
	SetMemberWeekOffs(ctx context.Context, tenantID, actorUserID, personID string, weekOffs []int, rowVersion int) (TimetablePersonRow, error)
	// Holidays lists the active holidays from..to (inclusive) that apply to parkID (or every park).
	Holidays(ctx context.Context, tenantID, parkID, from, to string) ([]HolidayRow, error)
	AddHoliday(ctx context.Context, tenantID, actorUserID, holidayOn, parkID, label string) (HolidayRow, error)
	RemoveHoliday(ctx context.Context, tenantID, actorUserID, holidayID string) error
}

// TimetableParkRow is one active park.
type TimetableParkRow struct {
	ParkID string
	Code   string
	Name   string
}
