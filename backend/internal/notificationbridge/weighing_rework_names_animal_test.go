package notificationbridge_test

// The rework push must name the animal that has to be redone.
//
// Both weighing rework notifications used to identify only the shed: the weighing lifecycle
// consumer sent "Godel 1 weighing proof was sent back", and the generic verification consumer
// sent "The verifier rejected a weighing proof." An operator who weighed fifteen animals in
// Godel 1 was told to redo something in Godel 1 and had no way to know which one.
//
// The identity is the scanned tag (weighing is free-flow; nothing resolves it to a goat) and
// the weight is what the verifier actually rejected, so both are carried to the operator.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

func reworkPayloadNamingAnimal() []byte {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id":           targetTenant,
		"campaign_id":         targetCampaign,
		"campaign_shed_id":    targetCampaignShed,
		"observation_id":      targetObs,
		"ref_type":            "weighing_observation",
		"park_id":             targetPark,
		"shed_id":             targetShed,
		"shed_label":          "Godel 1",
		"scanned_identifier":  "901007000504407",
		"weight_kg":           12.0,
		"operator_id":         targetOp,
		"verification_status": "rework",
		"operator_actionable": true,
		"reason":              "",
	})
	return raw
}

// TestWeighingReworkNotificationNamesTheAnimal covers the weighing lifecycle consumer, which
// pushes DOWNWARD to the operator who must re-shoot.
func TestWeighingReworkNotificationNamesTheAnimal(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	// Individual rework is BATCHED per shed now: the per-animal verdict is deliberately
	// silent and the shed's digest carries the naming. The property under test is unchanged
	// -- the operator must be told WHICH animal to re-capture, not just which shed -- so this
	// asserts it on the push the operator actually receives.
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingObservationRework,
		Payload: reworkPayloadNamingAnimal(),
	}); err != nil {
		t.Fatalf("HandleEvent(individual rework): %v", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("per-animal rework queued %d notifications, want 0 (batched per shed)", len(queue.queued))
	}

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:   "digest-names-animal",
		Type: notificationbridge.EventWeighingObservationReworkDigest,
		Payload: reworkDigest([]map[string]any{
			{"observation_id": targetObs, "scanned_identifier": "901007000504407", "weight_kg": 12.0},
		}, 1, ""),
	}); err != nil {
		t.Fatalf("HandleEvent(digest): %v", err)
	}
	if len(queue.queued) == 0 {
		t.Fatalf("rework digest notification not queued")
	}
	body := queue.queued[0].Body
	if !strings.Contains(body, "901007000504407") {
		t.Fatalf("rework body = %q, want it to name the tag 901007000504407", body)
	}
	if !strings.Contains(body, "Godel 1") {
		t.Fatalf("rework body = %q, want it to keep naming the shed", body)
	}
}

// A lump-sum rework has no per-animal identity and must not invent one; it still has to say
// which shed's total is being sent back.
func TestWeighingLumpSumReworkNotificationNamesShedWithoutInventingATag(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id":           targetTenant,
		"campaign_id":         targetCampaign,
		"campaign_shed_id":    targetCampaignShed,
		"observation_id":      targetObs,
		"ref_type":            "weighing_shed_observation",
		"park_id":             targetPark,
		"shed_id":             targetShed,
		"shed_label":          "Godel 1",
		"scanned_identifier":  "",
		"weight_kg":           250.0,
		"operator_id":         targetOp,
		"verification_status": "rework",
		"operator_actionable": true,
		"reason":              "",
	})
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingObservationRework,
		Payload: raw,
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) == 0 {
		t.Fatalf("rework notification not queued")
	}
	body := queue.queued[0].Body
	if !strings.Contains(body, "Godel 1") {
		t.Fatalf("lump-sum rework body = %q, want it to name the shed", body)
	}
	if strings.Contains(strings.ToLower(body), "tag ") {
		t.Fatalf("lump-sum rework body = %q, want no invented per-animal tag", body)
	}
}

// TestGenericVerificationReworkNamesTheSubject covers the generic verification consumer, whose
// rework body named nothing at all ("The verifier rejected a weighing proof."). It already
// receives the item's backend-composed subject; it just never used it.
func TestGenericVerificationReworkNamesTheSubject(t *testing.T) {
	recipients := &lifecycleRecipients{}
	queue := &fakeQueue{}
	consumer := notificationbridge.NewVerificationEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventVerificationVerdictRework,
		Payload: lifecyclePayload("weighing", "rejected", ""),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) == 0 {
		t.Fatalf("rework notification not queued")
	}
	body := queue.queued[0].Body
	if !strings.Contains(body, "Shed 4") {
		t.Fatalf("rework body = %q, want it to name the item's subject", body)
	}
}

// The same must hold when the verifier supplied a reason -- that branch builds a different
// sentence and would otherwise drop the subject again.
func TestGenericVerificationReworkWithReasonStillNamesTheSubject(t *testing.T) {
	recipients := &lifecycleRecipients{}
	queue := &fakeQueue{}
	consumer := notificationbridge.NewVerificationEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventVerificationVerdictRework,
		Payload: lifecyclePayload("weighing", "rejected", "blurred video"),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) == 0 {
		t.Fatalf("rework notification not queued")
	}
	body := queue.queued[0].Body
	if !strings.Contains(body, "Shed 4") {
		t.Fatalf("rework body with reason = %q, want it to name the item's subject", body)
	}
	if !strings.Contains(body, "blurred video") {
		t.Fatalf("rework body with reason = %q, want it to keep the verifier reason", body)
	}
}
