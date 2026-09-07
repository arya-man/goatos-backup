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
			{TaskID: "task-1", PenLabel: "Castro 2", Reasons: []string{penvisitdomain.ReasonDeworming, penvisitdomain.ReasonVaccination}},
			{TaskID: "task-2", PenLabel: "Godel 1 - Part 3", Reasons: []string{penvisitdomain.ReasonHoofTrimming}},
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
	if q.NotificationType != NotificationTypePenVisitDue || !strings.HasPrefix(q.EventKey, "pen_visit.due:2026-09-07:park-cbe:u-dinakar:") || q.Context["href"] != "/pen-visits" {
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

func TestPenVisitDueNotifierMergesCatchupDigestsBeforeQueueing(t *testing.T) {
	queue := &penVisitQueueFake{}
	recipients := &penVisitRecipientsFake{}
	n := NewPenVisitDueNotifier(recipients, queue, nil)
	digests := []penvisitports.CreatedDigest{
		{
			ParkID: "park-cbe", ParkName: "Coimbatore", AssigneeID: "u-dinakar", DueDate: "2026-09-08",
			Tasks: []penvisitdomain.Task{{TaskID: "task-1", PenLabel: "Castro 1", Reasons: []string{penvisitdomain.ReasonVaccination}}},
		},
		{
			ParkID: "park-cbe", ParkName: "Coimbatore", AssigneeID: "u-dinakar", DueDate: "2026-09-08",
			Tasks: []penvisitdomain.Task{{TaskID: "task-2", PenLabel: "Castro 2", Reasons: []string{penvisitdomain.ReasonDeworming}}},
		},
	}
	if err := n.NotifyCreated(context.Background(), "tenant-1", digests); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d digests, want 1", len(queue.queued))
	}
	q := queue.queued[0]
	if !strings.HasPrefix(q.EventKey, "pen_visit.due:2026-09-08:park-cbe:u-dinakar:") {
		t.Fatalf("event key = %q", q.EventKey)
	}
	if q.Context["pen_count"] != "2" || !strings.Contains(q.Body, "Castro 1") || !strings.Contains(q.Body, "Castro 2") {
		t.Fatalf("merged body/context = %#v / %q", q.Context, q.Body)
	}
}

func TestPenVisitDueNotifierUsesBatchSpecificIdempotencyKey(t *testing.T) {
	queue := &penVisitQueueFake{}
	recipients := &penVisitRecipientsFake{}
	n := NewPenVisitDueNotifier(recipients, queue, nil)
	first := penvisitports.CreatedDigest{
		ParkID: "park-cbe", ParkName: "Coimbatore", AssigneeID: "u-dinakar", DueDate: "2026-09-08",
		Tasks: []penvisitdomain.Task{{TaskID: "task-1", PenLabel: "Castro 1", Reasons: []string{penvisitdomain.ReasonVaccination}}},
	}
	second := first
	second.Tasks = []penvisitdomain.Task{{TaskID: "task-2", PenLabel: "Castro 2", Reasons: []string{penvisitdomain.ReasonDeworming}}}
	if err := n.NotifyCreated(context.Background(), "tenant-1", []penvisitports.CreatedDigest{first}); err != nil {
		t.Fatalf("first notify: %v", err)
	}
	if err := n.NotifyCreated(context.Background(), "tenant-1", []penvisitports.CreatedDigest{second}); err != nil {
		t.Fatalf("second notify: %v", err)
	}
	if len(queue.queued) != 2 {
		t.Fatalf("queued %d notifications, want 2", len(queue.queued))
	}
	if queue.queued[0].EventKey == queue.queued[1].EventKey {
		t.Fatalf("event keys must differ for later-created same-park/day batches: %q", queue.queued[0].EventKey)
	}
}
