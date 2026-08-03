package notificationbridge_test

// The rework push must be BATCHED PER SHED.
//
// A verifier reviewing a fifteen-animal shed and rejecting five of them used to send the
// operator FIVE separate pushes -- one per rework verdict event -- for one trip he makes back
// to one shed. At real herd sizes that is a notification storm for a single re-open.
//
// These tests assert notification COUNTS and BODIES, not absence of error:
//
//	1. five individual bounces in one shed queue ZERO operator pushes on the per-animal path;
//	2. the shed's digest queues exactly ONE push, and it NAMES the animals (bounded: the first
//	   few by tag and weight, then an honest "and N more");
//	3. a sixth rejection arriving after that digest fired still reaches the operator, as its
//	   own later digest with its own notification key;
//	4. a redelivered digest event reuses the same key, so the pipeline's existing
//	   (tenant, event_key, device) idempotency collapses it instead of re-notifying;
//	5. lump-sum -- one capture for the whole shed -- still produces exactly one push and is
//	   NOT routed through the batching path.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

func individualReworkVerdict(observationID, tag string, weightKg float64) []byte {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id":           targetTenant,
		"campaign_id":         targetCampaign,
		"campaign_shed_id":    targetCampaignShed,
		"observation_id":      observationID,
		"ref_type":            "weighing_observation",
		"park_id":             targetPark,
		"shed_id":             targetShed,
		"shed_label":          "Godel 1",
		"scanned_identifier":  tag,
		"weight_kg":           weightKg,
		"operator_id":         targetOp,
		"verification_status": "rework",
		"operator_actionable": true,
	})
	return raw
}

func reworkDigest(items []map[string]any, total int, reason string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id":        targetTenant,
		"campaign_id":      targetCampaign,
		"campaign_shed_id": targetCampaignShed,
		"park_id":          targetPark,
		"shed_id":          targetShed,
		"shed_label":       "Godel 1",
		"operator_id":      targetOp,
		"items":            items,
		"total_count":      total,
		"reason":           reason,
		"decided_at":       "2026-08-03T18:30:00+05:30",
	})
	return raw
}

// TestFiveBouncesInOneShedProduceOneNotificationNamingThem is the whole point of the change.
func TestFiveBouncesInOneShedProduceOneNotificationNamingThem(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	tags := []string{"901007000504401", "901007000504402", "901007000504403", "901007000504404", "901007000504405"}
	for i, tag := range tags {
		if err := consumer.HandleEvent(context.Background(), eventbus.Event{
			ID:      "verdict-" + tag,
			Type:    notificationbridge.EventWeighingObservationRework,
			Payload: individualReworkVerdict("obs-"+tag, tag, 12.0+float64(i)),
		}); err != nil {
			t.Fatalf("HandleEvent(individual rework %d): %v", i, err)
		}
	}
	// The per-animal path must be silent now: five bounces, zero pushes.
	if len(queue.queued) != 0 {
		t.Fatalf("individual rework verdicts queued %d notifications, want 0 (they are batched per shed); bodies=%v",
			len(queue.queued), queuedBodies(queue))
	}

	// The shed's digest is the ONE push.
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:   "digest-round-1",
		Type: notificationbridge.EventWeighingObservationReworkDigest,
		Payload: reworkDigest([]map[string]any{
			{"observation_id": "obs-1", "scanned_identifier": tags[0], "weight_kg": 12.0},
			{"observation_id": "obs-2", "scanned_identifier": tags[1], "weight_kg": 13.0},
			{"observation_id": "obs-3", "scanned_identifier": tags[2], "weight_kg": 14.0},
		}, 5, ""),
	}); err != nil {
		t.Fatalf("HandleEvent(digest): %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("digest queued %d notifications, want exactly 1; bodies=%v", len(queue.queued), queuedBodies(queue))
	}
	body := queue.queued[0].Body
	for _, tag := range tags[:3] {
		if !strings.Contains(body, tag) {
			t.Fatalf("digest body = %q, want it to name tag %s", body, tag)
		}
	}
	// Bounded: the two it did not name must be accounted for, not silently omitted.
	if !strings.Contains(body, "and 2 more") {
		t.Fatalf("digest body = %q, want it to say how many more animals are waiting", body)
	}
	// It must never render the whole roster of tags.
	for _, tag := range tags[3:] {
		if strings.Contains(body, tag) {
			t.Fatalf("digest body = %q, want the named list capped, not every tag", body)
		}
	}
	if !strings.Contains(body, "Godel 1") {
		t.Fatalf("digest body = %q, want it to name the shed", body)
	}
	if got := queue.queued[0].Context["campaign_shed_id"]; got != targetCampaignShed {
		t.Fatalf("digest campaign_shed_id = %q, want the tap route to reach the right shed", got)
	}
	if got := queue.queued[0].Context["target"]; got != "/weighing" {
		t.Fatalf("digest target = %q, want /weighing", got)
	}
}

