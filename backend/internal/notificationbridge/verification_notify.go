// Package notificationbridge is the notification PUSH LAYER for the vaccination verification loop
// (docs/decisions/vaccination-notification-rules.md §4c). It is a pure, read-only CONSUMER of
// vaccination.verify.rejected/accepted events published by the sopbridge/verification vertical --
// it never drives, reimplements, or depends on the verification state machine itself. This package's
// only job: turn an already-decided verify outcome into the right notification_requests rows, using
// three already-independent modules (calendar for completion context, workforce for recipient
// resolution, calendar for the durable notification write) the same way sopbridge composes
// sop+vaccination, without any of the three knowing this package exists.
package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Real event types published by sopbridge/verification (internal/vaccination/app/verification_handler.go):
// - vaccination.verify.rejected: proof rejected, recipient must rework
// - vaccination.verify.accepted: proof approved (no-op push; digest-only)
// Both carry the same VerificationEvent payload structure.
const (
	EventVaccinationVerifyRejected = "vaccination.verify.rejected"
	EventVaccinationVerifyAccepted = "vaccination.verify.accepted"
)

// VerificationEvent is the real event payload published by sopbridge for verify outcomes. Only
// completion_id is required; verified_by and reason are optional. The notifier resolves completion_id
// to its obligation context (obligation_id, park_id, sop_task_id, recorded_by) via a calendar repository
// lookup, decoupling from internal/vaccination imports.
type VerificationEvent struct {
	CompletionID string `json:"completion_id"`
	VerifiedBy   string `json:"verified_by,omitempty"`
	Reason       string `json:"reason,omitempty"`
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

	NotificationTypeVerificationPending = "verification_pending"
	NotificationTypeRework              = "rework"
	channelPushFCM                      = "push_fcm"
	priorityNormal                      = "normal"
	priorityHigh                        = "high"
)

// CompletionContextResolver is the slice of the calendar app service this bridge needs: resolve
// a vaccination completion_id to its obligation context (obligation_id, park_id, sop_task_id, executor).
type CompletionContextResolver interface {
	ResolveVaccinationCompletionContext(ctx context.Context, tenantID, completionID string) (calendarports.VaccinationCompletionContext, error)
}

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

// VerificationNotifier is a read-only consumer of vaccination.verify.rejected/accepted events that
// produces notification_requests rows. It resolves each completion_id to its obligation context via
// the calendar repository and routes notifications to verifiers (on submit) or executors + park head
// (on rework). Approved/completed outcomes are a deliberate no-op (digest-only, no push). Idempotent:
// every write is a set-based INSERT ... ON CONFLICT DO NOTHING keyed by (tenant, triggering event,
// recipient device), so at-least-once redelivery never duplicates a row.
type VerificationNotifier struct {
	contextResolver CompletionContextResolver
	recipients      RecipientResolver
	queue           NotificationQueue
	logger          *slog.Logger
}

// NewVerificationNotifier constructs the bridge over the calendar/workforce/calendar app-layer seams.
// logger is the process logger from platform/observability.New; when nil, gap warnings are skipped.
func NewVerificationNotifier(contextResolver CompletionContextResolver, recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *VerificationNotifier {
	return &VerificationNotifier{contextResolver: contextResolver, recipients: recipients, queue: queue, logger: logger}
}

var _ eventbus.Handler = (*VerificationNotifier)(nil)

// Register subscribes the notifier to the real vaccination verify events published by sopbridge.
func (n *VerificationNotifier) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVaccinationVerifyRejected, n)
	bus.Subscribe(EventVaccinationVerifyAccepted, n)
}

// HandleEvent routes the event by its type. Always idempotent and side-effect-free on
// malformed/unknown payloads (never panics, never partially notifies) -- a bad event is a no-op, not
// a dropped message the caller must know to retry differently.
func (n *VerificationNotifier) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if n == nil || n.contextResolver == nil || n.recipients == nil || n.queue == nil {
		return nil
	}
	if e.Type != EventVaccinationVerifyRejected && e.Type != EventVaccinationVerifyAccepted {
		return nil
	}

	var p VerificationEvent
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}

	tenantID := strings.TrimSpace(e.TenantID)
	completionID := strings.TrimSpace(p.CompletionID)
	if tenantID == "" || completionID == "" {
		return nil
	}

	// Resolve completion_id to its obligation context (obligation_id, park_id, sop_task_id, executor).
	ctx_, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	completionCtx, err := n.contextResolver.ResolveVaccinationCompletionContext(ctx_, tenantID, completionID)
	if err != nil {
		if err == calendarports.ErrNotFound {
			return nil // Completion does not exist or is not linked to an obligation; deliberate no-op.
		}
		return fmt.Errorf("notificationbridge: resolve completion context: %w", err)
	}

	switch e.Type {
	case EventVaccinationVerifyRejected:
		// Rejection → rework: notify the operator who executed + park head.
		return n.notifyRework(ctx, tenantID, completionCtx, p.Reason)
	case EventVaccinationVerifyAccepted:
		// Acceptance → no-op (digest/rollup only, no push notification).
		return nil
	default:
		return nil
	}
}

