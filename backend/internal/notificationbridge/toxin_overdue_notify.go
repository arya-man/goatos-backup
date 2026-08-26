package notificationbridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	toxindomain "github.com/vgoats/goatos/backend/internal/toxin/domain"
)

// The toxin start reminder (maintainer decision 2026-08-26).
//
// A feed load sits in the store until its strip test clears it, so a test nobody picks up is a bag
// of feed nobody can use. Twelve hours after the load is recorded, if NOBODY HAS STARTED the test,
// this sends one message to the people who run it and to the CEO's office.
//
// NOT STARTED, deliberately narrower than the overdue chip. The card reddens for any round still
// unfinished at twelve hours (domain.IsOverdue), because the load is uncleared either way. The
// MESSAGE goes only where nobody has begun: telling someone visibly working through the steps to
// "start" the test is the kind of nagging that teaches people to ignore notifications.
//
// NO PRIVATE SCHEDULER. It rides the shared operational cadence like the low-stock alert, and
// "once per day per task" comes from the business date in the idempotency key rather than state of
// its own -- the first tick of the day writes, every later tick writes nothing. That survives a
// worker restart, a mid-day redeploy, and both instances of an HA pair running it at once.
//
// A message NOBODY receives must not look sent: an empty recipient set is logged loudly and the
// run continues, rather than being silently counted as delivered.

// NotificationTypeToxinOverdue is the notification_requests.notification_type for the reminder.
const NotificationTypeToxinOverdue = "toxin_test_overdue"

const roleLabelToxinTester = "toxin_tester"

// ToxinOverdueReader is the slice of the toxin repository this bridge needs: the rounds that are
// past the start deadline with no step recorded against them.
type ToxinOverdueReader interface {
	UnstartedOverdueTasks(ctx context.Context, tenantID string, now time.Time) ([]toxindomain.Task, error)
}

// ToxinTesterDirectory resolves who currently holds the tester grant. It is read from the GRANT,
// not from a job title: toxin testing is granted to named individuals (perPersonGrants), so a
// future holder of either director seat must not inherit the reminder.
type ToxinTesterDirectory interface {
	ToxinTesterUserIDs(ctx context.Context, tenantID string) ([]string, error)
}

// ToxinOverdueNotifier queues the start reminders.
type ToxinOverdueNotifier struct {
	tasks      ToxinOverdueReader
	testers    ToxinTesterDirectory
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
}

// NewToxinOverdueNotifier wires the bridge.
func NewToxinOverdueNotifier(
	tasks ToxinOverdueReader,
	testers ToxinTesterDirectory,
	recipients RecipientResolver,
	queue NotificationQueue,
	logger *slog.Logger,
) *ToxinOverdueNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &ToxinOverdueNotifier{
		tasks: tasks, testers: testers, recipients: recipients,
		queue: queue, logger: logger, now: time.Now,
	}
}

// WithClock pins the clock for tests.
func (n *ToxinOverdueNotifier) WithClock(now func() time.Time) *ToxinOverdueNotifier {
	if now != nil {
		n.now = now
	}
	return n
}

