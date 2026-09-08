package notificationbridge_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	audiencedomain "github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Leadership Tasks push (maintainer decision 2026-09-04): a raised task reaches the ONE CXO
// it is addressed to, naming who asked, the task number and title, the attachment count
// and the date; a task marked done reaches the ONE director who asked. Every other
// transition is silent.

func leadershipTaskPayload(overrides map[string]any) []byte {
	payload := map[string]any{
		"task_id":            "22222222-2222-4222-8222-222222222222",
		"task_no":            12,
		"title":              "Approve the CPT feed vendor contract",
		"status":             "open",
		"previous_status":    "",
		"raised_by_user_id":  "33333333-3333-4333-8333-333333333333",
		"raised_by_name":     "Hemant",
		"assignee_user_id":   "44444444-4444-4444-8444-444444444444",
		"assignee_name":      "Ravi",
		"attachment_count":   2,
		"changed_by_user_id": "33333333-3333-4333-8333-333333333333",
		"occurred_at":        "2026-09-04T05:30:00Z",
	}
	for k, v := range overrides {
		payload[k] = v
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func TestLeadershipTaskRaisedPushGoesToTheAssigneeAndNamesTheAsk(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewLeadershipTaskNotifyConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:       "evt-1",
		Type:     notificationbridge.EventLeadershipTaskRaised,
		TenantID: "11111111-1111-4111-8111-111111111111",
		Payload:  leadershipTaskPayload(nil),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d notifications, want exactly 1", len(queue.queued))
	}
	n := queue.queued[0]
	if len(n.Recipients) != 1 || n.Recipients[0].MemberID != "44444444-4444-4444-8444-444444444444" {
		t.Fatalf("push must reach the ASSIGNEE's device only; recipients=%+v", n.Recipients)
	}
	if len(recipients.positionAsks) != 0 {
		t.Fatalf("a task is addressed to one person, never a position; asked %+v", recipients.positionAsks)
	}
	for _, want := range []string{"Hemant", "Approve the CPT feed vendor contract"} {
		if !strings.Contains(n.Title, want) {
			t.Fatalf("title %q is missing %q", n.Title, want)
		}
	}
	for _, want := range []string{"#12", "Hemant", "2 attachments", "04/09/2026"} {
		if !strings.Contains(n.Body, want) {
			t.Fatalf("body %q is missing %q", n.Body, want)
		}
	}
	if n.EventKey != "leadership_task.raised:22222222-2222-4222-8222-222222222222" {
		t.Fatalf("event key %q must be idempotent per task", n.EventKey)
	}
	if n.Context["href"] != "/leadership-tasks/22222222-2222-4222-8222-222222222222" || n.Context["screen"] != "leadership_task" {
		t.Fatalf("push must deep-link to the task; context=%v", n.Context)
	}
	if n.NotificationType != notificationbridge.NotificationTypeLeadershipTaskRaised {
		t.Fatalf("notification type %q", n.NotificationType)
	}
}

func TestLeadershipTaskDonePushGoesBackToTheRaiser(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewLeadershipTaskNotifyConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:       "evt-9",
		Type:     notificationbridge.EventLeadershipTaskStatusChanged,
		TenantID: "11111111-1111-4111-8111-111111111111",
		Payload:  leadershipTaskPayload(map[string]any{"status": "done", "previous_status": "in_progress", "changed_by_user_id": "44444444-4444-4444-8444-444444444444"}),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d notifications, want exactly 1", len(queue.queued))
	}
	n := queue.queued[0]
	if len(n.Recipients) != 1 || n.Recipients[0].MemberID != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("done must reach the RAISER's device only; recipients=%+v", n.Recipients)
	}
	for _, want := range []string{"Ravi", "#12"} {
		if !strings.Contains(n.Title, want) {
			t.Fatalf("title %q is missing %q", n.Title, want)
		}
	}
	for _, want := range []string{"#12", "Approve the CPT feed vendor contract", "Ravi", "04/09/2026"} {
		if !strings.Contains(n.Body, want) {
			t.Fatalf("body %q is missing %q", n.Body, want)
		}
	}
	if !strings.Contains(n.EventKey, ":evt-9") {
		t.Fatalf("a done push is keyed per event so a reopen/done cycle is new news; key=%q", n.EventKey)
	}
}

type leadershipAudienceGate struct {
	t             *testing.T
	ticked        []string
	addressedSeen []string
}

func (a *leadershipAudienceGate) Recipients(context.Context, string, string, string) ([]calendarports.NotificationRecipient, error) {
	return nil, nil
}

func (a *leadershipAudienceGate) Addressed(_ context.Context, _, _ string, alertKey string, addressee []string, addressed []calendarports.NotificationRecipient) ([]calendarports.NotificationRecipient, error) {
	a.addressedSeen = append([]string(nil), addressee...)
	if alertKey != audiencedomain.AlertLeadershipTaskDone {
		return addressed, nil
	}
	ticked := map[string]struct{}{}
	for _, code := range a.ticked {
		ticked[code] = struct{}{}
	}
	keepAddressed := false
	for _, code := range addressee {
		if _, ok := ticked[code]; ok {
			keepAddressed = true
		}
		delete(ticked, code)
	}
	var out []calendarports.NotificationRecipient
	if keepAddressed {
		out = append(out, addressed...)
	}
	for code := range ticked {
		out = append(out, calendarports.NotificationRecipient{MemberID: "copy-" + code, DeviceID: "device-" + code, FCMToken: "token-" + code, RoleLabel: code})
	}
	return out, nil
}

