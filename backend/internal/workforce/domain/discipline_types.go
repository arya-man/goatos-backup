package domain

import (
	"strconv"
	"strings"
)

// HRMS VIOLATIONS AND ENQUIRIES (maintainer decisions 2026-09-30). A violation is recorded against
// one person with a fine in rupees and is FINAL when recorded; a mistaken one is withdrawn with a
// reason, never deleted, and nothing deducts the fine from pay. An enquiry is opened by a farm
// event (first: an approved animal death); the park head (phone) or HR (web) fills it and it
// records the violations it names. WHICH violation types exist and each enquiry's questions and
// deadline are the published HRMS SOP (internal/hrmssop). A TYPE CARRIES NO MONEY (maintainer,
// 2026-09-30: "never map money to mistake, both are separate"): the fine is typed on each
// violation, blank meaning no fine, and is never priced from the list. Every visible sentence
// below is composed by the backend.

// Violation statuses and sources.
const (
	ViolationRecorded  = "recorded"
	ViolationWithdrawn = "withdrawn"
	// ViolationPending is an AUTOMATIC clock-in violation waiting for HR (2026-09-30); HR keeps it
	// (-> recorded) or closes it (-> closed).
	ViolationPending    = "pending"
	ViolationClosed     = "closed"
	ViolationManual     = "manual"
	ViolationEnquiry    = "enquiry"
	ViolationAttendance = "attendance"

	// Periods the per-person totals and the list cover.
	PeriodMonth = "month"
	PeriodYear  = "year"
	PeriodAll   = "all"

	EnquiryOpen      = "open"
	EnquirySubmitted = "submitted"
)

// ViolationTypeOption is an authored violation type a new record may use.
type ViolationTypeOption struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// PersonOption is a person a violation may be recorded against.
type PersonOption struct {
	PersonID    string `json:"person_id"`
	Name        string `json:"name"`
	Designation string `json:"designation"`
	ParkID      string `json:"park_id"`
	ParkLabel   string `json:"park_label"`
}

// Violation is one recorded violation, composed for display.
type Violation struct {
	ViolationID     string `json:"violation_id"`
	PersonID        string `json:"person_id"`
	PersonName      string `json:"person_name"`
	Designation     string `json:"designation"`
	ParkID          string `json:"park_id"`
	ParkLabel       string `json:"park_label"`
	TypeKey         string `json:"type_key"`
	TypeLabel       string `json:"type_label"`
	FineRupees      int    `json:"fine_rupees"`
	FineLabel       string `json:"fine_label"`
	OccurredOn      string `json:"occurred_on"`
	OccurredOnLabel string `json:"occurred_on_label"`
	Note            string `json:"note"`
	Source          string `json:"source"`
	SourceLabel     string `json:"source_label"`
	EnquiryID       string `json:"enquiry_id"`
	RecordedByName  string `json:"recorded_by_name"`
	RecordedAtLabel string `json:"recorded_at_label"`
	Status          string `json:"status"`
	StatusLabel     string `json:"status_label"`
	WithdrawReason  string `json:"withdraw_reason"`
	// AttendanceKind is "late" / "absent" on an automatic clock-in violation, "" otherwise.
	AttendanceKind string `json:"attendance_kind"`
	// Detail is the backend-composed fact behind an automatic one ("Clocked in 7:42 am · shift
	// starts 7:00 am · 42 min late").
	Detail         string `json:"detail"`
	DecidedByName  string `json:"decided_by_name"`
	DecidedAtLabel string `json:"decided_at_label"`
	DecisionNote   string `json:"decision_note"`
	SOPVersion     int    `json:"sop_version"`
	RowVersion     int    `json:"row_version"`
}

// ViolationPersonTotal is one person's totals for the period: violations that count (recorded),
// their fines, how many still wait for HR, how many HR closed, and the leave they took (approved)
// or applied for (pending). A person with leave but no violation is listed too.
type ViolationPersonTotal struct {
	PersonID         string `json:"person_id"`
	PersonName       string `json:"person_name"`
	Designation      string `json:"designation"`
	ParkLabel        string `json:"park_label"`
	Count            int    `json:"count"`
	FineRupees       int    `json:"fine_rupees"`
	FineLabel        string `json:"fine_label"`
	Pending          int    `json:"pending"`
	Closed           int    `json:"closed"`
	LeaveDays        int    `json:"leave_days"`
	LeavePendingDays int    `json:"leave_pending_days"`
	LeaveLabel       string `json:"leave_label"`
}

// ViolationSummary is the whole-filter aggregate (grain: recorded violation; people = distinct
// persons among them). Pagination never moves it.
type ViolationSummary struct {
	Count      int    `json:"count"`
	FineRupees int    `json:"fine_rupees"`
	FineLabel  string `json:"fine_label"`
	People     int    `json:"people"`
	// Pending is how many automatic violations still wait for HR in the period (every status).
	Pending int `json:"pending"`
}

// MonthOption is one month the page can show ("Sep 2026" -- a month heading, not a date).
type MonthOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// ViolationsPage is People / HRMS > Violations for one filter.
type ViolationsPage struct {
	Parks  []TimetablePark `json:"parks"`
	ParkID string          `json:"park_id"`
	Months []MonthOption   `json:"months"`
	Month  string          `json:"month"`
	// Period is month / year / all; the list, the summary and the per-person totals follow it.
	Periods     []MonthOption          `json:"periods"`
	Period      string                 `json:"period"`
	PeriodLabel string                 `json:"period_label"`
	Status      string                 `json:"status"`
	Summary     ViolationSummary       `json:"summary"`
	ByPerson    []ViolationPersonTotal `json:"by_person"`
	Items       []Violation            `json:"items"`
	NextCursor  string                 `json:"next_cursor"`
	Types       []ViolationTypeOption  `json:"types"`
	People      []PersonOption         `json:"people"`
	SOPVersion  int                    `json:"sop_version"`
	TraceID     string                 `json:"trace_id"`
}

