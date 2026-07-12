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
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("vaccination-execution-projection-recompute", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id")
	asOfRaw := fs.String("as-of", "", "projection as-of RFC3339; defaults to now")
	timeout := fs.Duration("timeout", 15*time.Minute, "recompute timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenantID) == "" {
		return errors.New("tenant-id is required")
	}
	if *timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	var asOf time.Time
	var err error
	if strings.TrimSpace(*asOfRaw) != "" {
		asOf, err = time.Parse(time.RFC3339, *asOfRaw)
		if err != nil {
			return fmt.Errorf("as-of must be RFC3339: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	cfg := platformpg.ConfigFromEnv()
	env := strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_ENV")))
	if env == "stg" {
		err = localtarget.ValidateStagingCloudSQLDatabaseTarget("vaccination-execution-projection-recompute", env, cfg.DatabaseURL)
	} else {
		err = localtarget.ValidateLocalDatabaseTarget("vaccination-execution-projection-recompute", env, cfg.DatabaseURL, "local", "dev")
	}
	if err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	result, err := vaccexecpg.NewRepository(pool, cfg.QueryTimeout).RecomputeExecutionProjection(ctx, domain.ExecutionProjectionRecomputeRequest{TenantID: *tenantID, AsOf: asOf})
	if err != nil {
		return err
	}
	fmt.Printf("recomputed vaccination execution projection tenant=%s rows=%d version=%d as_of=%s\n", result.TenantID, result.Rows, result.ProjectionVersion, result.AsOf.Format(time.RFC3339))
	return nil
}
