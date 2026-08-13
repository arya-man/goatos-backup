// Command notification-requeue is the operator-driven recovery + visibility tool for exhausted
// notification_requests rows (status='exhausted'). Nothing in the dispatcher (notification/app,
// notification/adapters/postgres) can ever move a row out of 'exhausted' on its own: ClaimDue only
// claims status IN ('queued','failed'). This is the deliberate, explicit, narrowly-scoped remedy —
// modeled on cmd/outbox-dlq's list/replay shape for the same reason: dead-letter recovery is an
// operator action, not something the dispatcher should do automatically.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	notificationpg "github.com/vgoats/goatos/backend/internal/notification/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/notification/ports"
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
	fs := flag.NewFlagSet("notification-requeue", flag.ContinueOnError)
	mode := fs.String("mode", "list", "list or requeue")
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id (required)")
	notificationType := fs.String("notification-type", "", "notification_type to scope to (required for requeue; optional filter for list)")
	since := fs.String("since", "", "RFC3339 lower bound on requested_at (optional)")
	until := fs.String("until", "", "RFC3339 upper bound on requested_at (optional, default now)")
	ids := fs.String("notification-request-id", "", "comma-separated notification_request_id values to further restrict a requeue")
	requeuedBy := fs.String("requeued-by", getenv("USER"), "operator identity recorded on requeued rows")
	limit := fs.Int("limit", 100, "max rows to list")
	confirm := fs.Bool("confirm", false, "required to actually requeue (mode=requeue is otherwise a dry validation of scope only)")
	timeout := fs.Duration("timeout", 30*time.Second, "command timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenantID) == "" {
		return errors.New("tenant-id is required")
	}
	sinceTime, err := parseOptionalTime(*since)
	if err != nil {
		return fmt.Errorf("since: %w", err)
	}
	untilTime, err := parseOptionalTime(*until)
	if err != nil {
		return fmt.Errorf("until: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "notification-requeue"})
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
	repo := notificationpg.NewRepository(pool, pgCfg.QueryTimeout)

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "list":
		return list(ctx, repo, *tenantID, *notificationType, sinceTime, untilTime, *limit)
	case "requeue":
		if strings.TrimSpace(*notificationType) == "" {
			return errors.New("notification-type is required for requeue (no all-types mode, by design)")
		}
		if !*confirm {
			return errors.New("mode=requeue requires -confirm=true; run mode=list first to preview scope")
		}
		result, err := repo.RequeueExhausted(ctx, ports.RequeueParams{
			TenantID:               *tenantID,
			NotificationType:       *notificationType,
			Since:                  sinceTime,
			Until:                  untilTime,
			NotificationRequestIDs: splitCSV(*ids),
			RequeuedBy:             *requeuedBy,
			Now:                    time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		fmt.Printf("notification requeue tenant=%s type=%s requeued=%d\n", *tenantID, *notificationType, result.Requeued)
		return nil
	default:
		return errors.New("mode must be list or requeue")
	}
}

func list(ctx context.Context, repo *notificationpg.Repository, tenantID, notificationType string, since, until time.Time, limit int) error {
	items, err := repo.ListExhausted(ctx, ports.ExhaustedQuery{
		TenantID:         tenantID,
		NotificationType: notificationType,
		Since:            since,
		Until:            until,
		Limit:            limit,
	})
	if err != nil {
		return err
	}
	fmt.Println("notification_request_id\tnotification_type\tchannel\ttarget\tattempts\trequeue_count\tfailure_reason\trequested_at")
	for _, item := range items {
		fmt.Printf("%s\t%s\t%s\t%s/%s\t%d\t%d\t%s\t%s\n",
			item.NotificationRequestID,
			item.NotificationType,
			item.Channel,
			item.TargetType, item.TargetID,
			item.DeliveryAttempts,
			item.RequeueCount,
			item.FailureReason,
			item.RequestedAt.UTC().Format(time.RFC3339),
		)
	}
	fmt.Printf("total=%d\n", len(items))
	return nil
}

func parseOptionalTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
