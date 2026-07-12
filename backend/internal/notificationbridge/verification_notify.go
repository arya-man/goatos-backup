// Package notificationbridge is the notification PUSH LAYER for the vaccination verification loop
// (docs/decisions/vaccination-notification-rules.md §4c). It is a pure, read-only CONSUMER of an
// obligation/verification status-changed event -- it never drives, reimplements, or depends on the
// verification state machine (that vertical -- the verifier app, the accept/reject transitions, the
// admin-web review UI -- is owned elsewhere). This package's only job: turn an already-decided status
// change into the right notification_requests rows, using two other already-independent modules
// (workforce for recipient resolution, calendar for the durable notification write) the same way
// sopbridge composes sop+vaccination, without any of the three knowing this package exists.
package notificationbridge

import (
	"context"
	"encoding/json"
	"strings"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// EventObligationVerificationStatus is the event type this package subscribes to: one obligation's
// verification/rework status changed. Published by whatever owns the verification state machine
// (directly, or via a thin adapter translating its own event/outbox contract onto this bus) --
// notificationbridge only consumes it, never publishes it itself, and carries no dependency on the
// vaccination or sop packages. The payload is intentionally self-sufficient (ObligationVerificationStatusEvent
// below) so this consumer never needs to read back into another module's tables to do its job.
const EventObligationVerificationStatus = "obligation.verification_status_changed"

// Status values this bridge acts on (vaccination-notification-rules.md §4c). Any other status
// (e.g. "completed" / "approved") is a deliberate no-op: approval is rollup/digest only, never a push.
const (
	StatusVerificationPending = "verification_pending"
	StatusRejected            = "rejected"
	StatusReworkDue           = "rework_due"
)

// ObligationVerificationStatusEvent is the payload contract this bridge consumes. Every field the
// recipient-resolution + notification write needs travels in the event itself -- ParkID and
// SOPTaskID are look-up KEYS (workforce position scope, calendar event linkage), not business
// decisions, so carrying them here does not leak verification-vertical logic into this package.
type ObligationVerificationStatusEvent struct {
	ObligationID string `json:"obligation_id"`
	CompletionID string `json:"completion_id,omitempty"`
	// Status is one of StatusVerificationPending / StatusRejected / StatusReworkDue / anything else
	// (e.g. "completed", "approved") -- the latter is a deliberate no-op for this bridge.
	Status string `json:"status"`
	// ParkID is the workforce_positions scope_id (scope_type='center') to resolve verifiers/park-head
	// against -- required for StatusVerificationPending and StatusRejected/StatusReworkDue.
	ParkID string `json:"park_id"`
	// SOPTaskID backs the calendar_event_id ("calendar:" + SOPTaskID) the notification links to. The
	// calendar_event_projections row for it MUST already exist (notification_requests.calendar_event_id
	// has a hard FK) -- required for every status this bridge acts on.
	SOPTaskID string `json:"sop_task_id"`
	// ExecutedBy is the workforce_member_id who performed the execution -- required only for the
	// rework leg (the operator who must redo the drive). Empty is a legitimate "unknown executor";
	// the rework notification then falls back to the park head alone.
	ExecutedBy string `json:"executed_by,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// Fixed vocabulary this bridge resolves recipients against (vaccination-notification-rules.md
// §4a/§4c): the only built module today is pc.vaccination, the only duty this slice notifies is
// 'verify', workforce position scope for a park is 'center', and the park's field-facing seat is
// 'park_head'.
const (
	moduleVaccination = "pc.vaccination"
	dutyVerify        = "verify"
	scopeCenter       = "center"
	positionParkHead  = "park_head"

	notificationTypeVerificationPending = "verification_pending"
	notificationTypeRework              = "rework"
	channelPushFCM                      = "push_fcm"
	priorityNormal                      = "normal"
	priorityHigh                        = "high"
)

// RecipientResolver is the slice of the workforce roster app service this bridge needs: resolve
// active, reachable devices for a duty holder, an explicit member, or a position holder.
type RecipientResolver interface {
	ResolveModuleDutyRecipients(ctx context.Context, tenantID, scopeType, scopeID, moduleCode, dutyType string) ([]workforcedomain.NotificationRecipient, error)
	ResolveMemberRecipients(ctx context.Context, tenantID, workforceMemberID string) ([]workforcedomain.NotificationRecipient, error)
	ResolvePositionRecipients(ctx context.Context, tenantID, scopeType, scopeID, positionCode string) ([]workforcedomain.NotificationRecipient, error)
}

// NotificationQueue is the slice of the calendar app service this bridge needs: the generic,
// event-triggered notification write.
type NotificationQueue interface {
	QueueRoleNotifications(ctx context.Context, in calendarports.QueueRoleNotifications) (int, error)
}

// VerificationNotifier is a read-only consumer of EventObligationVerificationStatus that produces
// notification_requests rows. It never subscribes to, publishes, or depends on any vaccination
// verify/reject event -- the verification state machine is entirely out of this package's blast
// radius. Idempotent: every write is a set-based INSERT ... ON CONFLICT DO NOTHING keyed by (tenant,
// triggering event, recipient device), so at-least-once redelivery never duplicates a row.
type VerificationNotifier struct {
	recipients RecipientResolver
	queue      NotificationQueue
}

// NewVerificationNotifier constructs the bridge over the workforce/calendar app-layer seams.
func NewVerificationNotifier(recipients RecipientResolver, queue NotificationQueue) *VerificationNotifier {
	return &VerificationNotifier{recipients: recipients, queue: queue}
}

var _ eventbus.Handler = (*VerificationNotifier)(nil)

// Register subscribes the notifier to the single event type it consumes.
func (n *VerificationNotifier) Register(bus eventbus.Bus) {
	bus.Subscribe(EventObligationVerificationStatus, n)
}

// HandleEvent routes the event by its Status field. Always idempotent and side-effect-free on
// malformed/unknown payloads (never panics, never partially notifies) -- a bad event is a no-op, not
// a dropped message the caller must know to retry differently.
func (n *VerificationNotifier) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if n == nil || n.recipients == nil || n.queue == nil || e.Type != EventObligationVerificationStatus {
		return nil
	}
	var p ObligationVerificationStatusEvent
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	tenantID := strings.TrimSpace(e.TenantID)
	obligationID := strings.TrimSpace(p.ObligationID)
	parkID := strings.TrimSpace(p.ParkID)
	sopTaskID := strings.TrimSpace(p.SOPTaskID)
	// Refuse rather than guess: a missing tenant/obligation/park/SOP-task means this consumer cannot
	// safely resolve recipients or link to a real calendar event. notification_requests.
	// calendar_event_id has a hard FK to calendar_event_projections -- a fabricated id would either
	// violate that FK or silently link the notification to the wrong drive.
	if tenantID == "" || obligationID == "" || parkID == "" || sopTaskID == "" {
		return nil
	}
	switch strings.TrimSpace(p.Status) {
	case StatusVerificationPending:
		return n.notifyVerifiers(ctx, tenantID, obligationID, parkID, sopTaskID, p.CompletionID)
	case StatusRejected, StatusReworkDue:
		return n.notifyRework(ctx, tenantID, obligationID, parkID, sopTaskID, p.CompletionID, strings.TrimSpace(p.ExecutedBy), p.Reason)
	default:
		// completed / approved / anything else: deliberate no-op (digest/rollup only, no push).
		return nil
	}
}

// eventKeyFor derives the idempotency scope for one (obligation, status) transition, further scoped
// by completion id when present (a rework after a later resubmission gets a fresh completion id, so
// it is correctly treated as a NEW event, not a replay of the earlier rejection).
func eventKeyFor(status, obligationID, completionID string) string {
	key := "obligation.verification_status_changed:" + status + ":" + obligationID
	if completionID != "" {
		key += ":" + completionID
	}
	return key
}

// notifyVerifiers implements the verification_pending row of §4c: notify the verifier(s) holding
// duty_type='verify' for pc.vaccination at the obligation's park. No verifier duty seeded for that
// park -> zero recipients -> QueueRoleNotifications is a legitimate no-op, never an error.
func (n *VerificationNotifier) notifyVerifiers(ctx context.Context, tenantID, obligationID, parkID, sopTaskID, completionID string) error {
	verifiers, err := n.recipients.ResolveModuleDutyRecipients(ctx, tenantID, scopeCenter, parkID, moduleVaccination, dutyVerify)
	if err != nil {
		return err
	}
	eventKey := eventKeyFor(StatusVerificationPending, obligationID, completionID)
	_, err = n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  calendarEventIDForTask(sopTaskID),
		TargetType:       "obligation",
		TargetID:         obligationID,
		NotificationType: notificationTypeVerificationPending,
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "Proof waiting for your review",
		Body:             "A vaccination proof is waiting for verification.",
		TraceID:          eventKey,
		EventKey:         eventKey,
		Recipients:       toQueueRecipients(verifiers, "verifier"),
	})
	return err
}

// notifyRework implements the rejected/rework_due row of §4c: notify the operator who performed the
// execution plus that park's park head. Leadership (Director/COO/CXO, scope_type='tenant') can never
// appear here by construction -- ResolveMemberRecipients is scoped to one explicit member id (the
// executor) and ResolvePositionRecipients is hardcoded to scopeCenter, never scope_type='tenant'.
func (n *VerificationNotifier) notifyRework(ctx context.Context, tenantID, obligationID, parkID, sopTaskID, completionID, executedBy, reason string) error {
	var recipients []calendarports.NotificationRecipient
	if executedBy != "" {
		operatorDevices, err := n.recipients.ResolveMemberRecipients(ctx, tenantID, executedBy)
		if err != nil {
			return err
		}
		recipients = append(recipients, toQueueRecipients(operatorDevices, "operator")...)
	}
	parkHeadDevices, err := n.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, parkID, positionParkHead)
	if err != nil {
		return err
	}
	recipients = append(recipients, toQueueRecipients(parkHeadDevices, "park_head")...)
	body := "The verifier rejected a vaccination proof. This drive needs rework."
	if reason != "" {
		body += " Reason: " + reason
	}
	eventKey := eventKeyFor(StatusRejected, obligationID, completionID)
	_, err = n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  calendarEventIDForTask(sopTaskID),
		TargetType:       "obligation",
		TargetID:         obligationID,
		NotificationType: notificationTypeRework,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            "Vaccination proof rejected — rework needed",
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Recipients:       recipients,
	})
	return err
}

// calendarEventIDForTask mirrors the exact event_id the calendar vaccination projector derives for a
// SOP task (internal/calendar/adapters/postgres/repository.go's sop_events CTE: 'calendar:' ||
// sop_task_id) so notification_requests.calendar_event_id satisfies its FK to
// calendar_event_projections. The projection row must already exist (production sequencing: the
// obligation sweeper / cmd/calendar-vaccination-projector refreshes it right after task creation).
func calendarEventIDForTask(sopTaskID string) string {
	return "calendar:" + sopTaskID
}

// toQueueRecipients adapts the workforce module's resolution result into the calendar module's queue
// input, attaching the caller-supplied role label (observability only -- see
// calendarports.NotificationRecipient.RoleLabel).
func toQueueRecipients(recipients []workforcedomain.NotificationRecipient, role string) []calendarports.NotificationRecipient {
	out := make([]calendarports.NotificationRecipient, 0, len(recipients))
	for _, r := range recipients {
		out = append(out, calendarports.NotificationRecipient{
			MemberID:  r.WorkforceMemberID,
			DeviceID:  r.DeviceID,
			FCMToken:  r.FCMToken,
			RoleLabel: role,
		})
	}
	return out
}
