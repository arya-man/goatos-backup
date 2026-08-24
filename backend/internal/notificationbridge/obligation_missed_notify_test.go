package notificationbridge

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

type missedContextFake struct {
	ctx calendarports.MissedObligationContext
	err error
}

func (f *missedContextFake) ResolveMissedObligationContext(context.Context, string, string) (calendarports.MissedObligationContext, error) {
	return f.ctx, f.err
}

// missedRecipientsFake resolves devices the way the real roster does: by member for the operator,
// by position for the park head (park scope) and the module's director (tenant scope).
type missedRecipientsFake struct {
	byMember   map[string][]workforcedomain.NotificationRecipient
	byPosition map[string][]workforcedomain.NotificationRecipient // key: scopeType|scopeID|positionCode
	asked      []string
}

func (f *missedRecipientsFake) ResolveModuleDutyRecipients(context.Context, string, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}

func (f *missedRecipientsFake) ResolveMemberRecipients(_ context.Context, _, memberOrUserID string) ([]workforcedomain.NotificationRecipient, error) {
	f.asked = append(f.asked, "member:"+memberOrUserID)
	return f.byMember[memberOrUserID], nil
}

func (f *missedRecipientsFake) ResolvePositionRecipients(_ context.Context, _, scopeType, scopeID, positionCode string) ([]workforcedomain.NotificationRecipient, error) {
	key := scopeType + "|" + scopeID + "|" + positionCode
	f.asked = append(f.asked, "position:"+key)
	return f.byPosition[key], nil
}

type missedQueueFake struct {
	queued []calendarports.QueueRoleNotifications
}

func (q *missedQueueFake) QueueRoleNotifications(_ context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	q.queued = append(q.queued, in)
	return len(in.Recipients), nil
}

const (
	missedTenant     = "00000000-0000-4000-8000-000000000001"
	missedPark       = "00000000-0000-4000-8000-000000003001"
	missedObligation = "86000000-0000-4000-8000-000000000001"
	missedOperator   = "e1000000-0000-4000-8000-000000000001"
)

func newMissedFixture() (*missedContextFake, *missedRecipientsFake, *missedQueueFake, *ObligationMissedNotifier) {
	contextFake := &missedContextFake{ctx: calendarports.MissedObligationContext{
		ObligationID: missedObligation,
		Module:       "vaccination",
		DueAt:        time.Date(2026, 7, 24, 4, 30, 0, 0, time.UTC), // 10:00 IST on 2026-07-24
		ParkID:       missedPark,
		ShedID:       "00000000-0000-4000-8000-000000004001",
		ShedLabel:    "Gandhi 1",
		OperatorID:   missedOperator,
	}}
	recipients := &missedRecipientsFake{
		byMember: map[string][]workforcedomain.NotificationRecipient{
			missedOperator: {{WorkforceMemberID: missedOperator, DeviceID: "d-op", FCMToken: "fcm-operator"}},
		},
		byPosition: map[string][]workforcedomain.NotificationRecipient{
			"center|" + missedPark + "|park_head":     {{WorkforceMemberID: "m-ph", DeviceID: "d-ph", FCMToken: "fcm-parkhead"}},
			"tenant|" + missedTenant + "|pc_director": {{WorkforceMemberID: "m-dir", DeviceID: "d-dir", FCMToken: "fcm-pc-director"}},
			// Another module's director is deliberately reachable in the fake; routing must NOT pick him.
			"tenant|" + missedTenant + "|growth_director": {{WorkforceMemberID: "m-gd", DeviceID: "d-gd", FCMToken: "fcm-growth-director"}},
		},
	}
	queue := &missedQueueFake{}
	return contextFake, recipients, queue, NewObligationMissedNotifier(contextFake, recipients, queue, nil)
}

func messagesByScreen(queued []calendarports.QueueRoleNotifications, screen string) *calendarports.QueueRoleNotifications {
	for i := range queued {
		if queued[i].Context["screen"] == screen {
			return &queued[i]
		}
	}
	return nil
}

func sortedTokens(in calendarports.QueueRoleNotifications) []string {
	tokens := make([]string, 0, len(in.Recipients))
	for _, recipient := range in.Recipients {
		tokens = append(tokens, recipient.FCMToken)
	}
	sort.Strings(tokens)
	return tokens
}

