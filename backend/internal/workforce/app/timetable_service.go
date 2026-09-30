package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// TimetableService serves People / HRMS > Timetable (maintainer request 2026-09-30): which shift
// each person works and the working hours of each shift at each park. The route table decides
// who may call it (workforce.timetable.read / .write -- HR and the CEO/CXO); the service
// validates the values and composes every sentence the page shows.
type TimetableService struct {
	repo ports.TimetableRepository
}

func NewTimetableService(repo ports.TimetableRepository) *TimetableService {
	return &TimetableService{repo: repo}
}

// ShiftFilterUnassigned is the filter value for "people on no shift yet".
const ShiftFilterUnassigned = "unassigned"

const (
	timetableDefaultLimit = 50
	timetableMaxLimit     = 100
	// minutesPerDay: a shift may END at 1440 (midnight -- the Second shift runs 15:00-24:00).
	minutesPerDay = 1440
)

// Copy the timetable composes into row fields. The page's own labels live in the page contract.
var timetableCopy = map[string]string{
	"timing.not_set":      "Not set",
	"timing.end_not_set":  "%s – end not set",
	"timing.range":        "%s – %s",
	"shift.none":          "Not assigned",
	"error.park":          "Choose a park.",
	"error.unknown_park":  "That park is not one of the farm's active parks.",
	"error.unknown_shift": "That shift is not on the farm's shift list.",
	"error.start":         "Choose when the shift starts.",
	"error.end":           "The end time must be between 12:01 am and midnight.",
	"error.start_range":   "The start time must be between 12:00 am and 11:59 pm.",
	"error.same":          "A shift cannot end at the same time it starts.",
	"error.end_no_start":  "Set when the shift starts before setting when it ends.",
	"error.person":        "That person was not found, or is no longer active.",
	"error.conflict":      "Someone else changed this just now. Reload and try again.",
	"error.filter":        "That filter is not valid.",
	"error.version":       "The version must not be negative.",
}

// Timetable reads one park's page. An empty parkID opens the first park (park-code order).
func (s *TimetableService) Timetable(ctx context.Context, tenantID, parkID, shift, cursor string, limit int, traceID string) (*domain.WorkforceTimetable, error) {
	parkID = strings.TrimSpace(parkID)
	shift = strings.TrimSpace(shift)
	parks, err := s.repo.TimetableParks(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := &domain.WorkforceTimetable{
		Parks:       make([]domain.TimetablePark, 0, len(parks)),
		Shifts:      []domain.TimetableShift{},
		People:      []domain.TimetablePerson{},
		ShiftFilter: shift,
		TraceID:     traceID,
	}
	for _, p := range parks {
		out.Parks = append(out.Parks, domain.TimetablePark{ParkID: p.ParkID, Label: p.Name})
		if p.ParkID == parkID {
			out.ParkID, out.ParkLabel = p.ParkID, p.Name
		}
	}
	if len(parks) == 0 {
		// No parks yet: the page says so and offers nothing to edit.
		return out, nil
	}
	if parkID == "" {
		out.ParkID, out.ParkLabel = parks[0].ParkID, parks[0].Name
	} else if out.ParkID == "" {
		return nil, &Error{Code: "unknown_park", Message: timetableCopy["error.unknown_park"], HTTPStatus: 422}
	}

	timings, err := s.repo.ParkShiftTimings(ctx, tenantID, out.ParkID)
	if err != nil {
		return nil, err
	}
	if shift != "" && shift != ShiftFilterUnassigned && !hasShift(timings, shift) {
		return nil, &Error{Code: "invalid_filter", Message: timetableCopy["error.filter"], HTTPStatus: 400}
	}
	counts, err := s.repo.ParkShiftHeadcount(ctx, tenantID, out.ParkID)
	if err != nil {
		return nil, err
	}
	byCode := make(map[string]ports.ShiftTimingRow, len(timings))
	for _, t := range timings {
		byCode[t.ShiftCode] = t
		composed := composeShift(t)
		composed.PeopleCount = counts[t.ShiftCode]
		out.Shifts = append(out.Shifts, composed)
	}
	for code, n := range counts {
		out.TotalPeople += n
		if code == "" {
			out.UnassignedCount += n
		}
	}

	if limit <= 0 {
		limit = timetableDefaultLimit
	}
	if limit > timetableMaxLimit {
		limit = timetableMaxLimit
	}
	rows, next, err := s.repo.ListTimetablePeople(ctx, ports.ListTimetablePeopleParams{
		TenantID: tenantID, ParkID: out.ParkID, Shift: shift, Cursor: cursor, Limit: limit,
	})
	if errors.Is(err, ports.ErrInvalidFilter) {
		return nil, &Error{Code: "invalid_cursor", Message: timetableCopy["error.filter"], HTTPStatus: 400}
	}
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out.People = append(out.People, composeTimetablePerson(row, byCode))
	}
	out.NextCursor = next
	out.Weekdays = weekdays()
	holidays, err := s.holidaysFor(ctx, tenantID, out.ParkID, time.Now())
	if err != nil {
		return nil, err
	}
	out.Holidays = holidays
	return out, nil
}