func TestLeadershipTaskDoneAudienceUsesTheRaisersActualDesignation(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	audience := &leadershipAudienceGate{t: t, ticked: []string{audiencedomain.DesignationFeedDirector}}
	consumer := notificationbridge.NewLeadershipTaskNotifyConsumer(recipients, queue, slog.Default()).WithAudience(audience)

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:       "evt-10",
		Type:     notificationbridge.EventLeadershipTaskStatusChanged,
		TenantID: "11111111-1111-4111-8111-111111111111",
		Payload: leadershipTaskPayload(map[string]any{
			"status":                "done",
			"previous_status":       "in_progress",
			"changed_by_user_id":    "44444444-4444-4444-8444-444444444444",
			"raised_by_designation": audiencedomain.DesignationPCDirector,
		}),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(audience.addressedSeen) != 1 || audience.addressedSeen[0] != audiencedomain.DesignationPCDirector {
		t.Fatalf("task_done must gate the addressed raiser by their actual title, got %v", audience.addressedSeen)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d notifications, want exactly 1", len(queue.queued))
	}
	got := queue.queued[0].Recipients
	if len(got) != 1 || got[0].MemberID != "copy-"+audiencedomain.DesignationFeedDirector {
		t.Fatalf("only the configured copy designation should receive this update; got %+v", got)
	}
}

// Every status change pushes to the OTHER party (maintainer decision 2026-09-08, superseding
// the 2026-09-04 done-only rule): a change the raiser made reaches the CXO it is addressed to,
// and never the raiser themself.
func TestLeadershipTaskChangesByTheRaiserReachTheAssignee(t *testing.T) {
	for _, status := range []string{"in_progress", "open", "cancelled"} {
		recipients := &targetTestRecipients{}
		queue := &targetTestQueue{}
		consumer := notificationbridge.NewLeadershipTaskNotifyConsumer(recipients, queue, slog.Default())
		if err := consumer.HandleEvent(context.Background(), eventbus.Event{
			ID:       "evt-2",
			Type:     notificationbridge.EventLeadershipTaskStatusChanged,
			TenantID: "11111111-1111-4111-8111-111111111111",
			Payload:  leadershipTaskPayload(map[string]any{"status": status}),
		}); err != nil {
			t.Fatalf("HandleEvent(%s): %v", status, err)
		}
		if len(queue.queued) != 1 {
			t.Fatalf("status %s must push once (to the assignee); queued %+v", status, queue.queued)
		}
		n := queue.queued[0]
		if len(n.Recipients) != 1 || n.Recipients[0].MemberID != "44444444-4444-4444-8444-444444444444" {
			t.Fatalf("status %s changed by the raiser must reach the ASSIGNEE only; recipients=%+v", status, n.Recipients)
		}
		if !strings.Contains(n.Title, "Hemant") || !strings.Contains(n.Title, "#12") {
			t.Fatalf("title %q must name the raiser and the task", n.Title)
		}
	}
}

// A change by a THIRD party (another CXO on the desk) reaches both the raiser and the assignee.
func TestLeadershipTaskChangeByAThirdPartyReachesBothParties(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewLeadershipTaskNotifyConsumer(recipients, queue, slog.Default())
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:       "evt-3",
		Type:     notificationbridge.EventLeadershipTaskStatusChanged,
		TenantID: "11111111-1111-4111-8111-111111111111",
		Payload:  leadershipTaskPayload(map[string]any{"status": "in_progress", "changed_by_user_id": "55555555-5555-4555-8555-555555555555"}),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 2 {
		t.Fatalf("queued %d, want raiser + assignee", len(queue.queued))
	}
	got := map[string]bool{}
	for _, n := range queue.queued {
		for _, r := range n.Recipients {
			got[r.MemberID] = true
		}
	}
	if !got["33333333-3333-4333-8333-333333333333"] || !got["44444444-4444-4444-8444-444444444444"] {
		t.Fatalf("both parties must be told; got %v", got)
	}
}

// A malformed payload is a permanent error (never retried into a poison loop); an event of
// another type is ignored.
func TestLeadershipTaskConsumerIgnoresOtherEventsAndRejectsBadPayloads(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewLeadershipTaskNotifyConsumer(recipients, queue, slog.Default())
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{Type: "weighing.shed.closed", TenantID: "t", Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("other event: %v", err)
	}
	err := consumer.HandleEvent(context.Background(), eventbus.Event{Type: notificationbridge.EventLeadershipTaskRaised, TenantID: "t", Payload: []byte(`not json`)})
	if err == nil || !eventbus.IsPermanentError(err) {
		t.Fatalf("bad payload must be a permanent error, got %v", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("nothing should be queued; got %+v", queue.queued)
	}
}
