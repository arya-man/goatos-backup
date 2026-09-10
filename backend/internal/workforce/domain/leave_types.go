package domain

// Leave requests (docs/features/leave-requests/plan.md, maintainer decisions
// 2026-09-10). An operator asks for leave from the phone Clock In / Out
// screen; their Park Head AND a holder of the `hr` role both have to approve,
// either one rejecting ends it, and the operator can withdraw while pending.
// Every label below is backend-owned: the phone and admin-web render these
// strings verbatim and never compose a date range, a status line or a slot
// name of their own.

const (
	LeaveRequestStatusPending   = "pending"
	LeaveRequestStatusApproved  = "approved"
	LeaveRequestStatusRejected  = "rejected"
	LeaveRequestStatusWithdrawn = "withdrawn"

	LeaveSlotParkHead = "park_head"
	LeaveSlotHR       = "hr"

	LeaveDecisionApproved = "approved"
	LeaveDecisionRejected = "rejected"

	// MaxLeaveReasonLength mirrors the workforce_leave_requests_reason_check.
	MaxLeaveReasonLength = 2000
	// MaxLeaveDays bounds one request; longer absences are HR's roster work,
	// not a phone ask.
	MaxLeaveDays = 90
	// MaxLeavePageSize caps one approver / admin page; keyset cursors page on.
	MaxLeavePageSize = 20
)

// LeaveSlotDecision is one approver slot's recorded decision, or nil while
// the slot is still open.
type LeaveSlotDecision struct {
	// Slot: park_head | hr.
	Slot string `json:"slot"`
	// Decision: approved | rejected.
	Decision      string `json:"decision"`
	DecidedByName string `json:"decided_by_name,omitempty"`
	DecidedAt     string `json:"decided_at"`
	Note          string `json:"note,omitempty"`
}

// LeaveRequest is the wire row shared by the requester's history, the
// approver queue and the People / HRMS list.
type LeaveRequest struct {
	LeaveRequestID    string  `json:"leave_request_id"`
	WorkforceMemberID string  `json:"workforce_member_id"`
	PersonName        string  `json:"person_name"`
	Designation       string  `json:"designation,omitempty"`
	ParkID            *string `json:"park_id,omitempty"`
	ParkLabel         *string `json:"park_label,omitempty"`
	StartsOn          string  `json:"starts_on"`
	EndsOn            string  `json:"ends_on"`
	DayCount          int     `json:"day_count"`
	// DatesLabel is the farm-readable window: "12/09/2026 – 14/09/2026 · 3 days". Both ends
	// render in full rather than eliding the shared month or year, because every visible date
	// is DD/MM/YYYY (maintainer decision 2026-09-10).
	DatesLabel string `json:"dates_label"`
	Reason     string `json:"reason"`
	// Status: pending | approved | rejected | withdrawn.
	Status      string `json:"status"`
	StatusLabel string `json:"status_label"`
	// StatusLine explains where the request stands: "Park head approved ·
	// waiting for HR", "Rejected by HR · <reason>", "Withdrawn".
	StatusLine       string             `json:"status_line"`
	ParkHeadRequired bool               `json:"park_head_required"`
	HRRequired       bool               `json:"hr_required"`
	ParkHead         *LeaveSlotDecision `json:"park_head,omitempty"`
	HR               *LeaveSlotDecision `json:"hr,omitempty"`
	RaisedAt         string             `json:"raised_at"`
	RaisedAtLabel    string             `json:"raised_at_label"`
	DecidedAt        *string            `json:"decided_at,omitempty"`
	// CanWithdraw is true for the requester while the request is pending.
	CanWithdraw bool `json:"can_withdraw"`
	// MySlot names the slot the CALLER decides on this row (approver queue
	// only): park_head | hr. Empty on the requester's own history.
	MySlot      string `json:"my_slot,omitempty"`
	MySlotLabel string `json:"my_slot_label,omitempty"`
	RowVersion  int    `json:"row_version"`
}

// LeaveRequestCreate is the body of POST /app/leave/requests.
type LeaveRequestCreate struct {
	// IdempotencyKey may also arrive via the Idempotency-Key header; body
	// wins so an outbox-queued request stays self-contained.
	IdempotencyKey string `json:"idempotency_key"`
	StartsOn       string `json:"starts_on"`
	EndsOn         string `json:"ends_on"`
	Reason         string `json:"reason"`
}

// LeaveRequestResponse is the result of a raise / withdraw / decision.
type LeaveRequestResponse struct {
	Request LeaveRequest `json:"request"`
	// IdempotentReplay is true when the write was an exact replay.
	IdempotentReplay bool   `json:"idempotent_replay"`
	TraceID          string `json:"trace_id"`
}

// LeaveRequestListResponse is a page of requests (own history, approver
// queue, or admin list). Summary counts are whole-filter, never page-local.
type LeaveRequestListResponse struct {
	Items      []LeaveRequest    `json:"items"`
	NextCursor string            `json:"next_cursor"`
	Copy       map[string]string `json:"copy"`
	TraceID    string            `json:"trace_id"`
}

// LeaveDecisionRequest is the body of the approve / reject verbs.
type LeaveDecisionRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	// Reason is REQUIRED on reject and optional on approve.
	Reason string `json:"reason"`
	// Slot is only honoured for a caller who may decide EITHER slot (the CEO
	// floor); a park head or HR caller's slot is derived server-side and a
	// mismatching value is refused.
	Slot string `json:"slot,omitempty"`
}

// LeaveApprovalConfig is the CEO-only routing config (GET/PUT
// /admin/leave/approval-config). An absent row reads as both required.
type LeaveApprovalConfig struct {
	ParkHeadRequired bool   `json:"park_head_required"`
	HRRequired       bool   `json:"hr_required"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	UpdatedByName    string `json:"updated_by_name,omitempty"`
	RowVersion       int    `json:"row_version"`
}

// LeaveApprovalConfigResponse wraps the config with its copy.
type LeaveApprovalConfigResponse struct {
	Config  LeaveApprovalConfig `json:"config"`
	Copy    map[string]string   `json:"copy"`
	TraceID string              `json:"trace_id"`
}

// LeaveApprovalConfigUpdate is the PUT body.
type LeaveApprovalConfigUpdate struct {
	ParkHeadRequired bool `json:"park_head_required"`
	HRRequired       bool `json:"hr_required"`
	RowVersion       int  `json:"row_version"`
}

// LeaveTodaySummary is the requester's own leave state for the Clock status
// read: whether an approved leave covers the business date.
type LeaveTodaySummary struct {
	OnLeave bool   `json:"on_leave"`
	Label   string `json:"label,omitempty"`
}
