// Package ports defines Calendar application dependencies.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
)

var (
	ErrNotFound              = errors.New("calendar: not found")
	ErrForbidden             = errors.New("calendar: forbidden")
	ErrIdempotencyConflict   = errors.New("calendar: idempotency conflict")
	ErrIdempotencyInProgress = errors.New("calendar: idempotency request in progress")
	ErrActiveSnoozeExists    = errors.New("calendar: active snooze exists")
	ErrEventNotActionable    = errors.New("calendar: event is not actionable")
	ErrInvalidReference      = errors.New("calendar: invalid reference")
	ErrProjectionUnavailable = errors.New("calendar: projection unavailable")
	ErrProjectionStale       = errors.New("calendar: projection stale")
)

type Repository interface {
	ListEvents(ctx context.Context, q domain.Query) (domain.CalendarEventListResponse, error)
	GetEventDetail(ctx context.Context, q domain.EventQuery) (domain.CalendarEventDetail, error)
	ListDriveTargets(ctx context.Context, q domain.DriveTargetQuery) (domain.CalendarDriveTargetListResponse, error)
	History(ctx context.Context, q domain.HistoryQuery) (domain.CalendarHistoryResponse, error)
	SendNudge(ctx context.Context, in SendNudge) (domain.CalendarActionResponse, error)
	Snooze(ctx context.Context, in Snooze) (domain.CalendarActionResponse, error)
	AcknowledgeEscalation(ctx context.Context, in AcknowledgeEscalation) (domain.CalendarActionResponse, error)
	ResolveEscalation(ctx context.Context, in ResolveEscalation) (domain.CalendarActionResponse, error)
	SweepDueReminders(ctx context.Context, tenantID string, limit int) (int, error)
	SweepEscalations(ctx context.Context, in SweepEscalations) (int, error)
	RefreshVaccinationProjection(ctx context.Context, in RefreshVaccinationProjection) (int, error)
	PruneClosedVaccinationProjection(ctx context.Context, tenantID string, cutoff time.Time, limit int) (int, error)
	// QueueRoleNotifications writes one notification_requests row per recipient (set-based INSERT,
	// no N+1) for a non-cadence, event-triggered notification such as vaccination verification_pending
	// / rework (vaccination-notification-rules.md §4c). Each recipient row is idempotent on its own
	// device-scoped key, so replaying the same event never duplicates a row. Returns the number of
	// rows actually inserted (0 on an exact replay of every recipient, or when Recipients is empty).
	QueueRoleNotifications(ctx context.Context, in QueueRoleNotifications) (int, error)

	// ResolveVaccinationCompletionContext resolves a vaccination completion_id to the obligation_id,
	// park_id (scope_id), sop_task_id, and recorded_by (executor) it was recorded against. Used by
	// the notification layer to resolve completion IDs from vaccination.verify.rejected events into
	// the obligation/park/task/executor context needed for recipient resolution and notification
	// queuing. Returns ErrNotFound if the completion does not exist or is not linked to a recorded
	// obligation.
	ResolveVaccinationCompletionContext(ctx context.Context, tenantID, completionID string) (VaccinationCompletionContext, error)
}

type SendNudge struct {
	TenantID       string
	EventID        string
	ActorID        string
	TraceID        string
	IdempotencyKey string
	Channel        string
	Message        string
	Reason         string
	Scope          domain.ScopeFilter
}

type Snooze struct {
	TenantID        string
	EventID         string
	ActorID         string
	TraceID         string
	IdempotencyKey  string
	SnoozeUntil     time.Time
	Reason          string
	ReplaceExisting bool
	Scope           domain.ScopeFilter
}

type AcknowledgeEscalation struct {
	TenantID       string
	EventID        string
	ActorID        string
	TraceID        string
	IdempotencyKey string
	Reason         string
	Scope          domain.ScopeFilter
	ActorGrants    []ActorGrant
}

type ResolveEscalation struct {
	TenantID       string
	EventID        string
	ActorID        string
	TraceID        string
	IdempotencyKey string
	Reason         string
	Scope          domain.ScopeFilter
	ActorGrants    []ActorGrant
}

type ActorGrant struct {
	Role      string
	ScopeType string
	ScopeID   string
}

type RefreshVaccinationProjection struct {
	TenantID string
	DateFrom time.Time
	DateTo   time.Time
	Limit    int
}

type SweepEscalations struct {
	TenantID     string
	ObligationID string
	Limit        int
	Now          time.Time
	Level1After  time.Duration
	Level2After  time.Duration
	Level3After  time.Duration
	Level4After  time.Duration
}

// NotificationRecipient is one device to queue a QueueRoleNotifications row for. RoleLabel and
// MemberID are descriptive only (carried into notification_requests.context for observability/
// support triage) -- they never affect delivery, which is entirely driven by FCMToken via
// recipient_ref.
type NotificationRecipient struct {
	MemberID  string
	DeviceID  string
	FCMToken  string
	RoleLabel string
}

// QueueRoleNotifications is the generic, non-cadence event-triggered notification write: given an
// already-resolved recipient list (from another module's own recipient-resolution query -- e.g.
// workforce's ResolveModuleDutyRecipients/ResolveMemberRecipients/ResolvePositionRecipients), write
// one queued notification_requests row per recipient device, linked to an EXISTING
// calendar_event_projections row (the FK the table enforces). Idempotent per (tenant, event,
// notification type, completion, device) — see idempotencyKeyForRecipient in the postgres adapter.
type QueueRoleNotifications struct {
	TenantID         string
	CalendarEventID  string // must already exist in calendar_event_projections (FK)
	TargetType       string
	TargetID         string
	NotificationType string // "verification_pending" | "rework"
	Channel          string // "push_fcm"
	Priority         string // "normal" | "high" -- no dedicated column; carried in context
	Title            string
	Body             string
	TraceID          string
	// EventKey scopes the idempotency key to the triggering event (e.g. "vaccination.verify.pending:"
	// + completionID) so a replay of the SAME event never duplicates a recipient's row, while a
	// DIFFERENT event (e.g. the rework that follows a later resubmission) gets its own rows.
	EventKey   string
	Recipients []NotificationRecipient
}

// VaccinationCompletionContext is the set of obligation/park/task/executor details resolved from a
// vaccination completion_id for notification routing. Returned by ResolveVaccinationCompletionContext
// to decouple the notification layer from importing internal/vaccination.
type VaccinationCompletionContext struct {
	ObligationID string // obligation_instances.obligation_id
	ParkID       string // obligation_instances.scope_id (when scope_type = 'center')
	ScopeType    string // obligation_instances.scope_type (should be 'center' for parks)
	SOPTaskID    string // obligation_instances.sop_task_id
	ExecutedBy   string // vaccination_completions.recorded_by (workforce_member_id, may be empty)
}
