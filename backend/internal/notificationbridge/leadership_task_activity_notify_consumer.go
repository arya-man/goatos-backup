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
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// WHAT HAPPENS ON MY TASK REACHES ME (maintainer instruction 2026-09-18).
//
// Until now a leadership task pushed twice: when it was RAISED, and when its STATUS moved
// (LeadershipTaskNotifyConsumer, which already excludes whoever made the change). Two things
// it owed nobody: a NOTE posted on the task, and an UPDATE to its brief. This consumer adds
// those two, plus the @-MENTION.
//
// Three rules, and each is load-bearing:
//
//  1. THE ACTOR IS NEVER TOLD ABOUT THEIR OWN ACT. The raiser writing a note pushes to the
//     assignee; the assignee writing one pushes to the raiser; a monitor writing one pushes to
//     both. Nobody is ever notified of what they just did.
//
//  2. ONE PERSON, ONE PUSH PER EVENT. The mention set and the party set are made DISJOINT in
//     Go before either is queued, and mention WINS the overlap because its copy is the more
//     specific of the two. A raiser who is also named in the note gets the mention, once --
//     never a mention and a comment notice for the same words.
//
//  3. A REPLAY ADDS NOTHING. Every queue write is keyed on the STABLE identity of the thing
//     that happened -- the note id for a note, the outbox event id for an update -- and the
//     calendar queue's insert is ON CONFLICT DO NOTHING per (tenant, event key, device). At
//     least once delivery therefore lands exactly one row per person per event.
//
// IT DELIBERATELY BYPASSES THE DESIGNATION AUDIENCE (audience.go). Every UPWARD leadership
// push resolves through Addressed(), so an admin can silence it per job title on People /
// HRMS -> Notifications. These three are not upward and not about a desk: they are addressed
// to the two people this task is between, and to the person someone typed the name of. A
// mention an admin could switch off for a whole title would make naming someone unreliable,
// which is worse than noisy. This is the first leadership push to make that choice; it is
// made deliberately, and it is why the consumer holds no AudienceResolver at all.
const (
	EventLeadershipTaskCommented = "leadership_task.commented"
	EventLeadershipTaskUpdated   = "leadership_task.updated"

	NotificationTypeLeadershipTaskMentioned = "leadership_task_mentioned"
	NotificationTypeLeadershipTaskCommented = "leadership_task_commented"
	NotificationTypeLeadershipTaskUpdated   = "leadership_task_updated"
)

// LeadershipTaskActivityNotifyConsumer turns a note or an update into one push per person who
// is owed it.
type LeadershipTaskActivityNotifyConsumer struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
}

