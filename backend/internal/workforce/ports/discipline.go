package ports

import (
	"context"
	"errors"
	"time"
)

// HRMS violations and enquiries (maintainer decisions 2026-09-30).

var (
	// ErrViolationVersionConflict: the violation was changed (or already withdrawn) by someone else.
	ErrViolationVersionConflict = errors.New("the violation was changed by someone else")
	// ErrEnquiryNotOpen: the enquiry was already submitted.
	ErrEnquiryNotOpen = errors.New("the enquiry was already submitted")
	// ErrEnquiryVersionConflict: the enquiry changed after the screen loaded it.
	ErrEnquiryVersionConflict = errors.New("the enquiry was changed by someone else")
)

// ViolationRow is a stored violation with the names the page shows.
type ViolationRow struct {
	ViolationID      string
	PersonID         string
	PersonName       string
	DesignationLabel string
	RoleHint         string
	DesignationGrade string
	ParkID           string
	ParkLabel        string
	TypeKey          string
	TypeLabel        string
	FineRupees       int
	OccurredOn       time.Time
	Note             string
	Source           string
	EnquiryID        string
	RecordedByName   string
	RecordedAt       time.Time
	Status           string
	WithdrawReason   string
	AttendanceKind   string
	Detail           string
	DecidedByName    string
	DecidedAt        *time.Time
	DecisionNote     string
	SOPVersion       int
	RowVersion       int
}

// ViolationFilter scopes the violations read. From/To bound occurred_on (inclusive/exclusive);
// ParkIDs nil means every park.
type ViolationFilter struct {
	TenantID string
	ParkIDs  []string
	From     time.Time
	To       time.Time
	Status   string
}

// ViolationTotalRow is one person's total for the filter.
type ViolationTotalRow struct {
	PersonID         string
	PersonName       string
	DesignationLabel string
	RoleHint         string
	DesignationGrade string
	ParkLabel        string
	Count            int
	FineRupees       int
	Pending          int
	Closed           int
	LeaveDays        int
	LeavePendingDays int
}

// ViolationSummaryRow is the whole-filter aggregate.
type ViolationSummaryRow struct {
	Count      int
	FineRupees int
	People     int
	// Pending counts violations waiting for HR under the same scope and period, any status filter.
	Pending int
}

// AttendanceQuery asks who is owed an automatic clock-in violation now (2026-09-30).
type AttendanceQuery struct {
	TenantID     string
	Today        string // IST business date; today and yesterday are checked
	Now          time.Time
	GraceMinutes int
	LateOn       bool
	AbsentOn     bool
	// StartsOn is the first business date checked ("" = no floor).
	StartsOn string
	Limit    int
}

// AttendanceCloseCopy is the reason a waiting violation closes itself with, per cause.
type AttendanceCloseCopy struct {
	Leave   string
	Holiday string
	WeekOff string
}

// AttendanceCandidate is one (person, day, kind) owed a violation. ClockInAt is nil when absent.
type AttendanceCandidate struct {
	PersonID    string
	ParkID      string
	ShiftCode   string
	ShiftLabel  string
	Day         time.Time
	Kind        string // late | absent
	StartMinute int
	ClockInAt   *time.Time
}

// NewAttendanceViolation is one waiting violation to insert.
type NewAttendanceViolation struct {
	PersonID  string
	ParkID    string
	TypeKey   string
	TypeLabel string
	Day       time.Time
	Kind      string
	ShiftCode string
	Detail    string
}

// PersonOptionRow is an active person a violation may name.
type PersonOptionRow struct {
	PersonID         string
	Name             string
	DesignationLabel string
	RoleHint         string
	DesignationGrade string
	ParkID           string
	ParkLabel        string
}

// NewViolation is one violation to insert.
type NewViolation struct {
	PersonID       string
	TypeKey        string
	TypeLabel      string
	FineRupees     int
	OccurredOn     time.Time
	Note           string
	SOPVersion     int
	IdempotencyKey string
	Fingerprint    string
}

