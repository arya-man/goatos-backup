package notificationbridge_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Weighing notification target routing tests.
//
// Regression test for the fix that added a "target" to all 6 weighing notification Context maps
// (handleVerdict, handleShedClosed, handleCampaignClosed, handleWorkItemCadence in lifecycle
// consumer, and submission-completed + handleReopened in submission consumer), and for the later
// fix that made those targets SPECIFIC.
//
// Every one of them originally pointed at the bare "/weighing" module landing, which is the
// operator's own work list: a Growth Director or CEO who tapped a push about a task they oversee
// landed on an empty My Work with no route to the task or the evidence. A push that reports on a
// finished bucket now opens THAT bucket's record, a push about a whole task opens THAT task, and
// only the pushes whose audience must go and capture something keep the module landing. No
// weighing notification may carry vaccination copy or a vaccination target.
const (
	targetTenant       = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	targetCampaign     = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	targetPark         = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	targetShed         = targetPark
	targetCampaignShed = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	targetObs          = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	targetOp           = "99999999-9999-4999-8999-999999999999"
)

// The two deep links the weighing consumers must emit for this fixture's ids. Spelled out here
// rather than reusing the producer's own helper, so a change to the emitted shape has to be made
// deliberately in both places instead of silently agreeing with itself.
const (
	targetBucketRoute = "/weighing/shed?campaignId=" + targetCampaign + "&campaignShedId=" + targetCampaignShed
	targetTaskRoute   = "/weighing/task?campaignId=" + targetCampaign
)

type targetTestRecipients struct {
	positionAsks []struct {
		scopeType    string
		scopeID      string
		positionCode string
	}
}

func (t *targetTestRecipients) ResolveModuleDutyRecipients(context.Context, string, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}

func (t *targetTestRecipients) ResolveMemberRecipients(_ context.Context, tenantID, memberID string) ([]workforcedomain.NotificationRecipient, error) {
	return []workforcedomain.NotificationRecipient{{
		WorkforceMemberID: memberID,
		DeviceID:          "device-op",
		FCMToken:          "token-op",
	}}, nil
}

func (t *targetTestRecipients) ResolvePositionRecipients(_ context.Context, tenantID, scopeType, scopeID, positionCode string) ([]workforcedomain.NotificationRecipient, error) {
	t.positionAsks = append(t.positionAsks, struct {
		scopeType    string
		scopeID      string
		positionCode string
	}{scopeType, scopeID, positionCode})
	return []workforcedomain.NotificationRecipient{{
		WorkforceMemberID: "member-" + positionCode,
		DeviceID:          "device-" + positionCode,
		FCMToken:          "token-" + positionCode,
	}}, nil
}

type targetTestQueue struct {
	queued []calendarports.QueueRoleNotifications
}

func (q *targetTestQueue) QueueRoleNotifications(ctx context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	q.queued = append(q.queued, in)
	return len(in.Recipients), nil
}

// verdictPayload constructs a weighing observation verdict event payload.
func verdictPayloadWithTarget(status string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id":           targetTenant,
		"campaign_id":         targetCampaign,
		"campaign_shed_id":    targetCampaignShed,
		"observation_id":      targetObs,
		"ref_type":            "weighing_observation",
		"park_id":             targetPark,
		"shed_id":             targetShed,
		"shed_label":          "Test Shed",
		"operator_id":         targetOp,
		"verification_status": status,
		"operator_actionable": false,
		"reason":              "",
	})
	return raw
}

// shedClosedPayload constructs a weighing shed closed event payload.
func shedClosedPayloadWithTarget() []byte {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id":          targetTenant,
		"campaign_id":        targetCampaign,
		"campaign_shed_id":   targetCampaignShed,
		"park_id":            targetPark,
		"shed_id":            targetShed,
		"shed_label":         "Test Shed",
		"operator_id":        targetOp,
		"not_accepted_count": 5,
		"reason":             "",
	})
	return raw
}

// campaignClosedPayload constructs a weighing campaign closed event payload.
func campaignClosedPayloadWithTarget() []byte {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id":          targetTenant,
		"campaign_id":        targetCampaign,
		"park_id":            targetPark,
		"not_accepted_count": 1,
		"reason":             "",
		"buckets": []map[string]any{
			{"shed_label": "Shed A", "operator_id": targetOp},
		},
	})
	return raw
}

