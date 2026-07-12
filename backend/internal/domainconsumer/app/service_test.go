package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

func TestHandleMessageDispatchesEnvelope(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	var got eventbus.Event
	bus.Subscribe("goat.location.changed", eventbus.HandlerFunc(func(_ context.Context, e eventbus.Event) error {
		got = e
		return nil
	}))
	service := NewService(bus, testValidator(t))
	err := service.HandleMessage(context.Background(), Message{
		ID:   "msg-1",
		Data: testEnvelope(t, "goat.location.changed", "10000000-0000-4000-8000-000000000001"),
		Attributes: map[string]string{
			"event_type": "goat.location.changed",
			"tenant_id":  "00000000-0000-4000-8000-000000000001",
		},
		DeliveryAttempt: 2,
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got.Type != "goat.location.changed" || got.Key != "10000000-0000-4000-8000-000000000001" || got.TenantID == "" {
		t.Fatalf("unexpected event: %#v", got)
	}
}

func TestHandleMessageReturnsErrorForInvalidEnvelope(t *testing.T) {
	service := NewService(eventbus.NewInProcessBus(), testValidator(t))
	if err := service.HandleMessage(context.Background(), Message{ID: "bad", Data: []byte(`{"event_type":"nope"}`)}); err == nil {
		t.Fatal("expected invalid envelope error")
	}
}

func TestHandleMessageReturnsHandlerErrorForRetry(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	bus.Subscribe("goat.created", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error {
		return errors.New("database temporarily unavailable")
	}))
	service := NewService(bus, testValidator(t))
	if err := service.HandleMessage(context.Background(), Message{ID: "msg-2", Data: testEnvelope(t, "goat.created", "10000000-0000-4000-8000-000000000002")}); err == nil {
		t.Fatal("expected handler error")
	}
}

func TestHandleMessageAcceptsVaccinationVerificationEnvelope(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	var got eventbus.Event
	bus.Subscribe("vaccination.verify.accepted", eventbus.HandlerFunc(func(_ context.Context, e eventbus.Event) error {
		got = e
		return nil
	}))
	service := NewService(bus, testValidator(t))
	completionID := "70000000-0000-4000-8000-000000000001"
	if err := service.HandleMessage(context.Background(), Message{
		ID:   "msg-verify-1",
		Data: testEnvelopeWithTypes(t, "vaccination.verify.accepted", "vaccination_completion", completionID, "vaccination_completion", completionID, map[string]any{"completion_id": completionID}),
	}); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got.Type != "vaccination.verify.accepted" || got.Key != completionID || got.TenantID == "" {
		t.Fatalf("unexpected verification event: %#v", got)
	}
}

func TestRunUsesSubscriberAndAcksThroughAdapterContract(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	service := NewService(bus, testValidator(t))
	subscriber := &fakeSubscriber{message: Message{ID: "msg-3", Data: testEnvelope(t, "goat.created", "10000000-0000-4000-8000-000000000003")}}
	if err := service.Run(context.Background(), subscriber, "domain-sub"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if subscriber.subscriptionID != "domain-sub" || subscriber.handled != 1 {
		t.Fatalf("subscriber = %#v", subscriber)
	}
}

func TestHandleMessageSkipsAlreadyProcessedEvent(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	calls := 0
	bus.Subscribe("goat.created", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error {
		calls++
		return nil
	}))
	store := &fakeProcessedStore{decision: ProcessDecisionAlreadyProcessed}
	service := NewService(bus, testValidator(t)).WithProcessedEventStore(store)
	err := service.HandleMessage(context.Background(), Message{
		ID:   "msg-processed",
		Data: testEnvelope(t, "goat.created", "10000000-0000-4000-8000-000000000004"),
		Attributes: map[string]string{
			"event_id": "attribute-event-id-must-not-win",
		},
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if calls != 0 || store.completed != 0 || store.failed != 0 {
		t.Fatalf("calls=%d store=%#v, want duplicate skip", calls, store)
	}
	if store.last.EventID != "60000000-0000-4000-8000-000000000001" {
		t.Fatalf("processed event id=%q, want envelope event_id", store.last.EventID)
	}
}

func TestHandleMessageReturnsInProgressErrorForActiveProcessingRedelivery(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	calls := 0
	bus.Subscribe("goat.created", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error {
		calls++
		return nil
	}))
	store := &fakeProcessedStore{decision: ProcessDecisionInProgress}
	service := NewService(bus, testValidator(t)).WithProcessedEventStore(store)
	err := service.HandleMessage(context.Background(), Message{
		ID:   "msg-processing",
		Data: testEnvelope(t, "goat.created", "10000000-0000-4000-8000-000000000006"),
	})
	if !errors.Is(err, ErrEventProcessingInProgress) {
		t.Fatalf("err=%v, want ErrEventProcessingInProgress", err)
	}
	if calls != 0 || store.completed != 0 || store.failed != 0 {
		t.Fatalf("calls=%d store=%#v, want in-progress retry without handler", calls, store)
	}
}

func TestHandleMessageMarksProcessedStoreFailedForRetry(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	bus.Subscribe("goat.created", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error {
		return errors.New("temporary handler failure")
	}))
	store := &fakeProcessedStore{decision: ProcessDecisionClaimed}
	service := NewService(bus, testValidator(t)).WithProcessedEventStore(store)
	err := service.HandleMessage(context.Background(), Message{
		ID:   "msg-failed",
		Data: testEnvelope(t, "goat.created", "10000000-0000-4000-8000-000000000005"),
		Attributes: map[string]string{
			"event_id": "60000000-0000-4000-8000-000000000005",
		},
	})
	if err == nil {
		t.Fatal("expected handler error")
	}
	if store.failed != 1 || store.completed != 0 {
		t.Fatalf("store=%#v, want one failed mark", store)
	}
}

func TestHandleMessageMarksPermanentHandlerErrorProcessed(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	bus.Subscribe("goat.created", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error {
		return eventbus.PermanentError(errors.New("invalid event payload"))
	}))
	store := &fakeProcessedStore{decision: ProcessDecisionClaimed}
	service := NewService(bus, testValidator(t)).WithProcessedEventStore(store)
	err := service.HandleMessage(context.Background(), Message{
		ID:   "msg-permanent",
		Data: testEnvelope(t, "goat.created", "10000000-0000-4000-8000-000000000009"),
	})
	if err == nil {
		t.Fatal("expected permanent handler error to NACK for DLQ")
	}
	if store.completed != 0 || store.failed != 1 {
		t.Fatalf("store=%#v, want permanent failure marked failed for DLQ", store)
	}
}

