package notificationbridge_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestVerificationNotifierReworkCopyIsSpecific proves the rework push (verification_notify.go,
// notifyRework) no longer reads "Vaccination proof rejected — rework needed" / "This drive needs
// rework" with zero place -- the exact abstract copy this bridge shipped before. Locations is left
// unattached (nil pool -> ResolveNames returns empty), which exercises the documented, honest
// fallback ("this park") rather than skipping the enrichment path entirely.
func TestVerificationNotifierReworkCopyIsSpecific(t *testing.T) {
	completionCtx := calendarports.VaccinationCompletionContext{
		CompletionID: "11111111-1111-4111-8111-111111111111",
		ObligationID: "22222222-2222-4222-8222-222222222222",
		ParkID:       "33333333-3333-4333-8333-333333333333",
		ScopeType:    "center",
		SOPTaskID:    "44444444-4444-4444-8444-444444444444",
		ExecutedBy:   "55555555-5555-4555-8555-555555555555",
	}
	resolver := fakeCompletionResolver{ctx: completionCtx}
	recipients := fakeCopyRecipients{
		member:   []workforcedomain.NotificationRecipient{{WorkforceMemberID: "op", DeviceID: "op-device", FCMToken: strings.Repeat("a", 80)}},
		position: []workforcedomain.NotificationRecipient{{WorkforceMemberID: "ph", DeviceID: "ph-device", FCMToken: strings.Repeat("b", 80)}},
	}
	queue := &fakeCopyQueue{}
	notifier := notificationbridge.NewVerificationNotifier(resolver, recipients, queue, discardLogger())
	// WithLocationNames intentionally NOT attached (nil pool path): asserts the honest fallback,
	// not a crash, when name enrichment is unavailable.
	notifier = notifier.WithLocationNames(notificationbridge.NewLocationNameResolver(nil))

	payload, _ := json.Marshal(notificationbridge.VerificationEvent{
		CompletionID: completionCtx.CompletionID,
		Reason:       "video too dark to verify dosage",
	})
	if err := notifier.HandleEvent(context.Background(), eventbus.Event{
		Type:     notificationbridge.EventVaccinationVerifyRejected,
		TenantID: "66666666-6666-4666-8666-666666666666",
		Payload:  payload,
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.captured) != 1 {
		t.Fatalf("expected exactly one queued notification, got %d", len(queue.captured))
	}
	in := queue.captured[0].in

	if !strings.Contains(in.Title, "this park") && !strings.Contains(in.Body, "this park") {
		t.Errorf("rework copy must name a place (or the documented fallback); title=%q body=%q", in.Title, in.Body)
	}
	if !strings.Contains(in.Body, "video too dark to verify dosage") {
		t.Errorf("rework body must carry the verifier's rejection reason: %q", in.Body)
	}
	// The pre-fix literal strings must never come back verbatim.
	if in.Title == "Vaccination proof rejected — rework needed" {
		t.Errorf("regressed to the pre-fix abstract title: %q", in.Title)
	}
	if in.Body == "The verifier rejected a vaccination proof. This drive needs rework." {
		t.Errorf("regressed to the pre-fix abstract body with no place named: %q", in.Body)
	}

	assertCopyClean(t, "rework title", in.Title)
	assertCopyClean(t, "rework body", in.Body)
}