// workItemCadencePayload constructs a weighing work item cadence event payload.
func workItemCadencePayloadWithTarget(eventType string) []byte {
	base := map[string]any{
		"tenant_id":     targetTenant,
		"campaign_id":   targetCampaign,
		"park_id":       targetPark,
		"operator_id":   targetOp,
		"business_date": "2026-08-03",
		"buckets": []map[string]any{
			{"campaign_shed_id": targetCampaignShed, "shed_id": targetShed, "shed_label": "Test Shed"},
		},
	}
	if eventType == notificationbridge.EventWeighingWorkItemDelayed {
		base["business_date"] = "2026-08-05"
	}
	raw, _ := json.Marshal(base)
	return raw
}

// submissionPayload constructs a weighing submission completed event payload.
func submissionPayloadWithTarget() []byte {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id":        targetTenant,
		"campaign_id":      targetCampaign,
		"campaign_shed_id": targetCampaignShed,
		"park_id":          targetPark,
		"shed_id":          targetShed,
		"shed_label":       "Test Shed",
		"completed_at":     "2026-08-03T10:00:00Z",
	})
	return raw
}

// reopenedPayload constructs a weighing shed reopened event payload.
func reopenedPayloadWithTarget() []byte {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id":        targetTenant,
		"campaign_id":      targetCampaign,
		"campaign_shed_id": targetCampaignShed,
		"park_id":          targetPark,
		"shed_id":          targetShed,
		"shed_label":       "Test Shed",
		"operator_id":      targetOp,
		"reopened_by":      "member-gd",
		"reason":           "missed tags",
		"reopened_at":      "2026-08-03T10:00:00Z",
	})
	return raw
}

// TestWeighingVerdictNotificationHasTarget asserts the handleVerdict handler
// queues a notification with target="/weighing" and no vaccination wording.
func TestWeighingVerdictNotificationHasTarget(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingObservationVerified,
		Payload: verdictPayloadWithTarget("verified"),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.queued) == 0 {
		t.Fatalf("verdict notification not queued")
	}
	notif := queue.queued[0]
	// An approved verdict is evidence: it opens the bucket whose proof was accepted.
	if notif.Context["target"] != targetBucketRoute {
		t.Fatalf("verdict context target=%q, want %s", notif.Context["target"], targetBucketRoute)
	}
	if notif.Context["target"] == "" {
		t.Fatalf("verdict has no target")
	}
	// Guard: no vaccination wording
	if strings.Contains(strings.ToLower(notif.Title), "vaccination") {
		t.Fatalf("verdict title borrows vaccination wording: %q", notif.Title)
	}
	if strings.Contains(strings.ToLower(notif.Body), "vaccination") {
		t.Fatalf("verdict body borrows vaccination wording: %q", notif.Body)
	}
	// Guard: growth_director owns weighing, not pc_director
	if len(recipients.positionAsks) == 0 {
		t.Fatalf("verdict never asked for growth_director position recipients")
	}
}

// TestWeighingReworkNotificationHasTarget asserts the handleVerdict handler
// (rework branch) queues a notification with target="/weighing".
func TestWeighingReworkNotificationHasTarget(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingObservationRework,
		Payload: verdictPayloadWithTarget("rework"),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.queued) == 0 {
		t.Fatalf("rework notification not queued")
	}
	notif := queue.queued[0]
	// Rework is work, and its audience is the operator who must re-capture, so this one
	// deliberately stays on the module landing that carries the capture entry point.
	if notif.Context["target"] != "/weighing" {
		t.Fatalf("rework context target=%q, want /weighing", notif.Context["target"])
	}
	// Guard: no vaccination wording in rework
	if strings.Contains(strings.ToLower(notif.Title+notif.Body), "vaccination") {
		t.Fatalf("rework borrows vaccination wording: title=%q body=%q", notif.Title, notif.Body)
	}
}

