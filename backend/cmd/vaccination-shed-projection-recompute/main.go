// Command vaccination-shed-projection-recompute rebuilds the vaccination_shed_projection_rows read model
// (backend/migrations/postgres/000167_vaccination_shed_projection.sql) for one tenant from canonical
// obligation/goat/capacity-config tables.
//
// This is intentionally off the request path (C35-002, vaccinationexecution half). GET /vaccination/sheds
// (ShedSummary) still serves the live compute-on-read CTE today; this command keeps the projection warm
// so a later change can flip that read over without a cold read model. Mirrors
// backend/cmd/process-integrity-projection-recompute.
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
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type config struct {
	TenantID string
	AsOf     string
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
	asOf := time.Time{}
	if cfg.AsOf != "" {
		asOf, err = time.Parse(time.RFC3339, cfg.AsOf)
		if err != nil {
			return fmt.Errorf("as-of must be RFC3339: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	if err := validateDatabaseTarget(pgCfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := vaccexecpg.NewRepository(pool, pgCfg.QueryTimeout)
	res, err := repo.RecomputeShedProjection(ctx, vaccexecdomain.ShedProjectionRecomputeRequest{TenantID: cfg.TenantID, AsOf: asOf})
	if err != nil {
		return fmt.Errorf("recompute vaccination shed projection: %w", err)
	}
	fmt.Printf("recomputed vaccination shed projection tenant=%s rows=%d version=%d as_of=%s projected_at=%s fresh_for=%s\n",
		res.TenantID,
		res.Rows,
		res.ProjectionVersion,
		res.AsOf.Format(time.RFC3339),
		res.ProjectedAt.Format(time.RFC3339),
		res.ProjectionFreshFor,
	)
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("vaccination-shed-projection-recompute", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id")
	fs.StringVar(&cfg.AsOf, "as-of", strings.TrimSpace(os.Getenv("GOATOS_VACCINATION_SHED_PROJECTION_AS_OF")), "projection as-of timestamp (RFC3339); defaults to now")
	fs.DurationVar(&cfg.Timeout, "timeout", 30*time.Minute, "recompute timeout")
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
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("vaccination-shed-projection-recompute", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("vaccination-shed-projection-recompute", env, databaseURL, "local", "dev")
}
