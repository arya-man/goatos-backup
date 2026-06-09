package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	reportingpg "github.com/vgoats/goatos/backend/internal/reporting/adapters/postgres"
	reportingapp "github.com/vgoats/goatos/backend/internal/reporting/app"
	"github.com/vgoats/goatos/backend/internal/reporting/ports"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("update identity counters failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var tenantID string
	var limit int
	var timeout time.Duration
	var retention time.Duration
	fs := flag.NewFlagSet("update-identity-counters", flag.ContinueOnError)
	fs.StringVar(&tenantID, "tenant-id", "", "tenant UUID to update")
	fs.IntVar(&limit, "limit", 500, "maximum goat identity events to process in one run")
	fs.DurationVar(&timeout, "timeout", 2*time.Minute, "one-shot update timeout")
	fs.DurationVar(&retention, "processed-events-retention", reportingapp.DefaultProcessedEventsRetention(), "processed-event retention before safe pruning")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := reportingpg.NewRepository(pool, timeout)
	service := reportingapp.NewService(repo)
	result, err := service.UpdateIdentityCounters(ctx, ports.UpdateIdentityCountersParams{
		TenantID:                 tenantID,
		Limit:                    limit,
		ProcessedEventsRetention: retention,
	})
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
