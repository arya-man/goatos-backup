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
	fs := flag.NewFlagSet("vaccination-projection-worker", flag.ContinueOnError)
	workerID := fs.String("worker-id", strings.TrimSpace(os.Getenv("HOSTNAME")), "stable worker identity")
	limit := fs.Int("limit", 50, "maximum dirty shed scopes to claim")
	leaseFor := fs.Duration("lease-for", 2*time.Minute, "claim lease duration")
	timeout := fs.Duration("timeout", 15*time.Minute, "worker run timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workerID) == "" {
		*workerID = "vaccination-projection-worker"
	}
	if *limit <= 0 || *limit > 500 {
		return errors.New("limit must be between 1 and 500")
	}
	if *leaseFor <= 0 || *timeout <= 0 {
		return errors.New("lease-for and timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	cfg := platformpg.ConfigFromEnv()
	env := strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_ENV")))
	var err error
	if env == "stg" {
		err = localtarget.ValidateStagingCloudSQLDatabaseTarget("vaccination-projection-worker", env, cfg.DatabaseURL)
	} else {
		err = localtarget.ValidateLocalDatabaseTarget("vaccination-projection-worker", env, cfg.DatabaseURL, "local", "dev")
	}
	if err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	result, err := vaccexecpg.NewRepository(pool, cfg.QueryTimeout).ProcessDirtyProjectionScopes(ctx, domain.DirtyProjectionWorkerRequest{
		WorkerID: *workerID, Limit: *limit, LeaseFor: *leaseFor,
	})
	fmt.Printf("vaccination projection worker claimed=%d completed=%d retried=%d dead_lettered=%d tenants=%d\n",
		result.Claimed, result.Completed, result.Retried, result.DeadLettered, result.Tenants)
	return err
}
