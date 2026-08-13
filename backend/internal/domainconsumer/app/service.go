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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/kmetrics"
	"github.com/vgoats/goatos/backend/internal/platform/tracecontext"
)

// tracer emits the "domainconsumer.handle" span per consumed message,
// continuing the outbox relay's "outbox.publish" span when the received
// message attributes carry a W3C traceparent (see
// internal/outbox/adapters/publisher/pubsub, which always injects the
// publish span's own trace context on top of any producer-set traceparent).
var tracer = otel.Tracer("github.com/vgoats/goatos/backend/domainconsumer")

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
	ProcessDecisionClaimed ProcessDecision = "claimed"
	// ProcessDecisionEffectsCommitted means a prior attempt already ran bus.Publish successfully
	// for this event (durably recorded) but never reached the terminal 'processed' mark. The
	// caller MUST NOT invoke bus.Publish again for this decision - only the finalize step may be
	// retried - otherwise a non-idempotent handler side effect would be replayed (C35-024).
	ProcessDecisionEffectsCommitted ProcessDecision = "effects_committed"
	ProcessDecisionAlreadyProcessed ProcessDecision = "already_processed"
	ProcessDecisionInProgress       ProcessDecision = "in_progress"
)

var (
	ErrEventProcessingInProgress      = errors.New("domain event processing in progress")
	ErrProcessedEventFinalizationLost = errors.New("domain event processed-event finalization lost")
	// ErrPermanentDispatchFailure marks a handler rejection that redelivery can never turn into a
	// success (e.g. a deterministic domain-rule rejection wrapped in eventbus.PermanentError). The
	// terminal MarkFailed row + the WarnContext log below are the durable, loud record of the
	// failure; a Subscriber implementation should treat this as errors.Is-detectable and ACK the
	// message instead of nacking it, so a permanently-invalid event is delivered, logged, and
	// recorded exactly once instead of spinning through redelivery forever (the incident this
	// guards against: verification.verdict.approved retried 3056+ times against a deterministic
	// "vaccination: stock gate blocked" rejection because nothing ever stopped the nack loop).
	ErrPermanentDispatchFailure = errors.New("domain event permanent dispatch failure")
)

