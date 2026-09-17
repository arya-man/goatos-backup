package notificationbridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	proutdomain "github.com/vgoats/goatos/backend/internal/penroutines/domain"
	proutports "github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// NotificationTypePenRoutineDue is the push naming the pens (or, for a whole-park routine, the
// park) the holders of a routine's roles owe today
// (maintainer instruction 2026-09-16). One push per ROUTINE per business date, sent once the
// routine's own notify time has passed in IST -- never one per pen: a round of ten pens is one
// morning's work and one message.
const NotificationTypePenRoutineDue = "pen_routine_due"

const roleLabelRoutineAssignee = "routine_assignee"

// PenRoutineDueNotifier pushes the day's digest per routine. The kernel stage calls it every
// tick with the open tasks due today; the business-date event key makes the write idempotent,
// so a digest whose notify time passed on an earlier tick queues nothing again.
type PenRoutineDueNotifier struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
}

// NewPenRoutineDueNotifier wires the notifier.
func NewPenRoutineDueNotifier(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *PenRoutineDueNotifier {
	return &PenRoutineDueNotifier{recipients: recipients, queue: queue, logger: logger, now: time.Now}
}

// WithClock pins the clock, for tests.
func (n *PenRoutineDueNotifier) WithClock(now func() time.Time) *PenRoutineDueNotifier {
	n.now = now
	return n
}

// NotifyDue queues one push per digest whose notify time has passed today, to the devices of
// EVERY person holding one of the routine's roles for its park (resolved by the digest read). A digest before its notify time is skipped this tick and offered again next tick.
func (n *PenRoutineDueNotifier) NotifyDue(ctx context.Context, tenantID string, digests []proutports.DueDigest) error {
	if n == nil || n.recipients == nil || n.queue == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("pen routine notification: tenant id is required")
	}
	now := n.now()
	today := biztime.BusinessDate(now)
	for _, d := range digests {
		if len(d.Tasks) == 0 || len(d.AssigneeIDs) == 0 || d.DueDate != today {
			continue
		}
		if !notifyTimePassed(d.NotifyTime, now) {
			continue
		}
		recipients := []calendarports.NotificationRecipient{}
		// Bounded by the routine's role holders in one park -- a handful of people, never by pens.
		// scale-guard:ignore: bounded per-holder recipient resolution, one read per role holder of the routine in its park (a handful of people per routine).
		for _, userID := range d.AssigneeIDs {
			devices, err := n.recipients.ResolveMemberRecipients(ctx, tenantID, userID)
			if err != nil {
				return fmt.Errorf("pen routine notification: resolve assignee: %w", err)
			}
			recipients = append(recipients, toQueueRecipients(devices, roleLabelRoutineAssignee)...)
		}
		recipients = dedupeQueueRecipients(recipients)
		if len(recipients) == 0 {
			// Loud, and no fallback: a check nobody is told about must not look announced.
			if n.logger != nil {
				n.logger.WarnContext(ctx, "pen_routine_due_notification_no_recipients",
					"tenant_id", tenantID, "routine_id", d.RoutineID, "park_id", d.ParkID, "assignees", len(d.AssigneeIDs), "pens", len(d.Tasks))
			}
			continue
		}
		title, body := PenRoutineDueCopy(d)
		eventKey := fmt.Sprintf("pen_routine.due:%s:%s", d.DueDate, d.RoutineID)
		// One write per ROUTINE due today (a handful per park), never per pen or animal.
		// scale-guard:ignore: bounded per-routine digest loop, one queue write per routine with open checks today (a handful per park).
		if _, err := n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  eventKey,
			TargetType:       "pen_routine",
			NotificationType: NotificationTypePenRoutineDue,
			Channel:          channelPushFCM,
			Priority:         priorityHigh,
			Title:            title,
			Body:             body,
			TraceID:          eventKey,
			EventKey:         eventKey,
			Context: map[string]string{
				"type":          NotificationTypePenRoutineDue,
				"message_key":   "pen_routine.due",
				"screen":        "pen_routines",
				"href":          "/pen-routines",
				"target":        "/pen-routines",
				"routine_id":    d.RoutineID,
				"routine_name":  d.RoutineName,
				"park_id":       d.ParkID,
				"park_name":     d.ParkName,
				"pen_count":     fmt.Sprintf("%d", len(d.Tasks)),
				"business_date": d.DueDate,
				"priority":      priorityHigh,
				"group_key":     "pen_routines:" + tenantID,
				"collapse_key":  "pen_routines:" + tenantID + ":" + d.RoutineID + ":" + d.DueDate,
			},
			Recipients: recipients,
		}); err != nil {
			return fmt.Errorf("pen routine notification: queue %s: %w", d.RoutineID, err)
		}
	}
	return nil
}

// notifyTimePassed reports whether the routine's local IST notify time is at or before now.
func notifyTimePassed(notifyTime string, now time.Time) bool {
	hour, minute, err := proutdomain.ParseClock(notifyTime)
	if err != nil {
		hour, minute = 7, 0
	}
	local := now.In(biztime.DefaultLocation())
	at := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, biztime.DefaultLocation())
	return !local.Before(at)
}

// PenRoutineDueCopy composes the push: the routine, the park, how many pens and which (or the
// whole park, for a general task), and the day -- every fact the notification-specificity rule
// requires, in farm words.
func PenRoutineDueCopy(d proutports.DueDigest) (title, body string) {
	park := strings.TrimSpace(d.ParkName)
	if park == "" {
		park = "your park"
	}
	name := strings.TrimSpace(d.RoutineName)
	if name == "" {
		name = "Routine check"
	}
	if len(d.Tasks) > 0 && d.Tasks[0].IsParkTask() {
		// A whole-park routine raises ONE task per day: there are no pens to count or name.
		due := biztime.FarmDateFromBusinessDate(d.DueDate)
		title = fmt.Sprintf("%s: due at %s", name, park)
		body = fmt.Sprintf("Due %s for the whole of %s. Open the card, answer, capture what is asked for and submit.", due, park)
		return title, body
	}
	count := len(d.Tasks)
	noun := "pens"
	if count == 1 {
		noun = "pen"
	}
	due := biztime.FarmDateFromBusinessDate(d.DueDate)
	title = fmt.Sprintf("%s: %d %s at %s", name, count, noun, park)
	parts := make([]string, 0, count)
	for i, t := range d.Tasks {
		if i == 4 && count > 5 {
			parts = append(parts, fmt.Sprintf("and %d more", count-4))
			break
		}
		pen := strings.TrimSpace(t.PenLabel)
		if pen == "" {
			pen = "a pen"
		}
		parts = append(parts, pen)
	}
	body = fmt.Sprintf("Due %s at %s: %s. Open each pen's card, answer, capture what is asked for and submit.",
		due, park, strings.Join(parts, ", "))
	return title, body
}
