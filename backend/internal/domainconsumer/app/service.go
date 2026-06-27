package app

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

type Message struct {
	ID              string
	Data            []byte
	Attributes      map[string]string
	DeliveryAttempt int
}

type Handler func(context.Context, Message) error

type Subscriber interface {
	Receive(ctx context.Context, subscriptionID string, handler Handler) error
}

type Result struct {
	EventType string
	TenantID  string
	Key       string
}

type Service struct {
	bus       eventbus.Bus
	validator *outboxapp.EnvelopeValidator
	log       *slog.Logger
}

func NewService(bus eventbus.Bus, validator *outboxapp.EnvelopeValidator, log ...*slog.Logger) *Service {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Service{bus: bus, validator: validator, log: l}
}

func (s *Service) Run(ctx context.Context, subscriber Subscriber, subscriptionID string) error {
	if subscriber == nil {
		return fmt.Errorf("domain consumer subscriber is not configured")
	}
	if subscriptionID == "" {
		return fmt.Errorf("domain consumer subscription id is required")
	}
	return subscriber.Receive(ctx, subscriptionID, s.HandleMessage)
}

func (s *Service) HandleMessage(ctx context.Context, message Message) (err error) {
	if s == nil || s.bus == nil {
		return fmt.Errorf("domain consumer bus is not configured")
	}
	defer func() {
		if p := recover(); p != nil {
			if s.log != nil {
				s.log.ErrorContext(ctx, "domain_consumer_panic",
					slog.Any("panic", p),
					slog.String("stack", string(debug.Stack())),
					slog.String("message_id", message.ID),
					slog.String("event_type", message.Attributes["event_type"]),
					slog.String("outbox_id", message.Attributes["outbox_id"]),
				)
			}
			err = fmt.Errorf("domain consumer panic")
		}
	}()
	if s.validator == nil {
		return fmt.Errorf("domain consumer envelope validator is not configured")
	}
	if err := s.validator.Validate(message.Data); err != nil {
		return fmt.Errorf("invalid domain event envelope: %w", err)
	}
	event, err := eventbus.EventFromEnvelope(message.Data, eventbus.Event{
		Type:     message.Attributes["event_type"],
		TenantID: message.Attributes["tenant_id"],
	})
	if err != nil {
		return err
	}
	if err := s.bus.Publish(ctx, event); err != nil {
		return fmt.Errorf("domain event dispatch %s/%s: %w", event.Type, event.Key, err)
	}
	if s.log != nil {
		s.log.InfoContext(ctx, "domain_event_consumed",
			slog.String("message_id", message.ID),
			slog.String("event_type", event.Type),
			slog.String("tenant_id", event.TenantID),
			slog.String("aggregate_id", event.Key),
			slog.Int("delivery_attempt", message.DeliveryAttempt),
		)
	}
	return nil
}
