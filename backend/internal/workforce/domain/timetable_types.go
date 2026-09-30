package domain

// HRMS Timetable (maintainer request 2026-09-30): People / HRMS > Timetable. One park at a time:
// that park's shifts with their working hours, then everyone whose HOME park it is, with the
// shift each person works. HR and the CEO/CXO edit both. Every visible word is composed here or
// in the page contract; the client renders it verbatim.

// TimetablePark is one park the page can be switched to.
type TimetablePark struct {
	ParkID string `json:"park_id"`
	Label  string `json:"label"`
}

// TimetableShift is one named shift at the selected park. Start and end are minutes after IST
// midnight; either may be absent (a shift whose hours were never set, or the Morning shift whose
// end was not stated). End may be 1440 (midnight) and may be earlier than start (a shift that
// crosses midnight).
type TimetableShift struct {
	ShiftCode   string `json:"shift_code"`
	Label       string `json:"label"`
	StartMinute *int   `json:"start_minute"`
	EndMinute   *int   `json:"end_minute"`
	// TimingLabel is the sentence the page shows: "8:30 am – 6:00 pm", "6:00 am – end not
	// set", or "Not set".
	TimingLabel string `json:"timing_label"`
	// IsSet is true when the start is known.
	IsSet bool `json:"is_set"`
	// PeopleCount is how many people at this park work this shift -- the WHOLE park, never the
	// page on screen.
	PeopleCount int `json:"people_count"`
	// RowVersion is 0 when this park has never stored a timing for this shift; the first write
	// sends 0.
	RowVersion int `json:"row_version"`
}

// TimetablePerson is one person whose home park is the selected park.
type TimetablePerson struct {
	PersonID    string `json:"person_id"`
	DisplayName string `json:"display_name"`
	Designation string `json:"designation"`
	Department  string `json:"department"`
	// ShiftCode is "" when no shift is assigned yet.
	ShiftCode  string `json:"shift_code"`
	ShiftLabel string `json:"shift_label"`
	// TimingLabel is the person's working hours: their shift's hours at their park, "Not set"
	// when that shift has no hours yet, or "" when no shift is assigned.
	TimingLabel string `json:"timing_label"`
	RowVersion  int    `json:"row_version"`
}

// WorkforceTimetable is the whole page read for one park.
type WorkforceTimetable struct {
	Parks     []TimetablePark  `json:"parks"`
	ParkID    string           `json:"park_id"`
	ParkLabel string           `json:"park_label"`
	Shifts    []TimetableShift `json:"shifts"`
	// ShiftFilter echoes the applied filter: "" (everyone), a shift code, or "unassigned".
	ShiftFilter string `json:"shift_filter"`
	// TotalPeople and UnassignedCount are whole-park aggregates (grain: person; the shift
	// buckets and the unassigned bucket are disjoint and sum to TotalPeople). Pagination moves
	// People only.
	TotalPeople     int               `json:"total_people"`
	UnassignedCount int               `json:"unassigned_count"`
	People          []TimetablePerson `json:"people"`
	NextCursor      string            `json:"next_cursor"`
	TraceID         string            `json:"trace_id"`
}

// ShiftTimingUpdate sets one shift's hours at one park. A nil start clears the timing; a nil end
// with a start leaves the end unset.
type ShiftTimingUpdate struct {
	StartMinute *int `json:"start_minute"`
	EndMinute   *int `json:"end_minute"`
	RowVersion  int  `json:"row_version"`
}

// ShiftTimingResponse is the stored timing after a write, composed the way the read shows it.
type ShiftTimingResponse struct {
	ParkID  string         `json:"park_id"`
	Shift   TimetableShift `json:"shift"`
	TraceID string         `json:"trace_id"`
}

// MemberShiftUpdate puts a person on a shift; an empty ShiftCode takes them off every shift.
type MemberShiftUpdate struct {
	ShiftCode  string `json:"shift_code"`
	RowVersion int    `json:"row_version"`
}

// MemberShiftResponse is the person's row after a write.
type MemberShiftResponse struct {
	Person  TimetablePerson `json:"person"`
	TraceID string          `json:"trace_id"`
}