type ProcessedEventStore interface {
	BeginProcessing(ctx context.Context, event ProcessedEvent) (ProcessDecision, error)
	// MarkEffectsCommitted durably records that bus.Publish has already succeeded for this event,
	// before the terminal MarkProcessed call is attempted. It must be safe to call more than once
	// (idempotent) and must also succeed when the row is already in the effects-committed state.
	MarkEffectsCommitted(ctx context.Context, event ProcessedEvent) error
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

	// Continue the outbox relay's "outbox.publish" span (see
	// internal/outbox/adapters/publisher/pubsub, which always injects its own
	// span's trace context onto the outbound message attributes) so the
	// write -> publish -> consume chain lands on one trace per
	// docs/observability/OBSERVABILITY_DESIGN.md section 2.2. This span
	// intentionally wraps only the handle call's duration/outcome - it does
	// not thread a span-carrying context into the bus/processedStore calls
	// below, to avoid touching the surrounding retry-safety logic.
	handleStart := time.Now()
	eventType := message.Attributes["event_type"]
	outcome := kmetrics.ConsumerOutcomeProcessed
	parentCtx := tracecontext.Extract(ctx, message.Attributes)
	_, span := tracer.Start(parentCtx, "domainconsumer.handle", trace.WithAttributes(
		attribute.String("event_type", eventType),
		attribute.String("tenant_id", message.Attributes["tenant_id"]),
		attribute.String("message_id", message.ID),
	))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			outcome = kmetrics.ConsumerOutcomeFailed
		}
		span.End()
		kmetrics.RecordConsumerHandle(ctx, outcome, eventType, time.Since(handleStart).Seconds())
	}()

	var processed ProcessedEvent
	claimed := false
	// effectsCommitted is true once bus.Publish has succeeded for this event - this attempt or a
	// prior one reclaimed via ProcessDecisionEffectsCommitted. Once true, a later failure (e.g. the
	// terminal MarkProcessed call erroring) must NEVER be recorded as MarkFailed: 'failed' rows are
	// reclaimed unconditionally on the next delivery, which would call bus.Publish again and replay
	// a non-idempotent handler side effect (C35-024). Only the finalize step may be retried.
	effectsCommitted := false
	// effectsCommittedPersisted is true once the durable 'effects_committed' marker write itself has
	// succeeded, so the defer below only needs a best-effort retry when that write did not happen
	// (e.g. the mainline call was interrupted by context cancellation).
	effectsCommittedPersisted := false
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
		if err != nil && claimed && s.processedStore != nil {
			// Pub/Sub may cancel the delivery context as the callback is ending. Failure evidence must
			// still get a short independent chance to commit before we NACK the message.
			if effectsCommitted {
				if effectsCommittedPersisted {
					return
				}
				commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				defer cancel()
				if commitErr := s.processedStore.MarkEffectsCommitted(commitCtx, processed); commitErr != nil {
					err = errors.Join(err, fmt.Errorf("domain consumer effects-committed finalization: %w", commitErr))
				}
				return
			}
			failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			if markErr := s.processedStore.MarkFailed(failureCtx, processed, err.Error()); markErr != nil {
				err = errors.Join(err, fmt.Errorf("domain consumer failed-state finalization: %w", markErr))
			}
		}
	}()
	if s.validator == nil {
		return fmt.Errorf("domain consumer envelope validator is not configured")
	}
	if err := s.validator.Validate(message.Data); err != nil {
		kmetrics.RecordConsumerValidationError(ctx, eventType)
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
		case ProcessDecisionEffectsCommitted:
			// A prior attempt already ran bus.Publish successfully and durably recorded it; only
			// the finalize step below may run. Do not fall through to bus.Publish.
			claimed = true
			effectsCommitted = true
			effectsCommittedPersisted = true
		case ProcessDecisionAlreadyProcessed:
			if s.log != nil {
				s.log.InfoContext(ctx, "domain_event_duplicate_skipped",
					slog.String("message_id", message.ID),
					slog.String("event_id", processed.EventID),
					slog.String("event_type", event.Type),
					slog.String("tenant_id", event.TenantID),
				)
			}
			outcome = kmetrics.ConsumerOutcomeDuplicateSkipped
			return nil
		case ProcessDecisionInProgress:
			return fmt.Errorf("%w: event_id=%s subscription_id=%s", ErrEventProcessingInProgress, processed.EventID, processed.SubscriptionID)
		default:
			return fmt.Errorf("domain consumer processed-event store returned unknown decision %q", decision)
		}
	}
	if !effectsCommitted {
		if err := s.bus.Publish(ctx, event); err != nil {
			if eventbus.IsPermanentError(err) {
				if s.log != nil {
					s.log.WarnContext(ctx, "domain_event_permanent_failure_terminal",
						slog.String("message_id", message.ID),
						slog.String("event_id", processed.EventID),
						slog.String("event_type", event.Type),
						slog.String("tenant_id", event.TenantID),
						slog.Any("error", err),
					)
				}
				// Wrapping in ErrPermanentDispatchFailure (in addition to the MarkFailed row the
				// defer above writes) lets a Subscriber distinguish "will never succeed" from
				// "retry me": errors.Is(err, ErrPermanentDispatchFailure) is the seam a Pub/Sub (or
				// any at-least-once) subscriber uses to ACK instead of NACK, so this message is
				// delivered, logged, and durably marked failed exactly once rather than redelivered
				// forever.
				return fmt.Errorf("domain event permanent dispatch %s/%s: %w: %w", event.Type, event.Key, ErrPermanentDispatchFailure, err)
			}
			return fmt.Errorf("domain event dispatch %s/%s: %w", event.Type, event.Key, err)
		}
		// Handler side effects have committed. Durably record that fact BEFORE attempting the
		// terminal finalize below, so a later finalize failure or crash can never cause a
		// redelivery to replay bus.Publish (C35-024).
		effectsCommitted = true
		if claimed {
			if commitErr := s.processedStore.MarkEffectsCommitted(ctx, processed); commitErr != nil {
				return fmt.Errorf("domain event effects-committed finalization %s/%s: %w", event.Type, event.Key, commitErr)
			}
			effectsCommittedPersisted = true
		}
	}
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
			return fmt.Errorf("domain event processed finalization %s/%s: %w", event.Type, event.Key, markErr)
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
