package app

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// WEEKLY OFFS AND HOLIDAYS (maintainer decisions 2026-09-30): each person has a fixed weekly off
// (one or more weekdays) set beside their shift; HR enters holidays for every park or one park. The
// clock-in check never checks either. None exist today -- the farm runs every day -- so an empty
// list is the normal state, not a gap.

var weekdayLabels = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

var offsCopy = map[string]string{
	"error.week_off":      "Choose weekdays (at most six: someone must work a day).",
	"error.no_shift":      "Put the person on a shift first; the weekly off sits beside it.",
	"error.holiday_date":  "Choose the holiday's date.",
	"error.holiday_label": "Name the holiday (up to 80 letters).",
	"error.holiday_park":  "That park is not one of the farm's active parks.",
	"error.holiday":       "That holiday was not found.",
	"holiday.every_park":  "Every park",
}

func weekdays() []domain.Weekday {
	out := make([]domain.Weekday, 0, 7)
	for i, l := range weekdayLabels {
		out = append(out, domain.Weekday{Key: i + 1, Label: l})
	}
	return out
}

// weekOffLabel reads ISO weekdays in week order: "Tue, Sun".
func weekOffLabel(days []int) string {
	sorted := append([]int(nil), days...)
	sort.Ints(sorted)
	parts := make([]string, 0, len(sorted))
	for _, d := range sorted {
		if d >= 1 && d <= 7 {
			parts = append(parts, weekdayLabels[d-1])
		}
	}
	return strings.Join(parts, ", ")
}

func composeHoliday(h ports.HolidayRow) domain.Holiday {
	park := h.ParkLabel
	if h.ParkID == "" {
		park = offsCopy["holiday.every_park"]
	}
	return domain.Holiday{HolidayID: h.HolidayID, HolidayOn: h.HolidayOn, DateLabel: biztime.FarmDateFromBusinessDate(h.HolidayOn),
		ParkID: h.ParkID, ParkLabel: park, Label: h.Label, RowVersion: h.RowVersion}
}

// holidaysFor lists the park's (and every park's) holidays from 30 days ago to a year on.
func (s *TimetableService) holidaysFor(ctx context.Context, tenantID, parkID string, now time.Time) ([]domain.Holiday, error) {
	today, err := time.ParseInLocation("2006-01-02", biztime.BusinessDate(now), biztime.DefaultLocation())
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.Holidays(ctx, tenantID, parkID, today.AddDate(0, 0, -30).Format("2006-01-02"), today.AddDate(1, 0, 0).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Holiday, 0, len(rows))
	for _, h := range rows {
		out = append(out, composeHoliday(h))
	}
	return out, nil
}

// SetWeekOffs sets a person's weekly off days.
func (s *TimetableService) SetWeekOffs(ctx context.Context, tenantID, actorID, personID string, update domain.WeekOffsUpdate, traceID string) (*domain.MemberShiftResponse, error) {
	seen := map[int]bool{}
	days := []int{}
	for _, d := range update.WeekOffs {
		if d < 1 || d > 7 {
			return nil, &Error{Code: "invalid_week_off", Message: offsCopy["error.week_off"], HTTPStatus: 422}
		}
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	if len(days) > 6 {
		return nil, &Error{Code: "invalid_week_off", Message: offsCopy["error.week_off"], HTTPStatus: 422}
	}
	sort.Ints(days)
	row, err := s.repo.SetMemberWeekOffs(ctx, tenantID, actorID, strings.TrimSpace(personID), days, update.RowVersion)
	if errors.Is(err, ports.ErrNoShift) {
		return nil, &Error{Code: "no_shift", Message: offsCopy["error.no_shift"], HTTPStatus: 422}
	}
	if err != nil {
		return nil, mapTimetableError(err)
	}
	byCode := map[string]ports.ShiftTimingRow{}
	if row.ParkID != "" {
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

// AddHoliday enters a holiday for every park (ParkID "") or one park.
func (s *TimetableService) AddHoliday(ctx context.Context, tenantID, actorID string, req domain.AddHolidayRequest, traceID string) (*domain.HolidayResponse, error) {
	on := strings.TrimSpace(req.HolidayOn)
	if _, err := time.Parse("2006-01-02", on); err != nil {
		return nil, &Error{Code: "invalid_date", Message: offsCopy["error.holiday_date"], HTTPStatus: 422}
	}
	label := strings.TrimSpace(req.Label)
	if label == "" || len([]rune(label)) > 80 {
		return nil, &Error{Code: "invalid_label", Message: offsCopy["error.holiday_label"], HTTPStatus: 422}
	}
	park := strings.TrimSpace(req.ParkID)
	if park != "" {
		parks, err := s.repo.TimetableParks(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		ok := false
		for _, p := range parks {
			ok = ok || p.ParkID == park
		}
		if !ok {
			return nil, &Error{Code: "unknown_park", Message: offsCopy["error.holiday_park"], HTTPStatus: 422}
		}
	}
	row, err := s.repo.AddHoliday(ctx, tenantID, actorID, on, park, label)
	if err != nil {
		return nil, err
	}
	return &domain.HolidayResponse{Holiday: composeHoliday(row), TraceID: traceID}, nil
}

// RemoveHoliday takes a holiday off.
func (s *TimetableService) RemoveHoliday(ctx context.Context, tenantID, actorID, holidayID string) error {
	return s.repo.RemoveHoliday(ctx, tenantID, actorID, strings.TrimSpace(holidayID))
}