// NewLeadershipTaskActivityNotifyConsumer wires the consumer. It takes no audience resolver
// on purpose -- see the package comment above.
func NewLeadershipTaskActivityNotifyConsumer(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *LeadershipTaskActivityNotifyConsumer {
	return &LeadershipTaskActivityNotifyConsumer{recipients: recipients, queue: queue, logger: logger, now: time.Now}
}

var _ eventbus.Handler = (*LeadershipTaskActivityNotifyConsumer)(nil)

// Register subscribes to the two new task events.
func (c *LeadershipTaskActivityNotifyConsumer) Register(bus eventbus.Bus) {
	bus.Subscribe(EventLeadershipTaskCommented, c)
	bus.Subscribe(EventLeadershipTaskUpdated, c)
}

// HandleEvent queues the pushes the event owes.
func (c *LeadershipTaskActivityNotifyConsumer) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if c == nil || c.recipients == nil || c.queue == nil {
		return nil
	}
	if event.Type != EventLeadershipTaskCommented && event.Type != EventLeadershipTaskUpdated {
		return nil
	}
	var payload leadershipTaskEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("leadership task activity notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(event.TenantID)
	taskID := strings.TrimSpace(payload.TaskID)
	if tenantID == "" || taskID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if event.Type == EventLeadershipTaskUpdated {
		return c.notifyUpdated(ctx, tenantID, event.ID, payload)
	}
	return c.notifyCommented(ctx, tenantID, event.ID, payload)
}

// notifyCommented pushes the note: the mention to whoever was named, the note itself to the
// task's other party. The two sets are disjoint by construction.
func (c *LeadershipTaskActivityNotifyConsumer) notifyCommented(ctx context.Context, tenantID, eventID string, p leadershipTaskEventPayload) error {
	author := strings.TrimSpace(p.ChangedBy)
	noteID := strings.TrimSpace(p.NoteID)
	if noteID == "" {
		// Nothing was written, so there is nothing to tell anyone. Also the key this push is
		// made idempotent on, so a note-less event must never queue.
		return nil
	}
	// The mention list arrives ALREADY VALIDATED: the write path checked every id under the
	// task's row lock against the same visibility rule that decides who may open the task, and
	// recorded each one as a participant. The consumer therefore never re-decides who may hear
	// about the task -- it only drops the author, who cannot be news to themselves.
	mentioned := dedupeUserIDs(p.MentionedUserIDs, author)
	// The parties minus the author minus anyone already getting the sharper mention copy.
	parties := dedupeUserIDs([]string{p.RaisedByUserID, p.AssigneeUserID}, author, mentioned...)

	number := leadershipTaskNumber(p.TaskNo)
	writer := nameOrFallback(authorName(p), "Someone on the leadership team")
	excerpt := strings.TrimSpace(p.NoteExcerpt)
	when := farmDateOrToday(p.OccurredAt, c.now())

	if len(mentioned) > 0 {
		title := fmt.Sprintf("%s named you on %s", writer, number)
		body := fmt.Sprintf("Task %s, %s. %s wrote on %s", number, leadershipTaskTitle(p.Title), writer, when)
		if excerpt != "" {
			body += fmt.Sprintf(": “%s”", excerpt)
		}
		body += ". Open it to reply."
		if err := c.queueTo(ctx, tenantID, eventID, p, mentioned,
			NotificationTypeLeadershipTaskMentioned, priorityHigh, title, body,
			EventLeadershipTaskCommented+":"+noteID+":mention", "leadership_task_mentioned", "leadership_task.mentioned"); err != nil {
			return err
		}
	}
	if len(parties) > 0 {
		title := fmt.Sprintf("%s wrote on %s", writer, number)
		body := fmt.Sprintf("Task %s, %s. %s added a note on %s", number, leadershipTaskTitle(p.Title), writer, when)
		if excerpt != "" {
			body += fmt.Sprintf(": “%s”", excerpt)
		}
		body += ". Open it to reply."
		if err := c.queueTo(ctx, tenantID, eventID, p, parties,
			NotificationTypeLeadershipTaskCommented, priorityNormal, title, body,
			EventLeadershipTaskCommented+":"+noteID+":party", "leadership_task_commented", "leadership_task.commented"); err != nil {
			return err
		}
	}
	return nil
}

// notifyUpdated pushes a change to the brief, the deadline or the attachments to the task's
// other party. The key carries the outbox event id because a task can be edited many times and
// each edit is its own news.
func (c *LeadershipTaskActivityNotifyConsumer) notifyUpdated(ctx context.Context, tenantID, eventID string, p leadershipTaskEventPayload) error {
	actor := strings.TrimSpace(p.ChangedBy)
	parties := dedupeUserIDs([]string{p.RaisedByUserID, p.AssigneeUserID}, actor)
	if len(parties) == 0 {
		return nil
	}
	number := leadershipTaskNumber(p.TaskNo)
	editor := nameOrFallback(authorName(p), "Someone on the leadership team")
	when := farmDateOrToday(p.OccurredAt, c.now())
	title := fmt.Sprintf("%s changed %s", editor, number)
	body := fmt.Sprintf("Task %s, %s, was changed by %s on %s. Open it to read what is being asked now.",
		number, leadershipTaskTitle(p.Title), editor, when)
	return c.queueTo(ctx, tenantID, eventID, p, parties,
		NotificationTypeLeadershipTaskUpdated, priorityNormal, title, body,
		EventLeadershipTaskUpdated+":"+p.TaskID+":"+eventID, "leadership_task_updated", "leadership_task.updated")
}

// queueTo resolves each person's devices and writes ONE queue call for the whole set, so a
// person cannot receive two rows from one call.
func (c *LeadershipTaskActivityNotifyConsumer) queueTo(
	ctx context.Context,
	tenantID, eventID string,
	p leadershipTaskEventPayload,
	userIDs []string,
	notificationType, priority, title, body, eventKey, contextType, messageKey string,
) error {
	recipients := make([]calendarports.NotificationRecipient, 0, len(userIDs))
	for _, userID := range userIDs {
		// scale-guard:ignore: the set is bounded by the task's two parties plus at most
		// domain.MaxMentionsPerNote (20) validated mentions, and the roster resolver has no
		// batch form; a per-person read is at most 22 bounded lookups for one event.
		devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, userID)
		if err != nil {
			return fmt.Errorf("leadership task activity notification: resolve %s: %w", notificationType, err)
		}
		recipients = append(recipients, toQueueRecipients(devices, roleLabelCEO)...)
	}
	recipients = dedupeQueueRecipients(recipients)
	if len(recipients) == 0 {
		if c.logger != nil {
			c.logger.WarnContext(ctx, "leadership_task_activity_notification_no_recipients",
				"tenant_id", tenantID, "task_id", p.TaskID, "notification_type", notificationType, "recipient_count", len(userIDs))
		}
		return nil
	}
	if _, err := c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "leadership_task:" + p.TaskID,
		TargetType:       "leadership_task",
		TargetID:         p.TaskID,
		NotificationType: notificationType,
		Channel:          channelPushFCM,
		Priority:         priority,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context:          leadershipTaskActivityContext(contextType, messageKey, p, eventID, priority),
		Recipients:       recipients,
	}); err != nil {
		return fmt.Errorf("leadership task activity notification: queue %s: %w", notificationType, err)
	}
	return nil
}

