package notificationbridge

import (
	"context"
	"strings"
	"testing"
	"time"

	proutdomain "github.com/vgoats/goatos/backend/internal/penroutines/domain"
	proutports "github.com/vgoats/goatos/backend/internal/penroutines/ports"
)

// TestPenRoutineDueCopyNamesRoutineParkAndPens pins the day's push against the
// notification-specificity rule: the routine, the park, the count, each pen by its display
// label and the farm-readable date -- and that the push waits for the routine's OWN notify
// time and goes out once per routine per day.
func TestPenRoutineDueCopyNamesRoutineParkAndPens(t *testing.T) {
	digest := proutports.DueDigest{
		RoutineID: "r-1", RoutineName: "Pen cleaning", ParkID: "park-cbe", ParkName: "Coimbatore",
		NotifyTime: "07:00", AssigneeIDs: []string{"u-head", "u-second"}, DueDate: "2026-09-16",
		Tasks: []proutdomain.Task{
			{TaskID: "task-1", PenLabel: "Castro 2"},
			{TaskID: "task-2", PenLabel: "Godel 1 - Part 3"},
		},
	}
	title, body := PenRoutineDueCopy(digest)
	if title != "Pen cleaning: 2 pens at Coimbatore" {
		t.Fatalf("title = %q", title)
	}
	for _, want := range []string{"Coimbatore", "Castro 2", "Godel 1 - Part 3", "16/09/2026", "submit"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body %q must name %q", body, want)
		}
	}
	ist, _ := time.LoadLocation("Asia/Kolkata")
	queue := &penVisitQueueFake{}
	recipients := &penVisitRecipientsFake{}
	n := NewPenRoutineDueNotifier(recipients, queue, nil)

	// 06:30 IST: before the routine's notify time, nothing is queued.
	n.WithClock(func() time.Time { return time.Date(2026, 9, 16, 6, 30, 0, 0, ist) })
	if err := n.NotifyDue(context.Background(), "tenant-1", []proutports.DueDigest{digest}); err != nil || len(queue.queued) != 0 {
		t.Fatalf("before notify time must queue nothing: %v / %d", err, len(queue.queued))
	}
	// 07:05 IST: queued once, to EVERY assignee's devices, keyed on routine + business date.
	n.WithClock(func() time.Time { return time.Date(2026, 9, 16, 7, 5, 0, 0, ist) })
	if err := n.NotifyDue(context.Background(), "tenant-1", []proutports.DueDigest{digest}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(queue.queued) != 1 || len(recipients.asked) != 2 || recipients.asked[0] != "u-head" || recipients.asked[1] != "u-second" {
		t.Fatalf("queued %d, asked %v", len(queue.queued), recipients.asked)
	}
	q := queue.queued[0]
	if q.NotificationType != NotificationTypePenRoutineDue || q.EventKey != "pen_routine.due:2026-09-16:r-1" || q.Context["href"] != "/pen-routines" || q.Context["pen_count"] != "2" {
		t.Fatalf("queued = %+v", q)
	}
	if len(q.Recipients) != 2 || q.Recipients[0].FCMToken != "tok-u-head" || q.Recipients[1].FCMToken != "tok-u-second" {
		t.Fatalf("recipients = %+v", q.Recipients)
	}
	// A digest for another day (a stale row) is never pushed as today's.
	stale := digest
	stale.DueDate = "2026-09-15"
	if err := n.NotifyDue(context.Background(), "tenant-1", []proutports.DueDigest{stale}); err != nil || len(queue.queued) != 1 {
		t.Fatalf("stale digest must queue nothing: %v / %d", err, len(queue.queued))
	}
}

// TestPenRoutineDueCopyForAWholeParkTaskNamesThePark pins the general-task push (2026-09-17
// revision): a whole-park routine owes no pens, so the push names the routine, the park and the
// date and never counts "1 pen".
func TestPenRoutineDueCopyForAWholeParkTaskNamesThePark(t *testing.T) {
	digest := proutports.DueDigest{
		RoutineID: "r-2", RoutineName: "Medicine store", ParkID: "park-cbe", ParkName: "Coimbatore",
		NotifyTime: "07:00", AssigneeIDs: []string{"u-cxo"}, DueDate: "2026-09-16",
		Tasks: []proutdomain.Task{{TaskID: "task-9", ScopeKind: proutdomain.ScopePark}},
	}
	title, body := PenRoutineDueCopy(digest)
	if title != "Medicine store: due at Coimbatore" {
		t.Fatalf("title = %q", title)
	}
	for _, want := range []string{"Coimbatore", "16/09/2026", "whole", "submit"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body %q must name %q", body, want)
		}
	}
	if strings.Contains(title+" "+body, " pen") {
		t.Fatalf("a whole-park push must not speak of pens: %q / %q", title, body)
	}
}
