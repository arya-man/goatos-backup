package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime/debug"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
	"github.com/vgoats/goatos/backend/internal/platform/kmetrics"
	"github.com/vgoats/goatos/backend/internal/platform/tracecontext"
)

// tracer emits the "outbox.publish" span per message publish attempt. Bound
// to the OTel global TracerProvider, which observability.SetupTelemetry
// installs at process startup - safe to use before that call runs too (see
// SetupTelemetry's doc comment on global-package delegation).
var tracer = otel.Tracer("github.com/vgoats/goatos/backend/outbox")

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

func (s *Service) RunOnce(ctx context.Context) (result *domain.RunResult, err error) {
	// Record the batch-level kernel.outbox.* counters (reclaimed,
	// retry_scheduled, dead_letters, queue_depth) regardless of which return
	// path fires below, so a mid-batch error still surfaces the partial
	// counts it produced.
	defer func() {
		if result != nil {
			kmetrics.RecordOutboxBatch(ctx, result.ReclaimedStaleCount, result.RetryScheduledCount, result.DeadLetterCount, result.ClaimedCount)
		}
	}()

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
	result = &domain.RunResult{
		ReclaimedStaleCount: int(reclaimed),
		ClaimedCount:        len(claim.Messages),
		DeadLetterCount:     claim.DeadLetterCount,
	}
	// ClaimPending dead-letters attempt-exhausted rows in SQL, so those deaths never reach
	// processMessage's logging below — without this they were a pure counter increment and no
	// human would ever learn that events had been abandoned.
	if claim.DeadLetterCount > 0 {
		s.log.ErrorContext(ctx, "outbox_messages_dead_lettered_on_claim",
			"count", claim.DeadLetterCount,
			"max_attempts", s.config.MaxAttempts,
			"reason", "max_attempts_exhausted",
		)
	}

	// Track claimed IDs and which ones were successfully processed.
	// On ctx cancellation mid-batch, release unprocessed messages so the next
	// tick re-claims them immediately instead of waiting for the lease to expire.
	claimedIDs := make([]string, 0, len(claim.Messages))
	processedIDs := make(map[string]bool)
	for _, msg := range claim.Messages {
		claimedIDs = append(claimedIDs, msg.OutboxID)
	}
	defer func() {
		// If the run was cancelled mid-batch, release unprocessed messages.
		if ctx.Err() != nil {
			s.releaseUnprocessedMessages(claimedIDs, processedIDs, now)
		}
	}()

	for _, message := range claim.Messages {
		if err := s.processMessage(ctx, message, result); err != nil {
			return result, err
		}
		processedIDs[message.OutboxID] = true
	}
	return result, nil
}

// RunUntilDrained keeps claiming ready outbox rows until there is no immediate
// queue movement left or the caller's context expires. For scheduled jobs, the
// configured limit is a batch size; it must not become a hard per-run ceiling.
func (s *Service) RunUntilDrained(ctx context.Context) (*domain.RunResult, error) {
	total := &domain.RunResult{}
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		result, err := s.RunOnce(ctx)
		if result != nil {
			total.BatchesProcessed++
			total.ReclaimedStaleCount += result.ReclaimedStaleCount
			total.ClaimedCount += result.ClaimedCount
			total.PublishedCount += result.PublishedCount
			total.RetryScheduledCount += result.RetryScheduledCount
			total.FailedCount += result.FailedCount
			total.DeadLetterCount += result.DeadLetterCount
		}
		if err != nil {
			return total, err
		}
		if result == nil || (result.ReclaimedStaleCount == 0 && result.ClaimedCount == 0 && result.DeadLetterCount == 0) {
			return total, nil
		}
	}
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

	publishStart := time.Now()
	err := s.publishWithTrace(ctx, message)
	publishDuration := time.Since(publishStart).Seconds()
	if err == nil {
		if markErr := s.repo.MarkPublished(ctx, message.OutboxID, now); markErr != nil {
			return fmt.Errorf("mark published outbox message failed: %w", markErr)
		}
		result.PublishedCount++
		kmetrics.RecordOutboxPublish(ctx, kmetrics.OutboxOutcomePublished, message.EventType, publishDuration)
		return nil
	}

	if !ports.IsRetryablePublishFailure(err) {
		if markErr := s.repo.MarkFailed(ctx, message.OutboxID, sanitizeError("publish_permanent_failure"), now); markErr != nil {
			return fmt.Errorf("mark permanently failed outbox message failed: %w", markErr)
		}
		// A permanently-failed row was previously recorded only as a metric counter, which a
		// human learns about only if a dashboard/alert happens to be watching it. This event
		// is a domain event that will NEVER be published — log it at Error so it also lands in
		// the service log a human actually reads when investigating "why did nothing happen".
		s.log.ErrorContext(ctx, "outbox_message_permanently_failed",
			"outbox_id", message.OutboxID,
			"event_type", message.EventType,
			"attempt_count", message.AttemptCount,
			"reason", "publish_permanent_failure",
		)
		result.FailedCount++
		kmetrics.RecordOutboxPublish(ctx, kmetrics.OutboxOutcomeFailed, message.EventType, publishDuration)
		return nil
	}

	if message.AttemptCount >= s.config.MaxAttempts {
		if markErr := s.repo.MarkDeadLetter(ctx, message.OutboxID, sanitizeError("max_attempts_exhausted"), now); markErr != nil {
			return fmt.Errorf("mark dead-letter outbox message failed: %w", markErr)
		}
		// Same reasoning as the permanent-failure branch above, and this one matters more: a
		// dead-lettered row is a domain event the rest of the system will simply never see, and
		// until now the ONLY trace was a counter. Logged with the outbox id so an operator report
		// ("my write never showed up") can be joined straight to the row that died.
		s.log.ErrorContext(ctx, "outbox_message_dead_lettered",
			"outbox_id", message.OutboxID,
			"event_type", message.EventType,
			"attempt_count", message.AttemptCount,
			"max_attempts", s.config.MaxAttempts,
			"reason", "max_attempts_exhausted",
		)
		result.DeadLetterCount++
		kmetrics.RecordOutboxPublish(ctx, kmetrics.OutboxOutcomeDeadLetter, message.EventType, publishDuration)
		return nil
	}

	nextAttemptAt := now.Add(s.backoff(message.AttemptCount))
	if markErr := s.repo.MarkRetry(ctx, message.OutboxID, nextAttemptAt, sanitizeError("publish_retry_scheduled"), now); markErr != nil {
		return fmt.Errorf("mark retry outbox message failed: %w", markErr)
	}
	result.RetryScheduledCount++
	kmetrics.RecordOutboxPublish(ctx, kmetrics.OutboxOutcomeRetry, message.EventType, publishDuration)
	return nil
}

