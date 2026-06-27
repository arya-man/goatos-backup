package pubsub

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

// fakeBroker is an in-memory MessagePublisher. It models broker-side dedup keyed on the event_id
// attribute: re-publishing the same event_id returns the original server id without a new delivery.
type fakeBroker struct {
	delivered map[string]string // event_id -> serverID
	lastTopic string
	lastAttrs map[string]string
	failWith  error
	seq       int
}

func (f *fakeBroker) Publish(_ context.Context, topicID string, _ []byte, attrs map[string]string) (string, error) {
	f.lastTopic = topicID
	f.lastAttrs = attrs
	if f.failWith != nil {
		return "", f.failWith
	}
	if f.delivered == nil {
		f.delivered = map[string]string{}
	}
	eid := attrs["event_id"]
	if id, ok := f.delivered[eid]; ok {
		return id, nil // dedup: already delivered this event
	}
	f.seq++
	id := "srv-" + strconv.Itoa(f.seq)
	f.delivered[eid] = id
	return id, nil
}

func msg(eventID string) ports.PublishMessage {
	trace := "trace-abc"
	return ports.PublishMessage{
		OutboxID:  "ob-1",
		TenantID:  "00000000-0000-4000-8000-000000000001",
		EventID:   eventID,
		EventType: "goat.created",
		Topic:     "identity.events",
		Payload:   []byte(`{"k":1}`),
		TraceID:   &trace,
	}
}

func TestPublishSuccessSetsAttributes(t *testing.T) {
	broker := &fakeBroker{}
	pub := NewPublisher(broker)
	if err := pub.Publish(context.Background(), msg("evt-1")); err != nil {
		t.Fatalf("publish success: %v", err)
	}
	if len(broker.delivered) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(broker.delivered))
	}
	for _, k := range []string{"event_id", "event_type", "tenant_id", "outbox_id", "trace_id"} {
		if broker.lastAttrs[k] == "" {
			t.Fatalf("expected attribute %q to be set, attrs=%v", k, broker.lastAttrs)
		}
	}
	if broker.lastTopic != "identity.events" {
		t.Fatalf("expected default logical topic publish, got %q", broker.lastTopic)
	}
	if broker.lastAttrs["logical_topic"] != "identity.events" {
		t.Fatalf("expected logical topic attribute, attrs=%v", broker.lastAttrs)
	}
}

func TestPublishUsesConfiguredPhysicalTopic(t *testing.T) {
	broker := &fakeBroker{}
	pub := NewPublisherWithConfig(broker, Config{TopicID: "goatos-dev-outbox-events"})
	if err := pub.Publish(context.Background(), msg("evt-1")); err != nil {
		t.Fatalf("publish success: %v", err)
	}
	if broker.lastTopic != "goatos-dev-outbox-events" {
		t.Fatalf("expected configured physical topic, got %q", broker.lastTopic)
	}
	if broker.lastAttrs["logical_topic"] != "identity.events" {
		t.Fatalf("expected logical topic attribute to remain identity.events, attrs=%v", broker.lastAttrs)
	}
}

func TestPublishTransientErrorIsRetryable(t *testing.T) {
	broker := &fakeBroker{failWith: ports.RetryablePublishError(errors.New("unavailable"))}
	err := NewPublisher(broker).Publish(context.Background(), msg("evt-1"))
	if err == nil || !ports.IsRetryablePublishFailure(err) {
		t.Fatalf("expected retryable failure, got %v (retryable=%v)", err, ports.IsRetryablePublishFailure(err))
	}
}

func TestPublishPermanentErrorGoesToDLQ(t *testing.T) {
	broker := &fakeBroker{failWith: ports.PermanentPublishError(errors.New("invalid argument"))}
	err := NewPublisher(broker).Publish(context.Background(), msg("evt-1"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if ports.IsRetryablePublishFailure(err) {
		t.Fatalf("expected permanent (non-retryable) failure for DLQ routing, got retryable: %v", err)
	}
}

func TestPublishUnclassifiedErrorDefaultsRetryable(t *testing.T) {
	broker := &fakeBroker{failWith: errors.New("boom")}
	err := NewPublisher(broker).Publish(context.Background(), msg("evt-1"))
	if err == nil || !ports.IsRetryablePublishFailure(err) {
		t.Fatalf("expected unclassified error to default retryable, got %v", err)
	}
}

func TestPublishDedupOnEventID(t *testing.T) {
	broker := &fakeBroker{}
	pub := NewPublisher(broker)
	for i := 0; i < 3; i++ {
		if err := pub.Publish(context.Background(), msg("evt-dup")); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	if len(broker.delivered) != 1 {
		t.Fatalf("expected broker dedup to a single delivery for one event_id, got %d", len(broker.delivered))
	}
}
