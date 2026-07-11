// Command process-integrity-projection-recompute rebuilds the process_integrity_projection_rows
// read model for one tenant from canonical source tables.
//
// This is intentionally off the request path. Control Tower, Action Center, Protocol Adherence,
// and Workflow detail should read the projection when fresh; this command is the current explicit
// rebuild path after seed/imports until the incremental outbox projector is wired.
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
	processpg "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
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

	repo := processpg.NewRepository(pool, pgCfg.QueryTimeout)
	res, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: cfg.TenantID, AsOf: asOf})
	if err != nil {
		return fmt.Errorf("recompute process-integrity projection: %w", err)
	}
	fmt.Printf("recomputed process-integrity projection tenant=%s rows=%d version=%d as_of=%s projected_at=%s fresh_for=%s counts=%s\n",
		res.TenantID,
		res.Rows,
		res.ProjectionVersion,
		res.AsOf.Format(time.RFC3339),
		res.ProjectedAt.Format(time.RFC3339),
		res.ProjectionFreshFor,
		formatCounts(res.CountsByWorkState),
	)
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("process-integrity-projection-recompute", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id")
	fs.StringVar(&cfg.AsOf, "as-of", strings.TrimSpace(os.Getenv("GOATOS_PROCESS_INTEGRITY_AS_OF")), "projection as-of timestamp (RFC3339); defaults to now")
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
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("process-integrity-projection-recompute", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("process-integrity-projection-recompute", env, databaseURL, "local", "dev")
}

func formatCounts(counts []domain.CountByWorkState) string {
	parts := make([]string, 0, len(counts))
	for _, c := range counts {
		parts = append(parts, fmt.Sprintf("%s:%d", c.WorkState, c.Count))
	}
	return strings.Join(parts, ",")
}
