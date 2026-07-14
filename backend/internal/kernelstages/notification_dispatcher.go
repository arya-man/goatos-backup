package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	notificationgateway "github.com/vgoats/goatos/backend/internal/notification/adapters/gateway"
	notificationpg "github.com/vgoats/goatos/backend/internal/notification/adapters/postgres"
	notificationapp "github.com/vgoats/goatos/backend/internal/notification/app"
)

// NotificationDispatcherStage claims queued notification requests and delivers
// them over the configured channels, reusing notificationapp.Service.RunOnce —
// the same code path as the notification-dispatcher one-shot. Fast-lane cadence.
type NotificationDispatcherStage struct {
	service  *notificationapp.Service
	tenantID string
	logger   *slog.Logger
}

// NewNotificationDispatcherStage builds the dispatcher stage. The gateway and
// service are constructed once from env and reused across ticks.
func NewNotificationDispatcherStage(deps Deps, tenantID string) *NotificationDispatcherStage {
	repo := notificationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	gateway := notificationgateway.New(notificationgateway.Config{
		WebhookURL:         getenv("GOATOS_NOTIFICATION_WEBHOOK_URL"),
		SlackWebhookURL:    getenv("GOATOS_SLACK_WEBHOOK_URL"),
		EmailWebhookURL:    getenv("GOATOS_EMAIL_WEBHOOK_URL"),
		EmailAuthToken:     getenv("GOATOS_EMAIL_WEBHOOK_AUTH_TOKEN"),
		EmailDefaultTo:     getenv("GOATOS_EMAIL_DEFAULT_TO"),
		IncidentWebhookURL: getenv("GOATOS_INCIDENT_WEBHOOK_URL"),
		IncidentAuthToken:  getenv("GOATOS_INCIDENT_WEBHOOK_AUTH_TOKEN"),
		FCMProjectID:       firstNonEmptyEnv("GOATOS_FCM_PROJECT_ID", "GOOGLE_CLOUD_PROJECT"),
		FCMEndpoint:        getenv("GOATOS_FCM_ENDPOINT"),
		FCMBearerToken:     getenv("GOATOS_FCM_BEARER_TOKEN"),
		FCMDefaultTopic:    getenv("GOATOS_FCM_DEFAULT_TOPIC"),
		DryRun:             envTruthy("GOATOS_NOTIFICATION_DRY_RUN"),
		HTTPTimeout:        durationEnv("GOATOS_NOTIFICATION_HTTP_TIMEOUT", 5*time.Second),
	}, deps.Logger)
	service := notificationapp.NewService(repo, gateway, notificationapp.Config{
		Limit:        intEnv("GOATOS_NOTIFICATION_LIMIT", 50),
		MaxAttempts:  intEnv("GOATOS_NOTIFICATION_MAX_ATTEMPTS", 5),
		LeaseTimeout: durationEnv("GOATOS_NOTIFICATION_LEASE_TIMEOUT", 2*time.Minute),
		BackoffBase:  durationEnv("GOATOS_NOTIFICATION_BACKOFF_BASE", 30*time.Second),
		BackoffMax:   durationEnv("GOATOS_NOTIFICATION_BACKOFF_MAX", 15*time.Minute),
	}, deps.Logger)
	return &NotificationDispatcherStage{service: service, tenantID: tenantID, logger: deps.Logger}
}

// Name implements worker.StageRunner.
func (s *NotificationDispatcherStage) Name() string { return "notification-dispatcher" }

// Run dispatches one batch of due notifications for the tenant.
func (s *NotificationDispatcherStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("notification dispatcher: tenant id is required")
	}
	result, err := s.service.RunOnce(ctx, s.tenantID)
	if err != nil {
		return fmt.Errorf("notification dispatch: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("notification_dispatch_stage_complete",
			"tenant_id", s.tenantID,
			"reclaimed_stale", result.ReclaimedStaleCount,
			"claimed", result.ClaimedCount,
			"sent", result.SentCount,
			"failed", result.FailedCount,
			"exhausted", result.ExhaustedCount,
		)
	}
	return nil
}
