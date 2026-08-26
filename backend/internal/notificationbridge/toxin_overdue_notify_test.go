package notificationbridge

import (
	"context"
	"strings"
	"testing"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	toxindomain "github.com/vgoats/goatos/backend/internal/toxin/domain"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

type stubOverdueTasks struct {
	tasks []toxindomain.Task
	asOf  time.Time
}

func (s *stubOverdueTasks) UnstartedOverdueTasks(_ context.Context, _ string, now time.Time) ([]toxindomain.Task, error) {
	s.asOf = now
	return s.tasks, nil
}

type stubTesters struct{ ids []string }

func (s *stubTesters) ToxinTesterUserIDs(context.Context, string) ([]string, error) {
	return s.ids, nil
}

type stubRecipients struct{ members, positions int }

func (s *stubRecipients) ResolveModuleDutyRecipients(context.Context, string, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}
func (s *stubRecipients) ResolveMemberRecipients(_ context.Context, _, id string) ([]workforcedomain.NotificationRecipient, error) {
	s.members++
	return []workforcedomain.NotificationRecipient{{WorkforceMemberID: id, DeviceID: "d-" + id, FCMToken: "t-" + id}}, nil
}
func (s *stubRecipients) ResolvePositionRecipients(context.Context, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	s.positions++
	return []workforcedomain.NotificationRecipient{{WorkforceMemberID: "ceo", DeviceID: "d-ceo", FCMToken: "t-ceo"}}, nil
}

type stubQueue struct {
	calls []calendarports.QueueRoleNotifications
}

func (s *stubQueue) QueueRoleNotifications(_ context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	s.calls = append(s.calls, in)
	return len(in.Recipients), nil
}

func overdueTask() toxindomain.Task {
	return toxindomain.Task{
		TaskID:         "11111111-1111-4111-8111-111111111111",
		FeedPurchaseID: "22222222-2222-4222-8222-222222222222",
		FeedItemLabel:  "Maize",
		Vendor:         "Kamadhenu Feeds (P) Limited",
		FarmLabel:      "CBE",
		BatchNo:        12,
		PurchaseDate:   "2026-08-25",
		QuantityKg:     4200,
		Status:         toxindomain.StatusInProgress,
		CreatedAt:      time.Date(2026, 8, 25, 18, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
	}
}

// TestOverdueReminderNamesTheLoad is the notification-specificity rule made concrete: a reminder
// that says "1 toxin test is overdue" tells nobody which bag of feed to go and test. Every fact a
// reader needs to act must be in the message.
func TestOverdueReminderNamesTheLoad(t *testing.T) {
	now := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	queue := &stubQueue{}
	n := NewToxinOverdueNotifier(
		&stubOverdueTasks{tasks: []toxindomain.Task{overdueTask()}},
		&stubTesters{ids: []string{"u-dinakar", "u-chandrakant"}},
		&stubRecipients{}, queue, nil,
	).WithClock(func() time.Time { return now })

	if err := n.NotifyOverdue(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(queue.calls) != 1 {
		t.Fatalf("queued %d messages, want 1", len(queue.calls))
	}
	msg := queue.calls[0]
	for _, fact := range []string{"Maize", "CBE", "Kamadhenu Feeds", "batch 12", "4200 kg", "15 hours"} {
		if !strings.Contains(msg.Title+" "+msg.Body, fact) {
			t.Fatalf("message omits %q; title=%q body=%q", fact, msg.Title, msg.Body)
		}
	}
	if msg.NotificationType != NotificationTypeToxinOverdue {
		t.Fatalf("notification type = %q", msg.NotificationType)
	}
	// Deep link to the task itself, not a module landing page.
	if href := msg.Context["href"]; !strings.HasSuffix(href, overdueTask().TaskID) {
		t.Fatalf("href = %q, want the task's own route", href)
	}
}

// TestReminderIsOncePerDayPerTask pins the property that replaces a scheduler: the idempotency key
// carries the BUSINESS DATE, so a second tick the same day writes nothing while the next day is a
// fresh key. Without it, riding the shared cadence would send one message per tick.
func TestReminderIsOncePerDayPerTask(t *testing.T) {
	// Instants chosen in IST, which is the farm's business day and what the key is keyed on.
	// 20:00 UTC would be 01:30 IST the NEXT morning — a genuinely different business day — so
	// picking it here would have asserted the opposite of the rule.
	ist := time.FixedZone("IST", 5*3600+1800)
	morning := time.Date(2026, 8, 26, 9, 0, 0, 0, ist)
	evening := time.Date(2026, 8, 26, 20, 0, 0, 0, ist)
	tomorrow := time.Date(2026, 8, 27, 9, 0, 0, 0, ist)

	keyAt := func(at time.Time) string {
		queue := &stubQueue{}
		n := NewToxinOverdueNotifier(
			&stubOverdueTasks{tasks: []toxindomain.Task{overdueTask()}},
			&stubTesters{ids: []string{"u-1"}}, &stubRecipients{}, queue, nil,
		).WithClock(func() time.Time { return at })
		if err := n.NotifyOverdue(context.Background(), "tenant-1"); err != nil {
			t.Fatalf("notify: %v", err)
		}
		return queue.calls[0].EventKey
	}
	if keyAt(morning) != keyAt(evening) {
		t.Fatal("two ticks on the same day produced different keys; the reminder would send twice")
	}
	if keyAt(morning) == keyAt(tomorrow) {
		t.Fatal("the next day reused the key; a load still unstarted would never be chased again")
	}
}

// TestTheAudienceIsTheGrantHoldersPlusCEO: toxin testing is granted per PERSON, so the reminder
// resolves the grant holders rather than a job title. A future holder of either director seat must
// not inherit the message by sitting in the chair.
func TestTheAudienceIsTheGrantHoldersPlusCEO(t *testing.T) {
	queue := &stubQueue{}
	recipients := &stubRecipients{}
	n := NewToxinOverdueNotifier(
		&stubOverdueTasks{tasks: []toxindomain.Task{overdueTask()}},
		&stubTesters{ids: []string{"u-dinakar", "u-chandrakant"}},
		recipients, queue, nil,
	).WithClock(func() time.Time { return time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC) })

	if err := n.NotifyOverdue(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if recipients.members != 2 {
		t.Fatalf("resolved %d tester(s) by grant, want 2", recipients.members)
	}
	if recipients.positions != 1 {
		t.Fatalf("resolved %d position seat(s), want the CEO office once", recipients.positions)
	}
	roles := map[string]bool{}
	for _, r := range queue.calls[0].Recipients {
		roles[r.RoleLabel] = true
	}
	if !roles[roleLabelToxinTester] || !roles[roleLabelCEO] {
		t.Fatalf("audience roles = %v, want both the testers and the CEO office", roles)
	}
}

// TestNothingOverdueSendsNothing — the quiet path must stay quiet, and must not resolve a roster.
func TestNothingOverdueSendsNothing(t *testing.T) {
	queue := &stubQueue{}
	recipients := &stubRecipients{}
	n := NewToxinOverdueNotifier(&stubOverdueTasks{}, &stubTesters{ids: []string{"u-1"}}, recipients, queue, nil)
	if err := n.NotifyOverdue(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(queue.calls) != 0 || recipients.members != 0 {
		t.Fatalf("quiet run queued %d and resolved %d recipients", len(queue.calls), recipients.members)
	}
}