// notifyRework implements the rejected/rework leg: notify the operator who executed + park head.
// Leadership (Director/COO/CXO, scope_type='tenant') can never appear here by construction --
// ResolveMemberRecipients is scoped to one explicit member id (the executor) and
// ResolvePositionRecipients is hardcoded to scopeCenter, never scope_type='tenant'.
func (n *VerificationNotifier) notifyRework(ctx context.Context, tenantID string, completionCtx calendarports.VaccinationCompletionContext, reason string) error {
	// Idempotency key must include completion_id so multiple completions on the same obligation
	// (each with distinct completion_id and possibly different goats) each get their own notification rows.
	// If the same completion is replayed, the key stays the same (exact-replay dedup).
	completionID := completionCtx.CompletionID
	if completionID == "" {
		return nil // No completion ID; should not happen after ResolveVaccinationCompletionContext succeeds.
	}
	eventKey := "vaccination.verify.rejected:" + completionID

	var recipients []calendarports.NotificationRecipient

	// Notify the operator who executed (recorded_by), if known.
	if completionCtx.ExecutedBy != "" {
		operatorDevices, err := n.recipients.ResolveMemberRecipients(ctx, tenantID, completionCtx.ExecutedBy)
		if err != nil {
			return err
		}
		recipients = append(recipients, toQueueRecipients(operatorDevices, "operator")...)
	}

	// Notify the park head (whoever currently holds the park_head position).
	// Workforce positions are scoped scope_type='center' for a park, whereas the obligation
	// carries scope_type='park' (obligation location model) for the SAME park UUID. Resolve
	// recipients against the workforce scope constant, never the obligation's scope_type, or
	// the park-head lookup finds nothing.
	parkHeadDevices, err := n.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, completionCtx.ParkID, positionParkHead)
	if err != nil {
		return err
	}
	recipients = append(recipients, toQueueRecipients(parkHeadDevices, "park_head")...)

	// If we resolved zero recipients (no operator AND no park head), log a warning so the gap is
	// observable (a rejected proof reaching nobody is an operational issue, not success).
	if len(recipients) == 0 && n.logger != nil {
		n.logger.WarnContext(ctx, "vaccination_rework_notification_no_recipients",
			"tenant_id", tenantID,
			"obligation_id", completionCtx.ObligationID,
			"park_id", completionCtx.ParkID,
		)
	}

	body := "The verifier rejected a vaccination proof. This drive needs rework."
	if reason != "" {
		body += " Reason: " + reason
	}

	// Build context fields for FCM deep-linking: type, obligation_id, park_id (all required),
	// plus priority for android. The gateway will merge these into fcmData and set android priority.
	notificationContext := map[string]string{
		"type":           NotificationTypeRework,
		"obligation_id":  completionCtx.ObligationID,
		"park_id":        completionCtx.ParkID,
		"priority":       priorityHigh,
	}

	_, err = n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  calendarEventIDForTask(completionCtx.SOPTaskID),
		TargetType:       "obligation",
		TargetID:         completionCtx.ObligationID,
		NotificationType: NotificationTypeRework,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            "Vaccination proof rejected — rework needed",
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context:          notificationContext,
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

// ---- Integration contract for verification_pending notifications -----
//
// TODO: verification_pending notifications are NOT YET WIRED end-to-end.
// This notifier subscribes to vaccination.verify.rejected and vaccination.verify.accepted today.
// For verification_pending notifications (notifying verifiers when a proof is submitted for review),
// the submission/proof vertical MUST publish a vaccination.verification.awaiting_review event when
// a proof enters verification-pending status. Define this contract:
//   - Topic: "vaccination.verification.awaiting_review"
//   - Payload JSON:
//     {
//       "completion_id": "<uuid>",
//       "submitted_by": "<workforce_member_id (executor)>",
//       (optional) "submitted_at": "<RFC3339 timestamp>"
//     }
//
// Once published, this notifier will subscribe to that event and route notifications to the park's
// 'verify' duty holders via ResolveModuleDutyRecipients(moduleVaccination, dutyVerify).
// See vaccination-notification-rules.md §4c for the full routing table.