func TestHandleMessageMarksProcessedStoreFailedAfterHandlerPanic(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	bus.Subscribe("goat.created", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error {
		panic("synthetic handler panic")
	}))
	store := &fakeProcessedStore{decision: ProcessDecisionClaimed}
	service := NewService(bus, testValidator(t)).WithProcessedEventStore(store)
	err := service.HandleMessage(context.Background(), Message{
		ID:   "msg-panic",
		Data: testEnvelope(t, "goat.created", "10000000-0000-4000-8000-000000000007"),
	})
	if err == nil {
		t.Fatal("expected panic recovery error")
	}
	if store.failed != 1 || store.completed != 0 {
		t.Fatalf("store=%#v, want one failed mark after panic", store)
	}
}

func TestHandleMessageAcksAfterDispatchWhenProcessedFinalizationFails(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	calls := 0
	bus.Subscribe("goat.created", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error {
		calls++
		return nil
	}))
	store := &fakeProcessedStore{decision: ProcessDecisionClaimed, markErr: ErrProcessedEventFinalizationLost}
	service := NewService(bus, testValidator(t)).WithProcessedEventStore(store)
	err := service.HandleMessage(context.Background(), Message{
		ID:   "msg-finalize-lost",
		Data: testEnvelope(t, "goat.created", "10000000-0000-4000-8000-000000000008"),
	})
	if err == nil {
		t.Fatal("expected processed-event finalization error")
	}
	// C35-024: the handler side effect already committed (calls==1). A MarkProcessed failure after a
	// successful dispatch must NOT be recorded via MarkFailed - that would allow an unconditional
	// reclaim on redelivery to replay bus.Publish and duplicate the side effect. It must instead be
	// durably recorded as effects-committed so redelivery only retries the finalize.
	if calls != 1 || store.completed != 1 || store.failed != 0 || store.effectsCommitted != 1 {
		t.Fatalf("calls=%d store=%#v, want dispatch recorded, effects committed once, and no failed mark", calls, store)
	}
}

// TestHandleMessageDoesNotReplayHandlerOnRedeliveryAfterMarkProcessedFailure reproduces C35-024: a
// handler side effect commits, the terminal MarkProcessed call then fails (e.g. a transient DB
// error), and a redelivery of the exact same event must reclaim for finalize-only retry - it must
// NEVER re-invoke bus.Publish, which would replay a non-idempotent handler side effect.
func TestHandleMessageDoesNotReplayHandlerOnRedeliveryAfterMarkProcessedFailure(t *testing.T) {
	bus := eventbus.NewInProcessBus()
	calls := 0
	bus.Subscribe("goat.created", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error {
		calls++
		return nil
	}))
	store := &replayGuardStore{markProcessedFailures: 1}
	service := NewService(bus, testValidator(t)).WithProcessedEventStore(store)
	message := Message{
		ID:   "msg-replay-guard",
		Data: testEnvelope(t, "goat.created", "10000000-0000-4000-8000-000000000010"),
	}

	if err := service.HandleMessage(context.Background(), message); err == nil {
		t.Fatal("expected first delivery to surface the synthetic finalize failure")
	}
	if calls != 1 {
		t.Fatalf("calls after first delivery = %d, want 1", calls)
	}
	if store.status != "effects_committed" {
		t.Fatalf("status after first delivery = %q, want effects_committed (must not fall back to failed)", store.status)
	}

	message.DeliveryAttempt = 1
	if err := service.HandleMessage(context.Background(), message); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls after redelivery = %d, want 1 (handler must not replay)", calls)
	}
	if store.status != "processed" {
		t.Fatalf("status after redelivery = %q, want processed", store.status)
	}
}

