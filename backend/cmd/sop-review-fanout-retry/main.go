// Command sop-review-fanout-retry durably retries accepted/reworked SOP review fanouts that were
// committed before their post-commit vaccination completion fanout ran.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

type config struct {
	TenantID string
	Limit    int
	Timeout  time.Duration
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
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "sop-review-fanout-retry"})
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

	sopRepo := soppg.NewRepository(pool, pgCfg.QueryTimeout)
	vaccinationRepo := vaccinationpg.NewRepository(pool, pgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, pgCfg.QueryTimeout)
	inventoryService := inventoryapp.NewService(inventorypg.NewRepository(pool, pgCfg.QueryTimeout))
	vaccinationService := vaccinationapp.NewService(vaccinationRepo)
	vaccinationCompletion := vaccinationapp.NewCompletionService(vaccinationService, obligationRepo, inventoryService)
	bus := eventbus.NewInProcessBus()
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).Register(bus)

	sopService := sopapp.NewService(sopRepo).WithTaskReviewFanout(sopbridge.NewVerifyFanout(vaccinationService, bus))
	applied, err := sopService.RetryReviewFanouts(ctx, cfg.TenantID, cfg.Limit)
	if err != nil {
		return err
	}
	fmt.Printf("sop review fanout retry applied=%d tenant=%s\n", applied, cfg.TenantID)
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("sop-review-fanout-retry", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id")
	fs.IntVar(&cfg.Limit, "limit", intEnv("GOATOS_SOP_REVIEW_FANOUT_RETRY_LIMIT", 100), "maximum fanouts to retry")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_SOP_REVIEW_FANOUT_RETRY_TIMEOUT", 60*time.Second), "retry timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if _, err := pgconv.UUID(cfg.TenantID); err != nil {
		return config{}, fmt.Errorf("tenant-id must be a uuid: %w", err)
	}
	if cfg.Limit < 1 {
		cfg.Limit = 100
	}
	if cfg.Limit > 1000 {
		cfg.Limit = 1000
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	return cfg, nil
}

func intEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil || value <= 0 {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}
