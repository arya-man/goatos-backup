package notificationbridge_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// WHAT HAPPENS ON MY TASK REACHES ME (maintainer instruction 2026-09-18). These tests hold the
// three rules the consumer exists for: the actor is never told about their own act, one person
// gets one push per event even when they are both a party and mentioned, and a replayed event
// adds nothing.
//
// The queue fake below models the ONE property the real calendar write gives us -- its insert
// is ON CONFLICT DO NOTHING per (tenant, event key, recipient device) -- so "how many rows
// would exist" is answered here the same way the database answers it.

const (
	ltaTenant   = "11111111-1111-4111-8111-111111111111"
	ltaTask     = "22222222-2222-4222-8222-222222222222"
	ltaRaiser   = "33333333-3333-4333-8333-333333333333"
	ltaAssignee = "44444444-4444-4444-8444-444444444444"
	ltaMonitor  = "55555555-5555-4555-8555-555555555555"
	ltaNote     = "66666666-6666-4666-8666-666666666666"
)

// activityRecipients gives each person their OWN device, so two people can never collapse into
// one recipient row by accident -- which is what makes the dedupe assertions meaningful.
type activityRecipients struct{ asks []string }

func (r *activityRecipients) ResolveModuleDutyRecipients(context.Context, string, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}

func (r *activityRecipients) ResolveMemberRecipients(_ context.Context, _ string, memberID string) ([]workforcedomain.NotificationRecipient, error) {
	r.asks = append(r.asks, memberID)
	return []workforcedomain.NotificationRecipient{{
		WorkforceMemberID: memberID,
		DeviceID:          "device-" + memberID,
		FCMToken:          "token-" + memberID,
	}}, nil
}

func (r *activityRecipients) ResolvePositionRecipients(context.Context, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}

// activityQueue records one ROW per (event key, device), refusing a duplicate exactly as the
// real notification_requests insert does.
type activityQueue struct {
	calls []calendarports.QueueRoleNotifications
	rows  map[string]calendarports.QueueRoleNotifications
}

func newActivityQueue() *activityQueue {
	return &activityQueue{rows: map[string]calendarports.QueueRoleNotifications{}}
}

func (q *activityQueue) QueueRoleNotifications(_ context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	q.calls = append(q.calls, in)
	written := 0
	for _, recipient := range in.Recipients {
		key := in.TenantID + "|" + in.EventKey + "|" + recipient.DeviceID
		if _, exists := q.rows[key]; exists {
			continue
		}
		q.rows[key] = in
		written++
	}
	return written, nil
}

// rowsFor lists the notification types written for one person, across every event key.
func (q *activityQueue) rowsFor(userID string) []string {
	out := make([]string, 0, 2)
	for key, in := range q.rows {
		if strings.HasSuffix(key, "|device-"+userID) {
			out = append(out, in.NotificationType)
		}
	}
	return out
}

func activityPayload(eventType string, overrides map[string]any) []byte {
	payload := map[string]any{
		"task_id":            ltaTask,
		"task_no":            12,
		"title":              "Approve the CPT feed vendor contract",
		"status":             "open",
		"raised_by_user_id":  ltaRaiser,
		"raised_by_name":     "Hemant",
		"assignee_user_id":   ltaAssignee,
		"assignee_name":      "Ravi",
		"changed_by_user_id": ltaRaiser,
		"occurred_at":        "2026-09-18T05:30:00Z",
	}
	if eventType == notificationbridge.EventLeadershipTaskCommented {
		payload["note_id"] = ltaNote
		payload["note_excerpt"] = "The vendor wants the rate fixed for six months"
	}
	for k, v := range overrides {
		payload[k] = v
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func handleActivity(t *testing.T, consumer *notificationbridge.LeadershipTaskActivityNotifyConsumer, eventType, eventID string, overrides map[string]any) {
	t.Helper()
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:       eventID,
		Type:     eventType,
		TenantID: ltaTenant,
		Payload:  activityPayload(eventType, overrides),
	}); err != nil {
		t.Fatalf("HandleEvent(%s): %v", eventType, err)
	}
}