// replayGuardStore is a minimal single-event fake that mirrors the real Postgres adapter's status
// state machine (processing / effects_committed / processed / failed) closely enough to exercise
// Service.handleMessage's decision branching without a real database.
type replayGuardStore struct {
	status                string
	markProcessedFailures int
}

func (s *replayGuardStore) BeginProcessing(context.Context, ProcessedEvent) (ProcessDecision, error) {
	switch s.status {
	case "", "failed":
		s.status = "processing"
		return ProcessDecisionClaimed, nil
	case "effects_committed":
		return ProcessDecisionEffectsCommitted, nil
	case "processed":
		return ProcessDecisionAlreadyProcessed, nil
	default:
		return ProcessDecisionInProgress, nil
	}
}

func (s *replayGuardStore) MarkEffectsCommitted(context.Context, ProcessedEvent) error {
	s.status = "effects_committed"
	return nil
}

func (s *replayGuardStore) MarkProcessed(context.Context, ProcessedEvent) error {
	if s.markProcessedFailures > 0 {
		s.markProcessedFailures--
		return errors.New("synthetic transient finalize failure")
	}
	s.status = "processed"
	return nil
}

func (s *replayGuardStore) MarkFailed(context.Context, ProcessedEvent, string) error {
	s.status = "failed"
	return nil
}

type fakeSubscriber struct {
	message        Message
	subscriptionID string
	handled        int
}

func (f *fakeSubscriber) Receive(ctx context.Context, subscriptionID string, handler Handler) error {
	f.subscriptionID = subscriptionID
	f.handled++
	return handler(ctx, f.message)
}

type fakeProcessedStore struct {
	decision         ProcessDecision
	started          int
	completed        int
	failed           int
	effectsCommitted int
	effectsCommitErr error
	last             ProcessedEvent
	markErr          error
}

func (f *fakeProcessedStore) BeginProcessing(_ context.Context, event ProcessedEvent) (ProcessDecision, error) {
	f.started++
	f.last = event
	return f.decision, nil
}

func (f *fakeProcessedStore) MarkEffectsCommitted(context.Context, ProcessedEvent) error {
	f.effectsCommitted++
	return f.effectsCommitErr
}

func (f *fakeProcessedStore) MarkProcessed(context.Context, ProcessedEvent) error {
	f.completed++
	return f.markErr
}

func (f *fakeProcessedStore) MarkFailed(context.Context, ProcessedEvent, string) error {
	f.failed++
	return nil
}

func testEnvelope(t *testing.T, eventType, aggregateID string) []byte {
	t.Helper()
	return testEnvelopeWithTypes(t, eventType, "goat", aggregateID, "goat", aggregateID, map[string]any{
		"goat_id":    aggregateID,
		"scope_type": "shed",
		"scope_id":   "00000000-0000-4000-8000-000000004001",
	})
}

func testEnvelopeWithTypes(t *testing.T, eventType, aggregateType, aggregateID, subjectType, subjectID string, payload map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"event_id":        "60000000-0000-4000-8000-000000000001",
		"event_type":      eventType,
		"schema_version":  "1.0.0",
		"schema_ref":      "contracts/jsonschema/domain-event-envelope.schema.json",
		"aggregate_type":  aggregateType,
		"aggregate_id":    aggregateID,
		"occurred_at":     "2026-06-27T01:02:03.000000Z",
		"recorded_at":     "2026-06-27T01:02:04.000000Z",
		"producer":        map[string]any{"service": "goatos-test", "module": "domainconsumer"},
		"idempotency_key": "tenant:domain-consumer-test:00000001",
		"actor":           map[string]any{"actor_type": "system_rule", "actor_id": nil, "actor_ref": "test"},
		"subject_type":    subjectType,
		"subject_id":      subjectID,
		"visibility_scope": map[string]any{
			"tenant_id": "00000000-0000-4000-8000-000000000001",
		},
		"evidence_refs": []map[string]string{},
		"payload":       payload,
		"trace_id":      "trace-domain-consumer-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func testValidator(t *testing.T) *outboxapp.EnvelopeValidator {
	t.Helper()
	validator, err := outboxapp.NewEnvelopeValidator(filepath.Join("..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	return validator
}