// EnquiryRow is a stored enquiry.
type EnquiryRow struct {
	EnquiryID       string
	TriggerKey      string
	SubjectType     string
	SubjectID       string
	ParkID          string
	ParkLabel       string
	SubjectLabel    string
	OccurredAt      time.Time
	OpenedAt        time.Time
	DueAt           time.Time
	SOPVersion      int
	Status          string
	Answers         map[string]any
	SubmittedByName string
	SubmittedAt     *time.Time
	PenaltyCount    int
	RowVersion      int
}

// EnquiryFilter scopes the enquiry list. Status "" = all, "open", "overdue", "submitted".
type EnquiryFilter struct {
	TenantID string
	ParkIDs  []string
	Status   string
	Now      time.Time
	Cursor   string
	Limit    int
}

// OpenEnquiryCommand opens one enquiry, idempotently on (trigger, subject).
type OpenEnquiryCommand struct {
	TenantID    string
	TriggerKey  string
	SubjectType string
	SubjectID   string
	ParkID      string
	ShedID      string
	OccurredAt  time.Time
	OpenedAt    time.Time
	DueHours    int
	SOPVersion  int
}

// SubmitEnquiryCommand submits a report and records its violations in one transaction.
type SubmitEnquiryCommand struct {
	TenantID    string
	EnquiryID   string
	ActorUserID string
	Answers     map[string]any
	Penalties   []NewViolation
	RowVersion  int
}

// DisciplineRepository is the violations + enquiries port.
type DisciplineRepository interface {
	ListViolations(ctx context.Context, f ViolationFilter, cursor string, limit int) ([]ViolationRow, string, error)
	ViolationSummary(ctx context.Context, f ViolationFilter) (ViolationSummaryRow, error)
	ViolationTotalsByPerson(ctx context.Context, f ViolationFilter, limit int) ([]ViolationTotalRow, error)
	PersonOptions(ctx context.Context, tenantID string, parkIDs []string) ([]PersonOptionRow, error)
	RecordViolation(ctx context.Context, tenantID, actorUserID string, v NewViolation) (ViolationRow, error)
	WithdrawViolation(ctx context.Context, tenantID, actorUserID, violationID, reason string, rowVersion int) (ViolationRow, error)
	// KeepViolation turns a WAITING violation into a recorded one with fineRupees and note.
	KeepViolation(ctx context.Context, tenantID, actorUserID, violationID string, fineRupees int, note string, rowVersion int) (ViolationRow, error)
	// CloseViolation closes a WAITING violation with a reason.
	CloseViolation(ctx context.Context, tenantID, actorUserID, violationID, reason string, rowVersion int) (ViolationRow, error)
	AttendanceCandidates(ctx context.Context, q AttendanceQuery) ([]AttendanceCandidate, error)
	InsertAttendanceViolations(ctx context.Context, tenantID string, sopVersion int, items []NewAttendanceViolation) (int, error)
	// CloseExcusedAttendance closes WAITING automatic violations whose day is now covered by leave
	// the person applied for, a holiday or their weekly off. Returns how many closed.
	CloseExcusedAttendance(ctx context.Context, tenantID string, reasons AttendanceCloseCopy) (int, error)
	ViolationsForEnquiry(ctx context.Context, tenantID, enquiryID string) ([]ViolationRow, error)

	OpenEnquiry(ctx context.Context, cmd OpenEnquiryCommand) (created bool, err error)
	ListEnquiries(ctx context.Context, f EnquiryFilter) ([]EnquiryRow, string, error)
	EnquirySummary(ctx context.Context, f EnquiryFilter) (open, overdue, submitted int, err error)
	GetEnquiry(ctx context.Context, tenantID, enquiryID string) (EnquiryRow, error)
	SubmitEnquiry(ctx context.Context, cmd SubmitEnquiryCommand) (EnquiryRow, error)

	// ParkHeadParks are the parks a user heads: park-scoped park_head grants, plus the home
	// park of a tenant-scoped one (the shape the live roster carries).
	ParkHeadParks(ctx context.Context, tenantID, userID string) ([]string, error)
	// MemberHomePark is a member's primary_location_id ("" when none).
	MemberHomePark(ctx context.Context, tenantID, personID string) (string, error)
}
