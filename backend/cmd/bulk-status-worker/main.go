// Command bulk-status-worker drains bulk_status_job_row rows for a tenant (and
// optionally a single job), applying each row through the identity per-goat
// transition. It mirrors the obligation-sweeper wiring: connect the pool, build
// the module repos + services, run bounded work, print a summary, exit.
//
// Usage:
//
//	bulk-status-worker -tenant-id <uuid> [-job-id <uuid>] \
//	  [-concurrency 8] [-rows-per-second 200] [-batch-size 200] \
//	  [-max-retries 5] [-timeout 5m]
//
// With -job-id it drains that one job to completion; without it, it drains every
// tenant job that currently has claimable rows.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	bulkpg "github.com/vgoats/goatos/backend/internal/bulkstatus/adapters/postgres"
	bulkapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
	"github.com/vgoats/goatos/backend/internal/bulkstatus/identitybridge"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/logger"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

type config struct {
	TenantID      string
	JobID         string
	Concurrency   int
	RowsPerSecond float64
	Burst         int
	BatchSize     int
	MaxRetries    int
	MaxIterations int
	ClaimLease    time.Duration
	MaxJobs       int
	Timeout       time.Duration
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	log := logger.New(os.Getenv("GOATOS_LOG_LEVEL"))

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "bulk-status-worker"})
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

	identityRepo := identitypg.NewRepository(pool, pgCfg.QueryTimeout)
	identityService := identityapp.NewService(identityRepo)
	bridge := identitybridge.New(identityService)
	repo := bulkpg.NewRepository(pool, pgCfg.QueryTimeout)

	settings := bulkapp.WorkerSettings{
		Concurrency:   cfg.Concurrency,
		RowsPerSecond: cfg.RowsPerSecond,
		Burst:         cfg.Burst,
		BatchSize:     cfg.BatchSize,
		MaxRetries:    cfg.MaxRetries,
		MaxIterations: cfg.MaxIterations,
		ClaimLease:    cfg.ClaimLease,
	}
	worker := bulkapp.NewWorkerService(repo, bridge, settings, log)

	started := time.Now().In(biztime.DefaultLocation())
	var drain bulkapp.DrainStats
	if strings.TrimSpace(cfg.JobID) != "" {
		drain, err = worker.RunUntilDrained(ctx, cfg.TenantID, cfg.JobID)
	} else {
		drain, err = worker.RunTenant(ctx, cfg.TenantID, cfg.MaxJobs)
	}
	if err != nil {
		log.Error("bulk_status_worker_error",
			slog.String("tenant_id", cfg.TenantID),
			slog.String("job_id", cfg.JobID),
			slog.String("error", err.Error()))
		return err
	}
	fmt.Printf("bulk-status-worker tenant=%s started=%s iterations=%d claimed=%d applied=%d skipped=%d retried=%d errored=%d jobs=%d\n",
		cfg.TenantID, started.Format(time.RFC3339),
		drain.Iterations, drain.Claimed, drain.Applied, drain.Skipped, drain.Retried, drain.Errored, len(drain.Jobs))
	for _, j := range drain.Jobs {
		fmt.Printf("  job=%s state=%s total=%d applied=%d skipped=%d failed=%d remaining=%d\n",
			j.JobID, j.FinalCounts.State, j.FinalCounts.Total, j.FinalCounts.Applied,
			j.FinalCounts.Skipped, j.FinalCounts.Failed, j.FinalCounts.Remaining)
	}
	// Tenant-wide mode drains many jobs; never exit 0 while any job settled 'failed'.
	if failed := drain.FailedJobs(); len(failed) > 0 {
		ids := make([]string, len(failed))
		for i, j := range failed {
			ids[i] = j.JobID
		}
		log.Error("bulk_status_worker_jobs_failed",
			slog.String("tenant_id", cfg.TenantID),
			slog.Int("failed_jobs", len(failed)),
			slog.String("job_ids", strings.Join(ids, ",")))
		return fmt.Errorf("bulk-status-worker: %d job(s) drained with row failures: %s", len(failed), strings.Join(ids, ", "))
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("bulk-status-worker", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id (required)")
	fs.StringVar(&cfg.JobID, "job-id", getenv("GOATOS_BULK_STATUS_JOB_ID"), "bulk status job id; empty drains all tenant jobs with claimable rows")
	fs.IntVar(&cfg.Concurrency, "concurrency", intEnv("GOATOS_BULK_STATUS_CONCURRENCY", 8), "max concurrent goat transitions")
	fs.Float64Var(&cfg.RowsPerSecond, "rows-per-second", floatEnv("GOATOS_BULK_STATUS_RPS", 200), "per-tenant dispatch rate limit (0 = unlimited)")
	fs.IntVar(&cfg.Burst, "burst", intEnv("GOATOS_BULK_STATUS_BURST", 50), "rate limiter burst")
	fs.IntVar(&cfg.BatchSize, "batch-size", intEnv("GOATOS_BULK_STATUS_BATCH_SIZE", 200), "rows claimed per pass (SKIP LOCKED)")
	fs.IntVar(&cfg.MaxRetries, "max-retries", intEnv("GOATOS_BULK_STATUS_MAX_RETRIES", 5), "transient retries before a row is parked as error")
	fs.IntVar(&cfg.MaxIterations, "max-iterations", intEnv("GOATOS_BULK_STATUS_MAX_ITERATIONS", 1000000), "safety bound on drain passes")
	fs.DurationVar(&cfg.ClaimLease, "claim-lease", durationEnv("GOATOS_BULK_STATUS_CLAIM_LEASE", 5*time.Minute), "reclaim rows stuck in 'claimed' older than this (0 disables)")
	fs.IntVar(&cfg.MaxJobs, "max-jobs", intEnv("GOATOS_BULK_STATUS_MAX_JOBS", 100), "max jobs to drain when no job-id is given")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_BULK_STATUS_TIMEOUT", 5*time.Minute), "overall worker timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.Concurrency < 1 {
		return config{}, errors.New("concurrency must be positive")
	}
	if cfg.BatchSize < 1 {
		return config{}, errors.New("batch-size must be positive")
	}
	return cfg, nil
}

func getenv(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func intEnv(key string, fallback int) int {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func floatEnv(key string, fallback float64) float64 {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}
