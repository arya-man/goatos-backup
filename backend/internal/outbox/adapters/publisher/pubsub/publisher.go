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
	"errors"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/outbox/ports"
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
	client MessagePublisher
}

// NewPublisher constructs a Publisher around a broker client.
func NewPublisher(client MessagePublisher) *Publisher {
	return &Publisher{client: client}
}

var _ ports.Publisher = (*Publisher)(nil)

// Publish forwards an outbox message to Pub/Sub. The event_id attribute drives broker-side
// dedup so a re-published outbox row does not double-deliver. Classification: an error already
// carrying retry/permanent intent is passed through; an unclassified transport error defaults to
// retryable (at-least-once safe).
func (p *Publisher) Publish(ctx context.Context, m ports.PublishMessage) error {
	attrs := map[string]string{
		"event_id":   m.EventID,
		"event_type": m.EventType,
		"tenant_id":  m.TenantID,
		"outbox_id":  m.OutboxID,
	}
	if m.TraceID != nil && *m.TraceID != "" {
		attrs["trace_id"] = *m.TraceID
	}

	if _, err := p.client.Publish(ctx, m.Topic, []byte(m.Payload), attrs); err != nil {
		var pe *ports.PublishError
		if errors.As(err, &pe) {
			return err // already classified (retryable or permanent)
		}
		return ports.RetryablePublishError(fmt.Errorf("pubsub publish topic %q event %q: %w", m.Topic, m.EventID, err))
	}
	return nil
}
