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
	MinConns       int32
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
	// AuthMaxConns sizes the dedicated pool that login/auth-session writes use
	// (GOATOS_PG_AUTH_MAX_CONNS, default 2). It exists so a burst of heavy reads
	// that fills the main pool cannot starve POST /auth/session-events
	// (goatos-stg incident 2026-09-24: 6-10s pool waits on a 40-100ms write).
	AuthMaxConns int32
	// AuthQueryTimeout bounds each auth-pool statement, pool acquire included
	// (GOATOS_PG_AUTH_QUERY_TIMEOUT, default 3s), so a stuck auth pool fails
	// fast instead of inheriting the main pool's longer read timeout.
	AuthQueryTimeout time.Duration
	// ApplicationName is set only by deployed services (ServiceApplicationName).
	// Empty leaves the DSN/driver default, which the manual change audit records.
	ApplicationName string
	// Warmer, when set, warms every new pooled connection in the background (see ConnWarmer).
	Warmer *ConnWarmer
	// WarmIdleConns is the pgxpool MinIdleConns used with Warmer (GOATOS_PG_WARM_IDLE_CONNS,
	// default 1): a spare idle connection dialed and warmed off the request path. Capped at
	// MaxConns-1; ignored without Warmer.
	WarmIdleConns int32
}

// ConfigFromEnv builds pool config from environment variables without
// committing secrets into repo config.
func ConfigFromEnv() Config {
	return Config{
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		MaxConns:         envInt32("GOATOS_PG_MAX_CONNS", 10),
		MinConns:         envInt32("GOATOS_PG_MIN_CONNS", 0),
		ConnectTimeout:   envDuration("GOATOS_PG_CONNECT_TIMEOUT", 5*time.Second),
		QueryTimeout:     envDuration("GOATOS_PG_QUERY_TIMEOUT", 3*time.Second),
		AuthMaxConns:     envInt32("GOATOS_PG_AUTH_MAX_CONNS", DefaultAuthMaxConns),
		AuthQueryTimeout: envDuration("GOATOS_PG_AUTH_QUERY_TIMEOUT", DefaultAuthQueryTimeout),
		WarmIdleConns:    envInt32("GOATOS_PG_WARM_IDLE_CONNS", 1),
	}
}

const (
	DefaultAuthMaxConns     int32 = 2
	DefaultAuthQueryTimeout       = 3 * time.Second
)

// AuthPoolConfig derives the config of the dedicated auth pool from the main
// pool config: same DSN, AuthMaxConns connections, no warm minimum, and an
// application_name suffixed "-auth" so pg_stat_activity tells the two apart.
func AuthPoolConfig(cfg Config) Config {
	auth := cfg
	auth.MaxConns = cfg.AuthMaxConns
	if auth.MaxConns <= 0 {
		auth.MaxConns = DefaultAuthMaxConns
	}
	auth.MinConns = 0
	auth.Warmer = nil
	auth.QueryTimeout = cfg.AuthQueryTimeout
	if auth.QueryTimeout <= 0 {
		auth.QueryTimeout = DefaultAuthQueryTimeout
	}
	if cfg.ApplicationName != "" {
		auth.ApplicationName = cfg.ApplicationName + "-auth"
	}
	return auth
}

// ConnectLazy opens a pgx pool WITHOUT making boot depend on it: the pool is
// built (no connections are dialed while MinConns is 0) and one boot ping is
// attempted, but a ping failure -- e.g. Postgres at max_connections while a
// new instance rolls out -- is returned as pingErr for the caller to log and
// count, never as a fatal error. The pool dials again on first use.
func ConnectLazy(ctx context.Context, cfg Config) (pool *pgxpool.Pool, pingErr error, err error) {
	if cfg.DatabaseURL == "" {
		return nil, nil, errors.New("DATABASE_URL is required")
	}
	cfg = normalizedConfig(cfg)
	cfg.MinConns = 0
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse postgres config: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = 0
	configureOLTPRuntime(poolCfg)
	applyApplicationName(poolCfg, cfg.ApplicationName)
	poolCfg.ConnConfig.Tracer = otelpgx.NewTracer()
	pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if perr := pool.Ping(pingCtx); perr != nil {
		pingErr = fmt.Errorf("ping postgres: %w", perr)
	}
	return pool, pingErr, nil
}

// Connect opens and verifies a pgx pool.
func Connect(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	cfg = normalizedConfig(cfg)

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	configureOLTPRuntime(poolCfg)
	if cfg.Warmer != nil {
		cfg.Warmer.install(poolCfg, cfg.WarmIdleConns)
	}
	applyApplicationName(poolCfg, cfg.ApplicationName)
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

func normalizedConfig(cfg Config) Config {
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	if cfg.MaxConns <= 0 {
		cfg.MaxConns = 10
	}
	if cfg.MinConns < 0 {
		cfg.MinConns = 0
	}
	if cfg.MinConns > cfg.MaxConns {
		cfg.MinConns = cfg.MaxConns
	}
	return cfg
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

// ServiceApplicationNamePrefix marks a connection as Goat OS service traffic. The
// manual DB change audit triggers (migration 000387) skip sessions whose
// application_name starts with this prefix and record every other session.
// Only deployed services / scheduled jobs opt in via ServiceApplicationName;
// operator CLIs (repairs, backfills, seeds) stay untagged so their writes are
// audited. See docs/runbooks/manual-db-change-audit.md.
const ServiceApplicationNamePrefix = "goatos-"

// ServiceApplicationName returns the application_name a deployed service binary
// sets on Config.ApplicationName.
func ServiceApplicationName(binary string) string {
	return ServiceApplicationNamePrefix + binary
}

func applyApplicationName(poolCfg *pgxpool.Config, name string) {
	if name == "" {
		return
	}
	poolCfg.ConnConfig.RuntimeParams["application_name"] = name
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
