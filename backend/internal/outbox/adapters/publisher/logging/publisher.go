package logging

import (
	"context"
	"log/slog"

	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

type Publisher struct {
	logger *slog.Logger
}

func NewPublisher(logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Publisher{logger: logger}
}

func (p *Publisher) Publish(ctx context.Context, message ports.PublishMessage) error {
	attrs := []any{
		"outbox_id", message.OutboxID,
		"tenant_id", message.TenantID,
		"event_id", message.EventID,
		"event_type", message.EventType,
		"topic", message.Topic,
	}
	if message.TraceID != nil {
		attrs = append(attrs, "trace_id", *message.TraceID)
	}
	p.logger.InfoContext(ctx, "outbox message publish noop", attrs...)
	return nil
}
