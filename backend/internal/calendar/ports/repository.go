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
	// QueueRoleNotifications writes one notification_requests row per recipient (set-based INSERT,
	// no N+1) for a non-cadence, event-triggered notification such as vaccination verification_pending
	// / rework (vaccination-notification-rules.md §4c). Each recipient row is idempotent on its own
	// device-scoped key, so replaying the same event never duplicates a row. Returns the number of
	// rows actually inserted (0 on an exact replay of every recipient, or when Recipients is empty).
	QueueRoleNotifications(ctx context.Context, in QueueRoleNotifications) (int, error)

	// SweepReminderCadence is the legacy 2-value wrapper over SweepReminderCadencePage (it discards the
	// returned keyset cursor). It computes the vaccination reminder cadence ladder fires
	// (vaccination-notification-rules.md §3) due as of in.Now, already collapsed per (park, fire
	// day, notification type, slot) so multiple obligations due the same park the same fire day
	// batch into ONE fire, and already excluding fires that already exist (idempotency) or that fall
	// inside quiet hours. Two bounded, indexed, set-based reads (candidate events, then already-fired
	// keys) -- no N+1. Returns the fires ready to have their audience resolved and be queued via
	// QueueReminderCadenceBatch.
	SweepReminderCadence(ctx context.Context, in ReminderCadenceQuery) ([]ReminderCadenceFire, error)

	// SweepReminderCadencePage is SweepReminderCadence plus a stable keyset cursor for cross-run
	// forward progress (CAL-MAIN-02, migration 000204). The candidate scan is paged over
	// (due_at, event_id) resumed from in.CursorDueAt/in.CursorEventID (a zero/empty cursor starts from
	// the beginning). The returned ReminderCadenceSweepCursor is the last (due_at, event_id) scanned
	// this page plus an Exhausted flag (the page was short of Limit). Callers persist the cursor and
	// wrap it back to the start on exhaustion so later farms are eventually reached instead of being
	// starved behind a LIMIT-full first page (see kernelstages.ReminderCadenceStage). Already-fired
	// candidates are NOT excluded from the scan; the exact per-fire-key dedup emits no fire for them.
	SweepReminderCadencePage(ctx context.Context, in ReminderCadenceQuery) ([]ReminderCadenceFire, ReminderCadenceSweepCursor, error)

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

	// ReconcileEventReferencesPage (FINDING 4 fix: migration 000192, CAL-MAIN-03 fix: migration 000202)
	// surfaces a bounded page of notification_requests/calendar_snoozes rows whose calendar_event_id
	// no longer maps to any canonical obligation/batch/drive/task/completion. Uses stable keyset
	// pagination: (cursorSourceTable, cursorRecordID) is the last processed row; pass ("", "") to
	// start from the beginning. Ordered by (source_table, record_id) for deterministic keyset
	// comparison. Used by kernelstages.CalendarReconcilerStage to process large orphan sets without
	// LIMIT/OFFSET rescanning or loading all into memory.
	ReconcileEventReferencesPage(ctx context.Context, tenantID, cursorSourceTable, cursorRecordID string, limit int) ([]OrphanedCalendarEventReference, error)

	// LoadReconcilerCursor / SaveReconcilerCursor persist the reconciler stage's keyset cursor across
	// daily runs (CAL-MAIN-03, migration 000203). LoadReconcilerCursor returns ("", "", nil) when no
	// cursor is stored yet (start from the beginning). SaveReconcilerCursor upserts the last processed
	// (source_table, record_id); passing ("", "") resets the tenant to the beginning on the next run.
	LoadReconcilerCursor(ctx context.Context, tenantID string) (cursorSourceTable, cursorRecordID string, err error)
	SaveReconcilerCursor(ctx context.Context, tenantID, cursorSourceTable, cursorRecordID string, now time.Time) error

	// LoadReminderCadenceCursor / SaveReminderCadenceCursor persist the reminder-cadence stage's keyset
	// cursor across ticks (CAL-MAIN-02, migration 000204). LoadReminderCadenceCursor returns
	// (zero time, "", nil) when no cursor is stored yet (start from the beginning). SaveReminderCadenceCursor
	// upserts the last processed (due_at, event_id); passing (zero time, "") resets the tenant to the
	// beginning on the next tick.
	LoadReminderCadenceCursor(ctx context.Context, tenantID string) (cursorDueAt time.Time, cursorEventID string, err error)
	SaveReminderCadenceCursor(ctx context.Context, tenantID string, cursorDueAt time.Time, cursorEventID string, now time.Time) error

	// ReconcileEventReferences (CR-004, calendar-canonical-5k50k review) surfaces every
	// notification_requests/calendar_snoozes row whose calendar_event_id no longer maps to any
	// canonical obligation/batch/drive/task/completion (the referential integrity check migration
	// 000189 introduced when those columns' FKs were dropped -- see
	// goatos_reconcile_calendar_event_references / goatos_calendar_event_reference_valid in
	// migration 000191). This is a callable integrity check, not yet on any recurring schedule --
	// see adapters/postgres/reconciler.go's doc comment for the housekeeping-stage wiring seam.
	// NOTE: deprecated in favor of ReconcileEventReferencesPage for bounded pagination.
	ReconcileEventReferences(ctx context.Context, tenantID string) ([]OrphanedCalendarEventReference, error)

	// ResolveMissedObligationContext resolves a missed obligation to the routing facts a
	// notification needs: which module's work it was, which park/shed it belongs to, the business
	// date it was due on, and which operator was assigned the drive that covered it. Returns
	// ErrNotFound when the obligation does not exist in the tenant. One bounded, indexed read.
	ResolveMissedObligationContext(ctx context.Context, tenantID, obligationID string) (MissedObligationContext, error)
}