// A NOTE REACHES THE OTHER PARTY, NEVER ITS WRITER.
func TestLeadershipTaskNoteReachesTheCounterpartyAndNeverItsAuthor(t *testing.T) {
	recipients := &activityRecipients{}
	queue := newActivityQueue()
	consumer := notificationbridge.NewLeadershipTaskActivityNotifyConsumer(recipients, queue, slog.Default())

	handleActivity(t, consumer, notificationbridge.EventLeadershipTaskCommented, "evt-note-1", nil)

	if got := queue.rowsFor(ltaAssignee); len(got) != 1 || got[0] != notificationbridge.NotificationTypeLeadershipTaskCommented {
		t.Fatalf("the assignee must be told the raiser wrote a note; rows=%v", got)
	}
	if got := queue.rowsFor(ltaRaiser); len(got) != 0 {
		t.Fatalf("the raiser WROTE the note and must not be told about it; rows=%v", got)
	}
	n := queue.calls[0]
	for _, want := range []string{"#12", "Approve the CPT feed vendor contract", "Hemant", "18/09/2026", "The vendor wants the rate fixed for six months"} {
		if !strings.Contains(n.Body, want) {
			t.Fatalf("body %q is missing %q -- a note push must say WHAT was written", n.Body, want)
		}
	}
	if n.Context["task_id"] != ltaTask || !strings.Contains(n.Context["href"], ltaTask) {
		t.Fatalf("push must deep-link to the task; context=%v", n.Context)
	}
}

// THE SAME RULE FROM THE OTHER SIDE: the assignee writes, the raiser hears.
func TestLeadershipTaskNoteByTheAssigneeReachesTheRaiser(t *testing.T) {
	recipients := &activityRecipients{}
	queue := newActivityQueue()
	consumer := notificationbridge.NewLeadershipTaskActivityNotifyConsumer(recipients, queue, slog.Default())

	handleActivity(t, consumer, notificationbridge.EventLeadershipTaskCommented, "evt-note-2",
		map[string]any{"changed_by_user_id": ltaAssignee})

	if got := queue.rowsFor(ltaRaiser); len(got) != 1 {
		t.Fatalf("the raiser must hear the assignee's note; rows=%v", got)
	}
	if got := queue.rowsFor(ltaAssignee); len(got) != 0 {
		t.Fatalf("the assignee wrote it; rows=%v", got)
	}
}

// AN UPDATE TO THE BRIEF REACHES THE OTHER PARTY, NEVER THE EDITOR.
func TestLeadershipTaskUpdateReachesTheCounterpartyAndNeverTheEditor(t *testing.T) {
	recipients := &activityRecipients{}
	queue := newActivityQueue()
	consumer := notificationbridge.NewLeadershipTaskActivityNotifyConsumer(recipients, queue, slog.Default())

	handleActivity(t, consumer, notificationbridge.EventLeadershipTaskUpdated, "evt-edit-1", nil)

	if got := queue.rowsFor(ltaAssignee); len(got) != 1 || got[0] != notificationbridge.NotificationTypeLeadershipTaskUpdated {
		t.Fatalf("the assignee must be told the ask changed; rows=%v", got)
	}
	if got := queue.rowsFor(ltaRaiser); len(got) != 0 {
		t.Fatalf("the raiser made the change; rows=%v", got)
	}
}

