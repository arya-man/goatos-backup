// Package notificationbridge provides the durable outbox consumer for verification events
// (backend/internal/verification/adapters/postgres/repository.go publishes
// verification.item.pending, verification.verdict.rework, verification.verdict.approved).
// This consumer is the NOTIFICATION PUSH LAYER for the generic Verification vertical
// (context/architecture/verification-module-design.md): it reads already-decided verification
// status changes from the durable outbox and produces notification_requests rows.
// It is a pure, read-only CONSUMER that never drives, reimplements, or depends on the
// verification state machine itself — its only job: resolve recipients and queue notifications.
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
)

// Real event types published by the verification module to outbox_messages
// (verification/adapters/postgres/repository.go: EventItemPending/EventVerdictRework/EventVerdictApproved):
// - verification.item.pending: proof submitted, an assigned verifier must review it.
// - verification.verdict.rework: proof rejected, the operator must rework + resubmit.
// - verification.verdict.approved: proof approved (no-op push; metrics/digest only).
const (
	EventVerificationItemPending     = "verification.item.pending"
	EventVerificationVerdictRework   = "verification.verdict.rework"
	EventVerificationVerdictApproved = "verification.verdict.approved"
	EventVerificationItemClosed      = "verification.item.closed"
)

// notification_type values this consumer writes. Both are in the migration 000179
// notification_requests_type_check allowed set ('verification_pending', 'rework'), the
// SAME two the legacy VerificationNotifier uses — see NotificationTypeVerificationPending /
// NotificationTypeRework in verification_notify.go and TestNotificationTypeEnumGuard.

// legacyVaccinationSourceModule / legacyVaccinationSourceRefType identify a generic
// verification_item that was materialized FROM a legacy vaccination SOP proof submission
// (sopbridge/vaccination_submission.go emitVerificationItem sets Source.Module="vaccination",
// Source.RefType="sop_submission"). Such an item's underlying SOP task, when reworked, ALSO
// fans out one vaccination.verify.rejected per completion (sopbridge/verify_fanout.go
// OnTaskReworked) that the legacy VerificationNotifier already routes to operator + park head.
// See legacyHandledVaccination below for the exact suppression rule.
const (
	legacyVaccinationSourceModule  = "vaccination"
	legacyVaccinationSourceRefType = "sop_submission"
	positionPCDirector             = "pc_director"
	positionCEOInternal            = "ceo_internal"
)

// verificationSource is the producer's source back-reference (verificationVerdictPayload /
// verificationItemPendingPayload in the verification repository). module + ref_type are the
// two fields that identify a legacy-overlapping vaccination item.
type verificationSource struct {
	Module       string `json:"module"`
	TaskID       string `json:"task_id"`
	SubmissionID string `json:"submission_id"`
	RefType      string `json:"ref_type"`
	RefID        string `json:"ref_id"`
}

// VerificationEventPayload is the outbox payload emitted by the verification repository for all
// three events (verificationItemPendingPayload / verificationVerdictPayload). Field set is fixed
// by that producer contract: tenant/item identity + classification + who-to-route-to
// (operator/shed/park) + the decision/reason + the source back-reference used for legacy dedup.
type VerificationEventPayload struct {
	TenantID   string             `json:"tenant_id"`
	ItemID     string             `json:"item_id"`
	Vertical   string             `json:"vertical"`
	Module     string             `json:"module"`
	Category   string             `json:"category"`
	OperatorID string             `json:"operator_id"`
	ShedID     string             `json:"shed_id"`
	ParkID     string             `json:"park_id"`
	Decision   string             `json:"decision"` // "approved" | "rejected" (verdict events)
	Status     string             `json:"status"`   // status alias kept alongside decision
	Reason     string             `json:"reason"`   // optional rework reason
	VerifiedBy string             `json:"verified_by"`
	ClosedBy   string             `json:"closed_by"`
	CapturedAt string             `json:"captured_at"`
	Source     verificationSource `json:"source"`
}