// publishWithTrace starts an "outbox.publish" span - continuing the
// producer's original trace when message.Headers carries a "traceparent"
// entry (see the tracecontext package doc for the current scope of that
// wiring) - then delegates to publishSafely with the span's context so the
// pubsub publisher adapter can further propagate it onto the outbound
// message attributes for the domain-event-consumer to pick up.
//
// TODO(observability): producer modules (protocol, calendar, vaccination,
// obligation, counts, notification, procurement, ...) do not yet populate
// message.Headers["traceparent"] at INSERT time - each currently reuses the
// "trace_id" column for a business/audit correlation label, not a real W3C
// traceparent, and retrofitting ~9 modules' outbox-insert call sites was
// judged too broad/risky for this change (see
// docs/observability/OBSERVABILITY_DESIGN.md section 2.2 and the PR
// description). Until a producer sets it, this span is a fresh root span
// per publish rather than a child of the original write's trace - it still
// gives real publish-stage tracing, just not yet chained end-to-end.
func (s *Service) publishWithTrace(ctx context.Context, message domain.Message) error {
	parentCtx := ctx
	if carrier := headersTraceCarrier(message.Headers); carrier != nil {
		parentCtx = tracecontext.Extract(ctx, carrier)
	}
	spanCtx, span := tracer.Start(parentCtx, "outbox.publish", trace.WithAttributes(
		attribute.String("outbox_id", message.OutboxID),
		attribute.String("event_type", message.EventType),
		attribute.String("tenant_id", message.TenantID),
	))
	defer span.End()

	err := s.publishSafely(spanCtx, message)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// headersTraceCarrier best-effort decodes an outbox message's headers JSON
// object into a string carrier for W3C trace-context extraction. Returns nil
// when headers is empty or not a flat string-valued JSON object (e.g. absent,
// as is the case for every producer today per the TODO above).
func headersTraceCarrier(headers json.RawMessage) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	var carrier map[string]string
	if err := json.Unmarshal(headers, &carrier); err != nil {
		return nil
	}
	return carrier
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

// releaseUnprocessedMessages releases claimed messages back to pending status
// using a fresh short-lived context so the release completes even if the
// original ctx is cancelled. This ensures unprocessed messages claimed before
// cancellation are re-available on the next tick rather than staying leased
// for ~5 minutes (KERN-02 mitigation).
func (s *Service) releaseUnprocessedMessages(claimedIDs []string, processedIDs map[string]bool, now time.Time) {
	// Collect unprocessed IDs into one batch instead of looping
	// (scale-guard: n-plus-one-fanout mitigation).
	unprocessedIDs := make([]string, 0, len(claimedIDs))
	for _, id := range claimedIDs {
		if !processedIDs[id] {
			unprocessedIDs = append(unprocessedIDs, id)
		}
	}
	if len(unprocessedIDs) == 0 {
		return
	}

	// Use a fresh context so the release completes even if the
	// original ctx is cancelled. Use a short timeout (5s) to avoid
	// holding the operation open indefinitely if the DB is down.
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.repo.ReleasePublishingByIDs(releaseCtx, unprocessedIDs, now); err != nil {
		if s.log != nil {
			s.log.Warn("failed_to_release_cancelled_outbox_messages",
				"outbox_ids_count", len(unprocessedIDs),
				"error", err.Error(),
			)
		}
	}
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
