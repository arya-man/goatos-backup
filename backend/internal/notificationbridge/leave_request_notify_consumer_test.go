package notificationbridge_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// leaveTestRecipients gives every member their OWN device, so a push addressed to two
// people is visible as two recipients (the shared fake hands every member the same
// device id, which the consumer rightly collapses to one).
type leaveTestRecipients struct{ targetTestRecipients }

func (r *leaveTestRecipients) ResolveMemberRecipients(_ context.Context, _ string, memberID string) ([]workforcedomain.NotificationRecipient, error) {
	return []workforcedomain.NotificationRecipient{{WorkforceMemberID: memberID, DeviceID: "device-" + memberID, FCMToken: "token-" + memberID}}, nil
}

// Leave request push (maintainer decisions 2026-09-10): a raise reaches every approver the
// write transaction resolved -- the park head of the requester's park and HR -- naming who,
// which days, how many and why; the final outcome reaches the requester alone.

func leavePayload(overrides map[string]any) []byte {
	payload := map[string]any{
		"leave_request_id":     "55555555-5555-4555-8555-555555555555",
		"workforce_member_id":  "66666666-6666-4666-8666-666666666666",
		"person_name":          "Ramesh",
		"park_id":              "77777777-7777-4777-8777-777777777777",
		"park_label":           "Coimbatore",
		"starts_on":            "2026-09-12",
		"ends_on":              "2026-09-14",
		"day_count":            3,
		"reason":               "Sister's wedding",
		"status":               "pending",
		"park_head_required":   true,
		"hr_required":          true,
		"park_head_member_ids": []string{"88888888-8888-4888-8888-888888888888"},
		"hr_member_ids":        []string{"99999999-9999-4999-8999-999999999999"},
		"occurred_at":          "2026-09-10T05:30:00Z",
	}
	for k, v := range overrides {
		payload[k] = v
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func TestLeaveRaisedPushReachesEveryApproverAndNamesTheAsk(t *testing.T) {
	recipients := &leaveTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewLeaveRequestNotifyConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:       "evt-1",
		Type:     notificationbridge.EventLeaveRequested,
		TenantID: "11111111-1111-4111-8111-111111111111",
		Payload:  leavePayload(nil),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d notifications, want exactly 1", len(queue.queued))
	}
	n := queue.queued[0]
	got := map[string]bool{}
	for _, r := range n.Recipients {
		got[r.MemberID] = true
	}
	if !got["88888888-8888-4888-8888-888888888888"] || !got["99999999-9999-4999-8999-999999999999"] || len(n.Recipients) != 2 {
		t.Fatalf("raise must reach the park head AND HR, nobody else; recipients=%+v", n.Recipients)
	}
	if len(recipients.positionAsks) != 0 {
		t.Fatalf("approvers are named people, never a position; asked %+v", recipients.positionAsks)
	}
	for _, want := range []string{"Ramesh", "12/09/2026", "14/09/2026", "3 days"} {
		if !strings.Contains(n.Title, want) {
			t.Fatalf("title %q is missing %q", n.Title, want)
		}
	}
	for _, want := range []string{"Ramesh", "Coimbatore", "Sister's wedding"} {
		if !strings.Contains(n.Body, want) {
			t.Fatalf("body %q is missing %q", n.Body, want)
		}
	}
	if n.EventKey != "workforce.leave.requested:55555555-5555-4555-8555-555555555555" {
		t.Fatalf("event key %q must be idempotent per request", n.EventKey)
	}
	if n.Context["target"] != "/leave/approvals" || n.Context["screen"] != "leave" {
		t.Fatalf("push must open the approver queue; context=%v", n.Context)
	}
	if n.NotificationType != notificationbridge.NotificationTypeLeaveRequestRaised {
		t.Fatalf("notification type %q", n.NotificationType)
	}
}

func TestLeaveRaisedPushSkipsAnApproverSlotTheRoutingDidNotRequire(t *testing.T) {
	recipients := &leaveTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewLeaveRequestNotifyConsumer(recipients, queue, slog.Default())
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID: "evt-2", Type: notificationbridge.EventLeaveRequested, TenantID: "11111111-1111-4111-8111-111111111111",
		Payload: leavePayload(map[string]any{"park_head_required": false}),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 || len(queue.queued[0].Recipients) != 1 || queue.queued[0].Recipients[0].MemberID != "99999999-9999-4999-8999-999999999999" {
		t.Fatalf("only HR must be pushed when the park-head slot is not required; queued=%+v", queue.queued)
	}
}

func TestLeaveDecidedPushReachesTheRequesterWithTheOutcome(t *testing.T) {
	for _, tc := range []struct {
		name      string
		eventType string
		status    string
		wantBody  []string
	}{
		{"approved", notificationbridge.EventLeaveApproved, "approved", []string{"approved", "12/09/2026", "park head and HR"}},
		{"rejected", notificationbridge.EventLeaveRejected, "rejected", []string{"rejected", "Priya", "HR", "Too many people away"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recipients := &leaveTestRecipients{}
			queue := &targetTestQueue{}
			consumer := notificationbridge.NewLeaveRequestNotifyConsumer(recipients, queue, slog.Default())
			if err := consumer.HandleEvent(context.Background(), eventbus.Event{
				ID: "evt-3", Type: tc.eventType, TenantID: "11111111-1111-4111-8111-111111111111",
				Payload: leavePayload(map[string]any{
					"status": tc.status, "decided_slot": "hr", "decided_by_name": "Priya", "decision_note": "Too many people away that week",
				}),
			}); err != nil {
				t.Fatalf("HandleEvent: %v", err)
			}
			if len(queue.queued) != 1 {
				t.Fatalf("queued %d, want 1", len(queue.queued))
			}
			n := queue.queued[0]
			if len(n.Recipients) != 1 || n.Recipients[0].MemberID != "66666666-6666-4666-8666-666666666666" {
				t.Fatalf("outcome must reach the REQUESTER only; recipients=%+v", n.Recipients)
			}
			for _, want := range tc.wantBody {
				if !strings.Contains(n.Body, want) {
					t.Fatalf("body %q is missing %q", n.Body, want)
				}
			}
			if n.Context["target"] != "/clock" {
				t.Fatalf("outcome must open the Clock screen; context=%v", n.Context)
			}
			if !strings.HasSuffix(n.EventKey, ":55555555-5555-4555-8555-555555555555:evt-3") {
				t.Fatalf("event key %q must carry the event id so each outcome is its own news", n.EventKey)
			}
		})
	}
}

func TestLeaveWithdrawnPushesNobody(t *testing.T) {
	recipients := &leaveTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewLeaveRequestNotifyConsumer(recipients, queue, slog.Default())
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID: "evt-4", Type: "workforce.leave.withdrawn", TenantID: "11111111-1111-4111-8111-111111111111",
		Payload: leavePayload(map[string]any{"status": "withdrawn"}),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("a withdrawal is the person's own act and pushes nobody; queued=%+v", queue.queued)
	}
}