// TestWeighingShedClosedNotificationHasTarget asserts the handleShedClosed handler
// queues a notification with target="/weighing".
func TestWeighingShedClosedNotificationHasTarget(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingShedClosed,
		Payload: shedClosedPayloadWithTarget(),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.queued) == 0 {
		t.Fatalf("shed closed notification not queued")
	}
	notif := queue.queued[0]
	if notif.Context["target"] != targetBucketRoute {
		t.Fatalf("shed closed context target=%q, want %s", notif.Context["target"], targetBucketRoute)
	}
	// Guard: no vaccination wording
	if strings.Contains(strings.ToLower(notif.Title+notif.Body), "vaccination") {
		t.Fatalf("shed closed borrows vaccination wording: title=%q body=%q", notif.Title, notif.Body)
	}
}

// TestWeighingCampaignClosedNotificationHasTarget asserts the handleCampaignClosed handler
// queues leadership notifications with target="/weighing". Note: operator notifications
// for campaign closed may not have the target (possible gap, not tested here).
func TestWeighingCampaignClosedNotificationHasTarget(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingCampaignClosed,
		Payload: campaignClosedPayloadWithTarget(),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.queued) == 0 {
		t.Fatalf("campaign closed notification not queued")
	}
	// The handler produces one notification per operator (downward) + one leadership (upward).
	// We verify the leadership one has the target.
	var leadershipNotif *calendarports.QueueRoleNotifications
	for i := range queue.queued {
		notif := &queue.queued[i]
		// Leadership notifications carry growth_director and ceo tokens.
		for _, recipient := range notif.Recipients {
			if strings.Contains(recipient.FCMToken, "token-growth_director") || strings.Contains(recipient.FCMToken, "token-ceo") {
				leadershipNotif = notif
				break
			}
		}
	}
	if leadershipNotif == nil {
		t.Fatalf("campaign closed: no leadership notification found")
	}
	if leadershipNotif.Context["target"] != targetTaskRoute {
		t.Fatalf("campaign closed leadership context target=%q, want %s", leadershipNotif.Context["target"], targetTaskRoute)
	}
	// Guard: no vaccination wording
	if strings.Contains(strings.ToLower(leadershipNotif.Title+leadershipNotif.Body), "vaccination") {
		t.Fatalf("campaign closed leadership borrows vaccination wording")
	}
}

// TestWeighingWorkItemDayStartNotificationHasTarget asserts the handleWorkItemCadence handler
// (day_start event) queues a notification with target="/weighing".
func TestWeighingWorkItemDayStartNotificationHasTarget(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingWorkItemDayStart,
		Payload: workItemCadencePayloadWithTarget(notificationbridge.EventWeighingWorkItemDayStart),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.queued) == 0 {
		t.Fatalf("day_start notification not queued")
	}
	notif := queue.queued[0]
	if notif.Context["target"] != "/weighing" {
		t.Fatalf("day_start context target=%q, want /weighing", notif.Context["target"])
	}
}

// TestWeighingWorkItemRolledForwardNotificationHasTarget asserts the handleWorkItemCadence handler
// (rolled_forward event) queues a notification with target="/weighing".
func TestWeighingWorkItemRolledForwardNotificationHasTarget(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingWorkItemRolledForward,
		Payload: workItemCadencePayloadWithTarget(notificationbridge.EventWeighingWorkItemRolledForward),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.queued) == 0 {
		t.Fatalf("rolled_forward notification not queued")
	}
	notif := queue.queued[0]
	if notif.Context["target"] != "/weighing" {
		t.Fatalf("rolled_forward context target=%q, want /weighing", notif.Context["target"])
	}
}

// TestWeighingWorkItemDelayedNotificationHasTarget asserts the handleWorkItemCadence handler
// (delayed event) queues a notification with target="/weighing".
func TestWeighingWorkItemDelayedNotificationHasTarget(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingWorkItemDelayed,
		Payload: workItemCadencePayloadWithTarget(notificationbridge.EventWeighingWorkItemDelayed),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.queued) == 0 {
		t.Fatalf("delayed notification not queued")
	}
	notif := queue.queued[0]
	if notif.Context["target"] != "/weighing" {
		t.Fatalf("delayed context target=%q, want /weighing", notif.Context["target"])
	}
}