// TestObligationMissedNotifierRoutesDownAndUp is the routing guard: the miss goes DOWN to the
// operator whose work it was and UP to the park head plus the OWNING module's director -- in
// vaccination's own words, at high priority, with a tap route that opens the missed work.
func TestObligationMissedNotifierRoutesDownAndUp(t *testing.T) {
	_, _, queue, notifier := newMissedFixture()

	if err := notifier.NotifyObligationMissed(context.Background(), missedTenant, missedObligation); err != nil {
		t.Fatalf("NotifyObligationMissed: %v", err)
	}
	if len(queue.queued) != 2 {
		t.Fatalf("queued %d messages, want 2 (operator + leadership)", len(queue.queued))
	}

	operatorMsg := messagesByScreen(queue.queued, "vaccination")
	if operatorMsg == nil {
		t.Fatalf("no message routed to the operator's work screen: %+v", queue.queued)
	}
	if got := sortedTokens(*operatorMsg); len(got) != 1 || got[0] != "fcm-operator" {
		t.Fatalf("operator message recipients = %v, want the assigned operator only", got)
	}
	if operatorMsg.Priority != priorityHigh || operatorMsg.Channel != channelPushFCM {
		t.Fatalf("operator message priority/channel = %q/%q, want high/push_fcm", operatorMsg.Priority, operatorMsg.Channel)
	}
	if operatorMsg.NotificationType != NotificationTypeObligationMissed {
		t.Fatalf("operator notification type = %q", operatorMsg.NotificationType)
	}
	// The visible body dates as dd/mm/yyyy; the ISO value lives in the structured context.
	if !strings.Contains(operatorMsg.Body, "Gandhi 1") || !strings.Contains(operatorMsg.Body, "24/07/2026") {
		t.Fatalf("operator body = %q, want the shed and the business date as dd/mm/yyyy", operatorMsg.Body)
	}
	if operatorMsg.Context["due_date"] != "2026-07-24" {
		t.Fatalf("structured due_date = %q, want ISO for clients to parse", operatorMsg.Context["due_date"])
	}

	leadershipMsg := messagesByScreen(queue.queued, "vaccination_overview")
	if leadershipMsg == nil {
		t.Fatalf("no message routed up to leadership: %+v", queue.queued)
	}
	got := sortedTokens(*leadershipMsg)
	want := []string{"fcm-parkhead", "fcm-pc-director"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("leadership recipients = %v, want park head + the vaccination module's director %v", got, want)
	}
	for _, token := range got {
		if token == "fcm-growth-director" {
			t.Fatalf("another module's director was notified about vaccination work: %v", got)
		}
	}
}

// TestObligationMissedNotifierCopyIsFarmLanguage guards the user-visible copy firewall: no internal
// words, no raw config tokens.
func TestObligationMissedNotifierCopyIsFarmLanguage(t *testing.T) {
	_, _, queue, notifier := newMissedFixture()
	if err := notifier.NotifyObligationMissed(context.Background(), missedTenant, missedObligation); err != nil {
		t.Fatalf("NotifyObligationMissed: %v", err)
	}
	banned := []string{"obligation", "payload", "API", "route", "outbox", "localhost", "mock", "debug", "et_tt", "pc.vaccination", "pc_director"}
	for _, message := range queue.queued {
		visible := message.Title + " " + message.Body
		for _, word := range banned {
			if strings.Contains(strings.ToLower(visible), strings.ToLower(word)) {
				t.Fatalf("visible copy %q contains internal word %q", visible, word)
			}
		}
	}
}

// TestObligationMissedNotifierHasNoFallbackProfile: an unclaimed module notifies NOBODY rather than
// borrowing vaccination's director and vaccination's wording.
func TestObligationMissedNotifierHasNoFallbackProfile(t *testing.T) {
	contextFake, _, queue, notifier := newMissedFixture()
	contextFake.ctx.Module = "some_unclaimed_module"

	if err := notifier.NotifyObligationMissed(context.Background(), missedTenant, missedObligation); err != nil {
		t.Fatalf("NotifyObligationMissed: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("unclaimed module queued %d messages, want 0: %+v", len(queue.queued), queue.queued)
	}
}

// TestObligationMissedNotifierStillTellsLeadershipWithoutAnOperator: unplanned work has no drive
// assignment, so there is no operator to tell -- leadership must still hear about it.
func TestObligationMissedNotifierStillTellsLeadershipWithoutAnOperator(t *testing.T) {
	contextFake, _, queue, notifier := newMissedFixture()
	contextFake.ctx.OperatorID = ""

	if err := notifier.NotifyObligationMissed(context.Background(), missedTenant, missedObligation); err != nil {
		t.Fatalf("NotifyObligationMissed: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d messages, want 1 (leadership only)", len(queue.queued))
	}
	if queue.queued[0].Context["screen"] != "vaccination_overview" {
		t.Fatalf("the single message was not the leadership one: %+v", queue.queued[0])
	}
}

// TestObligationMissedNotifierIsIdempotentPerEvent: the durable bus is at-least-once, so both
// messages must carry a stable per-event key the notification write dedups on.
func TestObligationMissedNotifierIsIdempotentPerEvent(t *testing.T) {
	_, _, queue, notifier := newMissedFixture()
	for i := 0; i < 2; i++ {
		if err := notifier.NotifyObligationMissed(context.Background(), missedTenant, missedObligation); err != nil {
			t.Fatalf("NotifyObligationMissed: %v", err)
		}
	}
	if len(queue.queued) != 4 {
		t.Fatalf("queued %d messages over two deliveries, want 4", len(queue.queued))
	}
	if queue.queued[0].EventKey == "" || queue.queued[0].EventKey != queue.queued[2].EventKey {
		t.Fatalf("redelivery changed the idempotency key: %q vs %q", queue.queued[0].EventKey, queue.queued[2].EventKey)
	}
	if queue.queued[0].EventKey == queue.queued[1].EventKey {
		t.Fatalf("the operator and leadership messages share one key, so one would suppress the other: %q", queue.queued[0].EventKey)
	}
}