// legacyHandledVaccination reports whether this item is ALSO covered by the legacy
// vaccination.verify.rejected notification path, so the generic rework push must be suppressed
// to avoid double-notifying the operator + park head. True exactly when the item was produced
// from a legacy vaccination SOP submission (Source.Module="vaccination" AND
// Source.RefType="sop_submission"). Non-vaccination generic verticals (feed/diagnosis/death/
// breeding, future modules) have NO legacy notifier and are never suppressed.
func (p VerificationEventPayload) legacyHandledVaccination() bool {
	return strings.EqualFold(strings.TrimSpace(p.Source.Module), legacyVaccinationSourceModule) &&
		strings.EqualFold(strings.TrimSpace(p.Source.RefType), legacyVaccinationSourceRefType)
}

// VerificationEventConsumer is a durable, idempotent consumer of verification.item.pending,
// verification.verdict.rework, and verification.verdict.approved. It resolves each event to its
// recipient set and produces notification_requests rows via the shared calendar notification queue.
//
// Idempotency: every write goes through QueueRoleNotifications, whose per-recipient INSERT is
// ON CONFLICT (tenant_id, idempotency_key) DO NOTHING keyed by (tenant, EventKey, device), so an
// at-least-once redelivery of the SAME event never duplicates a row.
//
// DLQ: a corrupt (unparseable) payload is a poison message — it can never succeed on retry — so it
// is returned as eventbus.PermanentError, which the durable domain-event consumer nacks straight to
// the DLQ (domainconsumer/app/service.go: eventbus.IsPermanentError → domain_event_permanent_failure_nacked_for_dlq).
// A transient failure (recipient resolution / queue write) is returned as a plain error so the same
// consumer RETRIES it; idempotency makes that replay side-effect-free.
//
// Advisory only: the consumer writes notification_requests rows and NOTHING else. It never completes
// obligations, completes/rejects verification items, or mutates any owning-vertical state — the
// verification vertical retains sole completion authority.
type VerificationEventConsumer struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
}

// NewVerificationEventConsumer constructs the consumer over the workforce recipient resolver and
// the calendar notification queue (the same two seams the legacy VerificationNotifier uses).
func NewVerificationEventConsumer(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *VerificationEventConsumer {
	return &VerificationEventConsumer{recipients: recipients, queue: queue, logger: logger}
}

var _ eventbus.Handler = (*VerificationEventConsumer)(nil)

// Register subscribes the consumer to the three verification events on a bus. In production the
// outbox relay (eventbus publisher) and the domain-event consumer both dispatch these to the bus.
func (c *VerificationEventConsumer) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVerificationItemPending, c)
	bus.Subscribe(EventVerificationVerdictRework, c)
	bus.Subscribe(EventVerificationVerdictApproved, c)
	bus.Subscribe(EventVerificationItemClosed, c)
}

// HandleEvent routes by event type. A corrupt payload → PermanentError (DLQ); a well-formed but
// non-actionable event (approved, or a missing routing field) → nil no-op; a transient dependency
// failure → plain error (retryable).
func (c *VerificationEventConsumer) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if c == nil || c.recipients == nil || c.queue == nil {
		return nil
	}

	switch e.Type {
	case EventVerificationItemPending, EventVerificationVerdictRework,
		EventVerificationVerdictApproved, EventVerificationItemClosed:
		p, err := decodePayload(e.Payload)
		if err != nil {
			// Poison message: unparseable JSON can never succeed on retry → DLQ.
			return eventbus.PermanentError(fmt.Errorf("verification_notify_consumer: decode %s payload: %w", e.Type, err))
		}
		if e.Type == EventVerificationItemPending {
			return c.handleItemPending(ctx, p)
		}
		if e.Type == EventVerificationVerdictRework {
			return c.handleVerdictRework(ctx, p)
		}
		if e.Type == EventVerificationVerdictApproved {
			return c.handleVerdictApproved(ctx, p)
		}
		return c.handleItemClosed(ctx, p)
	default:
		return nil
	}
}

