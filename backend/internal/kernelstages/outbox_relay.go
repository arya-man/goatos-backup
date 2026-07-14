package kernelstages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	outboxpublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher"
	eventbuspublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/eventbus"
	pubsubpublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/pubsub"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	outboxports "github.com/vgoats/goatos/backend/internal/outbox/ports"
)

// OutboxRelayConfig captures the outbox relay tunables, resolved from the same
// flags/env defaults as the outbox-relay one-shot command.
type OutboxRelayConfig struct {
	Limit        int
	MaxAttempts  int
	LeaseTimeout time.Duration
}

// OutboxRelayConfigFromEnv resolves the relay config from env with the same
// defaults the one-shot uses for its flags.
func OutboxRelayConfigFromEnv() OutboxRelayConfig {
	return OutboxRelayConfig{
		Limit:        intEnv("GOATOS_OUTBOX_LIMIT", 50),
		MaxAttempts:  intEnv("GOATOS_OUTBOX_MAX_ATTEMPTS", 5),
		LeaseTimeout: durationEnv("GOATOS_OUTBOX_LEASE_TIMEOUT", 5*time.Minute),
	}
}

// OutboxRelayStage drains the transactional outbox and publishes domain events.
// It reuses outboxapp.Service.RunUntilDrained — the same code path as the
// outbox-relay one-shot command. Fast-lane cadence.
type OutboxRelayStage struct {
	service *outboxapp.Service
	logger  *slog.Logger
}

// NewOutboxRelayStage builds the relay stage. The publisher (built once via
// BuildOutboxPublisher) is injected so the durable Pub/Sub client is reused
// across ticks rather than reconnected every minute.
func NewOutboxRelayStage(deps Deps, publisher outboxports.Publisher, validator *outboxapp.EnvelopeValidator, cfg OutboxRelayConfig) *OutboxRelayStage {
	repo := outboxpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	service := outboxapp.NewService(repo, publisher, validator, outboxapp.Config{
		Limit:        cfg.Limit,
		MaxAttempts:  cfg.MaxAttempts,
		LeaseTimeout: cfg.LeaseTimeout,
	}, deps.Logger)
	return &OutboxRelayStage{service: service, logger: deps.Logger}
}

// Name implements worker.StageRunner.
func (s *OutboxRelayStage) Name() string { return "outbox-relay" }

// Run drains the outbox until empty.
func (s *OutboxRelayStage) Run(ctx context.Context) error {
	result, err := s.service.RunUntilDrained(ctx)
	if err != nil {
		return fmt.Errorf("outbox relay: %w", err)
	}
	if s.logger != nil && result != nil {
		s.logger.Info("outbox_relay_stage_complete",
			"batches_processed", result.BatchesProcessed,
			"claimed", result.ClaimedCount,
			"published", result.PublishedCount,
			"retry_scheduled", result.RetryScheduledCount,
			"failed", result.FailedCount,
			"dead_letter", result.DeadLetterCount,
		)
	}
	return nil
}

// BuildOutboxPublisher constructs the durable (Pub/Sub) or non-durable
// (eventbus/logging) publisher from env, mirroring the outbox-relay command's
// buildPublisher. The returned close func releases the Pub/Sub client on
// shutdown; it is nil for the in-process publishers. Building this once and
// reusing it avoids reconnecting the Pub/Sub client on every fast-lane tick.
func BuildOutboxPublisher(ctx context.Context, deps Deps) (outboxports.Publisher, func() error, error) {
	kind := getenv("GOATOS_OUTBOX_PUBLISHER")
	normalized := strings.ToLower(kind)
	if normalized == "" {
		return nil, nil, fmt.Errorf("GOATOS_OUTBOX_PUBLISHER must be set: use 'pubsub' for staging/production; 'eventbus' or 'logging' require GOATOS_OUTBOX_ALLOW_NONDURABLE=1 and are for local/dev only")
	}
	switch normalized {
	case "logging", "log":
		if err := requireNonDurablePublisherAllowed(normalized); err != nil {
			return nil, nil, err
		}
		return outboxpublisher.Select(kind, deps.Logger, nil), nil, nil
	case "eventbus", "local", "inprocess":
		if err := requireNonDurablePublisherAllowed(normalized); err != nil {
			return nil, nil, err
		}
		bus := BuildDomainBus(deps.Pool, deps.PgCfg, deps.Logger)
		if deps.Logger != nil {
			deps.Logger.Info("kernelstages_outbox_eventbus_dispatcher_ready")
		}
		return eventbuspublisher.New(bus), nil, nil
	case outboxpublisher.KindPubSub:
		projectID := firstNonEmptyEnv("GOATOS_PUBSUB_PROJECT_ID", "GOOGLE_CLOUD_PROJECT")
		if projectID == "" {
			return nil, nil, fmt.Errorf("GOATOS_OUTBOX_PUBLISHER=pubsub requires GOATOS_PUBSUB_PROJECT_ID or GOOGLE_CLOUD_PROJECT")
		}
		topicID := firstNonEmptyEnv("GOATOS_OUTBOX_PUBSUB_TOPIC_ID", "GOATOS_PUBSUB_TOPIC_ID")
		if topicID == "" {
			return nil, nil, fmt.Errorf("GOATOS_OUTBOX_PUBLISHER=pubsub requires GOATOS_OUTBOX_PUBSUB_TOPIC_ID")
		}
		client, err := pubsubpublisher.NewGCPMessagePublisher(ctx, projectID)
		if err != nil {
			return nil, nil, err
		}
		if deps.Logger != nil {
			deps.Logger.Info("kernelstages_outbox_pubsub_publisher_ready", "project_id", projectID, "topic_id", topicID)
		}
		return pubsubpublisher.NewPublisherWithConfig(client, pubsubpublisher.Config{TopicID: topicID}), client.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported GOATOS_OUTBOX_PUBLISHER %q", kind)
	}
}

func requireNonDurablePublisherAllowed(kind string) error {
	if envTruthy("GOATOS_OUTBOX_ALLOW_NONDURABLE") {
		return nil
	}
	return fmt.Errorf("GOATOS_OUTBOX_PUBLISHER=%q is a non-durable publisher (no Pub/Sub egress); set GOATOS_OUTBOX_ALLOW_NONDURABLE=1 to permit it for local/dev, or use GOATOS_OUTBOX_PUBLISHER=pubsub for staging/production", kind)
}
