package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("calendar-reminder-sweeper", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	limit := fs.Int("limit", 100, "max reminders to queue")
	timeout := fs.Duration("timeout", 30*time.Second, "sweeper timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
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
	fmt.Printf("calendar reminder sweep queued=%d tenant=%s\n", count, *tenantID)
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
