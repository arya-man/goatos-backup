package notificationbridge_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Weighing lifecycle FCM routing tests.
//
// These drive the consumer through fakes for the two collaborators it owns
// (RecipientResolver, NotificationQueue) so every routing decision is asserted
// exactly: who is pushed, who is NOT pushed, and -- critically -- that an operator
// is never told about another operator's bucket.
const (
	lifecycleTenant   = "11111111-1111-4111-8111-111111111111"
	lifecycleCampaign = "22222222-2222-4222-8222-222222222222"
	lifecyclePark     = "33333333-3333-4333-8333-333333333333"
	lifecycleBucketA  = "44444444-4444-4444-8444-44444444000a"
	lifecycleBucketB  = "44444444-4444-4444-8444-44444444000b"
	lifecycleOpA      = "55555555-5555-4555-8555-55555555000a"
	lifecycleOpB      = "55555555-5555-4555-8555-55555555000b"
	lifecycleObs      = "66666666-6666-4666-8666-666666666666"

	tokenOpA      = "token-operator-a"
	tokenOpB      = "token-operator-b"
	tokenDirector = "token-growth-director"
	tokenCEO      = "token-ceo-internal"
)

// fakeRecipients resolves devices from explicit member/position tables. Nothing is
// name-based: the consumer must ask by the operator id carried on the event and by
// the leadership position code.
type fakeRecipients struct {
	byMember   map[string][]workforcedomain.NotificationRecipient
	byPosition map[string][]workforcedomain.NotificationRecipient
	memberErr  error
	memberAsks []string
}

func (f *fakeRecipients) ResolveModuleDutyRecipients(context.Context, string, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}

func (f *fakeRecipients) ResolveMemberRecipients(_ context.Context, tenantID, memberOrUserID string) ([]workforcedomain.NotificationRecipient, error) {
	if tenantID != lifecycleTenant {
		return nil, fmt.Errorf("unexpected tenant %q", tenantID)
	}
	f.memberAsks = append(f.memberAsks, memberOrUserID)
	if f.memberErr != nil {
		return nil, f.memberErr
	}
	return f.byMember[memberOrUserID], nil
}

func (f *fakeRecipients) ResolvePositionRecipients(_ context.Context, tenantID, scopeType, scopeID, positionCode string) ([]workforcedomain.NotificationRecipient, error) {
	if tenantID != lifecycleTenant {
		return nil, fmt.Errorf("unexpected tenant %q", tenantID)
	}
	if scopeType != "tenant" || scopeID != lifecycleTenant {
		return nil, fmt.Errorf("leadership must resolve at tenant scope, got %s/%s", scopeType, scopeID)
	}
	return f.byPosition[positionCode], nil
}

type fakeQueue struct {
	queued []calendarports.QueueRoleNotifications
	err    error
}

func (q *fakeQueue) QueueRoleNotifications(_ context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	if q.err != nil {
		return 0, q.err
	}
	q.queued = append(q.queued, in)
	return len(in.Recipients), nil
}

func newLifecycleFixture() (*fakeRecipients, *fakeQueue, *notificationbridge.WeighingLifecycleEventConsumer) {
	recipients := &fakeRecipients{
		byMember: map[string][]workforcedomain.NotificationRecipient{
			lifecycleOpA: {{WorkforceMemberID: "member-a", DeviceID: "device-a", FCMToken: tokenOpA}},
			lifecycleOpB: {{WorkforceMemberID: "member-b", DeviceID: "device-b", FCMToken: tokenOpB}},
		},
		byPosition: map[string][]workforcedomain.NotificationRecipient{
			"growth_director": {{WorkforceMemberID: "member-gd", DeviceID: "device-gd", FCMToken: tokenDirector}},
			"ceo_internal":    {{WorkforceMemberID: "member-ceo", DeviceID: "device-ceo", FCMToken: tokenCEO}},
		},
	}
	queue := &fakeQueue{}
	return recipients, queue, notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())
}

func lifecycleEvent(t *testing.T, eventType, eventID string, payload any) eventbus.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", eventType, err)
	}
	return eventbus.Event{ID: eventID, Type: eventType, TenantID: lifecycleTenant, Payload: raw}
}

func tokensOf(in calendarports.QueueRoleNotifications) []string {
	out := make([]string, 0, len(in.Recipients))
	for _, recipient := range in.Recipients {
		out = append(out, recipient.FCMToken)
	}
	sort.Strings(out)
	return out
}