// SetShiftTiming stores one shift's hours at one park.
func (s *TimetableService) SetShiftTiming(ctx context.Context, tenantID, actorID, parkID, shiftCode string, update domain.ShiftTimingUpdate, traceID string) (*domain.ShiftTimingResponse, error) {
	parkID, shiftCode = strings.TrimSpace(parkID), strings.TrimSpace(shiftCode)
	if parkID == "" {
		return nil, &Error{Code: "park_required", Message: timetableCopy["error.park"], HTTPStatus: 422}
	}
	if err := ValidateShiftTiming(update.StartMinute, update.EndMinute); err != nil {
		return nil, err
	}
	if update.RowVersion < 0 {
		return nil, &Error{Code: "invalid_row_version", Message: timetableCopy["error.version"], HTTPStatus: 422}
	}
	row, err := s.repo.SetParkShiftTiming(ctx, tenantID, actorID, parkID, shiftCode, update.StartMinute, update.EndMinute, update.RowVersion)
	if err != nil {
		return nil, mapTimetableError(err)
	}
	return &domain.ShiftTimingResponse{ParkID: parkID, Shift: composeShift(row), TraceID: traceID}, nil
}

// SetMemberShift puts one person on a shift, or ("") takes them off every shift.
func (s *TimetableService) SetMemberShift(ctx context.Context, tenantID, actorID, personID string, update domain.MemberShiftUpdate, traceID string) (*domain.MemberShiftResponse, error) {
	personID = strings.TrimSpace(personID)
	if update.RowVersion < 0 {
		return nil, &Error{Code: "invalid_row_version", Message: timetableCopy["error.version"], HTTPStatus: 422}
	}
	row, err := s.repo.SetMemberShift(ctx, tenantID, actorID, personID, strings.TrimSpace(update.ShiftCode), update.RowVersion)
	if err != nil {
		return nil, mapTimetableError(err)
	}
	// The person's hours are their shift's hours at their own park.
	byCode := map[string]ports.ShiftTimingRow{}
	if row.ShiftCode != "" && row.ParkID != "" {
		timings, err := s.repo.ParkShiftTimings(ctx, tenantID, row.ParkID)
		if err != nil {
			return nil, err
		}
		for _, t := range timings {
			byCode[t.ShiftCode] = t
		}
	}
	return &domain.MemberShiftResponse{Person: composeTimetablePerson(row, byCode), TraceID: traceID}, nil
}

// ValidateShiftTiming is the one rule for a shift's hours, mirrored by the table's CHECKs: a
// start in 0..1439, an end in 1..1440 that differs from the start (it may be earlier: a shift
// crossing midnight), and never an end without a start. Both nil clears the timing.
func ValidateShiftTiming(start, end *int) error {
	if start == nil {
		if end != nil {
			return &Error{Code: "shift_end_without_start", Message: timetableCopy["error.end_no_start"], HTTPStatus: 422}
		}
		return nil
	}
	if *start < 0 || *start >= minutesPerDay {
		return &Error{Code: "shift_start_out_of_range", Message: timetableCopy["error.start_range"], HTTPStatus: 422}
	}
	if end == nil {
		return nil
	}
	if *end < 1 || *end > minutesPerDay {
		return &Error{Code: "shift_end_out_of_range", Message: timetableCopy["error.end"], HTTPStatus: 422}
	}
	if *end%minutesPerDay == *start {
		return &Error{Code: "shift_empty", Message: timetableCopy["error.same"], HTTPStatus: 422}
	}
	return nil
}

