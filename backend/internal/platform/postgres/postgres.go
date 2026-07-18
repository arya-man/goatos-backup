package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config describes the Postgres pool for Goat OS API processes.
type Config struct {
	DatabaseURL    string
	MaxConns       int32
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
}

// ConfigFromEnv builds pool config from environment variables without
// committing secrets into repo config.
func ConfigFromEnv() Config {
	return Config{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		MaxConns:       envInt32("GOATOS_PG_MAX_CONNS", 10),
		ConnectTimeout: envDuration("GOATOS_PG_CONNECT_TIMEOUT", 5*time.Second),
		QueryTimeout:   envDuration("GOATOS_PG_QUERY_TIMEOUT", 3*time.Second),
	}
}

// Connect opens and verifies a pgx pool.
func Connect(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	if cfg.MaxConns <= 0 {
		cfg.MaxConns = 10
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	configureOLTPRuntime(poolCfg)
	// otelpgx attaches a span per query/batch/copy/prepare/acquire (using the
	// OTel global TracerProvider/MeterProvider, which observability.SetupTelemetry
	// installs) plus its own duration/error metrics. It defaults to
	// otel.GetTracerProvider()/otel.GetMeterProvider(), which are always safe
	// to call even before SetupTelemetry runs (delegating no-ops until a real
	// provider is installed). Query SQL is summarized, not bound-arg-included,
	// keeping span attributes low-cardinality per the design's cardinality guard.
	poolCfg.ConnConfig.Tracer = otelpgx.NewTracer()

	connectCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

func configureOLTPRuntime(poolCfg *pgxpool.Config) {
	// Goat OS API traffic is latency-sensitive OLTP. PostgreSQL JIT can spend
	// several seconds compiling the large canonical Calendar statement once its
	// estimated cost crosses jit_above_cost, even though the indexed execution
	// itself completes in a few hundred milliseconds. Disable JIT per connection
	// so every pooled request gets predictable latency instead of query-specific
	// compilation pauses.
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolCfg.ConnConfig.RuntimeParams["jit"] = "off"
}

// Ping verifies database readiness within the provided timeout.
func Ping(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) error {
	if pool == nil {
		return errors.New("postgres pool is nil")
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return pool.Ping(pingCtx)
}

func envInt32(name string, fallback int32) int32 {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || n <= 0 {
		return fallback
	}
	return int32(n)
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
