package notificationbridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	pccaredomain "github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// NotificationTypePCCareRepeatSkipped tells the person who planned a Preventive Care task that
// its SOP repeat could not be made (maintainer instruction 2026-09-30): none of the operators of
// the last task can still do the work, so the pens were not planned and nobody was substituted.
// ADDRESSED to one named planner -- the person whose plan it continued -- once per skipped task.
const NotificationTypePCCareRepeatSkipped = "pc_care_repeat_skipped"

const roleLabelPCCarePlanner = "pc_care_planner"

// PCCareRepeatSkippedNotifier pushes the skip to the planner.
type PCCareRepeatSkippedNotifier struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
}

// NewPCCareRepeatSkippedNotifier wires the notifier.
func NewPCCareRepeatSkippedNotifier(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *PCCareRepeatSkippedNotifier {
	return &PCCareRepeatSkippedNotifier{recipients: recipients, queue: queue, logger: logger}
}

var _ pccareapp.RepeatAlerter = (*PCCareRepeatSkippedNotifier)(nil)

// PCCareRepeatSkippedCopy is the farm-worded push: the work, the pens, the park, the date and
// why, so the planner knows exactly what to plan again.
func PCCareRepeatSkippedCopy(s pccareapp.RepeatSkip) (string, string) {
	work := pccaredomain.CategoryLabel(s.Category)
	pens := strings.Join(s.PenLabels, ", ")
	where := pens
	if s.ParkName != "" {
		where = pens + " (" + s.ParkName + ")"
	}
	date := biztime.FarmDate(s.DueDate)
	why := "none of the operators who did it last time can still be assigned"
	if s.Reason == pccareapp.RepeatSkipRemovalWindowClosed {
		why = "it is too late to remove feed and water the evening before"
	}
	if s.Rotation {
		title := work + " rotation stopped · " + pens
		body := fmt.Sprintf("The %s rotation was due to reach %s on %s but stopped: %s. Plan %s with someone who can do it and the rotation carries on from there.", strings.ToLower(work), where, date, why, pens)
		return title, body
	}
	title := work + " not repeated · " + pens
	body := fmt.Sprintf("%s for %s was due again on %s but was not planned: %s. Please plan it with someone who can do it.", work, where, date, why)
	return title, body
}

// NotifyRepeatSkipped queues the push to the planner's devices; no device is a loud log, never
// a fallback recipient.
func (n *PCCareRepeatSkippedNotifier) NotifyRepeatSkipped(ctx context.Context, tenantID string, s pccareapp.RepeatSkip) error {
	if n == nil || n.recipients == nil || n.queue == nil {
		return nil
	}
	if strings.TrimSpace(s.PlannerUserID) == "" {
		return nil
	}
	devices, err := n.recipients.ResolveMemberRecipients(ctx, tenantID, s.PlannerUserID)
	if err != nil {
		return fmt.Errorf("pc care repeat notification: resolve planner: %w", err)
	}
	recipients := dedupeQueueRecipients(toQueueRecipients(devices, roleLabelPCCarePlanner))
	if len(recipients) == 0 {
		if n.logger != nil {
			n.logger.WarnContext(ctx, "pc_care_repeat_skipped_no_recipients",
				"tenant_id", tenantID, "planner", s.PlannerUserID, "category", s.Category, "pens", len(s.PenLabels))
		}
		return nil
	}
	title, body := PCCareRepeatSkippedCopy(s)
	due := s.DueDate.Format("2006-01-02")
	eventKey := fmt.Sprintf("pc_care.repeat_skipped:%s:%s:%s:%s", s.Category, s.ParkID, due, strings.Join(s.PenLabels, ","))
	href := "/pc/" + strings.ReplaceAll(s.Category, "_", "-")
	if _, err := n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  eventKey,
		TargetType:       "pc_care_task",
		NotificationType: NotificationTypePCCareRepeatSkipped,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":          NotificationTypePCCareRepeatSkipped,
			"message_key":   "pc_care.repeat_skipped",
			"screen":        "pc_care",
			"href":          href,
			"target":        href,
			"category":      s.Category,
			"park_id":       s.ParkID,
			"park_name":     s.ParkName,
			"business_date": due,
			"priority":      priorityHigh,
			"group_key":     "pc_care_repeat:" + tenantID,
		},
		Recipients: recipients,
	}); err != nil {
		return fmt.Errorf("pc care repeat notification: queue: %w", err)
	}
	return nil
}
