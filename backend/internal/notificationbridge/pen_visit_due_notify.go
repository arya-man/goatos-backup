package notificationbridge

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	penvisitdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
	penvisitports "github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// NotificationTypePenVisitDue is the morning push naming the pens a park head owes today
// (maintainer decision 2026-09-07). One push per PARK per business date, never one per pen: a
// round of ten pens is one morning's work and one message.
const NotificationTypePenVisitDue = "pen_visit_due"

const roleLabelParkHead = "park_head"

// PenVisitDueNotifier pushes the digest of visits the materializer just created. It is called
// by the kernel stage with what THAT pass created, so a replay tick (which creates nothing)
// pushes nothing, and the business-date event key makes the write idempotent besides.
type PenVisitDueNotifier struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
}

// NewPenVisitDueNotifier wires the notifier.
func NewPenVisitDueNotifier(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *PenVisitDueNotifier {
	return &PenVisitDueNotifier{recipients: recipients, queue: queue, logger: logger, now: time.Now}
}

// WithClock pins the clock, for tests.
func (n *PenVisitDueNotifier) WithClock(now func() time.Time) *PenVisitDueNotifier {
	n.now = now
	return n
}

// NotifyCreated queues one push per digest key (one park, one due date), to EVERY person the
// park's HRMS config names as a visitor (maintainer decision 2026-09-12: one or more per park,
// any one of them recording is enough).
func (n *PenVisitDueNotifier) NotifyCreated(ctx context.Context, tenantID string, digests []penvisitports.CreatedDigest) error {
	if n == nil || n.recipients == nil || n.queue == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("pen visit notification: tenant id is required")
	}
	digests = mergePenVisitDueDigests(digests)
	for _, d := range digests {
		if len(d.Tasks) == 0 || len(d.VisitorIDs) == 0 {
			continue
		}
		recipients := []calendarports.NotificationRecipient{}
		// Bounded by the park's configured visitors -- a handful of people, never by pens.
		// scale-guard:ignore: bounded per-visitor recipient resolution, one read per configured park visitor (a handful of people per park).
		for _, visitorID := range d.VisitorIDs {
			devices, err := n.recipients.ResolveMemberRecipients(ctx, tenantID, visitorID)
			if err != nil {
				return fmt.Errorf("pen visit notification: resolve visitor: %w", err)
			}
			recipients = append(recipients, toQueueRecipients(devices, roleLabelParkHead)...)
		}
		recipients = dedupeQueueRecipients(recipients)
		if len(recipients) == 0 {
			// Loud, and no fallback: a visit nobody is told about must not look announced.
			if n.logger != nil {
				n.logger.WarnContext(ctx, "pen_visit_due_notification_no_recipients",
					"tenant_id", tenantID, "park_id", d.ParkID, "visitors", len(d.VisitorIDs), "pens", len(d.Tasks))
			}
			continue
		}
		title, body := PenVisitDueCopy(d)
		eventKey := fmt.Sprintf("pen_visit.due:%s:%s:%s", d.DueDate, d.ParkID, penVisitDigestBatchKey(d.Tasks))
		// One write per PARK that gained visits on this pass: the farm has two parks, so this loop
		// is bounded by the park count, never by pens or animals.
		// scale-guard:ignore: bounded per-park digest loop, one queue write per park with new visits (two parks on the farm).
		if _, err := n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  eventKey,
			TargetType:       "pen_visit",
			NotificationType: NotificationTypePenVisitDue,
			Channel:          channelPushFCM,
			Priority:         priorityHigh,
			Title:            title,
			Body:             body,
			TraceID:          eventKey,
			EventKey:         eventKey,
			Context: map[string]string{
				"type":          NotificationTypePenVisitDue,
				"message_key":   "pen_visit.due",
				"screen":        "pen_visits",
				"href":          "/pen-visits",
				"target":        "/pen-visits",
				"park_id":       d.ParkID,
				"park_name":     d.ParkName,
				"pen_count":     fmt.Sprintf("%d", len(d.Tasks)),
				"business_date": d.DueDate,
				"priority":      priorityHigh,
				"group_key":     "pen_visits:" + tenantID,
				"collapse_key":  "pen_visits:" + tenantID + ":" + d.ParkID + ":" + d.DueDate,
			},
			Recipients: recipients,
		}); err != nil {
			return fmt.Errorf("pen visit notification: queue %s: %w", d.ParkID, err)
		}
	}
	return nil
}

func mergePenVisitDueDigests(digests []penvisitports.CreatedDigest) []penvisitports.CreatedDigest {
	merged := map[string]*penvisitports.CreatedDigest{}
	order := make([]string, 0, len(digests))
	for _, d := range digests {
		key := d.DueDate + "|" + d.ParkID
		out, ok := merged[key]
		if !ok {
			copyDigest := d
			copyDigest.Tasks = nil
			out = &copyDigest
			merged[key] = out
			order = append(order, key)
		}
		if out.ParkName == "" {
			out.ParkName = d.ParkName
		}
		out.Tasks = append(out.Tasks, d.Tasks...)
	}
	out := make([]penvisitports.CreatedDigest, 0, len(order))
	for _, key := range order {
		out = append(out, *merged[key])
	}
	return out
}

func penVisitDigestBatchKey(tasks []penvisitdomain.Task) string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		id := strings.TrimSpace(task.TaskID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "no-task-ids"
	}
	sort.Strings(ids)
	sum := sha1.Sum([]byte(strings.Join(ids, ",")))
	return hex.EncodeToString(sum[:])[:12]
}

// PenVisitDueCopy composes the push: which park, how many pens, which pens and why, and the day
// -- every fact the notification-specificity rule requires, in farm words.
func PenVisitDueCopy(d penvisitports.CreatedDigest) (title, body string) {
	park := strings.TrimSpace(d.ParkName)
	if park == "" {
		park = "your park"
	}
	count := len(d.Tasks)
	noun := "pens"
	if count == 1 {
		noun = "pen"
	}
	due := biztime.FarmDateFromBusinessDate(d.DueDate)
	title = fmt.Sprintf("Visit %d %s at %s", count, noun, park)
	parts := make([]string, 0, count)
	for i, t := range d.Tasks {
		if i == 4 && count > 5 {
			parts = append(parts, fmt.Sprintf("and %d more", count-4))
			break
		}
		reasons := make([]string, 0, len(t.Reasons))
		for _, r := range penvisitdomain.SortReasons(t.Reasons) {
			reasons = append(reasons, strings.ToLower(penvisitdomain.ReasonLabel(r)))
		}
		pen := strings.TrimSpace(t.PenLabel)
		if pen == "" {
			pen = "a pen"
		}
		if len(reasons) == 0 {
			parts = append(parts, pen)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", pen, strings.Join(reasons, ", ")))
	}
	body = fmt.Sprintf("Due %s at %s: %s. Open each pen's card, record one video and it submits itself.",
		due, park, strings.Join(parts, ", "))
	return title, body
}