// RecordViolationRequest records one violation by hand. FineRupees nil means no fine -- never a
// price looked up from the type.
type RecordViolationRequest struct {
	PersonID       string `json:"person_id"`
	TypeKey        string `json:"type_key"`
	FineRupees     *int   `json:"fine_rupees"`
	OccurredOn     string `json:"occurred_on"`
	Note           string `json:"note"`
	IdempotencyKey string `json:"idempotency_key"`
}

// KeepViolationRequest is HR keeping an automatic violation: it becomes recorded, with the fine HR
// types (nil = no fine) and an optional note.
type KeepViolationRequest struct {
	FineRupees *int   `json:"fine_rupees"`
	Note       string `json:"note"`
	RowVersion int    `json:"row_version"`
}

// CloseViolationRequest is HR closing an automatic violation with a reason.
type CloseViolationRequest struct {
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

// WithdrawViolationRequest withdraws a mistaken violation.
type WithdrawViolationRequest struct {
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

// ViolationResponse is one violation after a write.
type ViolationResponse struct {
	Violation Violation `json:"violation"`
	TraceID   string    `json:"trace_id"`
}

// EnquiryQuestion is one question the enquiry asks, from its pinned SOP version.
type EnquiryQuestion struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Required bool   `json:"required"`
}

// Enquiry is one enquiry row, composed for a list.
type Enquiry struct {
	EnquiryID        string `json:"enquiry_id"`
	TriggerKey       string `json:"trigger_key"`
	Title            string `json:"title"`
	SubjectLabel     string `json:"subject_label"`
	ParkID           string `json:"park_id"`
	ParkLabel        string `json:"park_label"`
	OccurredAtLabel  string `json:"occurred_at_label"`
	OpenedAtLabel    string `json:"opened_at_label"`
	DueAt            string `json:"due_at"`
	DueAtLabel       string `json:"due_at_label"`
	Status           string `json:"status"`
	StatusLabel      string `json:"status_label"`
	Overdue          bool   `json:"overdue"`
	SubmittedByName  string `json:"submitted_by_name"`
	SubmittedAtLabel string `json:"submitted_at_label"`
	PenaltyCount     int    `json:"penalty_count"`
	PenaltyLabel     string `json:"penalty_label"`
	RowVersion       int    `json:"row_version"`
}

// EnquirySummary is whole-filter (grain: enquiry; open includes overdue).
type EnquirySummary struct {
	Open      int `json:"open"`
	Overdue   int `json:"overdue"`
	Submitted int `json:"submitted"`
}

// EnquiryPage is the enquiry list.
type EnquiryPage struct {
	Parks      []TimetablePark `json:"parks"`
	ParkID     string          `json:"park_id"`
	Status     string          `json:"status"`
	Summary    EnquirySummary  `json:"summary"`
	Items      []Enquiry       `json:"items"`
	NextCursor string          `json:"next_cursor"`
	TraceID    string          `json:"trace_id"`
}

// EnquiryDetail is one enquiry with everything its report needs.
type EnquiryDetail struct {
	Enquiry    Enquiry               `json:"enquiry"`
	Questions  []EnquiryQuestion     `json:"questions"`
	Answers    map[string]any        `json:"answers"`
	Violations []Violation           `json:"violations"`
	Types      []ViolationTypeOption `json:"types"`
	People     []PersonOption        `json:"people"`
	CanSubmit  bool                  `json:"can_submit"`
	SOPVersion int                   `json:"sop_version"`
	TraceID    string                `json:"trace_id"`
}

// EnquiryPenalty names one responsible person and what they are fined for.
type EnquiryPenalty struct {
	PersonID   string `json:"person_id"`
	TypeKey    string `json:"type_key"`
	FineRupees *int   `json:"fine_rupees"`
	Note       string `json:"note"`
}

// SubmitEnquiryRequest submits the report. No penalties is a valid report: nobody responsible.
type SubmitEnquiryRequest struct {
	Answers    map[string]any   `json:"answers"`
	Penalties  []EnquiryPenalty `json:"penalties"`
	RowVersion int              `json:"row_version"`
}

// FineLabel is one violation's fine as a reader sees it: "No fine" when none was given.
func FineLabel(amount int) string {
	if amount == 0 {
		return "No fine"
	}
	return RupeesLabel(amount)
}

// RupeesLabel renders a whole-rupee amount the Indian way: ₹1,500 / ₹1,00,000.
func RupeesLabel(amount int) string {
	neg := amount < 0
	if neg {
		amount = -amount
	}
	s := strconv.Itoa(amount)
	if len(s) > 3 {
		head, tail := s[:len(s)-3], s[len(s)-3:]
		var groups []string
		for len(head) > 2 {
			groups = append([]string{head[len(head)-2:]}, groups...)
			head = head[:len(head)-2]
		}
		if head != "" {
			groups = append([]string{head}, groups...)
		}
		s = strings.Join(groups, ",") + "," + tail
	}
	if neg {
		return "-₹" + s
	}
	return "₹" + s
}