func publishPayload() map[string]any {
	return map[string]any{
		"tenant_id":           lifecycleTenant,
		"campaign_id":         lifecycleCampaign,
		"park_id":             lifecyclePark,
		"published_by":        "77777777-7777-4777-8777-777777777777",
		"start_business_date": "2026-08-03",
		"buckets": []map[string]any{
			{"campaign_shed_id": lifecycleBucketA, "shed_id": lifecyclePark, "shed_label": "Gandhi 1", "operator_id": lifecycleOpA, "weighing_category": "individual_animal", "expected_animal_count": 40},
			{"campaign_shed_id": lifecycleBucketB, "shed_id": lifecyclePark, "shed_label": "Godel 2", "operator_id": lifecycleOpB, "weighing_category": "per_shed_partition", "expected_animal_count": 60},
		},
	}
}

// PUBLISH: each operator gets a private DOWNWARD push naming ONLY their own
// bucket, and leadership gets one UPWARD campaign push. An operator receiving
// another operator's shed label is the failure this test exists to catch.
func TestWeighingPublishFansOutOnlyOwnBucketsPerOperatorPlusLeadership(t *testing.T) {
	recipients, queue, consumer := newLifecycleFixture()

	if err := consumer.HandleEvent(context.Background(), lifecycleEvent(t, notificationbridge.EventWeighingCampaignPublished, "evt-publish", publishPayload())); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(queue.queued) != 3 {
		t.Fatalf("queued notifications=%d, want 3 (one per operator + one leadership)", len(queue.queued))
	}

	var operatorMessages []calendarports.QueueRoleNotifications
	var leadershipMessages []calendarports.QueueRoleNotifications
	for _, message := range queue.queued {
		if strings.Contains(strings.Join(tokensOf(message), ","), "operator") {
			operatorMessages = append(operatorMessages, message)
			continue
		}
		leadershipMessages = append(leadershipMessages, message)
	}
	if len(operatorMessages) != 2 || len(leadershipMessages) != 1 {
		t.Fatalf("operator messages=%d leadership messages=%d, want 2/1", len(operatorMessages), len(leadershipMessages))
	}

	for _, message := range operatorMessages {
		tokens := tokensOf(message)
		if len(tokens) != 1 {
			t.Fatalf("operator message recipients=%v, want exactly one operator device", tokens)
		}
		switch tokens[0] {
		case tokenOpA:
			if !strings.Contains(message.Body, "Gandhi 1") {
				t.Fatalf("operator A body %q does not name their own bucket", message.Body)
			}
			if strings.Contains(message.Body, "Godel 2") {
				t.Fatalf("operator A body %q leaks operator B's bucket", message.Body)
			}
		case tokenOpB:
			if !strings.Contains(message.Body, "Godel 2") {
				t.Fatalf("operator B body %q does not name their own bucket", message.Body)
			}
			if strings.Contains(message.Body, "Gandhi 1") {
				t.Fatalf("operator B body %q leaks operator A's bucket", message.Body)
			}
		default:
			t.Fatalf("unexpected operator token %q", tokens[0])
		}
		if message.EventKey == "" || !strings.Contains(message.EventKey, lifecycleCampaign) {
			t.Fatalf("operator message EventKey=%q must scope idempotency to the campaign", message.EventKey)
		}
	}

	leadership := tokensOf(leadershipMessages[0])
	if len(leadership) != 2 || leadership[0] != tokenCEO || leadership[1] != tokenDirector {
		t.Fatalf("leadership recipients=%v, want exactly CEO + growth director", leadership)
	}

	// Recipients came from the operator ids on the event, never from a hardcoded list.
	sort.Strings(recipients.memberAsks)
	if len(recipients.memberAsks) != 2 || recipients.memberAsks[0] != lifecycleOpA || recipients.memberAsks[1] != lifecycleOpB {
		t.Fatalf("member resolutions=%v, want the two assigned operator ids", recipients.memberAsks)
	}
}

// Redelivery must produce the SAME idempotency event keys, so the queue's
// ON CONFLICT dedupe collapses the replay instead of double-pushing.
func TestWeighingPublishReplayReusesStableEventKeys(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()
	event := lifecycleEvent(t, notificationbridge.EventWeighingCampaignPublished, "evt-publish", publishPayload())

	for i := 0; i < 2; i++ {
		if err := consumer.HandleEvent(context.Background(), event); err != nil {
			t.Fatalf("delivery %d errored: %v", i+1, err)
		}
	}
	if len(queue.queued) != 6 {
		t.Fatalf("queue writes=%d, want 6 (3 per delivery)", len(queue.queued))
	}
	first := make([]string, 0, 3)
	second := make([]string, 0, 3)
	for i, message := range queue.queued {
		if i < 3 {
			first = append(first, message.EventKey)
		} else {
			second = append(second, message.EventKey)
		}
	}
	sort.Strings(first)
	sort.Strings(second)
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("replay event key %q != original %q; the queue could not dedupe", second[i], first[i])
		}
	}
}

