package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

func ip(v int) *int { return &v }

// The farm says a time the way these read (maintainer message 2026-09-30: "6 am", "8:30 am to
// 6 pm", "3 pm to 12 am"). Midnight is 12:00 am whether it is the start of a day (0) or the end
// of the Second shift (1440), and noon is 12:00 pm.
func TestClockTimeLabelSpeaksTheFarmsTime(t *testing.T) {
	for minute, want := range map[int]string{
		0: "12:00 am", 360: "6:00 am", 420: "7:00 am", 510: "8:30 am", 720: "12:00 pm",
		900: "3:00 pm", 1080: "6:00 pm", 1439: "11:59 pm", 1440: "12:00 am",
	} {
		if got := ClockTimeLabel(minute); got != want {
			t.Errorf("ClockTimeLabel(%d) = %q, want %q", minute, got, want)
		}
	}
}

// The three stated shifts, and the Morning shift whose end was not given: the page says the end
// is not set rather than inventing one.
func TestShiftTimingLabelNamesAHalfKnownShiftHonestly(t *testing.T) {
	cases := []struct {
		start, end *int
		want       string
	}{
		{ip(510), ip(1080), "8:30 am – 6:00 pm"},
		{ip(900), ip(1440), "3:00 pm – 12:00 am"},
		{ip(360), nil, "6:00 am – end not set"},
		{nil, nil, "Not set"},
	}
	for _, c := range cases {
		if got := ShiftTimingLabel(c.start, c.end); got != c.want {
			t.Errorf("ShiftTimingLabel = %q, want %q", got, c.want)
		}
	}
}

func TestValidateShiftTimingMirrorsTheTableChecks(t *testing.T) {
	ok := []struct{ start, end *int }{
		{nil, nil}, {ip(0), nil}, {ip(900), ip(1440)}, {ip(1320), ip(360)}, // crosses midnight
	}
	for _, c := range ok {
		if err := ValidateShiftTiming(c.start, c.end); err != nil {
			t.Errorf("ValidateShiftTiming(%v, %v) refused: %v", c.start, c.end, err)
		}
	}
	bad := map[string]struct{ start, end *int }{
		"shift_end_without_start":  {nil, ip(600)},
		"shift_start_out_of_range": {ip(1440), nil},
		"shift_end_out_of_range":   {ip(60), ip(0)},
		"shift_empty":              {ip(0), ip(1440)},
	}
	for code, c := range bad {
		err := ValidateShiftTiming(c.start, c.end)
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != code || appErr.HTTPStatus != 422 {
			t.Errorf("ValidateShiftTiming(%v, %v) = %v, want 422 %s", c.start, c.end, err, code)
		}
	}
}

type fakeTimetableRepo struct {
	parks    []ports.TimetableParkRow
	timings  map[string][]ports.ShiftTimingRow
	counts   map[string]map[string]int
	people   map[string][]ports.TimetablePersonRow
	lastList ports.ListTimetablePeopleParams
}

func (f *fakeTimetableRepo) TimetableParks(context.Context, string) ([]ports.TimetableParkRow, error) {
	return f.parks, nil
}
func (f *fakeTimetableRepo) ParkShiftTimings(_ context.Context, _, parkID string) ([]ports.ShiftTimingRow, error) {
	return f.timings[parkID], nil
}
func (f *fakeTimetableRepo) ParkShiftHeadcount(_ context.Context, _, parkID string) (map[string]int, error) {
	return f.counts[parkID], nil
}
func (f *fakeTimetableRepo) ListTimetablePeople(_ context.Context, p ports.ListTimetablePeopleParams) ([]ports.TimetablePersonRow, string, error) {
	f.lastList = p
	return f.people[p.ParkID], "", nil
}
func (f *fakeTimetableRepo) SetParkShiftTiming(context.Context, string, string, string, string, *int, *int, int) (ports.ShiftTimingRow, error) {
	return ports.ShiftTimingRow{}, nil
}
func (f *fakeTimetableRepo) SetMemberShift(context.Context, string, string, string, string, int) (ports.TimetablePersonRow, error) {
	return ports.TimetablePersonRow{}, nil
}

func twoParkRepo() *fakeTimetableRepo {
	catalog := func(morningStart int) []ports.ShiftTimingRow {
		return []ports.ShiftTimingRow{
			{ShiftCode: "morning", Label: "Morning shift", StartMinute: ip(morningStart), RowVersion: 1},
			{ShiftCode: "general", Label: "General shift", StartMinute: ip(510), EndMinute: ip(1080), RowVersion: 1},
			{ShiftCode: "second", Label: "Second shift", StartMinute: ip(900), EndMinute: ip(1440), RowVersion: 1},
		}
	}
	return &fakeTimetableRepo{
		parks: []ports.TimetableParkRow{
			{ParkID: "cbe", Code: "CBE", Name: "Coimbatore"},
			{ParkID: "cpt", Code: "CPT", Name: "Channapatna"},
			// A park added tomorrow: no timing rows stored yet.
			{ParkID: "new", Code: "NEW", Name: "New park"},
		},
		timings: map[string][]ports.ShiftTimingRow{
			"cbe": catalog(360), "cpt": catalog(420),
			"new": {
				{ShiftCode: "morning", Label: "Morning shift"},
				{ShiftCode: "general", Label: "General shift"},
				{ShiftCode: "second", Label: "Second shift"},
			},
		},
		counts: map[string]map[string]int{"cpt": {"general": 2, "": 1}},
		people: map[string][]ports.TimetablePersonRow{"cpt": {
			{PersonID: "p1", DisplayName: "Amit", DesignationLabel: "Feed Manager", ParkID: "cpt", ShiftCode: "general", RowVersion: 3},
			{PersonID: "p2", DisplayName: "Sagar", ParkID: "cpt"},
		}},
	}
}

