package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	leadershiptasksdomain "github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	audiencedomain "github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
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

	leadershipTaskScreen = "leadership_task"
)

type leadershipTaskEventPayload struct {
	TaskID              string `json:"task_id"`
	TaskNo              int64  `json:"task_no"`
	Title               string `json:"title"`
	Status              string `json:"status"`
	PreviousStatus      string `json:"previous_status"`
	RaisedByUserID      string `json:"raised_by_user_id"`
	RaisedByName        string `json:"raised_by_name"`
	RaisedByDesignation string `json:"raised_by_designation"`
	AssigneeUserID      string `json:"assignee_user_id"`
	AssigneeName        string `json:"assignee_name"`
	AttachmentCount     int    `json:"attachment_count"`
	ChangedBy           string `json:"changed_by_user_id"`
	OccurredAt          string `json:"occurred_at"`
}

// LeadershipTaskNotifyConsumer turns the two task events into one push each.
type LeadershipTaskNotifyConsumer struct {
	recipients RecipientResolver
	// audience gates both pushes per designation (leadership.task_raised / task_done): the
	// addressed person is kept while their job title is ticked, other ticked titles get a copy.
	audience AudienceResolver
	queue    NotificationQueue
	logger   *slog.Logger
	now      func() time.Time
}

// NewLeadershipTaskNotifyConsumer wires the consumer.
func NewLeadershipTaskNotifyConsumer(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *LeadershipTaskNotifyConsumer {
	return &LeadershipTaskNotifyConsumer{recipients: recipients, audience: defaultAudience(recipients), queue: queue, logger: logger, now: time.Now}
}

// WithAudience attaches the stored per-designation audience (production wiring).
func (c *LeadershipTaskNotifyConsumer) WithAudience(audience AudienceResolver) *LeadershipTaskNotifyConsumer {
	if audience != nil {
		c.audience = audience
	}
	return c
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
		// EVERY status change pushes (maintainer decision 2026-09-08, superseding the 2026-09-04
		// done-only rule): the other party is told -- the raiser when the CXO moved it, the CXO
		// when the raiser did. Whoever made the change is never pushed about their own act.
		return c.notifyStatusChanged(ctx, tenantID, event.ID, payload)
	}
	return nil
}

func (c *LeadershipTaskNotifyConsumer) notifyRaised(ctx context.Context, tenantID, eventID string, p leadershipTaskEventPayload) error {
	devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, p.AssigneeUserID)
	if err != nil {
		return fmt.Errorf("leadership task notification: resolve assignee: %w", err)
	}
	recipients, err := c.audience.Addressed(ctx, tenantID, "", audiencedomain.AlertLeadershipTaskRaised,
		[]string{audiencedomain.DesignationCEO}, dedupeQueueRecipients(toQueueRecipients(devices, roleLabelCEO)))
	if err != nil {
		return fmt.Errorf("leadership task notification: %w", err)
	}
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