func verdictPayload(status string, operatorActionable bool, reason string) map[string]any {
	return map[string]any{
		"tenant_id":           lifecycleTenant,
		"campaign_id":         lifecycleCampaign,
		"campaign_shed_id":    lifecycleBucketA,
		"observation_id":      lifecycleObs,
		"ref_type":            "weighing_observation",
		"park_id":             lifecyclePark,
		"shed_id":             lifecyclePark,
		"shed_label":          "Gandhi 1",
		"operator_id":         lifecycleOpA,
		"verification_status": status,
		"operator_actionable": operatorActionable,
		"reason":              reason,
	}
}

// APPROVED: UPWARD only. The operator has nothing to do after an approval, so
// pushing them would be noise -- they are included only when the event says an
// action is needed.
func TestWeighingVerdictApprovedGoesUpwardOnlyWhenNoOperatorActionNeeded(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()

	if err := consumer.HandleEvent(context.Background(), lifecycleEvent(t, notificationbridge.EventWeighingObservationVerified, "evt-approve", verdictPayload("verified", false, ""))); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued notifications=%d, want 1", len(queue.queued))
	}
	tokens := tokensOf(queue.queued[0])
	if len(tokens) != 2 || tokens[0] != tokenCEO || tokens[1] != tokenDirector {
		t.Fatalf("approved recipients=%v, want exactly CEO + growth director (no operator)", tokens)
	}
	if queue.queued[0].NotificationType != "verification_approved" {
		t.Fatalf("approved notification type=%q", queue.queued[0].NotificationType)
	}
}

// APPROVED with an action still outstanding: the assigned operator IS included.
func TestWeighingVerdictApprovedIncludesOperatorWhenActionIsNeeded(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()

	if err := consumer.HandleEvent(context.Background(), lifecycleEvent(t, notificationbridge.EventWeighingObservationVerified, "evt-approve", verdictPayload("verified", true, ""))); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	tokens := tokensOf(queue.queued[0])
	if len(tokens) != 3 {
		t.Fatalf("recipients=%v, want operator + CEO + growth director", tokens)
	}
	if tokens[0] != tokenCEO || tokens[1] != tokenDirector || tokens[2] != tokenOpA {
		t.Fatalf("recipients=%v, want CEO + growth director + operator A", tokens)
	}
}

// REWORK: DOWNWARD to the operator who must re-shoot, UPWARD to the owning
// director. The CEO is deliberately not paged for an execution redo.
//
// The routing is unchanged by the per-shed batching (maintainer decision 2026-08-03); what
// changed is WHICH event carries it. An individual bounce is now one of possibly several the
// verifier sends while working through the same shed, so the push is emitted once per shed
// from weighing.observation.rework_digest instead of once per animal from the verdict.
func TestWeighingVerdictReworkGoesDownwardToOperatorAndUpwardToDirector(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()

	digest := map[string]any{
		"tenant_id":        lifecycleTenant,
		"campaign_id":      lifecycleCampaign,
		"campaign_shed_id": lifecycleBucketA,
		"park_id":          lifecyclePark,
		"shed_id":          lifecyclePark,
		"shed_label":       "Gandhi 1",
		"operator_id":      lifecycleOpA,
		"items": []map[string]any{
			{"observation_id": "obs-1", "scanned_identifier": "901007000504401", "weight_kg": 12.0},
		},
		"total_count": 1,
		"reason":      "video too dark",
	}
	if err := consumer.HandleEvent(context.Background(), lifecycleEvent(t, notificationbridge.EventWeighingObservationReworkDigest, "evt-rework", digest)); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued notifications=%d, want 1", len(queue.queued))
	}
	message := queue.queued[0]
	tokens := tokensOf(message)
	if len(tokens) != 2 || tokens[0] != tokenDirector || tokens[1] != tokenOpA {
		t.Fatalf("rework recipients=%v, want the assigned operator + growth director", tokens)
	}
	if message.NotificationType != "rework" {
		t.Fatalf("rework notification type=%q", message.NotificationType)
	}
	if !strings.Contains(message.Body, "video too dark") {
		t.Fatalf("rework body %q does not carry the verifier reason", message.Body)
	}
}

