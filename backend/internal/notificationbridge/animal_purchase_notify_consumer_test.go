package notificationbridge_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

func animalPurchasePayload(overrides map[string]any) []byte {
	payload := map[string]any{
		"candidate_id":    "55555555-5555-4555-8555-555555555555",
		"load_id":         "44444444-4444-4444-8444-444444444444",
		"load_ref":        "132",
		"seq_no":          7,
		"species":         "goat",
		"sex":             "female",
		"breed":           "Sirohi",
		"farm_label":      "CPT",
		"vendor_name":     "Ramesh Traders",
		"decision":        "accepted",
		"decided_by_name": "Ravi",
		"decision_note":   "",
		"recorded_by":     "66666666-6666-4666-8666-666666666666",
		"occurred_at":     "2026-09-13T05:30:00Z",
		"load_pending":    3,
	}
	for k, v := range overrides {
		payload[k] = v
	}
	raw, _ := json.Marshal(payload)
	return raw
}

// The person who filmed the animal hears the decision: the load, the animal, the answer, who
// decided, and the note -- addressed to THEM, not to a position.
func TestAnimalPurchaseDecidedPushReachesTheRecorderWithTheAnswer(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decision string
		note     string
		wantBody []string
	}{
		{"accepted", "accepted", "", []string{"Animal 7 (Sirohi)", "Load 132 from Ramesh Traders", "accepted by Ravi", "13/09/2026", "3 still awaiting"}},
		{"rejected", "rejected", "Too thin for the price", []string{"rejected by Ravi", "Note: Too thin for the price"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recipients := &leaveTestRecipients{}
			queue := &targetTestQueue{}
			consumer := notificationbridge.NewAnimalPurchaseNotifyConsumer(recipients, queue, slog.Default())
			if err := consumer.HandleEvent(context.Background(), eventbus.Event{
				ID: "evt-9", Type: notificationbridge.EventAnimalPurchaseDecided, TenantID: "11111111-1111-4111-8111-111111111111",
				Payload: animalPurchasePayload(map[string]any{"decision": tc.decision, "decision_note": tc.note}),
			}); err != nil {
				t.Fatalf("HandleEvent: %v", err)
			}
			if len(queue.queued) != 1 {
				t.Fatalf("queued %d, want 1", len(queue.queued))
			}
			n := queue.queued[0]
			if len(n.Recipients) != 1 || n.Recipients[0].MemberID != "66666666-6666-4666-8666-666666666666" {
				t.Fatalf("the recorder alone must be pushed; recipients=%+v", n.Recipients)
			}
			if len(recipients.positionAsks) != 0 {
				t.Fatalf("addressed to a person, never a position; asked %+v", recipients.positionAsks)
			}
			for _, want := range tc.wantBody {
				if !strings.Contains(n.Body, want) {
					t.Fatalf("body %q is missing %q", n.Body, want)
				}
			}
			if !strings.Contains(n.Title, strings.Title(tc.decision)) {
				t.Fatalf("title %q must lead with the decision", n.Title)
			}
			if n.EventKey != "procurement.animal_purchase.decided:55555555-5555-4555-8555-555555555555:evt-9" {
				t.Fatalf("event key %q must be idempotent per decision event", n.EventKey)
			}
			if n.Context["target"] != "/vendors/animal-purchases/loads/44444444-4444-4444-8444-444444444444" || n.Context["screen"] != "animal_purchases" {
				t.Fatalf("push must open the load; context=%v", n.Context)
			}
			if n.NotificationType != notificationbridge.NotificationTypeAnimalPurchaseDecided {
				t.Fatalf("notification type %q", n.NotificationType)
			}
		})
	}
}

func TestAnimalPurchaseDecidedIgnoresOtherEvents(t *testing.T) {
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewAnimalPurchaseNotifyConsumer(&leaveTestRecipients{}, queue, slog.Default())
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{ID: "x", Type: "procurement.feed_purchase.reached", TenantID: "t", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("queued %d for a foreign event", len(queue.queued))
	}
}