func (c *LeadershipTaskNotifyConsumer) notifyStatusChanged(ctx context.Context, tenantID, eventID string, p leadershipTaskEventPayload) error {
	changedBy := strings.TrimSpace(p.ChangedBy)
	number := leadershipTaskNumber(p.TaskNo)
	chip := leadershiptasksdomain.StatusChip(p.Status)
	when := farmDateOrToday(p.OccurredAt, c.now())
	// The key carries the event id: a task can be moved, reopened and moved again, and each
	// change is its own news.
	eventKey := EventLeadershipTaskStatusChanged + ":" + p.TaskID + ":" + eventID

	// UP to the raiser (a director) when someone else moved the task: gated by the
	// leadership.task_done row, whose default is every director title.
	if raiser := strings.TrimSpace(p.RaisedByUserID); raiser != "" && raiser != changedBy {
		devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, raiser)
		if err != nil {
			return fmt.Errorf("leadership task notification: resolve raiser: %w", err)
		}
		addressee := []string{strings.TrimSpace(p.RaisedByDesignation)}
		if addressee[0] == "" {
			addressee = audiencedomain.DirectorDesignations
		}
		recipients, err := c.audience.Addressed(ctx, tenantID, "", audiencedomain.AlertLeadershipTaskDone,
			addressee, dedupeQueueRecipients(toQueueRecipients(devices, "director")))
		if err != nil {
			return fmt.Errorf("leadership task notification: %w", err)
		}
		if len(recipients) == 0 {
			if c.logger != nil {
				c.logger.WarnContext(ctx, "leadership_task_status_notification_no_raiser_recipients",
					"tenant_id", tenantID, "task_id", p.TaskID, "raised_by_user_id", raiser, "status", p.Status)
			}
		} else {
			assignee := nameOrFallback(p.AssigneeName, "The leadership desk")
			title := fmt.Sprintf("%s marked %s %s", assignee, number, chip)
			body := fmt.Sprintf("Task %s, %s, was marked %s by %s on %s.", number, leadershipTaskTitle(p.Title), chip, assignee, when)
			if _, err := c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
				TenantID:         tenantID,
				CalendarEventID:  "leadership_task:" + p.TaskID,
				TargetType:       "leadership_task",
				TargetID:         p.TaskID,
				NotificationType: NotificationTypeLeadershipTaskDone,
				Channel:          channelPushFCM,
				Priority:         priorityNormal,
				Title:            title,
				Body:             body,
				TraceID:          eventKey + ":raiser",
				EventKey:         eventKey + ":raiser",
				Context:          leadershipTaskContext("leadership_task_status", "leadership_task.status", p, eventID),
				Recipients:       recipients,
			}); err != nil {
				return fmt.Errorf("leadership task notification: queue status to raiser: %w", err)
			}
		}
	}

	// DOWN to the CXO the task is addressed to when someone else moved it (the raiser cancelled
	// or reopened it): gated by the leadership.task_raised row, whose default is CEO / CXO.
	if assignee := strings.TrimSpace(p.AssigneeUserID); assignee != "" && assignee != changedBy {
		devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, assignee)
		if err != nil {
			return fmt.Errorf("leadership task notification: resolve assignee: %w", err)
		}
		recipients, err := c.audience.Addressed(ctx, tenantID, "", audiencedomain.AlertLeadershipTaskRaised,
			[]string{audiencedomain.DesignationCEO}, dedupeQueueRecipients(toQueueRecipients(devices, roleLabelCEO)))
		if err != nil {
			return fmt.Errorf("leadership task notification: %w", err)
		}
		if len(recipients) == 0 {
			if c.logger != nil {
				c.logger.WarnContext(ctx, "leadership_task_status_notification_no_assignee_recipients",
					"tenant_id", tenantID, "task_id", p.TaskID, "assignee_user_id", assignee, "status", p.Status)
			}
			return nil
		}
		raiser := nameOrFallback(p.RaisedByName, "A director")
		title := fmt.Sprintf("%s marked %s %s", raiser, number, chip)
		body := fmt.Sprintf("Task %s, %s, was marked %s by %s on %s.", number, leadershipTaskTitle(p.Title), chip, raiser, when)
		if _, err := c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  "leadership_task:" + p.TaskID,
			TargetType:       "leadership_task",
			TargetID:         p.TaskID,
			NotificationType: NotificationTypeLeadershipTaskDone,
			Channel:          channelPushFCM,
			Priority:         priorityNormal,
			Title:            title,
			Body:             body,
			TraceID:          eventKey + ":assignee",
			EventKey:         eventKey + ":assignee",
			Context:          leadershipTaskContext("leadership_task_status", "leadership_task.status", p, eventID),
			Recipients:       recipients,
		}); err != nil {
			return fmt.Errorf("leadership task notification: queue status to assignee: %w", err)
		}
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