// One park at a time: that park's own hours (CPT's Morning starts at 7, not CBE's 6), every
// person's working hours read through THEIR park's shift, and whole-park counts that sum.
func TestTimetableReadsOneParksShiftsAndPeople(t *testing.T) {
	repo := twoParkRepo()
	got, err := NewTimetableService(repo).Timetable(context.Background(), "t", "cpt", "", "", 0, "trace")
	if err != nil {
		t.Fatal(err)
	}
	if got.ParkID != "cpt" || got.ParkLabel != "Channapatna" || len(got.Parks) != 3 {
		t.Fatalf("park = %q %q, parks = %d", got.ParkID, got.ParkLabel, len(got.Parks))
	}
	if got.Shifts[0].TimingLabel != "7:00 am – end not set" || !got.Shifts[0].IsSet {
		t.Fatalf("CPT morning = %+v", got.Shifts[0])
	}
	if got.Shifts[1].PeopleCount != 2 || got.TotalPeople != 3 || got.UnassignedCount != 1 {
		t.Fatalf("counts: general=%d total=%d unassigned=%d", got.Shifts[1].PeopleCount, got.TotalPeople, got.UnassignedCount)
	}
	amit, sagar := got.People[0], got.People[1]
	if amit.ShiftLabel != "General shift" || amit.TimingLabel != "8:30 am – 6:00 pm" || amit.Designation != "Feed Manager" || amit.RowVersion != 3 {
		t.Fatalf("amit = %+v", amit)
	}
	if sagar.ShiftLabel != "Not assigned" || sagar.TimingLabel != "" || sagar.ShiftCode != "" {
		t.Fatalf("sagar = %+v", sagar)
	}
	if repo.lastList.Limit != timetableDefaultLimit {
		t.Fatalf("default page = %d", repo.lastList.Limit)
	}
}

// No park chosen opens the first park in park-code order; a park added in config with no hours
// shows every shift "Not set" so the page can offer Set time.
func TestTimetableDefaultsToTheFirstParkAndShowsANewParksShiftsUnset(t *testing.T) {
	svc := NewTimetableService(twoParkRepo())
	got, err := svc.Timetable(context.Background(), "t", "", "", "", 0, "")
	if err != nil || got.ParkID != "cbe" || got.Shifts[0].TimingLabel != "6:00 am – end not set" {
		t.Fatalf("default park = %+v, %v", got, err)
	}
	fresh, err := svc.Timetable(context.Background(), "t", "new", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range fresh.Shifts {
		if s.IsSet || s.TimingLabel != "Not set" || s.RowVersion != 0 {
			t.Fatalf("new park shift %s = %+v, want unset", s.ShiftCode, s)
		}
	}
}

func TestTimetableRefusesAnUnknownParkOrShiftFilter(t *testing.T) {
	svc := NewTimetableService(twoParkRepo())
	for name, call := range map[string]func() (*domain.WorkforceTimetable, error){
		"unknown_park": func() (*domain.WorkforceTimetable, error) {
			return svc.Timetable(context.Background(), "t", "elsewhere", "", "", 0, "")
		},
		"invalid_filter": func() (*domain.WorkforceTimetable, error) {
			return svc.Timetable(context.Background(), "t", "cpt", "night", "", 0, "")
		},
	} {
		_, err := call()
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != name {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if _, err := svc.Timetable(context.Background(), "t", "cpt", ShiftFilterUnassigned, "", 0, ""); err != nil {
		t.Fatalf("unassigned filter refused: %v", err)
	}
}

// The ground-tier role hints (2026-09-23) reached People, Clock, Leave and Timetable as the raw
// codes "manager" / "assistant_manager". They read in farm words now, and a hint the catalog has
// no words for shows nothing rather than the code.
func TestDesignationNeverShowsARawRoleCode(t *testing.T) {
	for hint, want := range map[string]string{
		"manager": "Manager", "assistant_manager": "Assistant Manager", "park_head": "Park Head",
		"operator": "", "some_new_hint": "",
	} {
		got := composeTimetablePerson(ports.TimetablePersonRow{RoleHint: hint}, nil).Designation
		if got != want {
			t.Errorf("designation for hint %q = %q, want %q", hint, got, want)
		}
	}
	if got := composeTimetablePerson(ports.TimetablePersonRow{DesignationLabel: "Feed Manager", RoleHint: "manager"}, nil).Designation; got != "Feed Manager" {
		t.Fatalf("the catalog designation must win over the hint, got %q", got)
	}
}

func (f *fakeTimetableRepo) SetMemberWeekOffs(context.Context, string, string, string, []int, int) (ports.TimetablePersonRow, error) {
	return ports.TimetablePersonRow{}, nil
}

func (f *fakeTimetableRepo) Holidays(context.Context, string, string, string, string) ([]ports.HolidayRow, error) {
	return nil, nil
}

func (f *fakeTimetableRepo) AddHoliday(context.Context, string, string, string, string, string) (ports.HolidayRow, error) {
	return ports.HolidayRow{}, nil
}

func (f *fakeTimetableRepo) RemoveHoliday(context.Context, string, string, string) error { return nil }
