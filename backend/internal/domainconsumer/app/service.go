package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

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

type ProcessedEvent struct {
	TenantID        string
	EventID         string
	EventType       string
	SubscriptionID  string
	MessageID       string
	DeliveryAttempt int
	Now             time.Time
}

type ProcessDecision string

const (
	ProcessDecisionClaimed          ProcessDecision = "claimed"
	ProcessDecisionAlreadyProcessed ProcessDecision = "already_processed"
	ProcessDecisionInProgress       ProcessDecision = "in_progress"
)

var (
	ErrEventProcessingInProgress      = errors.New("domain event processing in progress")
	ErrProcessedEventFinalizationLost = errors.New("domain event processed-event finalization lost")
)

type ProcessedEventStore interface {
	BeginProcessing(ctx context.Context, event ProcessedEvent) (ProcessDecision, error)
	MarkProcessed(ctx context.Context, event ProcessedEvent) error
	MarkFailed(ctx context.Context, event ProcessedEvent, reason string) error
}

type Service struct {
	bus            eventbus.Bus
	validator      *outboxapp.EnvelopeValidator
	processedStore ProcessedEventStore
	now            func() time.Time
	log            *slog.Logger
}

func NewService(bus eventbus.Bus, validator *outboxapp.EnvelopeValidator, log ...*slog.Logger) *Service {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Service{bus: bus, validator: validator, now: func() time.Time { return time.Now().UTC() }, log: l}
}

func (s *Service) WithProcessedEventStore(store ProcessedEventStore) *Service {
	s.processedStore = store
	return s
}

func (s *Service) Run(ctx context.Context, subscriber Subscriber, subscriptionID string) error {
	if subscriber == nil {
		return fmt.Errorf("domain consumer subscriber is not configured")
	}
	if subscriptionID == "" {
		return fmt.Errorf("domain consumer subscription id is required")
	}
	return subscriber.Receive(ctx, subscriptionID, func(ctx context.Context, message Message) error {
		return s.handleMessage(ctx, subscriptionID, message)
	})
}

func (s *Service) HandleMessage(ctx context.Context, message Message) (err error) {
	return s.handleMessage(ctx, "direct", message)
}

func (s *Service) handleMessage(ctx context.Context, subscriptionID string, message Message) (err error) {
	if s == nil || s.bus == nil {
		return fmt.Errorf("domain consumer bus is not configured")
	}
	var processed ProcessedEvent
	claimed := false
	dispatched := false
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
			if dispatched {
				err = nil
			} else {
				err = fmt.Errorf("domain consumer panic")
			}
		}
		if err != nil && claimed && !dispatched && s.processedStore != nil {
			_ = s.processedStore.MarkFailed(ctx, processed, err.Error())
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
	processed = ProcessedEvent{
		TenantID:        event.TenantID,
		EventID:         firstNonEmpty(envelopeEventID(message.Data), message.Attributes["event_id"], message.ID),
		EventType:       event.Type,
		SubscriptionID:  firstNonEmpty(subscriptionID, "direct"),
		MessageID:       message.ID,
		DeliveryAttempt: message.DeliveryAttempt,
		Now:             s.now().UTC(),
	}
	if s.processedStore != nil {
		decision, err := s.processedStore.BeginProcessing(ctx, processed)
		if err != nil {
			return err
		}
		switch decision {
		case ProcessDecisionClaimed:
			claimed = true
		case ProcessDecisionAlreadyProcessed:
			if s.log != nil {
				s.log.InfoContext(ctx, "domain_event_duplicate_skipped",
					slog.String("message_id", message.ID),
					slog.String("event_id", processed.EventID),
					slog.String("event_type", event.Type),
					slog.String("tenant_id", event.TenantID),
				)
			}
			return nil
		case ProcessDecisionInProgress:
			return fmt.Errorf("%w: event_id=%s subscription_id=%s", ErrEventProcessingInProgress, processed.EventID, processed.SubscriptionID)
		default:
			return fmt.Errorf("domain consumer processed-event store returned unknown decision %q", decision)
		}
	}
	if err := s.bus.Publish(ctx, event); err != nil {
		if eventbus.IsPermanentError(err) {
			if claimed {
				if markErr := s.processedStore.MarkProcessed(ctx, processed); markErr != nil {
					if s.log != nil {
						s.log.ErrorContext(ctx, "domain_event_permanent_failure_finalization_failed",
							slog.String("message_id", message.ID),
							slog.String("event_id", processed.EventID),
							slog.String("event_type", event.Type),
							slog.String("tenant_id", event.TenantID),
							slog.Any("error", markErr),
						)
					}
					return nil
				}
			}
			if s.log != nil {
				s.log.WarnContext(ctx, "domain_event_permanent_failure_acked",
					slog.String("message_id", message.ID),
					slog.String("event_id", processed.EventID),
					slog.String("event_type", event.Type),
					slog.String("tenant_id", event.TenantID),
					slog.Any("error", err),
				)
			}
			return nil
		}
		return fmt.Errorf("domain event dispatch %s/%s: %w", event.Type, event.Key, err)
	}
	dispatched = true
	if claimed {
		if markErr := s.processedStore.MarkProcessed(ctx, processed); markErr != nil {
			if s.log != nil {
				s.log.ErrorContext(ctx, "domain_event_processed_finalization_failed",
					slog.String("message_id", message.ID),
					slog.String("event_id", processed.EventID),
					slog.String("event_type", event.Type),
					slog.String("tenant_id", event.TenantID),
					slog.Any("error", markErr),
				)
			}
			return nil
		}
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

func envelopeEventID(payload []byte) string {
	var env struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return ""
	}
	return strings.TrimSpace(env.EventID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