func (c *VerificationEventConsumer) handleVerdictApproved(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	itemID := strings.TrimSpace(p.ItemID)
	parkID := strings.TrimSpace(p.ParkID)
	if tenantID == "" || itemID == "" || parkID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	parkHeadDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, parkID, positionParkHead)
	if err != nil {
		return err
	}
	eventKey := EventVerificationVerdictApproved + ":" + itemID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(itemID),
		TargetType:       "verification_item",
		TargetID:         itemID,
		NotificationType: "verification_approved",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "Vaccination proof verified",
		Body:             "The proof is ready for operational closure.",
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":         "verification_approved",
			"screen":       "leadership_close",
			"item_id":      itemID,
			"park_id":      parkID,
			"shed_id":      p.ShedID,
			"category":     p.Category,
			"group_key":    "verification:" + parkID + ":" + p.Category,
			"collapse_key": "verification:" + parkID + ":" + p.Category,
			"priority":     priorityNormal,
		},
		Recipients: toQueueRecipients(parkHeadDevices, "park_head"),
	})
	return err
}

func (c *VerificationEventConsumer) handleItemClosed(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	itemID := strings.TrimSpace(p.ItemID)
	operatorID := strings.TrimSpace(p.OperatorID)
	parkID := strings.TrimSpace(p.ParkID)
	if tenantID == "" || itemID == "" || operatorID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	operatorDevices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
	if err != nil {
		return err
	}
	eventKey := EventVerificationItemClosed + ":" + itemID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(itemID),
		TargetType:       "verification_item",
		TargetID:         itemID,
		NotificationType: "verification_closed",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "Vaccination record closed",
		Body:             "The verified vaccination record is now complete.",
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":         "verification_closed",
			"screen":       "record",
			"item_id":      itemID,
			"park_id":      parkID,
			"shed_id":      p.ShedID,
			"category":     p.Category,
			"group_key":    "verification:" + parkID + ":" + p.Category,
			"collapse_key": "verification:" + parkID + ":" + p.Category,
			"priority":     priorityNormal,
		},
		Recipients: toQueueRecipients(operatorDevices, "operator"),
	})
	return err
}

// decodePayload parses the outbox payload. Returns an error only for genuinely corrupt JSON (the
// DLQ trigger); an empty payload decodes to a zero-value struct that the handlers treat as a no-op.
func decodePayload(raw []byte) (VerificationEventPayload, error) {
	var p VerificationEventPayload
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return VerificationEventPayload{}, err
	}
	return p, nil
}

// handleItemPending notifies everyone who must react when an operator submits a shed for review:
// the park's vaccination verifier duty holder, that park's head, tenant PC directors, and tenant
// CEOs. Multiple goat-level verification items can be created for one shed submission; when the
// source carries submission_id, the idempotency key is submission-scoped so those items collapse to
// one queued push per recipient device.
func (c *VerificationEventConsumer) handleItemPending(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	itemID := strings.TrimSpace(p.ItemID)
	parkID := strings.TrimSpace(p.ParkID)
	if tenantID == "" || itemID == "" || parkID == "" {
		return nil // Well-formed but non-routable → no-op.
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	verifierDevices, err := c.recipients.ResolveModuleDutyRecipients(ctx, tenantID, scopeCenter, parkID, moduleVaccination, dutyVerify)
	if err != nil {
		return fmt.Errorf("verification_notify_consumer: resolve verifier recipients: %w", err) // retryable
	}
	parkHeadDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, parkID, positionParkHead)
	if err != nil {
		return fmt.Errorf("verification_notify_consumer: resolve park head recipients: %w", err)
	}
	directorDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionPCDirector)
	if err != nil {
		return fmt.Errorf("verification_notify_consumer: resolve pc director recipients: %w", err)
	}
	ceoDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionCEOInternal)
	if err != nil {
		return fmt.Errorf("verification_notify_consumer: resolve ceo recipients: %w", err)
	}
	recipients := dedupeQueueRecipients(
		append(append(append(
			toQueueRecipients(verifierDevices, "verifier"),
			toQueueRecipients(parkHeadDevices, "park_head")...),
			toQueueRecipients(directorDevices, "pc_director")...),
			toQueueRecipients(ceoDevices, "ceo")...),
	)
	if len(recipients) == 0 && c.logger != nil {
		c.logger.WarnContext(ctx, "verification_pending_notification_no_recipients",
			"tenant_id", tenantID, "item_id", itemID, "park_id", parkID)
	}

	eventKeySubject := itemID
	if sourceSubmissionID := strings.TrimSpace(p.Source.SubmissionID); sourceSubmissionID != "" {
		eventKeySubject = "submission:" + sourceSubmissionID
	}
	eventKey := EventVerificationItemPending + ":" + eventKeySubject
	body := "A proof has been submitted for your review."
	if p.Category != "" {
		body = "A " + p.Category + " proof has been submitted for your review."
	}
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(itemID),
		TargetType:       "verification_item",
		TargetID:         itemID,
		NotificationType: NotificationTypeVerificationPending,
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "New verification request",
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":         NotificationTypeVerificationPending,
			"screen":       "verification",
			"item_id":      itemID,
			"park_id":      parkID,
			"shed_id":      p.ShedID,
			"category":     p.Category,
			"group_key":    "verification:" + parkID + ":" + p.Category,
			"collapse_key": "verification:" + parkID + ":" + p.Category,
			"priority":     priorityNormal,
		},
		Recipients: recipients,
	})
	return err
}

