package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	domainconsumerpg "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const defaultRetention = 14 * 24 * time.Hour

type config struct {
	TenantID  string
	Limit     int
	Timeout   time.Duration
	Retention time.Duration
	Before    time.Time
	DryRun    bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args, time.Now)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "domain-event-processed-sweeper"})
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

	store := domainconsumerpg.NewProcessedEventStore(pool, pgCfg.QueryTimeout)
	count, err := store.SweepProcessedBefore(ctx, cfg.TenantID, cfg.Before, cfg.Limit, cfg.DryRun)
	if err != nil {
		return err
	}
	action := "deleted"
	if cfg.DryRun {
		action = "would_delete"
	}
	tenant := cfg.TenantID
	if tenant == "" {
		tenant = "all"
	}
	fmt.Printf("domain processed-event sweep %s=%d before=%s tenant=%s\n", action, count, cfg.Before.UTC().Format(time.RFC3339), tenant)
	return nil
}

func parseFlags(args []string, now func() time.Time) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("domain-event-processed-sweeper", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "optional tenant id filter")
	fs.IntVar(&cfg.Limit, "limit", 1000, "maximum processed events to delete in one run")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_DOMAIN_EVENT_PROCESSED_SWEEPER_TIMEOUT", 30*time.Second), "sweeper timeout")
	fs.DurationVar(&cfg.Retention, "retention", durationEnv("GOATOS_DOMAIN_EVENT_PROCESSED_RETENTION", defaultRetention), "processed-event retention window")
	beforeRaw := fs.String("before", getenv("GOATOS_DOMAIN_EVENT_PROCESSED_SWEEPER_BEFORE"), "RFC3339 processed_at cutoff; overrides retention")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "count old processed events without deleting")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	if cfg.TenantID != "" {
		if _, err := pgconv.UUID(cfg.TenantID); err != nil {
			return config{}, fmt.Errorf("tenant-id must be a uuid: %w", err)
		}
	}
	if cfg.Limit < 1 || cfg.Limit > 5000 {
		return config{}, errors.New("limit must be between 1 and 5000")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.Retention <= 0 {
		return config{}, errors.New("retention must be positive")
	}
	if now == nil {
		now = time.Now
	}
	cfg.Before = now().UTC().Add(-cfg.Retention)
	if strings.TrimSpace(*beforeRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*beforeRaw))
		if err != nil {
			return config{}, errors.New("before must be RFC3339")
		}
		cfg.Before = parsed.UTC()
	}
	return cfg, nil
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