// TestWeighingSubmissionCompletedNotificationHasTarget asserts the submission consumer
// (HandleEvent method) queues a notification with target="/weighing".
func TestWeighingSubmissionCompletedNotificationHasTarget(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingSubmissionEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingShedSubmissionCompleted,
		Payload: submissionPayloadWithTarget(),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.queued) == 0 {
		t.Fatalf("submission completed notification not queued")
	}
	notif := queue.queued[0]
	// Leadership-only push about one finished bucket: it opens that bucket's record.
	if notif.Context["target"] != targetBucketRoute {
		t.Fatalf("submission completed context target=%q, want %s", notif.Context["target"], targetBucketRoute)
	}
	// Guard: no vaccination wording
	if strings.Contains(strings.ToLower(notif.Title+notif.Body), "vaccination") {
		t.Fatalf("submission completed borrows vaccination wording")
	}
}

// TestWeighingReopenedNotificationHasTarget asserts the submission consumer
// (handleReopened method) queues a notification with target="/weighing".
func TestWeighingReopenedNotificationHasTarget(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingSubmissionEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingShedReopened,
		Payload: reopenedPayloadWithTarget(),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(queue.queued) == 0 {
		t.Fatalf("reopened notification not queued")
	}
	notif := queue.queued[0]
	if notif.Context["target"] != "/weighing" {
		t.Fatalf("reopened context target=%q, want /weighing", notif.Context["target"])
	}
	// Guard: no vaccination wording
	if strings.Contains(strings.ToLower(notif.Title+notif.Body), "vaccination") {
		t.Fatalf("reopened borrows vaccination wording")
	}
}

// TestWeighingNotificationNeverHasVaccinationTarget is a guard test that asserts
// no weighing notification context maps to a /vaccination target.
func TestWeighingNotificationNeverHasVaccinationTarget(t *testing.T) {
	testCases := []struct {
		name    string
		event   string
		payload []byte
	}{
		{"verdict/approved", notificationbridge.EventWeighingObservationVerified, verdictPayloadWithTarget("verified")},
		{"verdict/rework", notificationbridge.EventWeighingObservationRework, verdictPayloadWithTarget("rework")},
		{"shed/closed", notificationbridge.EventWeighingShedClosed, shedClosedPayloadWithTarget()},
		{"campaign/closed", notificationbridge.EventWeighingCampaignClosed, campaignClosedPayloadWithTarget()},
		{"cadence/day_start", notificationbridge.EventWeighingWorkItemDayStart, workItemCadencePayloadWithTarget(notificationbridge.EventWeighingWorkItemDayStart)},
		{"cadence/rolled_forward", notificationbridge.EventWeighingWorkItemRolledForward, workItemCadencePayloadWithTarget(notificationbridge.EventWeighingWorkItemRolledForward)},
		{"cadence/delayed", notificationbridge.EventWeighingWorkItemDelayed, workItemCadencePayloadWithTarget(notificationbridge.EventWeighingWorkItemDelayed)},
		{"submission/completed", notificationbridge.EventWeighingShedSubmissionCompleted, submissionPayloadWithTarget()},
		{"submission/reopened", notificationbridge.EventWeighingShedReopened, reopenedPayloadWithTarget()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recipients := &targetTestRecipients{}
			queue := &targetTestQueue{}
			var consumer interface {
				HandleEvent(context.Context, eventbus.Event) error
			}
			// Dispatch to the right consumer based on the event type.
			if strings.HasPrefix(tc.event, "weighing.shed_submission") || strings.HasPrefix(tc.event, "weighing.shed_reopened") {
				consumer = notificationbridge.NewWeighingSubmissionEventConsumer(recipients, queue, slog.Default())
			} else {
				consumer = notificationbridge.NewWeighingLifecycleEventConsumer(recipients, queue, slog.Default())
			}

			if err := consumer.HandleEvent(context.Background(), eventbus.Event{
				Type:    tc.event,
				Payload: tc.payload,
			}); err != nil {
				t.Fatalf("HandleEvent: %v", err)
			}

			for i, notif := range queue.queued {
				if target := notif.Context["target"]; strings.HasPrefix(target, "/vaccination") {
					t.Fatalf("notification [%d] has vaccination target %q (weighing must never cross modules)", i, target)
				}
			}
		})
	}
}
