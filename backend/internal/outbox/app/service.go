package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime/debug"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

const (
	defaultLimit        = 50
	defaultMaxAttempts  = 5
	defaultLeaseTimeout = 5 * time.Minute
	defaultBackoffBase  = 5 * time.Second
	defaultBackoffMax   = 5 * time.Minute
	maxErrorLength      = 240
)

type Config struct {
	Limit        int
	MaxAttempts  int
	LeaseTimeout time.Duration
	BackoffBase  time.Duration
	BackoffMax   time.Duration
	Now          func() time.Time
}

type Service struct {
	repo      ports.Repository
	publisher ports.Publisher
	validator *EnvelopeValidator
	config    Config
	log       *slog.Logger
}

// NewService constructs the outbox relay service.
// log may be nil; slog.Default() is used in that case.
func NewService(repo ports.Repository, publisher ports.Publisher, validator *EnvelopeValidator, config Config, log ...*slog.Logger) *Service {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Service{
		repo:      repo,
		publisher: publisher,
		validator: validator,
		config:    normalizeConfig(config),
		log:       l,
	}
}

func (s *Service) RunOnce(ctx context.Context) (*domain.RunResult, error) {
	now := s.now()
	reclaimed, err := s.repo.ReclaimStalePublishing(ctx, now, s.config.LeaseTimeout)
	if err != nil {
		return nil, err
	}
	claim, err := s.repo.ClaimPending(ctx, ports.ClaimParams{
		Limit:       s.config.Limit,
		MaxAttempts: s.config.MaxAttempts,
		Now:         now,
	})
	if err != nil {
		return nil, err
	}
	result := &domain.RunResult{
		ReclaimedStaleCount: int(reclaimed),
		ClaimedCount:        len(claim.Messages),
		DeadLetterCount:     claim.DeadLetterCount,
	}
	for _, message := range claim.Messages {
		if err := s.processMessage(ctx, message, result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *Service) processMessage(ctx context.Context, message domain.Message, result *domain.RunResult) error {
	now := s.now()
	if err := s.validator.Validate(message.Payload); err != nil {
		if markErr := s.repo.MarkFailed(ctx, message.OutboxID, sanitizeError("invalid_event_envelope"), now); markErr != nil {
			return fmt.Errorf("mark invalid outbox message failed: %w", markErr)
		}
		result.FailedCount++
		return nil
	}

	err := s.publishSafely(ctx, message)
	if err == nil {
		if markErr := s.repo.MarkPublished(ctx, message.OutboxID, now); markErr != nil {
			return fmt.Errorf("mark published outbox message failed: %w", markErr)
		}
		result.PublishedCount++
		return nil
	}

	if !ports.IsRetryablePublishFailure(err) {
		if markErr := s.repo.MarkFailed(ctx, message.OutboxID, sanitizeError("publish_permanent_failure"), now); markErr != nil {
			return fmt.Errorf("mark permanently failed outbox message failed: %w", markErr)
		}
		result.FailedCount++
		return nil
	}

	if message.AttemptCount >= s.config.MaxAttempts {
		if markErr := s.repo.MarkDeadLetter(ctx, message.OutboxID, sanitizeError("max_attempts_exhausted"), now); markErr != nil {
			return fmt.Errorf("mark dead-letter outbox message failed: %w", markErr)
		}
		result.DeadLetterCount++
		return nil
	}

	nextAttemptAt := now.Add(s.backoff(message.AttemptCount))
	if markErr := s.repo.MarkRetry(ctx, message.OutboxID, nextAttemptAt, sanitizeError("publish_retry_scheduled"), now); markErr != nil {
		return fmt.Errorf("mark retry outbox message failed: %w", markErr)
	}
	result.RetryScheduledCount++
	return nil
}

func (s *Service) publishSafely(ctx context.Context, message domain.Message) (err error) {
	defer func() {
		if p := recover(); p != nil {
			stack := string(debug.Stack())
			s.log.ErrorContext(ctx, "outbox_publisher_panic",
				slog.Any("panic", p),
				slog.String("stack", stack),
				slog.String("outbox_id", message.OutboxID),
				slog.String("event_type", message.EventType),
				slog.String("trace_id", derefString(message.TraceID)),
			)
			err = ports.RetryablePublishError(errors.New("publisher panic"))
		}
	}()
	return s.publisher.Publish(ctx, ports.PublishMessage{
		OutboxID:  message.OutboxID,
		TenantID:  message.TenantID,
		EventID:   message.EventID,
		EventType: message.EventType,
		Topic:     message.Topic,
		Headers:   message.Headers,
		Payload:   message.Payload,
		TraceID:   message.TraceID,
	})
}

func (s *Service) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(s.config.BackoffBase) * multiplier)
	if delay <= 0 || delay > s.config.BackoffMax {
		return s.config.BackoffMax
	}
	return delay
}

func (s *Service) now() time.Time {
	return s.config.Now().UTC()
}

func normalizeConfig(config Config) Config {
	if config.Limit <= 0 {
		config.Limit = defaultLimit
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = defaultMaxAttempts
	}
	if config.LeaseTimeout <= 0 {
		config.LeaseTimeout = defaultLeaseTimeout
	}
	if config.BackoffBase <= 0 {
		config.BackoffBase = defaultBackoffBase
	}
	if config.BackoffMax <= 0 {
		config.BackoffMax = defaultBackoffMax
	}
	if config.BackoffMax < config.BackoffBase {
		config.BackoffMax = config.BackoffBase
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return config
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func sanitizeError(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "outbox_relay_error"
	}
	if len(kind) > maxErrorLength {
		return kind[:maxErrorLength]
	}
	return kind
}

type EnvelopeValidator struct {
	schema *jsonschema.Schema
}

func NewEnvelopeValidator(schemaPath string) (*EnvelopeValidator, error) {
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("compile domain event envelope schema: %w", err)
	}
	return &EnvelopeValidator{schema: schema}, nil
}

func (v *EnvelopeValidator) Validate(payload []byte) error {
	if v == nil || v.schema == nil {
		return errors.New("domain event envelope validator is not configured")
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(payload))
	if err != nil {
		return err
	}
	return v.schema.Validate(doc)
}