func shedClosedPayload(notAcceptedCount int) map[string]any {
	return map[string]any{
		"tenant_id":          lifecycleTenant,
		"campaign_id":        lifecycleCampaign,
		"campaign_shed_id":   lifecycleBucketA,
		"park_id":            lifecyclePark,
		"shed_id":            lifecyclePark,
		"shed_label":         "Gandhi 1",
		"operator_id":        lifecycleOpA,
		"closed_by":          "77777777-7777-4777-8777-777777777777",
		"reason":             "shed emptied early",
		"not_accepted_count": notAcceptedCount,
	}
}

// BUCKET CLOSED with work not accepted: leadership UPWARD and the bucket's one
// operator DOWNWARD, because their outstanding work just disappeared.
func TestWeighingShedClosedWithNotAcceptedWorkNotifiesLeadershipAndTheOperator(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()

	if err := consumer.HandleEvent(context.Background(), lifecycleEvent(t, notificationbridge.EventWeighingShedClosed, "evt-close", shedClosedPayload(7))); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued notifications=%d, want 1", len(queue.queued))
	}
	message := queue.queued[0]
	tokens := tokensOf(message)
	if len(tokens) != 3 || tokens[0] != tokenCEO || tokens[1] != tokenDirector || tokens[2] != tokenOpA {
		t.Fatalf("close recipients=%v, want CEO + growth director + the bucket operator", tokens)
	}
	if !strings.Contains(message.Body, "7") || !strings.Contains(message.Body, "shed emptied early") {
		t.Fatalf("close body %q must carry the not-accepted count and the reason", message.Body)
	}
	if message.Context["not_accepted_count"] != "7" {
		t.Fatalf("close context not_accepted_count=%q, want 7", message.Context["not_accepted_count"])
	}
}

// BUCKET CLOSED with everything already accepted: UPWARD only. The operator owes
// nothing, so pushing them would be noise.
func TestWeighingShedClosedWithAllWorkAcceptedNotifiesLeadershipOnly(t *testing.T) {
	recipients, queue, consumer := newLifecycleFixture()

	if err := consumer.HandleEvent(context.Background(), lifecycleEvent(t, notificationbridge.EventWeighingShedClosed, "evt-close", shedClosedPayload(0))); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	tokens := tokensOf(queue.queued[0])
	if len(tokens) != 2 || tokens[0] != tokenCEO || tokens[1] != tokenDirector {
		t.Fatalf("close recipients=%v, want leadership only", tokens)
	}
	if len(recipients.memberAsks) != 0 {
		t.Fatalf("operator was resolved %v for a fully-accepted close", recipients.memberAsks)
	}
}

// TASK CLOSED: leadership UPWARD once, plus one private DOWNWARD message per
// operator whose bucket ended not accepted -- again naming only their own buckets.
func TestWeighingCampaignClosedNotifiesLeadershipAndEachAffectedOperatorSeparately(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()
	payload := map[string]any{
		"tenant_id":          lifecycleTenant,
		"campaign_id":        lifecycleCampaign,
		"park_id":            lifecyclePark,
		"closed_by":          "77777777-7777-4777-8777-777777777777",
		"reason":             "monsoon",
		"not_accepted_count": 2,
		"buckets": []map[string]any{
			{"campaign_shed_id": lifecycleBucketA, "shed_id": lifecyclePark, "shed_label": "Gandhi 1", "operator_id": lifecycleOpA, "previous_status": "in_progress"},
			{"campaign_shed_id": lifecycleBucketB, "shed_id": lifecyclePark, "shed_label": "Godel 2", "operator_id": lifecycleOpB, "previous_status": "pending"},
		},
	}

	if err := consumer.HandleEvent(context.Background(), lifecycleEvent(t, notificationbridge.EventWeighingCampaignClosed, "evt-campaign-close", payload)); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(queue.queued) != 3 {
		t.Fatalf("queued notifications=%d, want 3 (two operators + leadership)", len(queue.queued))
	}
	for _, message := range queue.queued {
		tokens := tokensOf(message)
		switch {
		case len(tokens) == 1 && tokens[0] == tokenOpA:
			if strings.Contains(message.Body, "Godel 2") {
				t.Fatalf("operator A body %q leaks operator B's bucket", message.Body)
			}
		case len(tokens) == 1 && tokens[0] == tokenOpB:
			if strings.Contains(message.Body, "Gandhi 1") {
				t.Fatalf("operator B body %q leaks operator A's bucket", message.Body)
			}
		case len(tokens) == 2 && tokens[0] == tokenCEO && tokens[1] == tokenDirector:
			if !strings.Contains(message.Body, "2") {
				t.Fatalf("leadership body %q must carry the not-completed shed count", message.Body)
			}
		default:
			t.Fatalf("unexpected recipient set %v", tokens)
		}
	}
}

