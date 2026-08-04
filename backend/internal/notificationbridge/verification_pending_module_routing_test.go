package notificationbridge_test

// Module routing of verification.item.pending.
//
// The generic verification consumer is the push layer for EVERY module that enqueues a generic
// verification item (vaccination, weighing, feed, ...). Weighing submissions create items with
// Module="weighing" (weighing/adapters/verificationbridge/enqueue.go), so a weighing proof must
// reach the WEIGHING verify-duty holders and the GROWTH director -- never the vaccination verifier
// or the PC director, who do not own weighing at all.
//
// projection-review: recipient resolution is a routing decision, not an aggregate. The grain proof
// is the routing key: the producer's item carries exactly one Module value
// (verificationdomain.CreateItem.Module, 1 row per submission), and the consumer must select its
// duty-module + leadership-position pair from that same single key. Producer key = {module};
// consumer match key = {module}; multiplicity 1:1, so no fan-out is possible.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

const (
	pendTenant = "aaaaaaaa-1111-4111-8111-111111111111"
	pendPark   = "aaaaaaaa-2222-4222-8222-222222222222"
	pendShed   = "aaaaaaaa-3333-4333-8333-333333333333"
	pendItem   = "aaaaaaaa-4444-4444-8444-444444444444"
)

type dutyAsk struct {
	scopeType string
	scopeID   string
	module    string
	duty      string
}

type positionAsk struct {
	scopeType string
	scopeID   string
	position  string
}

// pendRecipients records EVERY resolution question the consumer asks, and answers with one device
// per (module,duty) / position so the queued recipient set names who was actually selected.
type pendRecipients struct {
	dutyAsks     []dutyAsk
	positionAsks []positionAsk
}

func (f *pendRecipients) ResolveModuleDutyRecipients(_ context.Context, tenantID, scopeType, scopeID, moduleCode, dutyType string) ([]workforcedomain.NotificationRecipient, error) {
	if tenantID != pendTenant {
		return nil, fmt.Errorf("unexpected tenant %q", tenantID)
	}
	f.dutyAsks = append(f.dutyAsks, dutyAsk{scopeType: scopeType, scopeID: scopeID, module: moduleCode, duty: dutyType})
	return []workforcedomain.NotificationRecipient{{
		WorkforceMemberID: "member-" + moduleCode + "-" + dutyType,
		DeviceID:          "device-" + moduleCode + "-" + dutyType,
		FCMToken:          "token-" + moduleCode + "-" + dutyType,
	}}, nil
}

func (f *pendRecipients) ResolveMemberRecipients(context.Context, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}

func (f *pendRecipients) ResolvePositionRecipients(_ context.Context, tenantID, scopeType, scopeID, positionCode string) ([]workforcedomain.NotificationRecipient, error) {
	if tenantID != pendTenant {
		return nil, fmt.Errorf("unexpected tenant %q", tenantID)
	}
	f.positionAsks = append(f.positionAsks, positionAsk{scopeType: scopeType, scopeID: scopeID, position: positionCode})
	return []workforcedomain.NotificationRecipient{{
		WorkforceMemberID: "member-" + positionCode,
		DeviceID:          "device-" + positionCode,
		FCMToken:          "token-" + positionCode,
	}}, nil
}