// leadershipTaskActivityContext deep-links to the task. The href carries the team-progress
// scope so a monitor or a mentioned participant lands on the task itself rather than on a tab
// that does not list it -- a participant reads the task by direct open and never gains a list.
func leadershipTaskActivityContext(typ, messageKey string, p leadershipTaskEventPayload, eventID, priority string) map[string]string {
	href := fmt.Sprintf("/tasks?scope=team_progress&task=%s", p.TaskID)
	out := map[string]string{
		"type":        typ,
		"message_key": messageKey,
		"screen":      leadershipTaskScreen,
		"href":        href,
		"target":      href,
		"task_id":     p.TaskID,
		"task_no":     fmt.Sprintf("%d", p.TaskNo),
		"status":      p.Status,
		"event_id":    eventID,
		"priority":    priority,
		"group_key":   "leadership_tasks",
	}
	if note := strings.TrimSpace(p.NoteID); note != "" {
		out["note_id"] = note
	}
	return out
}

// authorName reads whichever party's name matches the actor, so the copy can say WHO wrote.
// A note by a monitor resolves to neither and falls back to the neutral wording.
func authorName(p leadershipTaskEventPayload) string {
	actor := strings.TrimSpace(p.ChangedBy)
	switch actor {
	case strings.TrimSpace(p.RaisedByUserID):
		return p.RaisedByName
	case strings.TrimSpace(p.AssigneeUserID):
		return p.AssigneeName
	}
	return ""
}

// dedupeUserIDs trims, drops blanks, drops the actor, drops anything in `exclude`, and dedupes
// while keeping order. It is what makes the mention set and the party set disjoint, and what
// guarantees one push per person per event.
func dedupeUserIDs(ids []string, actor string, exclude ...string) []string {
	skip := make(map[string]struct{}, len(exclude)+1)
	if trimmed := strings.TrimSpace(actor); trimmed != "" {
		skip[trimmed] = struct{}{}
	}
	for _, id := range exclude {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			skip[trimmed] = struct{}{}
		}
	}
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, dropped := skip[id]; dropped {
			continue
		}
		skip[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// Compile-time proof that the mention bound this consumer relies on is the domain's own.
var _ = leadershiptasksdomain.MaxMentionsPerNote
