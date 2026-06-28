// Command inventory-batch-reconciler releases excess reserved stock for
// obligation batches explicitly marked stock_reconcile_required after
// defer/shift/cancel changed the batch membership.
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
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
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

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	service := inventoryapp.NewService(inventorypg.NewRepository(pool, pgCfg.QueryTimeout))
	summary, err := service.ReleaseBatchReconcileRemainders(ctx, cfg.TenantID, cfg.Limit)
	if err != nil {
		return err
	}
	fmt.Printf("inventory batch reconcile batches=%d movements=%d released=%d tenant=%s\n",
		summary.Batches, summary.Movements, summary.Released, cfg.TenantID)
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("inventory-batch-reconciler", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id")
	fs.IntVar(&cfg.Limit, "limit", intEnv("GOATOS_INVENTORY_BATCH_RECONCILE_LIMIT", 1000), "max batches to reconcile")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_INVENTORY_BATCH_RECONCILE_TIMEOUT", 60*time.Second), "reconcile timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if cfg.Limit <= 0 {
		cfg.Limit = 1000
	}
	if cfg.Limit > 5000 {
		cfg.Limit = 5000
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
