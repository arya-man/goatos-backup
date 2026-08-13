package notificationbridge_test

import (
	"context"
	"strings"
	"testing"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestObligationMissedNotifierCopyNamesShedAndDate proves the missed-work push names the actual
// shed and due date (already present before this change) and, with WithLocationNames unattached,
// still degrades honestly rather than silently dropping the "where" clause.
func TestObligationMissedNotifierCopyNamesShedAndDate(t *testing.T) {
	dueAt := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	missed := calendarports.MissedObligationContext{
		ObligationID: "22222222-2222-4222-8222-222222222222",
		Module:       "vaccination",
		DueAt:        dueAt,
		ParkID:       "33333333-3333-4333-8333-333333333333",
		ShedID:       "77777777-7777-4777-8777-777777777777",
		ShedLabel:    "Godel 1 Part 8",
		OperatorID:   "88888888-8888-4888-8888-888888888888",
	}
	resolver := fakeMissedResolver{ctx: missed}
	recipients := fakeCopyRecipients{
		member:   []workforcedomain.NotificationRecipient{{WorkforceMemberID: "op", DeviceID: "op-device", FCMToken: strings.Repeat("a", 80)}},
		position: []workforcedomain.NotificationRecipient{{WorkforceMemberID: "ph", DeviceID: "ph-device", FCMToken: strings.Repeat("b", 80)}},
	}
	queue := &fakeCopyQueue{}
	notifier := notificationbridge.NewObligationMissedNotifier(resolver, recipients, queue, discardLogger())
	notifier = notifier.WithLocationNames(notificationbridge.NewLocationNameResolver(nil))

	if err := notifier.NotifyObligationMissed(context.Background(), "66666666-6666-4666-8666-666666666666", missed.ObligationID); err != nil {
		t.Fatalf("NotifyObligationMissed: %v", err)
	}

	if len(queue.captured) != 2 {
		t.Fatalf("expected operator + leadership notifications, got %d", len(queue.captured))
	}
	for _, c := range queue.captured {
		if !strings.Contains(c.in.Body, "Godel 1 Part 8") {
			t.Errorf("missed-work body must name the shed: %q", c.in.Body)
		}
		if !strings.Contains(c.in.Body, "2026-08-01") {
			t.Errorf("missed-work body must name the business date it was missed on: %q", c.in.Body)
		}
		assertCopyClean(t, "missed-work title", c.in.Title)
		assertCopyClean(t, "missed-work body", c.in.Body)
	}
}
