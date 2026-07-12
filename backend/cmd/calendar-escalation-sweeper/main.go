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
	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
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
	fs := flag.NewFlagSet("calendar-escalation-sweeper", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	limit := fs.Int("limit", 100, "max escalations to queue")
	timeout := fs.Duration("timeout", 30*time.Second, "sweeper timeout")
	level1After := fs.Duration("level1-after", envDuration("GOATOS_ESCALATION_LEVEL1_AFTER", 0), "level 1 SLA threshold after due_at")
	level2After := fs.Duration("level2-after", envDuration("GOATOS_ESCALATION_LEVEL2_AFTER", 4*time.Hour), "level 2 SLA threshold after due_at")
	level3After := fs.Duration("level3-after", envDuration("GOATOS_ESCALATION_LEVEL3_AFTER", 24*time.Hour), "level 3 SLA threshold after due_at")
	level4After := fs.Duration("level4-after", envDuration("GOATOS_ESCALATION_LEVEL4_AFTER", 48*time.Hour), "level 4 SLA threshold after due_at")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenantID) == "" {
		return errors.New("tenant-id is required")
	}
	if *limit < 1 || *limit > 500 {
		return errors.New("limit must be between 1 and 500")
	}
	if *level1After < 0 || *level2After < *level1After || *level3After < *level2After || *level4After < *level3After {
		return errors.New("SLA thresholds must be non-negative and increasing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "calendar-escalation-sweeper"})
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
	count, err := service.SweepEscalations(ctx, calendarports.SweepEscalations{
		TenantID:    *tenantID,
		Limit:       *limit,
		Now:         time.Now().In(biztime.DefaultLocation()),
		Level1After: *level1After,
		Level2After: *level2After,
		Level3After: *level3After,
		Level4After: *level4After,
	})
	if err != nil {
		return err
	}
	if count > 0 {
		if err := enqueueNotificationDispatcher(ctx, *tenantID, "calendar-escalation-sweeper"); err != nil {
			return err
		}
	}
	fmt.Printf("calendar escalation sweep queued=%d tenant=%s\n", count, *tenantID)
	return nil
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return fallback
	}
	return d
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
