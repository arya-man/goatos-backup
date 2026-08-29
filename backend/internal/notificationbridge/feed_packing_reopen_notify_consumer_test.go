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

// Feed packing reopen push (maintainer decision 2026-08-29): when the afternoon correction takes a
// packed bag back, the PACKER hears about it -- with the pen, session, feed day, and the old-vs-new
// quantities -- instead of discovering a silently rewritten card (the STG 2026-08-28 confusion).

func feedPackingReopenedPayload(overrides map[string]any) []byte {
	payload := map[string]any{
		"tenant_id":                    "11111111-1111-4111-8111-111111111111",
		"completion_id":                "22222222-2222-4222-8222-222222222222",
		"park_id":                      "33333333-3333-4333-8333-333333333333",
		"park_label":                   "Channapatna",
		"shed_id":                      "44444444-4444-4444-8444-444444444444",
		"partition_label":              "2",
		"operational_location_display": "Castro 2",
		"session_no":                   1,
		"session_label":                "Morning",
		"target_date":                  "2026-08-29",
		"workflow":                     "normal",
		"operator_id":                  "55555555-5555-4555-8555-555555555555",
		"reason":                       "Animals moved in or out of this pen after you packed. This bag was 4 kg for 2 animals; it is now 24 kg for 12 animals. Pack the new amounts and record a new video.",
		"packed_head_count":            2,
		"packed_total_kg":              "4.000",
		"new_head_count":               12,
		"new_total_kg":                 "24.000",
	}
	for k, v := range overrides {
		if v == nil {
			delete(payload, k)
			continue
		}
		payload[k] = v
	}
	raw, _ := json.Marshal(payload)
	return raw
}

// The ordinary case: one push, addressed to the packer, whose body names the place, the change in
// numbers, the re-shoot instruction, and the feed day -- specific enough to act on without opening
// the app (the 2026-08-02 meaningful-notification rule).
func TestFeedPackingReopenPushNamesThePenAndTheOldVsNewQuantities(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewFeedPackingReopenNotifyConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:      "evt-1",
		Type:    notificationbridge.EventFeedPackingReopened,
		Payload: feedPackingReopenedPayload(nil),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d notifications, want exactly 1", len(queue.queued))
	}
	notif := queue.queued[0]
	if len(notif.Recipients) == 0 || notif.Recipients[0].FCMToken != "token-op" {
		t.Fatalf("push must reach the packer's device; recipients=%+v", notif.Recipients)
	}
	for _, want := range []string{
		"Channapatna", "Castro 2", // the place
		"Morning",                           // the session (one pen has two bags)
		"you packed 4 kg for 2 animals",     // what the bag was filled for
		"it now needs 24 kg for 12 animals", // what the corrected sheet directs
		"record a new video",                // the action owed
		"29/08/2026",                        // the feed day, farm-readable (biztime.FarmDateFromBusinessDate)
	} {
		if !strings.Contains(notif.Body, want) {
			t.Fatalf("push body %q is missing %q", notif.Body, want)
		}
	}
	if !strings.Contains(notif.Title, "Castro 2") {
		t.Fatalf("push title %q does not name the pen", notif.Title)
	}
	if notif.Context["target"] != "/feed" || notif.Context["screen"] != "feed_packing" {
		t.Fatalf("push route = %q/%q, want the feed packing surface", notif.Context["target"], notif.Context["screen"])
	}
}

// A row that predates the packed-against snapshot still pushes -- the copy degrades to the
// corrected numbers only, and must not fabricate an old value.
func TestFeedPackingReopenPushDegradesWithoutASnapshot(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewFeedPackingReopenNotifyConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:   "evt-2",
		Type: notificationbridge.EventFeedPackingReopened,
		Payload: feedPackingReopenedPayload(map[string]any{
			"packed_head_count": nil,
			"packed_total_kg":   nil,
		}),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d notifications, want 1", len(queue.queued))
	}
	body := queue.queued[0].Body
	if !strings.Contains(body, "it now needs 24 kg for 12 animals") {
		t.Fatalf("degraded body %q must still carry the corrected numbers", body)
	}
	if strings.Contains(body, "4 kg for 2 animals") {
		t.Fatalf("degraded body %q invents an old value the row never recorded", body)
	}
}

// An event with no operator (a legacy completion whose completed_by was blank) has nobody to
// address: no push, no error -- the reopened card itself still carries the reason.
func TestFeedPackingReopenPushSkipsWhenThereIsNoPacker(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewFeedPackingReopenNotifyConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:      "evt-3",
		Type:    notificationbridge.EventFeedPackingReopened,
		Payload: feedPackingReopenedPayload(map[string]any{"operator_id": ""}),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("queued %d notifications for an event with no packer, want 0", len(queue.queued))
	}
}
