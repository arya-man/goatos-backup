package notificationbridge

import (
	"context"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	penvisitdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
	penvisitports "github.com/vgoats/goatos/backend/internal/penvisits/ports"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

type penVisitQueueFake struct {
	queued []calendarports.QueueRoleNotifications
}

func (f *penVisitQueueFake) QueueRoleNotifications(_ context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	f.queued = append(f.queued, in)
	return len(in.Recipients), nil
}

type penVisitRecipientsFake struct{ asked []string }

func (f *penVisitRecipientsFake) ResolveModuleDutyRecipients(context.Context, string, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}
func (f *penVisitRecipientsFake) ResolveMemberRecipients(_ context.Context, _, memberOrUserID string) ([]workforcedomain.NotificationRecipient, error) {
	f.asked = append(f.asked, memberOrUserID)
	return []workforcedomain.NotificationRecipient{{WorkforceMemberID: "wm-" + memberOrUserID, DeviceID: "dev-" + memberOrUserID, FCMToken: "tok-" + memberOrUserID}}, nil
}
func (f *penVisitRecipientsFake) ResolvePositionRecipients(context.Context, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}

// TestPenVisitDueCopyNamesParkPensAndReasons pins the morning push against the
// notification-specificity rule: the park, the count, each pen by its display label with why,
// and the farm-readable date -- never an abstract "3 tasks due".
func TestPenVisitDueCopyNamesParkPensAndReasons(t *testing.T) {
	digest := penvisitports.CreatedDigest{
		ParkID: "park-cbe", ParkName: "Coimbatore", AssigneeID: "u-dinakar", DueDate: "2026-09-07",
		Tasks: []penvisitdomain.Task{
			{PenLabel: "Castro 2", Reasons: []string{penvisitdomain.ReasonDeworming, penvisitdomain.ReasonVaccination}},
			{PenLabel: "Godel 1 - Part 3", Reasons: []string{penvisitdomain.ReasonHoofTrimming}},
		},
	}
	title, body := PenVisitDueCopy(digest)
	if title != "Visit 2 pens at Coimbatore" {
		t.Fatalf("title = %q", title)
	}
	for _, want := range []string{"Coimbatore", "Castro 2 (vaccination, deworming)", "Godel 1 - Part 3 (hoof trimming)", "07/09/2026", "For me"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body %q must name %q", body, want)
		}
	}

	// Queued once per park, to the assignee's own devices, keyed on the business date so a
	// replay tick writes nothing new.
	queue := &penVisitQueueFake{}
	recipients := &penVisitRecipientsFake{}
	n := NewPenVisitDueNotifier(recipients, queue, nil)
	if err := n.NotifyCreated(context.Background(), "tenant-1", []penvisitports.CreatedDigest{digest}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(queue.queued) != 1 || len(recipients.asked) != 1 || recipients.asked[0] != "u-dinakar" {
		t.Fatalf("queued %d, asked %v", len(queue.queued), recipients.asked)
	}
	q := queue.queued[0]
	if q.NotificationType != NotificationTypePenVisitDue || q.EventKey != "pen_visit.due:2026-09-07:park-cbe:u-dinakar" || q.Context["href"] != "/pen-visits" {
		t.Fatalf("queued = %+v", q)
	}
	if len(q.Recipients) != 1 || q.Recipients[0].FCMToken != "tok-u-dinakar" {
		t.Fatalf("recipients = %+v", q.Recipients)
	}
	// An empty digest (a replay pass) queues nothing.
	if err := n.NotifyCreated(context.Background(), "tenant-1", nil); err != nil || len(queue.queued) != 1 {
		t.Fatalf("empty digest must queue nothing: %v / %d", err, len(queue.queued))
	}
}