// ShiftTimingLabel is the sentence for a shift's hours: "8:30 am – 6:00 pm", "6:00 am – end not
// set", or "Not set".
func ShiftTimingLabel(start, end *int) string {
	if start == nil {
		return timetableCopy["timing.not_set"]
	}
	if end == nil {
		return fmt.Sprintf(timetableCopy["timing.end_not_set"], ClockTimeLabel(*start))
	}
	return fmt.Sprintf(timetableCopy["timing.range"], ClockTimeLabel(*start), ClockTimeLabel(*end))
}

// ClockTimeLabel renders minutes after midnight the way the farm says a time: "6:00 am",
// "12:00 pm" (noon), "12:00 am" (midnight, for both 0 and 1440).
func ClockTimeLabel(minute int) string {
	minute = ((minute % minutesPerDay) + minutesPerDay) % minutesPerDay
	h, m := minute/60, minute%60
	suffix := "am"
	if h >= 12 {
		suffix = "pm"
	}
	h12 := h % 12
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%d:%02d %s", h12, m, suffix)
}

func composeShift(row ports.ShiftTimingRow) domain.TimetableShift {
	return domain.TimetableShift{
		ShiftCode:   row.ShiftCode,
		Label:       row.Label,
		StartMinute: row.StartMinute,
		EndMinute:   row.EndMinute,
		TimingLabel: ShiftTimingLabel(row.StartMinute, row.EndMinute),
		IsSet:       row.StartMinute != nil,
		RowVersion:  row.RowVersion,
	}
}

func composeTimetablePerson(row ports.TimetablePersonRow, shifts map[string]ports.ShiftTimingRow) domain.TimetablePerson {
	out := domain.TimetablePerson{
		PersonID:    row.PersonID,
		DisplayName: row.DisplayName,
		Designation: designationLabel(row.DesignationLabel, row.RoleHint, row.DesignationGrade, clockCopyFor("en")),
		Department:  row.Department,
		ShiftCode:   row.ShiftCode,
		ShiftLabel:  timetableCopy["shift.none"],
		WeekOffs:    append([]int{}, row.WeekOffs...),
		RowVersion:  row.RowVersion,
	}
	out.WeekOffLabel = weekOffLabel(row.WeekOffs)
	if row.ShiftCode == "" {
		return out
	}
	if shift, ok := shifts[row.ShiftCode]; ok {
		out.ShiftLabel = shift.Label
		out.TimingLabel = ShiftTimingLabel(shift.StartMinute, shift.EndMinute)
	} else {
		// A retired shift still names itself by nothing better than "Not set" hours; the code
		// never reaches the screen.
		out.ShiftLabel = timetableCopy["shift.none"]
	}
	return out
}

func hasShift(rows []ports.ShiftTimingRow, code string) bool {
	for _, r := range rows {
		if r.ShiftCode == code {
			return true
		}
	}
	return false
}

func mapTimetableError(err error) error {
	switch {
	case errors.Is(err, ports.ErrTimetableVersionConflict):
		return Conflict("timetable_version_conflict", timetableCopy["error.conflict"])
	case errors.Is(err, ports.ErrUnknownPark):
		return &Error{Code: "unknown_park", Message: timetableCopy["error.unknown_park"], HTTPStatus: 422}
	case errors.Is(err, ports.ErrUnknownShift):
		return &Error{Code: "unknown_shift", Message: timetableCopy["error.unknown_shift"], HTTPStatus: 422}
	case errors.Is(err, ports.ErrPersonNotFound), errors.Is(err, ports.ErrNotFound):
		return &Error{Code: "person_not_found", Message: timetableCopy["error.person"], HTTPStatus: 404}
	}
	return err
}
