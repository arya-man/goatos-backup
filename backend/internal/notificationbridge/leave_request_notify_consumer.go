package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	audiencedomain "github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Leave request push (maintainer decisions 2026-09-10).
//
// An operator asks for leave from the Clock screen; the people who must sign it -- the
// park head of their park and the HR desk -- hear about it at once: WHO is asking, WHICH
// days, HOW MANY, and WHY. When the request reaches its final answer the requester hears
// that back: approved, or rejected by whom and why. One slot approving while the other is
// still open pushes nothing; the requester sees that on their Clock screen.
//
// Addressed to the PEOPLE the event names (approver member ids resolved inside the write
// transaction; the requester by member id), never to a position. Rides the durable event
// spine (workforce.leave.*) emitted inside the write transaction, so a committed request
// always announces itself and a rolled-back one never does.
const (
	EventLeaveRequested = "workforce.leave.requested"
	EventLeaveApproved  = "workforce.leave.approved"
	EventLeaveRejected  = "workforce.leave.rejected"

	NotificationTypeLeaveRequestRaised  = "leave_request_raised"
	NotificationTypeLeaveRequestDecided = "leave_request_decided"

	leaveScreen = "leave"
)

type leaveEventPayload struct {
	LeaveRequestID    string   `json:"leave_request_id"`
	WorkforceMemberID string   `json:"workforce_member_id"`
	PersonName        string   `json:"person_name"`
	ParkID            string   `json:"park_id"`
	ParkLabel         string   `json:"park_label"`
	StartsOn          string   `json:"starts_on"`
	EndsOn            string   `json:"ends_on"`
	DayCount          int      `json:"day_count"`
	Reason            string   `json:"reason"`
	Status            string   `json:"status"`
	DecidedSlot       string   `json:"decided_slot"`
	DecidedByName     string   `json:"decided_by_name"`
	DecisionNote      string   `json:"decision_note"`
	ParkHeadRequired  bool     `json:"park_head_required"`
	HRRequired        bool     `json:"hr_required"`
	ParkHeadMemberIDs []string `json:"park_head_member_ids"`
	HRMemberIDs       []string `json:"hr_member_ids"`
	OccurredAt        string   `json:"occurred_at"`
}

// LeaveRequestNotifyConsumer turns the leave events into pushes.
type LeaveRequestNotifyConsumer struct {
	recipients RecipientResolver
	audience   AudienceResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
}

// NewLeaveRequestNotifyConsumer wires the consumer.
func NewLeaveRequestNotifyConsumer(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *LeaveRequestNotifyConsumer {
	return &LeaveRequestNotifyConsumer{recipients: recipients, audience: defaultAudience(recipients), queue: queue, logger: logger, now: time.Now}
}

// WithAudience attaches the stored per-designation audience (production wiring).
func (c *LeaveRequestNotifyConsumer) WithAudience(audience AudienceResolver) *LeaveRequestNotifyConsumer {
	if audience != nil {
		c.audience = audience
	}
	return c
}

var _ eventbus.Handler = (*LeaveRequestNotifyConsumer)(nil)

// Register subscribes to the raise and the two final outcomes. A withdrawal
// pushes nobody: the person withdrew their own ask.
func (c *LeaveRequestNotifyConsumer) Register(bus eventbus.Bus) {
	bus.Subscribe(EventLeaveRequested, c)
	bus.Subscribe(EventLeaveApproved, c)
	bus.Subscribe(EventLeaveRejected, c)
}

// HandleEvent queues the push the event owes, if any.
func (c *LeaveRequestNotifyConsumer) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if c == nil || c.recipients == nil || c.queue == nil {
		return nil
	}
	if event.Type != EventLeaveRequested && event.Type != EventLeaveApproved && event.Type != EventLeaveRejected {
		return nil
	}
	var payload leaveEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("leave notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(event.TenantID)
	requestID := strings.TrimSpace(payload.LeaveRequestID)
	if tenantID == "" || requestID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	switch event.Type {
	case EventLeaveRequested:
		return c.notifyRaised(ctx, tenantID, payload)
	default:
		return c.notifyDecided(ctx, tenantID, event.ID, payload)
	}
}

func (c *LeaveRequestNotifyConsumer) notifyRaised(ctx context.Context, tenantID string, p leaveEventPayload) error {
	members := make([]string, 0, len(p.ParkHeadMemberIDs)+len(p.HRMemberIDs))
	if p.ParkHeadRequired {
		members = append(members, p.ParkHeadMemberIDs...)
	}
	if p.HRRequired {
		members = append(members, p.HRMemberIDs...)
	}
	devices := make([]workforcedomain.NotificationRecipient, 0, len(members))
	for _, memberID := range dedupeStrings(members) {
		resolved, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, memberID)
		if err != nil {
			return fmt.Errorf("leave notification: resolve approver %s: %w", memberID, err)
		}
		devices = append(devices, resolved...)
	}
	recipients, err := c.audience.Addressed(ctx, tenantID, p.ParkID, audiencedomain.AlertLeaveRequestRaised,
		[]string{audiencedomain.DesignationParkHead, audiencedomain.DesignationHR},
		dedupeQueueRecipients(toQueueRecipients(devices, "approver")))
	if err != nil {
		return fmt.Errorf("leave notification: %w", err)
	}
	if len(recipients) == 0 {
		if c.logger != nil {
			c.logger.WarnContext(ctx, "leave_request_raised_notification_no_recipients",
				"tenant_id", tenantID, "leave_request_id", p.LeaveRequestID, "park_id", p.ParkID)
		}
		return nil
	}
	person := nameOrFallback(p.PersonName, "A team member")
	window := leaveWindowLabel(p.StartsOn, p.EndsOn, p.DayCount)
	title := fmt.Sprintf("Leave request: %s · %s", person, window)
	body := fmt.Sprintf("%s asked for leave %s", person, window)
	if park := strings.TrimSpace(p.ParkLabel); park != "" {
		body += " at " + park
	}
	body += ". Reason: " + leaveReason(p.Reason) + " Open Approvals to decide."
	eventKey := EventLeaveRequested + ":" + p.LeaveRequestID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "leave_request:" + p.LeaveRequestID,
		TargetType:       "leave_request",
		TargetID:         p.LeaveRequestID,
		NotificationType: NotificationTypeLeaveRequestRaised,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context:          leaveContext("leave_request_raised", "leave_request.raised", "/leave/approvals", p),
		Recipients:       recipients,
	})
	if err != nil {
		return fmt.Errorf("leave notification: queue raised: %w", err)
	}
	return nil
}

