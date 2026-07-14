package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	domainconsumerpg "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/postgres"
	domainconsumerpubsub "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/pubsub"
	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
)

// DomainConsumerConfig identifies the Pub/Sub project and subscription the
// continuous consumer reads domain events from.
type DomainConsumerConfig struct {
	ProjectID      string
	SubscriptionID string
}

// DomainConsumerConfigFromEnv resolves the consumer config from env. Enabled is
// false when neither project nor subscription is configured (local/dev runs
// without Pub/Sub), so the kernel worker can boot without the continuous
// subscriber. When one but not both are set the config is invalid and an error
// is returned so a half-configured deployment fails fast.
func DomainConsumerConfigFromEnv() (cfg DomainConsumerConfig, enabled bool, err error) {
	cfg = DomainConsumerConfig{
		ProjectID:      firstNonEmptyEnv("GOATOS_PUBSUB_PROJECT_ID", "GOOGLE_CLOUD_PROJECT"),
		SubscriptionID: firstNonEmptyEnv("GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID", "GOATOS_PUBSUB_SUBSCRIPTION_ID"),
	}
	if cfg.ProjectID == "" && cfg.SubscriptionID == "" {
		return cfg, false, nil
	}
	if cfg.ProjectID == "" {
		return cfg, true, errors.New("domain consumer project id is required via GOATOS_PUBSUB_PROJECT_ID or GOOGLE_CLOUD_PROJECT")
	}
	if cfg.SubscriptionID == "" {
		return cfg, true, errors.New("domain consumer subscription is required via GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID or GOATOS_PUBSUB_SUBSCRIPTION_ID")
	}
	return cfg, true, nil
}

// DomainConsumerStage runs the continuous Pub/Sub domain-event consumer,
// reusing consumerapp.Service and the shared BuildDomainBus handler wiring — the
// same code path as the domain-event-consumer one-shot. Continuous cadence.
type DomainConsumerStage struct {
	deps      Deps
	validator *outboxapp.EnvelopeValidator
	cfg       DomainConsumerConfig
	logger    *slog.Logger
}

// NewDomainConsumerStage builds the continuous consumer stage.
func NewDomainConsumerStage(deps Deps, validator *outboxapp.EnvelopeValidator, cfg DomainConsumerConfig) *DomainConsumerStage {
	return &DomainConsumerStage{deps: deps, validator: validator, cfg: cfg, logger: deps.Logger}
}

// Name implements worker.StageRunner.
func (s *DomainConsumerStage) Name() string { return "domain-event-consumer" }

// Run subscribes to Pub/Sub and dispatches domain events until ctx is canceled.
func (s *DomainConsumerStage) Run(ctx context.Context) error {
	bus := BuildDomainBus(s.deps.Pool, s.deps.PgCfg, s.logger)
	consumer := consumerapp.NewService(bus, s.validator, s.logger).
		WithProcessedEventStore(domainconsumerpg.NewProcessedEventStore(s.deps.Pool, s.deps.PgCfg.QueryTimeout))
	subscriber, err := domainconsumerpubsub.NewGCPSubscriber(ctx, s.cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("domain consumer subscriber: %w", err)
	}
	defer subscriber.Close()
	if s.logger != nil {
		s.logger.Info("kernelstages_domain_event_consumer_ready", "project_id", s.cfg.ProjectID, "subscription_id", s.cfg.SubscriptionID)
	}
	if err := consumer.Run(ctx, subscriber, s.cfg.SubscriptionID); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return fmt.Errorf("domain consumer run: %w", err)
	}
	return nil
}