func TestWeighingCampaignClosedOperatorBodyUsesBoundedLabelSample(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()
	payload := map[string]any{
		"tenant_id":          lifecycleTenant,
		"campaign_id":        lifecycleCampaign,
		"park_id":            lifecyclePark,
		"closed_by":          "77777777-7777-4777-8777-777777777777",
		"not_accepted_count": 10,
		"operators": []map[string]any{
			{
				"operator_id":  lifecycleOpA,
				"bucket_count": 10,
				"shed_labels":  []string{"Shed 01", "Shed 02", "Shed 03", "Shed 04", "Shed 05", "Shed 06"},
			},
		},
	}

	if err := consumer.HandleEvent(context.Background(), lifecycleEvent(t, notificationbridge.EventWeighingCampaignClosed, "evt-campaign-close-many", payload)); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	var operatorBody string
	for _, message := range queue.queued {
		if tokens := tokensOf(message); len(tokens) == 1 && tokens[0] == tokenOpA {
			operatorBody = message.Body
			break
		}
	}
	if operatorBody == "" {
		t.Fatal("operator notification was not queued")
	}
	if !strings.Contains(operatorBody, "10 sheds") {
		t.Fatalf("operator body %q must include exact bucket count", operatorBody)
	}
	if !strings.Contains(operatorBody, "Shed 05") {
		t.Fatalf("operator body %q must include the bounded sample", operatorBody)
	}
	if strings.Contains(operatorBody, "Shed 06") {
		t.Fatalf("operator body %q leaked labels beyond the bounded sample", operatorBody)
	}
}

// A malformed payload can never be fixed by retrying, so it must fail PERMANENTLY.
func TestWeighingLifecycleConsumerFailsPermanentlyOnMalformedPayload(t *testing.T) {
	_, _, consumer := newLifecycleFixture()
	event := eventbus.Event{ID: "evt", Type: notificationbridge.EventWeighingShedClosed, TenantID: lifecycleTenant, Payload: []byte("{not json")}

	err := consumer.HandleEvent(context.Background(), event)
	if err == nil {
		t.Fatal("malformed payload returned nil")
	}
	if !eventbus.IsPermanentError(err) {
		t.Fatalf("err=%v is retryable; a malformed payload is unrecoverable", err)
	}
}

// A resolver failure must surface (retryable) rather than silently pushing nobody.
func TestWeighingLifecycleConsumerSurfacesRecipientResolutionFailure(t *testing.T) {
	recipients, queue, consumer := newLifecycleFixture()
	recipients.memberErr = errors.New("device table unavailable")

	err := consumer.HandleEvent(context.Background(), lifecycleEvent(t, notificationbridge.EventWeighingCampaignPublished, "evt-publish", publishPayload()))
	if err == nil {
		t.Fatal("resolver failure returned nil; the push would be silently lost")
	}
	if eventbus.IsPermanentError(err) {
		t.Fatalf("err=%v is permanent; a resolver outage must be retried", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("queue received %d writes despite the resolver failure", len(queue.queued))
	}
}

// The consumer must be registered for all five weighing lifecycle events.
func TestWeighingLifecycleConsumerSubscribesToEveryLifecycleEvent(t *testing.T) {
	_, _, consumer := newLifecycleFixture()
	bus := &countingBus{subs: map[string]int{}}
	consumer.Register(bus)
	for _, eventType := range []string{
		notificationbridge.EventWeighingCampaignPublished,
		notificationbridge.EventWeighingObservationVerified,
		notificationbridge.EventWeighingObservationRework,
		notificationbridge.EventWeighingShedClosed,
		notificationbridge.EventWeighingCampaignClosed,
	} {
		if bus.subs[eventType] != 1 {
			t.Fatalf("%s subscribers=%d, want 1", eventType, bus.subs[eventType])
		}
	}
}

type countingBus struct{ subs map[string]int }

func (b *countingBus) Subscribe(eventType string, _ eventbus.Handler) { b.subs[eventType]++ }
func (b *countingBus) Publish(context.Context, eventbus.Event) error  { return nil }
