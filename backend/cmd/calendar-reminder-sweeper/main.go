package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/taskqueue"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("calendar-reminder-sweeper", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	limit := fs.Int("limit", 100, "max reminders to queue")
	timeout := fs.Duration("timeout", 30*time.Second, "sweeper timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenantID) == "" {
		return errors.New("tenant-id is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "calendar-reminder-sweeper"})
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
	service := calendarapp.NewService(calendarpg.NewRepository(pool, pgCfg.QueryTimeout))
	count, err := service.SweepDueReminders(ctx, *tenantID, *limit)
	if err != nil {
		return err
	}
	if count > 0 {
		if err := enqueueNotificationDispatcher(ctx, *tenantID, "calendar-reminder-sweeper"); err != nil {
			return err
		}
	}
	fmt.Printf("calendar reminder sweep queued=%d tenant=%s\n", count, *tenantID)
	return nil
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func enqueueNotificationDispatcher(ctx context.Context, tenantID, source string) error {
	cfg, enabled, err := taskqueue.ConfigFromEnv()
	if err != nil || !enabled {
		return err
	}
	enqueuer, err := taskqueue.NewEnqueuer(ctx, cfg)
	if err != nil {
		return err
	}
	defer enqueuer.Close()
	taskID := taskqueue.SafeTaskID(fmt.Sprintf("notification-dispatcher-%s-%s-%s", tenantID, source, time.Now().In(biztime.DefaultLocation()).Format("200601021504")))
	return enqueuer.EnqueueJSONPost(ctx, taskID, map[string]any{}, time.Time{})
}