// ONE PERSON, ONE PUSH: a party who is ALSO named in the note gets the mention only.
func TestLeadershipTaskMentionedPartyGetsExactlyOneNotification(t *testing.T) {
	recipients := &activityRecipients{}
	queue := newActivityQueue()
	consumer := notificationbridge.NewLeadershipTaskActivityNotifyConsumer(recipients, queue, slog.Default())

	// The raiser writes a note naming the assignee -- who is both the counterparty AND
	// mentioned. A monitor is named too: mentioned, party to nothing.
	handleActivity(t, consumer, notificationbridge.EventLeadershipTaskCommented, "evt-note-3",
		map[string]any{"mentioned_user_ids": []string{ltaAssignee, ltaMonitor}})

	got := queue.rowsFor(ltaAssignee)
	if len(got) != 1 {
		t.Fatalf("a mentioned party owes exactly ONE notification, got %v", got)
	}
	if got[0] != notificationbridge.NotificationTypeLeadershipTaskMentioned {
		t.Fatalf("the mention is the sharper copy and must win the overlap; got %q", got[0])
	}
	if mon := queue.rowsFor(ltaMonitor); len(mon) != 1 || mon[0] != notificationbridge.NotificationTypeLeadershipTaskMentioned {
		t.Fatalf("a mentioned non-party must be told they were named; rows=%v", mon)
	}
	if raiser := queue.rowsFor(ltaRaiser); len(raiser) != 0 {
		t.Fatalf("the author of the note is never notified, even by their own mention list; rows=%v", raiser)
	}
}

// MENTIONING YOURSELF IS NOT NEWS.
func TestLeadershipTaskSelfMentionNotifiesNobody(t *testing.T) {
	recipients := &activityRecipients{}
	queue := newActivityQueue()
	consumer := notificationbridge.NewLeadershipTaskActivityNotifyConsumer(recipients, queue, slog.Default())

	handleActivity(t, consumer, notificationbridge.EventLeadershipTaskCommented, "evt-note-4",
		map[string]any{"mentioned_user_ids": []string{ltaRaiser}})

	if got := queue.rowsFor(ltaRaiser); len(got) != 0 {
		t.Fatalf("a self-mention must notify nobody; rows=%v", got)
	}
	for _, call := range queue.calls {
		if call.NotificationType == notificationbridge.NotificationTypeLeadershipTaskMentioned {
			t.Fatalf("no mention push should have been composed at all; call=%+v", call)
		}
	}
}

// A REPLAYED EVENT ADDS NOTHING. Both events are replayed with the SAME outbox identity, which
// is what at-least-once delivery actually does.
func TestLeadershipTaskActivityReplayDoesNotDoubleNotify(t *testing.T) {
	recipients := &activityRecipients{}
	queue := newActivityQueue()
	consumer := notificationbridge.NewLeadershipTaskActivityNotifyConsumer(recipients, queue, slog.Default())

	for i := 0; i < 3; i++ {
		handleActivity(t, consumer, notificationbridge.EventLeadershipTaskCommented, "evt-note-5",
			map[string]any{"mentioned_user_ids": []string{ltaMonitor}})
		handleActivity(t, consumer, notificationbridge.EventLeadershipTaskUpdated, "evt-edit-5", nil)
	}
	if got := queue.rowsFor(ltaAssignee); len(got) != 2 {
		t.Fatalf("the assignee owes ONE note row and ONE update row however often the events replay; rows=%v", got)
	}
	if got := queue.rowsFor(ltaMonitor); len(got) != 1 {
		t.Fatalf("the mentioned person owes exactly one row however often the note event replays; rows=%v", got)
	}
	if got := queue.rowsFor(ltaRaiser); len(got) != 0 {
		t.Fatalf("the actor stays unnotified across replays; rows=%v", got)
	}
}

// A NOTE-LESS COMMENT CALL (an older phone clearing the assignee field) is not news, and must
// not queue a row keyed on an empty note.
func TestLeadershipTaskCommentWithoutANoteQueuesNothing(t *testing.T) {
	recipients := &activityRecipients{}
	queue := newActivityQueue()
	consumer := notificationbridge.NewLeadershipTaskActivityNotifyConsumer(recipients, queue, slog.Default())

	handleActivity(t, consumer, notificationbridge.EventLeadershipTaskCommented, "evt-note-6",
		map[string]any{"note_id": "", "note_excerpt": ""})

	if len(queue.calls) != 0 {
		t.Fatalf("nothing was written on the task, so nothing is owed; calls=%+v", queue.calls)
	}
}