// handleVerdictRework notifies the operator who submitted + the park head, mirroring the legacy
// VerificationNotifier's rejected path. LEGACY DEDUP: if this item is a legacy-handled vaccination
// item (Source.Module="vaccination" AND Source.RefType="sop_submission"), the legacy
// vaccination.verify.rejected fan-out already notifies the SAME operator + park head, so this
// generic push is SUPPRESSED to avoid a duplicate. The suppression is derived purely from the
// item's source ref carried in the event payload — no cross-module lookup needed.
func (c *VerificationEventConsumer) handleVerdictRework(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	itemID := strings.TrimSpace(p.ItemID)
	operatorID := strings.TrimSpace(p.OperatorID)
	parkID := strings.TrimSpace(p.ParkID)
	if tenantID == "" || itemID == "" || parkID == "" {
		return nil // Well-formed but non-routable → no-op.
	}

	// LEGACY DEDUP — suppress the duplicate rework push for a legacy vaccination SOP item.
	if p.legacyHandledVaccination() {
		if c.logger != nil {
			c.logger.InfoContext(ctx, "verification_rework_notification_suppressed_legacy_vaccination",
				"tenant_id", tenantID, "item_id", itemID, "park_id", parkID,
				"source_module", p.Source.Module, "source_ref_type", p.Source.RefType,
				"source_task_id", p.Source.TaskID)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var recipients []calendarports.NotificationRecipient
	if operatorID != "" {
		operatorDevices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
		if err != nil {
			return err // retryable
		}
		recipients = append(recipients, toQueueRecipients(operatorDevices, "operator")...)
	}
	// Park-head positions are scoped scope_type='center' for a park (see the legacy notifier's
	// identical note): resolve against scopeCenter, never the item's park scope directly.
	parkHeadDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, parkID, positionParkHead)
	if err != nil {
		return err // retryable
	}
	recipients = append(recipients, toQueueRecipients(parkHeadDevices, "park_head")...)

	if len(recipients) == 0 && c.logger != nil {
		c.logger.WarnContext(ctx, "verification_rework_notification_no_recipients",
			"tenant_id", tenantID, "item_id", itemID, "park_id", parkID)
	}

	eventKey := EventVerificationVerdictRework + ":" + itemID
	body := "The verifier rejected a proof. This needs to be resubmitted."
	if p.Reason != "" {
		body = "The verifier rejected a proof. Reason: " + p.Reason + " Please resubmit."
	}
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(itemID),
		TargetType:       "verification_item",
		TargetID:         itemID,
		NotificationType: NotificationTypeRework,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            "Verification proof rejected — rework needed",
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":         NotificationTypeRework,
			"screen":       "record",
			"item_id":      itemID,
			"park_id":      parkID,
			"shed_id":      p.ShedID,
			"category":     p.Category,
			"group_key":    "verification:" + parkID + ":" + p.Category,
			"collapse_key": "verification:" + parkID + ":" + p.Category,
			"priority":     priorityHigh,
		},
		Recipients: recipients,
	})
	return err
}

// verificationCalendarEventID is the calendar_event_id a verification notification_requests row
// links to. There is no FK to enforce against (calendar_event_projections is retired; the FK was
// already dropped in migration 000186), so this is a plain naming convention: distinct from the
// vaccination "calendar:<sop_task_id>" namespace so a generic verification item and its underlying
// SOP task never collide on the same key.
func verificationCalendarEventID(itemID string) string {
	return "verification:" + itemID
}
