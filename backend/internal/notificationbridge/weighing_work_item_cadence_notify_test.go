package notificationbridge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
)

// WEIGHING PHASE 2 cadence FCM routing.
//
// Direction is fixed by the EVENT TYPE, so these tests assert both who is pushed
// and who is NOT:
//
//	day_start      -> operator ONLY (routine daily work is not leadership noise)
//	rolled_forward -> operator + leadership
//	delayed        -> leadership ONLY (escalation; the operator already has it)
//
// Every recipient is resolved through the fake resolver, which only answers by
// (tenant, operator id from the assignment row) and by (tenant scope, leadership
// position code). A hardcoded name, phone, token, or seed-time route would make
// these tests fail because the fake would never be asked.
const cadenceBusinessDate = "2026-08-05"

func cadencePayload(operatorID string, buckets ...map[string]any) map[string]any {
	return map[string]any{
		"tenant_id":     lifecycleTenant,
		"campaign_id":   lifecycleCampaign,
		"park_id":       lifecyclePark,
		"operator_id":   operatorID,
		"business_date": cadenceBusinessDate,
		"buckets":       buckets,
	}
}

func cadenceBucket(campaignShedID, label, planned string) map[string]any {
	return map[string]any{
		"campaign_shed_id":      campaignShedID,
		"shed_id":               lifecyclePark,
		"shed_label":            label,
		"planned_business_date": planned,
		"due_business_date":     cadenceBusinessDate,
	}
}

// DAY-START: DOWNWARD to the assigned operator only. Leadership must not be
// paged for routine daily work, and the message must name only that operator's
// own bucket.
func TestWeighingDayStartPushesOnlyTheAssignedOperator(t *testing.T) {
	recipients, queue, consumer := newLifecycleFixture()

	event := lifecycleEvent(t, notificationbridge.EventWeighingWorkItemDayStart, "evt-day-start",
		cadencePayload(lifecycleOpA, cadenceBucket(lifecycleBucketA, "Gandhi 1", cadenceBusinessDate)))
	if err := consumer.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued notifications=%d, want 1", len(queue.queued))
	}
	got := queue.queued[0]
	if tokens := tokensOf(got); len(tokens) != 1 || tokens[0] != tokenOpA {
		t.Fatalf("day-start recipients=%v, want only the assigned operator %q", tokens, tokenOpA)
	}
	if !strings.Contains(got.Body, "Gandhi 1") {
		t.Fatalf("day-start body %q does not name the operator's own bucket", got.Body)
	}
	if strings.Contains(got.Body, "Godel") {
		t.Fatalf("day-start body %q leaked another operator's bucket", got.Body)
	}
	if len(recipients.memberAsks) != 1 || recipients.memberAsks[0] != lifecycleOpA {
		t.Fatalf("recipient resolution asked %v, want exactly the assignment-row operator %q", recipients.memberAsks, lifecycleOpA)
	}
	if got.Context["business_date"] != cadenceBusinessDate {
		t.Fatalf("day-start context business_date=%q, want %q", got.Context["business_date"], cadenceBusinessDate)
	}
	// The dedupe key is (event type, campaign, operator, business date): an
	// at-least-once redelivery collapses instead of pushing twice in one day.
	wantKey := notificationbridge.EventWeighingWorkItemDayStart + ":" + lifecycleCampaign + ":" + lifecycleOpA + ":" + cadenceBusinessDate
	if got.EventKey != wantKey {
		t.Fatalf("day-start event key=%q, want %q", got.EventKey, wantKey)
	}
}

// ROLLED FORWARD: DOWNWARD to the operator (still executable today) AND UPWARD to
// growth_director + ceo_internal (the plan slipped). The body must name the
// ORIGINAL planned business date.
func TestWeighingRolledForwardPushesOperatorAndLeadership(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()

	event := lifecycleEvent(t, notificationbridge.EventWeighingWorkItemRolledForward, "evt-rolled",
		cadencePayload(lifecycleOpA, cadenceBucket(lifecycleBucketA, "Gandhi 1", "2026-07-29")))
	if err := consumer.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued notifications=%d, want 1", len(queue.queued))
	}
	got := queue.queued[0]
	tokens := tokensOf(got)
	want := map[string]bool{tokenOpA: true, tokenDirector: true, tokenCEO: true}
	if len(tokens) != len(want) {
		t.Fatalf("rolled-forward recipients=%v, want operator + growth_director + ceo_internal", tokens)
	}
	for _, token := range tokens {
		if !want[token] {
			t.Fatalf("rolled-forward pushed unexpected recipient %q", token)
		}
	}
	if !strings.Contains(got.Body, "2026-07-29") {
		t.Fatalf("rolled-forward body %q must name the ORIGINAL planned business date", got.Body)
	}
	if got.Context["planned_date"] != "2026-07-29" {
		t.Fatalf("rolled-forward context planned_date=%q, want 2026-07-29", got.Context["planned_date"])
	}
}

// DELAYED: UPWARD escalation only. The operator must NOT be re-pushed, so the
// resolver is never asked for a member.
func TestWeighingDelayedEscalatesUpwardOnly(t *testing.T) {
	recipients, queue, consumer := newLifecycleFixture()

	event := lifecycleEvent(t, notificationbridge.EventWeighingWorkItemDelayed, "evt-delayed",
		cadencePayload(lifecycleOpA, cadenceBucket(lifecycleBucketA, "Gandhi 1", "2026-07-29")))
	if err := consumer.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued notifications=%d, want 1", len(queue.queued))
	}
	got := queue.queued[0]
	tokens := tokensOf(got)
	if len(tokens) != 2 {
		t.Fatalf("delayed escalation recipients=%v, want exactly growth_director + ceo_internal", tokens)
	}
	for _, token := range tokens {
		if token != tokenDirector && token != tokenCEO {
			t.Fatalf("delayed escalation pushed %q; escalation is leadership-only", token)
		}
	}
	if len(recipients.memberAsks) != 0 {
		t.Fatalf("delayed escalation resolved operator devices %v; the operator must not be re-pushed", recipients.memberAsks)
	}
	if got.NotificationType != "escalation" {
		t.Fatalf("delayed notification type=%q, want escalation", got.NotificationType)
	}
}

// An empty bucket list is a no-op rather than an empty push.
func TestWeighingWorkItemCadenceIgnoresEmptyBucketList(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()
	event := lifecycleEvent(t, notificationbridge.EventWeighingWorkItemDayStart, "evt-empty",
		cadencePayload(lifecycleOpA))
	if err := consumer.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("queued notifications=%d, want 0", len(queue.queued))
	}
}