func pendPendingPayload(module, parkID string) []byte {
	raw, err := json.Marshal(map[string]any{
		"tenant_id":     pendTenant,
		"item_id":       pendItem,
		"vertical":      module,
		"module":        module,
		"category":      module,
		"subject_label": "Shed 4",
		"shed_id":       pendShed,
		"park_id":       parkID,
		"source":        map[string]any{"module": module, "ref_type": "shed", "ref_id": pendItem},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

// TestVerificationItemPendingRoutesWeighingToWeighingOwners is the RED test for the wrong-recipient
// half of the defect: a weighing proof must notify weighing verify-duty holders and the growth
// director, and must NOT notify the vaccination verifier or the PC director.
func TestVerificationItemPendingRoutesWeighingToWeighingOwners(t *testing.T) {
	recipients := &pendRecipients{}
	queue := &fakeQueue{}
	consumer := notificationbridge.NewVerificationEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventVerificationItemPending,
		Payload: pendPendingPayload("weighing", pendPark),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	for _, ask := range recipients.dutyAsks {
		if ask.module != "weighing" {
			t.Fatalf("weighing item resolved verify duty for module %q, want weighing", ask.module)
		}
		if ask.duty != "verify" {
			t.Fatalf("verify duty type = %q", ask.duty)
		}
	}
	if len(recipients.dutyAsks) == 0 {
		t.Fatal("no verify-duty resolution was attempted for a weighing item")
	}
	sawGrowth := false
	for _, ask := range recipients.positionAsks {
		if ask.position == "pc_director" {
			t.Fatalf("weighing item notified the PC director (positions asked: %+v)", recipients.positionAsks)
		}
		if ask.position == "growth_director" {
			sawGrowth = true
		}
	}
	if !sawGrowth {
		t.Fatalf("weighing item never notified the growth director (positions asked: %+v)", recipients.positionAsks)
	}
	if len(queue.queued) == 0 {
		t.Fatal("no notification was queued for a weighing pending proof")
	}
	for _, queued := range queue.queued {
		assertNoTokens(t, queued, "token-pc.vaccination-verify", "token-pc_director")
	}
}

// TestVerificationItemPendingKeepsVaccinationRouting pins the vaccination side so the module branch
// cannot regress the path that already worked.
func TestVerificationItemPendingKeepsVaccinationRouting(t *testing.T) {
	recipients := &pendRecipients{}
	queue := &fakeQueue{}
	consumer := notificationbridge.NewVerificationEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventVerificationItemPending,
		Payload: pendPendingPayload("vaccination", pendPark),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	for _, ask := range recipients.dutyAsks {
		if ask.module != "pc.vaccination" {
			t.Fatalf("vaccination item resolved verify duty for module %q", ask.module)
		}
	}
	sawPCDirector := false
	for _, ask := range recipients.positionAsks {
		if ask.position == "pc_director" {
			sawPCDirector = true
		}
		if ask.position == "growth_director" {
			t.Fatalf("vaccination item notified the growth director")
		}
	}
	if !sawPCDirector {
		t.Fatal("vaccination item no longer notifies the PC director")
	}
}

// capturingHandler records the log records the consumer emits so a silent no-op is observable.
type capturingHandler struct {
	slog.Handler
	messages []string
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, record slog.Record) error {
	h.messages = append(h.messages, record.Message)
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

// TestVerificationItemPendingWarnsOnMissingPark pins the second half of the defect: a well-formed
// item with no park cannot notify anyone, and must say so instead of returning nil in silence.
func TestVerificationItemPendingWarnsOnMissingPark(t *testing.T) {
	handler := &capturingHandler{}
	recipients := &pendRecipients{}
	queue := &fakeQueue{}
	consumer := notificationbridge.NewVerificationEventConsumer(recipients, queue, slog.New(handler))

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventVerificationItemPending,
		Payload: pendPendingPayload("weighing", ""),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("a park-less item queued %d notifications", len(queue.queued))
	}
	found := false
	for _, message := range handler.messages {
		if message == "verification_pending_notification_missing_park" {
			found = true
		}
	}
	if !found {
		t.Fatalf("park-less item was dropped silently; logged: %v", handler.messages)
	}
}

func assertNoTokens(t *testing.T, queued calendarports.QueueRoleNotifications, banned ...string) {
	t.Helper()
	for _, recipient := range queued.Recipients {
		for _, token := range banned {
			if recipient.FCMToken == token {
				t.Fatalf("queued notification %q reached banned recipient %q", queued.Title, token)
			}
		}
	}
}

// TestVerificationItemPendingRoutesFeedToFeedDirector and its counts twin extend the same
// wrong-recipient proof to the two modules claimed on 2026-08-01. A feed proof must reach the
// FEED verify-duty holders and the FEED director, and a counts proof the HEALTH director --
// never the PC director, and never in another module's words.
func TestVerificationItemPendingRoutesFeedToFeedDirector(t *testing.T) {
	recipients := &pendRecipients{}
	queue := &fakeQueue{}
	consumer := notificationbridge.NewVerificationEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventVerificationItemPending,
		Payload: pendPendingPayload("feed", pendPark),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(recipients.dutyAsks) == 0 {
		t.Fatal("no verify-duty resolution was attempted for a feed item")
	}
	for _, ask := range recipients.dutyAsks {
		if ask.module != "feed.direction" || ask.duty != "verify" {
			t.Fatalf("feed item resolved duty (module=%q duty=%q), want (feed.direction, verify)", ask.module, ask.duty)
		}
	}
	sawFeedDirector := false
	for _, ask := range recipients.positionAsks {
		switch ask.position {
		case "pc_director", "growth_director", "health_director":
			t.Fatalf("feed item notified %q, which does not own feed", ask.position)
		case "feed_director":
			sawFeedDirector = true
		}
	}
	if !sawFeedDirector {
		t.Fatalf("feed item never notified the feed director (positions asked: %+v)", recipients.positionAsks)
	}
	assertModuleWording(t, queue, "feed", "/feed")
}

func TestVerificationItemPendingRoutesCountsToHealthDirector(t *testing.T) {
	recipients := &pendRecipients{}
	queue := &fakeQueue{}
	consumer := notificationbridge.NewVerificationEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventVerificationItemPending,
		Payload: pendPendingPayload("counts", pendPark),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(recipients.dutyAsks) == 0 {
		t.Fatal("no verify-duty resolution was attempted for a counts item")
	}
	for _, ask := range recipients.dutyAsks {
		if ask.module != "counts" || ask.duty != "verify" {
			t.Fatalf("counts item resolved duty (module=%q duty=%q), want (counts, verify)", ask.module, ask.duty)
		}
	}
	sawHealthDirector := false
	for _, ask := range recipients.positionAsks {
		// health_director is a DISTINCT role from pc_director; routing counts to the PC
		// Director is the exact wrong-module defect this profile exists to prevent.
		if ask.position == "pc_director" {
			t.Fatalf("counts item notified the PC director (positions asked: %+v)", recipients.positionAsks)
		}
		if ask.position == "health_director" {
			sawHealthDirector = true
		}
	}
	if !sawHealthDirector {
		t.Fatalf("counts item never notified the health director (positions asked: %+v)", recipients.positionAsks)
	}
	assertModuleWording(t, queue, "counts", "/counts")
}

// assertModuleWording proves the copy and tap route belong to the module: nothing queued may
// say "vaccinated" or point at /vaccination, and the leadership push must target the module's
// own surface.
func assertModuleWording(t *testing.T, queue *fakeQueue, module, wantTarget string) {
	t.Helper()
	if len(queue.queued) == 0 {
		t.Fatalf("no notification was queued for a %s pending proof", module)
	}
	sawTarget := false
	for _, queued := range queue.queued {
		lowered := strings.ToLower(queued.Title + " " + queued.Body)
		if strings.Contains(lowered, "vaccinat") {
			t.Fatalf("%s notification borrows vaccination wording: %q / %q", module, queued.Title, queued.Body)
		}
		if target := queued.Context["target"]; strings.HasPrefix(target, "/vaccination") {
			t.Fatalf("%s notification taps through to %q", module, target)
		}
		if queued.Context["target"] == wantTarget {
			sawTarget = true
		}
	}
	if !sawTarget {
		t.Fatalf("no %s notification tapped through to %s", module, wantTarget)
	}
}