// MissedObligationContext is everything the missed-work notification needs to decide WHO to tell and
// WHERE to send them. Module is the protocol category ("vaccination"); routing is per module and
// there is deliberately NO fallback profile, so an unclaimed module notifies nobody loudly rather
// than the wrong people quietly.
type MissedObligationContext struct {
	ObligationID string
	Module       string    // protocol_definitions.category, e.g. "vaccination"
	DueAt        time.Time // the obligation's due instant; the business date is derived in Asia/Kolkata
	ParkID       string
	ShedID       string
	ShedLabel    string // human shed name for farm-language copy; empty when the work is park-wide
	// OperatorID is the workforce member assigned the drive that covered this obligation on its due
	// business date. Empty when no drive assignment covered it (unplanned work) -- the notification
	// then still goes UP, because someone must know.
	OperatorID string
}

// OrphanedCalendarEventReference is one row ReconcileEventReferences flags: a notification_requests
// or calendar_snoozes row whose calendar_event_id does not resolve to any canonical record.
type OrphanedCalendarEventReference struct {
	SourceTable     string // "notification_requests" | "calendar_snoozes"
	RecordID        string
	CalendarEventID string
	Issue           string
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
// one queued notification_requests row per recipient device. calendar_event_id is a plain,
// unconstrained text column (the calendar_event_projections FK it used to enforce was dropped in
// migration 000189 along with the table); CalendarEventID is still expected to follow the same
// naming convention every canonical event uses (see calendarEventIDForTask), just without a database
// constraint enforcing it. Idempotent per (tenant, event, notification type, completion, device) —
// see idempotencyKeyForRecipient in the postgres adapter.
type QueueRoleNotifications struct {
	TenantID         string
	CalendarEventID  string
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

// ReminderCadenceQuery is the input to SweepReminderCadence / SweepReminderCadencePage.
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
	// CursorDueAt/CursorEventID are the keyset resume point for the candidate scan (CAL-MAIN-02): the
	// scan returns candidates ordered by (due_at, event_id) strictly greater than this cursor. A zero
	// CursorDueAt with an empty CursorEventID starts from the beginning of the candidate set.
	CursorDueAt   time.Time
	CursorEventID string
}

// ReminderCadenceSweepCursor is the keyset progress of one SweepReminderCadencePage call (CAL-MAIN-02).
// The caller (kernelstages.ReminderCadenceStage) persists it and resumes from it next tick, wrapping to
// the zero cursor once Exhausted so no farm is permanently starved behind a LIMIT-full first page. It is
// derived from the candidates SCANNED this page (not the collapsed fires), so it advances even when
// every candidate on the page was already fired.
type ReminderCadenceSweepCursor struct {
	// DueAt/EventID are the last (due_at, event_id) scanned this page. Zero/empty when no candidate was
	// scanned (quiet hours, or the cursor was already past the end of the set).
	DueAt   time.Time
	EventID string
	// Exhausted is true when the candidate scan returned fewer than Limit rows -- i.e. this page was the
	// tail of the set. The caller wraps its persisted cursor back to the start when Exhausted so newly
	// due candidates are re-scanned on the next full cycle.
	Exhausted bool
}

// ReminderCadenceFire is one collapsed, ready-to-queue cadence fire: a batch across every open
// vaccination obligation in the SAME park whose ladder lands a fire on the SAME calendar day,
// notification type, and slot, so one recipient device gets one push, not N
// (vaccination-notification-rules.md §3 "Dedup/collapse").
type ReminderCadenceFire struct {
	ParkID string
	// RepresentativeCalendarEventID/RepresentativeObligationID identify ONE of the collapsed
	// obligations (deterministically the earliest due_at, then lowest event_id) -- used to populate
	// notification_requests.calendar_event_id (a plain text column now, no FK to satisfy since
	// migration 000189) and to carry a concrete id in the FCM deep-link context; the push
	// itself represents the whole batch. SourceTargetType specifies what type of entity
	// RepresentativeObligationID actually refers to (obligation | batch | catchup | park_drive).
	RepresentativeCalendarEventID string
	RepresentativeObligationID    string
	SourceTargetType              string // "obligation" | "batch" | "catchup" | "park_drive" — derived from source_events
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
	// ShedLabels are the human-readable shed/partition names for the obligations collapsed into this fire.
	// Unique sheds within the fire, capped at a small count so the notification stays readable. Empty when
	// the fire has no shed scope (e.g., park-wide obligations). Fetched in one batched read per sweep page.
	ShedLabels []string
	// VaccineLabels are the human-readable vaccine names (e.g., "ET+TT", "PPR · Booster") for the
	// obligations collapsed into this fire. Unique vaccines, capped at a small count. Empty when no
	// vaccine is specified. Fetched in one batched read per sweep page.
	VaccineLabels []string
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
