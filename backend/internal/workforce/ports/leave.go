package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Leave requests (docs/features/leave-requests/plan.md, maintainer decisions
// 2026-09-10). The repository owns ONE transaction per write: idempotency
// reservation, the row change, the workforce_absences mirror on final
// approval, the audit row and the outbox event.

// ErrLeaveOverlap refuses a request whose window overlaps a pending or
// approved request of the same person.
var ErrLeaveOverlap = errors.New("leave window overlaps an open request")

// ErrLeaveNotPending refuses a withdraw or a decision on a request that has
// already reached a final status.
var ErrLeaveNotPending = errors.New("leave request is no longer pending")

// ErrLeaveSlotDecided refuses a second decision on a slot that already
// carries one.
var ErrLeaveSlotDecided = errors.New("this approver slot is already decided")

// ErrLeaveSlotNotRequired refuses a decision on a slot the routing snapshot
// did not require for this request.
var ErrLeaveSlotNotRequired = errors.New("this approver slot is not required for the request")

// ErrLeaveConfigConflict is an optimistic-lock miss on the routing config.
var ErrLeaveConfigConflict = errors.New("leave approval config was changed by someone else")

// LeaveRequestRow is the raw row plus the 1:1 label joins (requester name and
// designation, park label, decider names). Label composition is the app
// service's job.
type LeaveRequestRow struct {
	LeaveRequestID    string
	WorkforceMemberID string
	PersonName        string
	RoleHint          string
	DesignationGrade  string
	ParkID            string
	ParkLabel         string
	StartsOn          string
	EndsOn            string
	Reason            string
	Status            string
	ParkHeadRequired  bool
	HRRequired        bool
	ParkHeadDecision  string
	ParkHeadDecidedBy string
	ParkHeadDeciderNm string
	ParkHeadDecidedAt *time.Time
	ParkHeadNote      string
	HRDecision        string
	HRDecidedBy       string
	HRDeciderName     string
	HRDecidedAt       *time.Time
	HRNote            string
	DecidedAt         *time.Time
	AbsenceID         string
	RaisedByUserID    string
	RaisedAt          time.Time
	RowVersion        int
}

// CreateLeaveRequestCommand is the fully-resolved raise.
type CreateLeaveRequestCommand struct {
	TenantID          string
	WorkforceMemberID string
	ActorUserID       string
	// ParkID is the requester's primary_location_id at raise time ("" = none).
	ParkID           string
	StartsOn         string
	EndsOn           string
	Reason           string
	IdempotencyKey   string
	ParkHeadRequired bool
	HRRequired       bool
	// BusinessDate is the raise-day IST date for the outbox headers.
	BusinessDate string
}

// LeaveRequestWrite is what a write hands back.
type LeaveRequestWrite struct {
	Row      LeaveRequestRow
	Replayed bool
}

// WithdrawLeaveRequestCommand withdraws a pending request; only the
// requester may (the service checks member ownership before calling).
type WithdrawLeaveRequestCommand struct {
	TenantID          string
	LeaveRequestID    string
	WorkforceMemberID string
	ActorUserID       string
	IdempotencyKey    string
	BusinessDate      string
}

// DecideLeaveRequestCommand records one approver slot's decision. On the
// LAST required approval the repository flips the request to approved and
// mirrors it into workforce_absences; on ANY rejection it flips to rejected.
type DecideLeaveRequestCommand struct {
	TenantID       string
	LeaveRequestID string
	// Slot: park_head | hr.
	Slot string
	// Decision: approved | rejected.
	Decision       string
	Note           string
	ActorUserID    string
	IdempotencyKey string
	BusinessDate   string
}

// LeaveQueueParams is the approver's open queue. A row is listed when the
// caller may decide an OPEN required slot on it: park heads see their parks'
// park_head slot, HR sees the hr slot, the CEO sees both.
type LeaveQueueParams struct {
	TenantID string
	// ParkHeadParks lists the park ids the caller heads (nil = not a park head).
	ParkHeadParks []string
	// HR is true when the caller decides the hr slot.
	HR bool
	// Any is true for the CEO floor: every open slot.
	Any    bool
	Limit  int
	Cursor string
}

// LeaveAdminListParams is the People / HRMS list.
type LeaveAdminListParams struct {
	TenantID string
	// Status filter ("" = every status).
	Status string
	ParkID string
	Limit  int
	Cursor string
}

// LeavePage is one keyset page of raw rows.
type LeavePage struct {
	Rows       []LeaveRequestRow
	NextCursor string
}

// LeaveApprovalConfigRow is the routing config row ("" UpdatedBy = default).
type LeaveApprovalConfigRow struct {
	ParkHeadRequired bool
	HRRequired       bool
	UpdatedBy        string
	UpdatedByName    string
	UpdatedAt        *time.Time
	RowVersion       int
	Stored           bool
}

// LeaveApproverMembers lists the workforce members holding each slot for a
// park, resolved from ACTIVE role grants (park_head scoped to the park; hr at
// tenant scope). Used by the notification bridge to fan a raise out.
type LeaveApproverMembers struct {
	ParkHead []string
	HR       []string
}

// LeaveRepository is the leave-request port, implemented by the same
// postgres Repository as the clock and roster ports.
type LeaveRepository interface {
	CreateLeaveRequest(ctx context.Context, cmd CreateLeaveRequestCommand) (LeaveRequestWrite, error)
	WithdrawLeaveRequest(ctx context.Context, cmd WithdrawLeaveRequestCommand) (LeaveRequestWrite, error)
	DecideLeaveRequest(ctx context.Context, cmd DecideLeaveRequestCommand) (LeaveRequestWrite, error)
	GetLeaveRequest(ctx context.Context, tenantID, leaveRequestID string) (LeaveRequestRow, error)
	// ListLeaveRequestsForMember returns every pending or future request plus
	// the most recent decided ones, newest window first, bounded by limit.
	ListLeaveRequestsForMember(ctx context.Context, tenantID, workforceMemberID, today string, limit int) ([]LeaveRequestRow, error)
	ListLeaveQueue(ctx context.Context, params LeaveQueueParams) (LeavePage, error)
	ListLeaveRequestsAdmin(ctx context.Context, params LeaveAdminListParams) (LeavePage, error)
	// LeaveTodayForMember reports whether an APPROVED request covers the date.
	LeaveTodayForMember(ctx context.Context, tenantID, workforceMemberID, businessDate string) (*LeaveRequestRow, error)
	GetLeaveApprovalConfig(ctx context.Context, tenantID string) (LeaveApprovalConfigRow, error)
	SetLeaveApprovalConfig(ctx context.Context, tenantID, actorUserID string, update domain.LeaveApprovalConfigUpdate) (LeaveApprovalConfigRow, error)
	ResolveLeaveApproverMembers(ctx context.Context, tenantID, parkID string) (LeaveApproverMembers, error)
}