func (c *LeaveRequestNotifyConsumer) notifyDecided(ctx context.Context, tenantID, eventID string, p leaveEventPayload) error {
	devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, p.WorkforceMemberID)
	if err != nil {
		return fmt.Errorf("leave notification: resolve requester: %w", err)
	}
	recipients, err := c.audience.Addressed(ctx, tenantID, p.ParkID, audiencedomain.AlertLeaveRequestDecided,
		[]string{audiencedomain.DesignationOperator}, dedupeQueueRecipients(toQueueRecipients(devices, "requester")))
	if err != nil {
		return fmt.Errorf("leave notification: %w", err)
	}
	if len(recipients) == 0 {
		if c.logger != nil {
			c.logger.WarnContext(ctx, "leave_request_decided_notification_no_recipients",
				"tenant_id", tenantID, "leave_request_id", p.LeaveRequestID, "workforce_member_id", p.WorkforceMemberID)
		}
		return nil
	}
	window := leaveWindowLabel(p.StartsOn, p.EndsOn, p.DayCount)
	when := farmDateOrToday(p.OccurredAt, c.now())
	var title, body string
	if p.Status == "approved" {
		title = fmt.Sprintf("Leave approved · %s", window)
		body = fmt.Sprintf("Your leave for %s was approved on %s by the park head and HR.", window, when)
	} else {
		by := leaveSlotLabel(p.DecidedSlot)
		if name := strings.TrimSpace(p.DecidedByName); name != "" {
			by = name + " (" + by + ")"
		}
		title = fmt.Sprintf("Leave rejected · %s", window)
		body = fmt.Sprintf("Your leave for %s was rejected on %s by %s.", window, when, by)
		if note := strings.TrimSpace(p.DecisionNote); note != "" {
			body += " Reason: " + leaveReason(note)
		}
	}
	eventKey := EventLeaveApproved + ":" + p.LeaveRequestID + ":" + eventID
	if p.Status != "approved" {
		eventKey = EventLeaveRejected + ":" + p.LeaveRequestID + ":" + eventID
	}
	if _, err := c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "leave_request:" + p.LeaveRequestID,
		TargetType:       "leave_request",
		TargetID:         p.LeaveRequestID,
		NotificationType: NotificationTypeLeaveRequestDecided,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context:          leaveContext("leave_request_decided", "leave_request.decided", "/clock", p),
		Recipients:       recipients,
	}); err != nil {
		return fmt.Errorf("leave notification: queue decided: %w", err)
	}
	return nil
}

func leaveContext(typ, messageKey, target string, p leaveEventPayload) map[string]string {
	return map[string]string{
		"type":             typ,
		"message_key":      messageKey,
		"screen":           leaveScreen,
		"href":             target,
		"target":           target,
		"leave_request_id": p.LeaveRequestID,
		"park_id":          p.ParkID,
		"business_date":    p.StartsOn,
		"status":           p.Status,
		"priority":         priorityHigh,
		"group_key":        "leave_requests",
	}
}

// leaveWindowLabel renders the request window as farm dates: "12/09/2026 –
// 14/09/2026 (3 days)". Both bounds are business DATES.
func leaveWindowLabel(startsOn, endsOn string, days int) string {
	start := biztime.FarmDateFromBusinessDate(startsOn)
	end := biztime.FarmDateFromBusinessDate(endsOn)
	if days <= 0 {
		days = 1
	}
	if start == end {
		return fmt.Sprintf("%s (%d day%s)", start, days, plural(days))
	}
	return fmt.Sprintf("%s – %s (%d day%s)", start, end, days, plural(days))
}

func leaveSlotLabel(slot string) string {
	switch slot {
	case "park_head":
		return "the park head"
	case "hr":
		return "HR"
	}
	return "an approver"
}

// leaveReason keeps the push readable on a lock screen.
func leaveReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "not given."
	}
	const max = 120
	if len([]rune(reason)) > max {
		reason = string([]rune(reason)[:max-1]) + "…"
	}
	if !strings.HasSuffix(reason, ".") {
		reason += "."
	}
	return reason
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
