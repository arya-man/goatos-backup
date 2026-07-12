package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/observability"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

type config struct {
	TenantID string
	Limit    int
	Timeout  time.Duration
	Before   time.Time
	DryRun   bool
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

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "idempotency-key-sweeper"})
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

	count, err := sweepExpired(ctx, pool, cfg)
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
	fmt.Printf("idempotency key sweep %s=%d before=%s tenant=%s\n", action, count, cfg.Before.UTC().Format(time.RFC3339), tenant)
	return nil
}

func parseFlags(args []string, now func() time.Time) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("idempotency-key-sweeper", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "optional tenant id filter")
	fs.IntVar(&cfg.Limit, "limit", 1000, "maximum expired keys to delete in one run")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_IDEMPOTENCY_SWEEPER_TIMEOUT", 30*time.Second), "sweeper timeout")
	beforeRaw := fs.String("before", getenv("GOATOS_IDEMPOTENCY_SWEEPER_BEFORE"), "RFC3339 expiry cutoff; default now")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "count expired keys without deleting")
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
	if now == nil {
		now = time.Now
	}
	cfg.Before = now().UTC()
	if strings.TrimSpace(*beforeRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*beforeRaw))
		if err != nil {
			return config{}, errors.New("before must be RFC3339")
		}
		cfg.Before = parsed.UTC()
	}
	return cfg, nil
}

func sweepExpired(ctx context.Context, pool *pgxpool.Pool, cfg config) (int, error) {
	tenant, err := tenantArg(cfg.TenantID)
	if err != nil {
		return 0, err
	}
	if cfg.DryRun {
		return countExpired(ctx, pool, cfg, tenant)
	}
	tag, err := pool.Exec(ctx, `
WITH expired AS (
  SELECT idempotency_key
  FROM idempotency_keys
  WHERE expires_at IS NOT NULL
    AND expires_at <= $1::timestamptz
    AND ($2::uuid IS NULL OR tenant_id = $2::uuid)
  ORDER BY expires_at ASC, idempotency_key ASC
  LIMIT $3
  FOR UPDATE SKIP LOCKED
)
DELETE FROM idempotency_keys k
USING expired
WHERE k.idempotency_key = expired.idempotency_key`, cfg.Before, tenant, cfg.Limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired idempotency keys: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func countExpired(ctx context.Context, pool *pgxpool.Pool, cfg config, tenant any) (int, error) {
	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int
FROM (
  SELECT 1
  FROM idempotency_keys
  WHERE expires_at IS NOT NULL
    AND expires_at <= $1::timestamptz
    AND ($2::uuid IS NULL OR tenant_id = $2::uuid)
  ORDER BY expires_at ASC, idempotency_key ASC
  LIMIT $3
) expired`, cfg.Before, tenant, cfg.Limit).Scan(&count); err != nil {
		return 0, fmt.Errorf("count expired idempotency keys: %w", err)
	}
	return count, nil
}

func tenantArg(tenantID string) (any, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, nil
	}
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant-id must be a uuid: %w", err)
	}
	return tenant, nil
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
