package notificationbridge_test

// Module routing of the three verification LIFECYCLE pushes (approved / rework / closed).
//
// The pending push was already module-routed; these three were not. They hardcoded the PC
// Director as the leadership recipient for EVERY module and spoke vaccination words
// ("Vaccination proof verified", "The verified vaccination record is now complete"), so a feed
// or counts verdict pushed vaccination copy to a director who does not own that module. Same
// defect class as the weighing pending push, one event later in the lifecycle.
//
// projection-review: the routing key is still the item's single Module value (1 row per item),
// matched 1:1 against pendingModuleProfiles. No fan-out is possible.

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

const lifecycleOperator = "aaaaaaaa-5555-4555-8555-555555555555"

// lifecycleRecipients answers the operator lookup too, so the closed push (operator-only) has a
// recipient and its wording is observable.
type lifecycleRecipients struct {
	pendRecipients
}

func (f *lifecycleRecipients) ResolveMemberRecipients(context.Context, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return []workforcedomain.NotificationRecipient{{
		WorkforceMemberID: lifecycleOperator,
		DeviceID:          "device-operator",
		FCMToken:          "token-operator",
	}}, nil
}

func lifecyclePayload(module, decision, reason string) []byte {
	raw, err := json.Marshal(map[string]any{
		"tenant_id":     pendTenant,
		"item_id":       pendItem,
		"vertical":      module,
		"module":        module,
		"category":      module,
		"subject_label": "Shed 4",
		"shed_id":       pendShed,
		"park_id":       pendPark,
		"operator_id":   lifecycleOperator,
		"decision":      decision,
		"reason":        reason,
		// Deliberately NOT a legacy vaccination sop_submission source: the legacy dedup path is
		// exercised by the pgtest suite, and suppressing the operator here would hide the copy.
		"source": map[string]any{"module": module, "ref_type": "shed", "ref_id": pendItem},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

// eachLifecycleEvent is the set under test: every post-pending push the consumer emits for a
// generic verification item.
var eachLifecycleEvent = []struct {
	name     string
	event    string
	decision string
	reason   string
}{
	{"approved", notificationbridge.EventVerificationVerdictApproved, "approved", ""},
	{"rework", notificationbridge.EventVerificationVerdictRework, "rejected", "blurred video"},
	{"closed", notificationbridge.EventVerificationItemClosed, "approved", ""},
}

// TestVerificationLifecycleRoutesToOwningDirectorInOwnWords is the RED test for the hardcoded
// PC-director/vaccination-copy defect on the verdict + closed handlers.
func TestVerificationLifecycleRoutesToOwningDirectorInOwnWords(t *testing.T) {
	cases := []struct {
		module      string
		wantOwner   string
		bannedOwner []string
		wantTarget  string
		bannedWord  string
	}{
		{module: "feed", wantOwner: "feed_director", bannedOwner: []string{"pc_director", "growth_director", "health_director"}, wantTarget: "/feed", bannedWord: "vaccinat"},
		{module: "counts", wantOwner: "health_director", bannedOwner: []string{"pc_director", "growth_director", "feed_director"}, wantTarget: "/counts", bannedWord: "vaccinat"},
		{module: "weighing", wantOwner: "growth_director", bannedOwner: []string{"pc_director", "feed_director", "health_director"}, wantTarget: "/weighing", bannedWord: "vaccinat"},
	}
	for _, tc := range cases {
		for _, lc := range eachLifecycleEvent {
			t.Run(tc.module+"/"+lc.name, func(t *testing.T) {
				recipients := &lifecycleRecipients{}
				queue := &fakeQueue{}
				consumer := notificationbridge.NewVerificationEventConsumer(recipients, queue, slog.Default())

				if err := consumer.HandleEvent(context.Background(), eventbus.Event{
					Type:    lc.event,
					Payload: lifecyclePayload(tc.module, lc.decision, lc.reason),
				}); err != nil {
					t.Fatalf("HandleEvent: %v", err)
				}
				if len(queue.queued) == 0 {
					t.Fatalf("%s %s queued nothing", tc.module, lc.name)
				}
				// The closed push is operator-only by design, so only the two verdict pushes
				// carry a leadership recipient to check.
				if lc.name != "closed" {
					sawOwner := false
					for _, ask := range recipients.positionAsks {
						for _, banned := range tc.bannedOwner {
							if ask.position == banned {
								t.Fatalf("%s %s notified %q, which does not own %s", tc.module, lc.name, banned, tc.module)
							}
						}
						if ask.position == tc.wantOwner {
							sawOwner = true
						}
					}
					if !sawOwner {
						t.Fatalf("%s %s never notified %s (positions asked: %+v)", tc.module, lc.name, tc.wantOwner, recipients.positionAsks)
					}
				}
				sawTarget := false
				for _, queued := range queue.queued {
					lowered := strings.ToLower(queued.Title + " " + queued.Body)
					if strings.Contains(lowered, tc.bannedWord) {
						t.Fatalf("%s %s borrows vaccination wording: %q / %q", tc.module, lc.name, queued.Title, queued.Body)
					}
					// Prefix, not equality: a module may deep-link INTO its own surface (weighing's
					// leadership pushes open the proof gallery rather than the operator's work
					// list). What must never happen is landing in ANOTHER module, which the
					// /vaccination guard below asserts.
					if strings.HasPrefix(queued.Context["target"], tc.wantTarget) {
						sawTarget = true
					}
					if strings.HasPrefix(queued.Context["target"], "/vaccination") {
						t.Fatalf("%s %s taps through to %q", tc.module, lc.name, queued.Context["target"])
					}
				}
				if !sawTarget {
					t.Fatalf("%s %s never tapped through to %s", tc.module, lc.name, tc.wantTarget)
				}
			})
		}
	}
}

// TestVerificationLifecycleDropsUnroutedModule pins the no-fallback rule on all three handlers:
// an unclaimed module notifies nobody LOUDLY rather than defaulting to vaccination recipients.
func TestVerificationLifecycleDropsUnroutedModule(t *testing.T) {
	wantLog := map[string]string{
		notificationbridge.EventVerificationVerdictApproved: "verification_approved_notification_unrouted_module",
		notificationbridge.EventVerificationVerdictRework:   "verification_rework_notification_unrouted_module",
		notificationbridge.EventVerificationItemClosed:      "verification_closed_notification_unrouted_module",
	}
	for _, lc := range eachLifecycleEvent {
		t.Run(lc.name, func(t *testing.T) {
			handler := &capturingHandler{}
			recipients := &lifecycleRecipients{}
			queue := &fakeQueue{}
			consumer := notificationbridge.NewVerificationEventConsumer(recipients, queue, slog.New(handler))

			if err := consumer.HandleEvent(context.Background(), eventbus.Event{
				Type:    lc.event,
				Payload: lifecyclePayload("breeding", lc.decision, lc.reason),
			}); err != nil {
				t.Fatalf("HandleEvent: %v", err)
			}
			if len(queue.queued) != 0 {
				t.Fatalf("unclaimed module queued %d notifications on %s", len(queue.queued), lc.name)
			}
			found := false
			for _, message := range handler.messages {
				if message == wantLog[lc.event] {
					found = true
				}
			}
			if !found {
				t.Fatalf("unrouted %s was dropped silently; logged: %v", lc.name, handler.messages)
			}
		})
	}
}
