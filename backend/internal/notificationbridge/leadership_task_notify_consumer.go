package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Leadership Tasks push (maintainer decision 2026-09-04).
//
// A director raises a task for a CXO; the CXO hears about it at once -- WHO asked, WHAT
// (the task number and title), and how much is attached -- so the ask is not discovered on
// the next drawer open. When the CXO marks it done, the director who asked hears that back.
// Nothing else pushes: "in progress" and "reopened" are visible on the list and would only
// be noise on a leadership phone.
//
// Addressed to ONE PERSON each time, resolved by user id through the roster's device
// registry, never to a position: the task names its assignee, and a raise addressed to Ravi
// must not reach Manju. Both messages ride the durable event spine (leadership_task.raised,
// leadership_task.status_changed) emitted inside the write transaction, so a committed task
// always announces itself and a rolled-back one never does.
const (
	EventLeadershipTaskRaised        = "leadership_task.raised"
	EventLeadershipTaskStatusChanged = "leadership_task.status_changed"

	NotificationTypeLeadershipTaskRaised = "leadership_task_raised"
	NotificationTypeLeadershipTaskDone   = "leadership_task_done"

	leadershipTaskStatusDone = "done"
	leadershipTaskScreen     = "leadership_task"
)

type leadershipTaskEventPayload struct {
	TaskID          string `json:"task_id"`
	TaskNo          int64  `json:"task_no"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	PreviousStatus  string `json:"previous_status"`
	RaisedByUserID  string `json:"raised_by_user_id"`
	RaisedByName    string `json:"raised_by_name"`
	AssigneeUserID  string `json:"assignee_user_id"`
	AssigneeName    string `json:"assignee_name"`
	AttachmentCount int    `json:"attachment_count"`
	ChangedBy       string `json:"changed_by_user_id"`
	OccurredAt      string `json:"occurred_at"`
}

// LeadershipTaskNotifyConsumer turns the two task events into one push each.
type LeadershipTaskNotifyConsumer struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
}

// NewLeadershipTaskNotifyConsumer wires the consumer.
func NewLeadershipTaskNotifyConsumer(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *LeadershipTaskNotifyConsumer {
	return &LeadershipTaskNotifyConsumer{recipients: recipients, queue: queue, logger: logger, now: time.Now}
}

var _ eventbus.Handler = (*LeadershipTaskNotifyConsumer)(nil)

// Register subscribes to both task events.
func (c *LeadershipTaskNotifyConsumer) Register(bus eventbus.Bus) {
	bus.Subscribe(EventLeadershipTaskRaised, c)
	bus.Subscribe(EventLeadershipTaskStatusChanged, c)
}

// HandleEvent queues the push the event owes, if any.
func (c *LeadershipTaskNotifyConsumer) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if c == nil || c.recipients == nil || c.queue == nil {
		return nil
	}
	if event.Type != EventLeadershipTaskRaised && event.Type != EventLeadershipTaskStatusChanged {
		return nil
	}
	var payload leadershipTaskEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("leadership task notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(event.TenantID)
	taskID := strings.TrimSpace(payload.TaskID)
	if tenantID == "" || taskID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	switch event.Type {
	case EventLeadershipTaskRaised:
		return c.notifyRaised(ctx, tenantID, event.ID, payload)
	case EventLeadershipTaskStatusChanged:
		// Only DONE is worth a push, and only to the person who asked.
		if payload.Status != leadershipTaskStatusDone {
			return nil
		}
		return c.notifyDone(ctx, tenantID, event.ID, payload)
	}
	return nil
}

func (c *LeadershipTaskNotifyConsumer) notifyRaised(ctx context.Context, tenantID, eventID string, p leadershipTaskEventPayload) error {
	devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, p.AssigneeUserID)
	if err != nil {
		return fmt.Errorf("leadership task notification: resolve assignee: %w", err)
	}
	recipients := dedupeQueueRecipients(toQueueRecipients(devices, roleLabelCEO))
	if len(recipients) == 0 {
		if c.logger != nil {
			c.logger.WarnContext(ctx, "leadership_task_raised_notification_no_recipients",
				"tenant_id", tenantID, "task_id", p.TaskID, "assignee_user_id", p.AssigneeUserID)
		}
		return nil
	}
	number := leadershipTaskNumber(p.TaskNo)
	raiser := nameOrFallback(p.RaisedByName, "A director")
	title := fmt.Sprintf("%s asked you: %s", raiser, leadershipTaskTitle(p.Title))
	body := fmt.Sprintf("Task %s from %s, raised %s", number, raiser, farmDateOrToday(p.OccurredAt, c.now()))
	if p.AttachmentCount > 0 {
		body += fmt.Sprintf(", with %d attachment%s", p.AttachmentCount, plural(p.AttachmentCount))
	}
	body += ". Open it to see the brief."
	eventKey := EventLeadershipTaskRaised + ":" + p.TaskID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "leadership_task:" + p.TaskID,
		TargetType:       "leadership_task",
		TargetID:         p.TaskID,
		NotificationType: NotificationTypeLeadershipTaskRaised,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context:          leadershipTaskContext("leadership_task_raised", "leadership_task.raised", p, eventID),
		Recipients:       recipients,
	})
	if err != nil {
		return fmt.Errorf("leadership task notification: queue raised: %w", err)
	}
	return nil
}

func (c *LeadershipTaskNotifyConsumer) notifyDone(ctx context.Context, tenantID, eventID string, p leadershipTaskEventPayload) error {
	devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, p.RaisedByUserID)
	if err != nil {
		return fmt.Errorf("leadership task notification: resolve raiser: %w", err)
	}
	recipients := dedupeQueueRecipients(toQueueRecipients(devices, "director"))
	if len(recipients) == 0 {
		if c.logger != nil {
			c.logger.WarnContext(ctx, "leadership_task_done_notification_no_recipients",
				"tenant_id", tenantID, "task_id", p.TaskID, "raised_by_user_id", p.RaisedByUserID)
		}
		return nil
	}
	number := leadershipTaskNumber(p.TaskNo)
	assignee := nameOrFallback(p.AssigneeName, "The leadership desk")
	title := fmt.Sprintf("%s marked %s done", assignee, number)
	body := fmt.Sprintf("Task %s, %s, was completed by %s on %s.", number, leadershipTaskTitle(p.Title), assignee, farmDateOrToday(p.OccurredAt, c.now()))
	// The key carries the event id: a task can be marked done, reopened and marked done
	// again, and each completion is its own news.
	eventKey := EventLeadershipTaskStatusChanged + ":" + p.TaskID + ":" + eventID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "leadership_task:" + p.TaskID,
		TargetType:       "leadership_task",
		TargetID:         p.TaskID,
		NotificationType: NotificationTypeLeadershipTaskDone,
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context:          leadershipTaskContext("leadership_task_done", "leadership_task.done", p, eventID),
		Recipients:       recipients,
	})
	if err != nil {
		return fmt.Errorf("leadership task notification: queue done: %w", err)
	}
	return nil
}

func leadershipTaskContext(typ, messageKey string, p leadershipTaskEventPayload, eventID string) map[string]string {
	return map[string]string{
		"type":        typ,
		"message_key": messageKey,
		"screen":      leadershipTaskScreen,
		"href":        "/leadership-tasks/" + p.TaskID,
		"target":      "/leadership-tasks/" + p.TaskID,
		"task_id":     p.TaskID,
		"task_no":     fmt.Sprintf("%d", p.TaskNo),
		"status":      p.Status,
		"event_id":    eventID,
		"priority":    priorityHigh,
		"group_key":   "leadership_tasks",
	}
}

func leadershipTaskNumber(n int64) string { return fmt.Sprintf("#%d", n) }

// leadershipTaskTitle keeps the push readable on a lock screen: the title is quoted whole
// when short and cut with an ellipsis when long.
func leadershipTaskTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "a task"
	}
	const max = 80
	if len([]rune(title)) > max {
		return string([]rune(title)[:max-1]) + "…"
	}
	return title
}

func nameOrFallback(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return strings.TrimSpace(name)
}

func farmDateOrToday(occurredAt string, now time.Time) string {
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(occurredAt)); err == nil {
		return biztime.FarmDate(t)
	}
	return biztime.FarmDate(now)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
