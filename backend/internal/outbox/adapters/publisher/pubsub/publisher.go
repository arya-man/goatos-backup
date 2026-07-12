// Package pubsub adapts the outbox ports.Publisher to a Google Cloud Pub/Sub topic.
//
// The adapter logic (attribute building, error classification, event_id dedup) depends only on a
// small MessagePublisher seam, so it is fully unit-testable without the GCP SDK, an emulator, or
// network. The real binding to cloud.google.com/go/pubsub/v2 (a thin wrapper implementing
// MessagePublisher, honouring PUBSUB_EMULATOR_HOST for local dev and mapping gRPC status codes to
// retryable/permanent errors) is added at deploy wiring time — it is the only network-gated piece.
package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/outbox/ports"
	"github.com/vgoats/goatos/backend/internal/platform/tracecontext"
)

// MessagePublisher is the broker seam the adapter needs. Implementations:
//   - the real GCP Pub/Sub client wrapper (deploy-time), and
//   - the in-memory fake used by tests.
//
// Publish sends a message body + attributes to a topic and returns the broker's server-assigned
// id. It SHOULD classify failures by wrapping them in ports.RetryablePublishError /
// ports.PermanentPublishError; the adapter treats any unclassified error as retryable.
type MessagePublisher interface {
	Publish(ctx context.Context, topicID string, data []byte, attributes map[string]string) (serverID string, err error)
}

// Publisher implements ports.Publisher over a MessagePublisher.
type Publisher struct {
	client  MessagePublisher
	topicID string
}

type Config struct {
	// TopicID is the physical Pub/Sub topic used for the outbox stream.
	// Outbox rows keep their logical topic in the message and attributes.
	TopicID string
}

// NewPublisher constructs a Publisher around a broker client.
func NewPublisher(client MessagePublisher) *Publisher {
	return &Publisher{client: client}
}

// NewPublisherWithConfig constructs a Publisher with a physical Pub/Sub topic override.
func NewPublisherWithConfig(client MessagePublisher, config Config) *Publisher {
	return &Publisher{client: client, topicID: config.TopicID}
}

var _ ports.Publisher = (*Publisher)(nil)

// Publish forwards an outbox message to Pub/Sub. The event_id attribute drives broker-side
// dedup so a re-published outbox row does not double-deliver. Classification: an error already
// carrying retry/permanent intent is passed through; an unclassified transport error defaults to
// retryable (at-least-once safe).
func (p *Publisher) Publish(ctx context.Context, m ports.PublishMessage) error {
	attrs := map[string]string{
		"event_id":      m.EventID,
		"event_type":    m.EventType,
		"tenant_id":     m.TenantID,
		"outbox_id":     m.OutboxID,
		"logical_topic": m.Topic,
	}
	if m.TraceID != nil && *m.TraceID != "" {
		attrs["trace_id"] = *m.TraceID
	}
	// Forward any headers the producer attached (e.g. a future producer's
	// "traceparent" entry - see internal/platform/tracecontext's doc comment
	// for the current scope of that wiring), then stamp the CURRENT span's
	// own W3C trace context on top so the consumer can always continue a
	// trace from this publish span, even for producers that have not yet
	// been wired to set traceparent at write time.
	if len(m.Headers) > 0 {
		var headers map[string]string
		if err := json.Unmarshal(m.Headers, &headers); err == nil {
			for k, v := range headers {
				if _, exists := attrs[k]; !exists {
					attrs[k] = v
				}
			}
		}
	}
	tracecontext.Inject(ctx, attrs)

	topicID := p.topicID
	if topicID == "" {
		topicID = m.Topic
	}
	if _, err := p.client.Publish(ctx, topicID, []byte(m.Payload), attrs); err != nil {
		var pe *ports.PublishError
		if errors.As(err, &pe) {
			return err // already classified (retryable or permanent)
		}
		return ports.RetryablePublishError(fmt.Errorf("pubsub publish topic %q event %q: %w", topicID, m.EventID, err))
	}
	return nil
}
