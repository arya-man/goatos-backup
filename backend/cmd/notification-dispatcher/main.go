package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	notificationgateway "github.com/vgoats/goatos/backend/internal/notification/adapters/gateway"
	notificationpg "github.com/vgoats/goatos/backend/internal/notification/adapters/postgres"
	notificationapp "github.com/vgoats/goatos/backend/internal/notification/app"
	notificationdomain "github.com/vgoats/goatos/backend/internal/notification/domain"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("notification-dispatcher", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	limit := fs.Int("limit", 50, "max notification requests to dispatch")
	maxAttempts := fs.Int("max-attempts", 5, "max delivery attempts before leaving failed")
	timeout := fs.Duration("timeout", 30*time.Second, "dispatcher timeout")
	leaseTimeout := fs.Duration("lease-timeout", 2*time.Minute, "stale sending lease timeout")
	backoffBase := fs.Duration("backoff-base", 30*time.Second, "base retry backoff")
	backoffMax := fs.Duration("backoff-max", 15*time.Minute, "max retry backoff")
	dryRun := fs.Bool("dry-run", envBool("GOATOS_NOTIFICATION_DRY_RUN"), "mark all channels delivered without external sends")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenantID) == "" {
		return errors.New("tenant-id is required")
	}
	if *limit < 1 || *limit > 500 {
		return errors.New("limit must be between 1 and 500")
	}
	if *maxAttempts < 1 {
		return errors.New("max-attempts must be positive")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "notification-dispatcher"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	logger := observability.New(observability.Config{Service: "notification-dispatcher"})
	repo := notificationpg.NewRepository(pool, pgCfg.QueryTimeout)
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
		DryRun:             *dryRun,
		HTTPTimeout:        envDuration("GOATOS_NOTIFICATION_HTTP_TIMEOUT", 5*time.Second),
	}, logger)
	service := notificationapp.NewService(repo, gateway, notificationapp.Config{
		Limit:        *limit,
		MaxAttempts:  *maxAttempts,
		LeaseTimeout: *leaseTimeout,
		BackoffBase:  *backoffBase,
		BackoffMax:   *backoffMax,
	}, logger)
	result, err := service.RunOnce(ctx, *tenantID)
	if err != nil {
		return err
	}
	logger.Info("notification dispatch complete",
		"tenant_id", *tenantID,
		"reclaimed_stale", result.ReclaimedStaleCount,
		"claimed", result.ClaimedCount,
		"sent", result.SentCount,
		"failed", result.FailedCount,
		"exhausted", result.ExhaustedCount,
	)
	fmt.Print(formatDispatchResult(result, *tenantID))
	return nil
}

func formatDispatchResult(result notificationdomain.DispatchResult, tenantID string) string {
	return fmt.Sprintf("notification dispatch reclaimed=%d claimed=%d sent=%d failed=%d exhausted=%d tenant=%s\n",
		result.ReclaimedStaleCount, result.ClaimedCount, result.SentCount, result.FailedCount, result.ExhaustedCount, tenantID)
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func envBool(key string) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return raw == "1" || raw == "true" || raw == "yes"
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
