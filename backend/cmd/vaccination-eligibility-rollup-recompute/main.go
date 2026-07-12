// Command vaccination-eligibility-rollup-recompute fully rebuilds the vaccination_eligibility_rollups
// read model for a tenant from the source tables (goats + location operational attributes + shed
// profiles + animal stage lookup). This read model backs the Config "Preview impact" button and any
// planning aggregate so the UI request path reads simple top-level numbers WITHOUT scanning goats.
//
// Run it after a seed/import and before preview / staging verification:
//
//	GOATOS_ENV=stg DATABASE_URL=... go run ./cmd/vaccination-eligibility-rollup-recompute -tenant-id <tenant>
//
// This is the CURRENT (explicit, full-recompute) projector entry point. The FUTURE event-driven path:
// goat created/updated/moved / stage changed / health changed / shed operational-attributes changed
// -> outbox / Pub-Sub -> a projector consumer that incrementally updates the affected rollup grains,
// instead of a full tenant rebuild. Until that lands, run this command after any bulk change.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
)

type config struct {
	TenantID string
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

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "vaccination-eligibility-rollup-recompute"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

	pgCfg := platformpg.ConfigFromEnv()
	if err := validateDatabaseTarget(pgCfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	// The recompute is a heavy full-herd aggregate; give it the CLI timeout, not the short per-query
	// timeout. Passing 0 lets the repository use ctx directly.
	repo := vaccinationpg.NewRepository(pool, pgCfg.QueryTimeout)
	res, err := repo.RecomputeEligibilityRollup(ctx, cfg.TenantID)
	if err != nil {
		return fmt.Errorf("recompute vaccination eligibility rollup: %w", err)
	}
	fmt.Printf("recomputed vaccination eligibility rollup tenant=%s grains=%d eligible_animals=%d source_revision=%d recomputed_at=%s\n",
		res.TenantID, res.Grains, res.EligibleAnimals, res.SourceRevision, res.RecomputedAt.Format(time.RFC3339))
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("vaccination-eligibility-rollup-recompute", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id")
	fs.DurationVar(&cfg.Timeout, "timeout", 10*time.Minute, "recompute timeout (full-herd aggregate)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	return cfg, nil
}

func validateDatabaseTarget(databaseURL string) error {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_ENV")))
	if env == "stg" {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("vaccination-eligibility-rollup-recompute", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("vaccination-eligibility-rollup-recompute", env, databaseURL, "local", "dev")
}