// NotifyOverdue queues one reminder per unstarted overdue round.
func (n *ToxinOverdueNotifier) NotifyOverdue(ctx context.Context, tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("toxin overdue reminder: tenant id is required")
	}
	now := n.now()
	overdue, err := n.tasks.UnstartedOverdueTasks(ctx, tenantID, now)
	if err != nil {
		return fmt.Errorf("toxin overdue reminder: read tasks: %w", err)
	}
	if len(overdue) == 0 {
		return nil
	}

	// Resolved ONCE for the whole run, not per task: the audience is the same for every reminder
	// and resolving inside the loop would be one roster read per late load.
	recipients, err := n.audience(ctx, tenantID)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		n.logger.WarnContext(ctx, "toxin_overdue_notification_no_recipients",
			"tenant_id", tenantID, "overdue_tasks", len(overdue))
		return nil
	}

	businessDate := biztime.BusinessDate(now)
	visibleDate := biztime.FarmDateFromBusinessDate(businessDate)

	// One write per LATE load. Bounded by the loads actually past their deadline with nobody on
	// them -- normally zero, and a handful at worst; never herd-sized.
	//
	// scale-guard:ignore: bounded per-task reminder loop over already-late rounds, see above.
	for _, task := range overdue {
		createdAt, parseErr := time.Parse(time.RFC3339Nano, task.CreatedAt)
		if parseErr != nil {
			// A round whose creation instant will not parse cannot be judged late. Skip it loudly
			// rather than sending a reminder with no age in it.
			n.logger.WarnContext(ctx, "toxin_overdue_notification_unparsable_created_at",
				"tenant_id", tenantID, "task_id", task.TaskID)
			continue
		}
		waited := hoursWaiting(createdAt, now)
		// The business date makes this once-per-day-per-task with no scheduler and no state.
		eventKey := fmt.Sprintf("toxin.test.overdue:%s:%s", businessDate, task.TaskID)
		body := fmt.Sprintf(
			"%s from %s at %s (%s, batch %d) arrived %s and the strip test has not been started. It has been waiting %s.",
			task.FeedItemLabel, vendorPhrase(task.Vendor), task.FarmLabel,
			quantityPhrase(task.QuantityKg), task.BatchNo,
			biztime.FarmDateFromBusinessDate(task.PurchaseDate), waited)
		// One write per LATE load by design: each reminder deep-links to its own task, which a
		// digest cannot do. Bounded by the rounds actually past the deadline with nobody on them
		// -- normally zero, a handful at worst -- and never herd-sized.
		//
		// scale-guard:ignore: bounded per-task reminder loop over already-late rounds, see above.
		if _, err := n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  eventKey,
			TargetType:       "toxin_test",
			NotificationType: NotificationTypeToxinOverdue,
			Channel:          channelPushFCM,
			Priority:         priorityHigh,
			// Names the feed, the supplier, the park, the load and how long it has waited. An
			// abstract "1 toxin test is overdue" is the exact defect the specificity rule bans.
			Title:      fmt.Sprintf("Toxin test overdue — %s at %s", task.FeedItemLabel, task.FarmLabel),
			Body:       body,
			TraceID:    eventKey,
			EventKey:   eventKey,
			Recipients: recipients,
			Context: map[string]string{
				"type":             "toxin_test_overdue",
				"message_key":      "toxin.test.overdue",
				"screen":           "toxin_task",
				"href":             "/toxin/tasks/" + task.TaskID,
				"task_id":          task.TaskID,
				"feed_purchase_id": task.FeedPurchaseID,
				"feed_item_label":  task.FeedItemLabel,
				"vendor":           task.Vendor,
				"farm_label":       task.FarmLabel,
				"batch_no":         fmt.Sprintf("%d", task.BatchNo),
				"purchase_date":    task.PurchaseDate,
				"business_date":    businessDate,
				"visible_date":     visibleDate,
				"priority":         priorityHigh,
				"group_key":        "toxin_overdue:" + tenantID,
				"collapse_key":     "toxin_overdue:" + tenantID + ":" + task.TaskID,
			},
		}); err != nil {
			return fmt.Errorf("toxin overdue reminder: queue %s: %w", task.TaskID, err)
		}
	}
	return nil
}

// audience is the named testers plus the CEO's office. The testers come from the GRANT rather than
// a position, because toxin testing is granted per person; the CEO seat is included because that
// office owns the module and carries the verdict.
func (n *ToxinOverdueNotifier) audience(ctx context.Context, tenantID string) ([]calendarports.NotificationRecipient, error) {
	var out []calendarports.NotificationRecipient

	testerIDs, err := n.testers.ToxinTesterUserIDs(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("toxin overdue reminder: resolve testers: %w", err)
	}
	for _, userID := range testerIDs {
		devices, err := n.recipients.ResolveMemberRecipients(ctx, tenantID, userID)
		if err != nil {
			return nil, fmt.Errorf("toxin overdue reminder: resolve tester %s: %w", userID, err)
		}
		if len(devices) == 0 {
			n.logger.WarnContext(ctx, "toxin_overdue_notification_no_devices_for_tester",
				"tenant_id", tenantID, "user_id", userID)
			continue
		}
		out = append(out, toQueueRecipients(devices, roleLabelToxinTester)...)
	}

	ceo, err := n.recipients.ResolvePositionRecipients(ctx, tenantID, scopeTenant, tenantID, positionCEOInternal)
	if err != nil {
		return nil, fmt.Errorf("toxin overdue reminder: resolve ceo: %w", err)
	}
	out = append(out, toQueueRecipients(ceo, roleLabelCEO)...)
	return out, nil
}

// hoursWaiting phrases the age in farm words. Hours up to two days, then days.
func hoursWaiting(createdAt, now time.Time) string {
	elapsed := now.Sub(createdAt)
	if elapsed < 0 {
		elapsed = 0
	}
	hours := int(elapsed / time.Hour)
	switch {
	case hours >= 48:
		return fmt.Sprintf("%d days", hours/24)
	case hours == 1:
		return "1 hour"
	default:
		return fmt.Sprintf("%d hours", hours)
	}
}

func vendorPhrase(vendor string) string {
	if strings.TrimSpace(vendor) == "" {
		return "an unnamed supplier"
	}
	return vendor
}

// quantityPhrase trims the stored 3-decimal scale down to what a person reads: "4200 kg", not
// "4200.000 kg" and not "4200.00 kg". TrimRight, not TrimSuffix — the latter strips ONE trailing
// zero and leaves the rest.
func quantityPhrase(kg float64) string {
	text := strings.TrimRight(fmt.Sprintf("%.3f", kg), "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-" {
		text = "0"
	}
	return text + " kg"
}
