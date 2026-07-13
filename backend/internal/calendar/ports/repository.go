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

	// SweepReminderCadence computes the vaccination reminder cadence ladder fires
	// (vaccination-notification-rules.md §3) due as of in.Now, already collapsed per (park, fire
	// day, notification type, slot) so multiple obligations due the same park the same fire day
	// batch into ONE fire, and already excluding fires that already exist (idempotency) or that fall
	// inside quiet hours. Two bounded, indexed, set-based reads (candidate events, then already-fired
	// keys) -- no N+1. Returns the fires ready to have their audience resolved and be queued via
	// QueueReminderCadenceBatch.
	SweepReminderCadence(ctx context.Context, in ReminderCadenceQuery) ([]ReminderCadenceFire, error)

	// QueueReminderCadenceBatch writes ALL recipient rows for MULTIPLE already-collapsed cadence
	// fires (from SweepReminderCadence) in one set-based pass: it first claims each fire atomically
	// (INSERT ... ON CONFLICT DO NOTHING against the fire-marker table, so two concurrent sweeper
	// runs never double-fire), then bulk-inserts one notification_requests row per (won fire,
	// recipient) via unnest -- no per-fire loop calling an injected recipient-resolution dependency
	// (the caller resolves audience once, in a single batched call, before invoking this). Returns
	// the number of notification_requests rows actually inserted.
	QueueReminderCadenceBatch(ctx context.Context, in QueueReminderCadenceBatch) (int, error)

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

// RefreshVaccinationHistoryProjection asks the projector to rebuild the tenant's completed-history
// projection (calendar_history_projection_rows / calendar_history_date_markers) from the same
// vaccination_completions/obligation_instances/protocol_* canonical tables the request path used to
// join on read before this projection existed. See history_projection.go.
type RefreshVaccinationHistoryProjection struct {
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
	EventKey string
	// Context is optional metadata to merge into the notification_requests.context JSON field
	// and (for push_fcm) into the FCM data payload. May include type, obligation_id, park_id,
	// shed_id, target, href, screen for deep-linking on mobile.
	Context    map[string]string
	Recipients []NotificationRecipient
}

// VaccinationCompletionContext is the set of obligation/park/task/executor details resolved from a
// vaccination completion_id for notification routing. Returned by ResolveVaccinationCompletionContext
// to decouple the notification layer from importing internal/vaccination.
type VaccinationCompletionContext struct {
	CompletionID string // vaccination_completions.completion_id (stable for idempotency)
	ObligationID string // obligation_instances.obligation_id
	ParkID       string // obligation_instances.scope_id (when scope_type = 'center')
	ScopeType    string // obligation_instances.scope_type (should be 'center' for parks)
	SOPTaskID    string // obligation_instances.sop_task_id
	ExecutedBy   string // vaccination_completions.recorded_by (workforce_member_id, may be empty)
}

// ReminderCadenceQuery is the input to SweepReminderCadence.
type ReminderCadenceQuery struct {
	TenantID string
	// Now is the as-of instant the cadence is evaluated at (any timezone; converted to IST
	// internally). Defaults to the server's current time when zero.
	Now time.Time
	// Ladder is the reminder cadence ladder (offsets/slots/type/priority). Defaults to
	// domain.DefaultReminderLadder() when empty.
	Ladder []domain.ReminderLadderStep
	// QuietHoursStartIST/QuietHoursEndIST are "HH:MM" IST bounds of the no-push window. Default to
	// domain.DefaultQuietHoursStartIST/EndIST when empty.
	QuietHoursStartIST string
	QuietHoursEndIST   string
	// Limit bounds how many candidate obligations/events are scanned in one sweep tick.
	Limit int
}

// ReminderCadenceFire is one collapsed, ready-to-queue cadence fire: a batch across every open
// vaccination obligation in the SAME park whose ladder lands a fire on the SAME calendar day,
// notification type, and slot, so one recipient device gets one push, not N
// (vaccination-notification-rules.md §3 "Dedup/collapse").
type ReminderCadenceFire struct {
	ParkID string
	// RepresentativeCalendarEventID/RepresentativeObligationID identify ONE of the collapsed
	// obligations (deterministically the earliest due_at, then lowest event_id) -- used only to
	// satisfy notification_requests' FK to calendar_event_projections and to carry a concrete
	// obligation_id in the FCM deep-link context; the push itself represents the whole batch.
	RepresentativeCalendarEventID string
	RepresentativeObligationID    string
	FireDayIST                    string // "YYYY-MM-DD", the calendar day this fire is scheduled on (IST)
	NotificationType              string // advance_notice | reminder | due_today
	Priority                      string // normal | high
	Slot                          string // "HH:MM" IST
	ReminderNumber                int    // 1-based position within a multi-day ladder step; 0 for single-day steps
	ObligationCount               int    // how many obligations collapsed into this one fire
	// FireKey is the batch/collapse + idempotency identity: "<park_id>:<FireDayIST>:<type>:<slot>".
	// Claimed exactly once via the fire-marker table inside QueueReminderCadenceBatch.
	FireKey string
	// ClaimKeys is every ladder fire-day key (same "<park_id>:<date>:<type>:<slot>" shape as FireKey,
	// ALWAYS including FireKey itself) that was due as of Now for the obligations collapsed into this
	// fire, INCLUDING ones an earlier/skipped slot the sweeper never got to queue because a later
	// slot for the same obligation had already become due by the time it ran (e.g. the sweeper missed
	// several daily ticks). All of them are claimed in the fire-marker table alongside FireKey so a
	// later sweep does NOT "catch up" on the superseded slots and burst multiple stale reminders --
	// only the single latest (FireKey) ever produces a notification.
	ClaimKeys []string
}

// ReminderCadenceFireInput pairs an already-collapsed ReminderCadenceFire with its rendered
// title/body/context and its already-resolved audience (from workforce's recipient-resolution
// queries), ready for QueueReminderCadenceBatch.
type ReminderCadenceFireInput struct {
	Fire       ReminderCadenceFire
	Title      string
	Body       string
	Context    map[string]string
	Recipients []NotificationRecipient
}

// QueueReminderCadenceBatch is the input to Repository.QueueReminderCadenceBatch.
type QueueReminderCadenceBatch struct {
	TenantID string
	Channel  string // push_fcm
	TraceID  string
	Fires    []ReminderCadenceFireInput
}