// A late rejection -- one the verifier sent after the shed's digest already fired -- must still
// reach the operator. It arrives as its own later digest with its own notification key.
func TestLateRejectionAfterDigestStillReachesTheOperator(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	first := eventbus.Event{
		ID:   "digest-round-1",
		Type: notificationbridge.EventWeighingObservationReworkDigest,
		Payload: reworkDigest([]map[string]any{
			{"observation_id": "obs-1", "scanned_identifier": "901007000504401", "weight_kg": 12.0},
		}, 5, ""),
	}
	late := eventbus.Event{
		ID:   "digest-round-2",
		Type: notificationbridge.EventWeighingObservationReworkDigest,
		Payload: reworkDigest([]map[string]any{
			{"observation_id": "obs-6", "scanned_identifier": "901007000504406", "weight_kg": 17.5},
		}, 1, ""),
	}
	for _, event := range []eventbus.Event{first, late} {
		if err := consumer.HandleEvent(context.Background(), event); err != nil {
			t.Fatalf("HandleEvent(%s): %v", event.ID, err)
		}
	}
	if len(queue.queued) != 2 {
		t.Fatalf("queued %d notifications, want 2 (the batch, then the late sixth); bodies=%v",
			len(queue.queued), queuedBodies(queue))
	}
	if !strings.Contains(queue.queued[1].Body, "901007000504406") {
		t.Fatalf("late notification body = %q, want it to name the sixth animal", queue.queued[1].Body)
	}
	if queue.queued[0].EventKey == queue.queued[1].EventKey {
		t.Fatalf("late rejection reused event key %q, so the pipeline would dedupe it away and the operator would never hear about it",
			queue.queued[1].EventKey)
	}
}

// A redelivered digest event must not re-notify. The durable bus is at-least-once, so the SAME
// digest can arrive many times; the notification pipeline dedupes on
// (tenant, event_key, device), which only works if the key is derived from the digest event
// itself rather than from the wall clock or a fresh id.
func TestRedeliveredDigestReusesTheSameNotificationKey(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	event := eventbus.Event{
		ID:   "digest-round-1",
		Type: notificationbridge.EventWeighingObservationReworkDigest,
		Payload: reworkDigest([]map[string]any{
			{"observation_id": "obs-1", "scanned_identifier": "901007000504401", "weight_kg": 12.0},
		}, 2, ""),
	}
	for i := 0; i < 3; i++ {
		if err := consumer.HandleEvent(context.Background(), event); err != nil {
			t.Fatalf("HandleEvent(redelivery %d): %v", i, err)
		}
	}
	if len(queue.queued) != 3 {
		t.Fatalf("queue saw %d writes, want 3 (dedupe is the pipeline's job, not the consumer's)", len(queue.queued))
	}
	first := queue.queued[0]
	for i, q := range queue.queued[1:] {
		if q.EventKey != first.EventKey {
			t.Fatalf("redelivery %d used event key %q, want %q -- a changing key defeats the pipeline's idempotency and re-notifies the operator",
				i+1, q.EventKey, first.EventKey)
		}
		if q.Body != first.Body {
			t.Fatalf("redelivery %d body = %q, want %q", i+1, q.Body, first.Body)
		}
	}
}

// Lump-sum has ONE video for the whole shed, so its rework push was never one of several. It
// must stay on the immediate path and still produce exactly one notification.
func TestLumpSumReworkStillProducesExactlyOneImmediateNotification(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	raw, _ := json.Marshal(map[string]any{
		"tenant_id":           targetTenant,
		"campaign_id":         targetCampaign,
		"campaign_shed_id":    targetCampaignShed,
		"observation_id":      targetObs,
		"ref_type":            "weighing_shed_observation",
		"park_id":             targetPark,
		"shed_id":             targetShed,
		"shed_label":          "Godel 1",
		"weight_kg":           250.0,
		"operator_id":         targetOp,
		"verification_status": "rework",
		"operator_actionable": true,
	})
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:      "lumpsum-verdict",
		Type:    notificationbridge.EventWeighingObservationRework,
		Payload: raw,
	}); err != nil {
		t.Fatalf("HandleEvent(lump-sum rework): %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("lump-sum rework queued %d notifications, want exactly 1; bodies=%v", len(queue.queued), queuedBodies(queue))
	}
	if !strings.Contains(queue.queued[0].Body, "Godel 1") {
		t.Fatalf("lump-sum rework body = %q, want it to name the shed", queue.queued[0].Body)
	}
	if strings.Contains(strings.ToLower(queue.queued[0].Body), "tag ") {
		t.Fatalf("lump-sum rework body = %q, want no invented per-animal tag", queue.queued[0].Body)
	}
}

// A digest whose named animals somehow carry no scanned identity must still say which shed and
// how many -- never a bare count with no farm entity in it.
func TestDigestWithoutTagsStillNamesTheShedAndTheCount(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		ID:      "digest-no-tags",
		Type:    notificationbridge.EventWeighingObservationReworkDigest,
		Payload: reworkDigest([]map[string]any{{"observation_id": "obs-1", "scanned_identifier": "", "weight_kg": 12.0}}, 4, ""),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d notifications, want 1", len(queue.queued))
	}
	body := queue.queued[0].Body
	if !strings.Contains(body, "Godel 1") || !strings.Contains(body, "4") {
		t.Fatalf("digest body = %q, want it to name the shed and the count", body)
	}
}

func queuedBodies(q *targetTestQueue) []string {
	bodies := make([]string, 0, len(q.queued))
	for _, item := range q.queued {
		bodies = append(bodies, item.Body)
	}
	return bodies
}
