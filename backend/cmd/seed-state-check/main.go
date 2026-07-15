// Command seed-state-check enforces the persisted seed-run state machine (VACC-REV-02) at the
// closeout / promotion boundary. A source-backed seed records its lifecycle in seed_runs
// (loading -> generating -> verified, or failed on any error). This command reads the latest run
// for a tenant and refuses to let a broken database pass:
//
//	-mode=closeout   FAIL if the latest run is `failed` (RESET_REQUIRED). A database with no
//	                 seed_runs at all (e.g. a legacy environment seeded before the ledger existed)
//	                 is allowed through — it is not a *known-broken* half-seed.
//	-mode=promotion  FAIL unless a `verified`/ready run exists AND the latest run is not `failed`.
//	                 A database that was never verified is not promotable.
//
// Exit code 1 means "reject this database"; exit 0 means "acceptable for the requested boundary".
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/seedrun"
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "seed-state-check: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-state-check", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	mode := fs.String("mode", "closeout", "closeout | promotion")
	timeout := fs.Duration("timeout", 30*time.Second, "query timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *mode != seedrun.ModeCloseout && *mode != seedrun.ModePromotion {
		return fmt.Errorf("invalid -mode %q (want closeout|promotion)", *mode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	latestState, hasRun, err := latestSeedRunState(ctx, pool, *tenantID)
	if err != nil {
		return err
	}
	hasVerified, err := hasVerifiedRun(ctx, pool, *tenantID)
	if err != nil {
		return err
	}

	if err := seedrun.EvaluateGate(*mode, hasRun, latestState, hasVerified); err != nil {
		return err
	}
	fmt.Printf("seed-state-check: OK mode=%s tenant=%s latest_state=%s has_verified=%t\n",
		*mode, *tenantID, displayState(hasRun, latestState), hasVerified)
	return nil
}

func displayState(hasRun bool, latestState string) string {
	if !hasRun {
		return "none"
	}
	return latestState
}

func latestSeedRunState(ctx context.Context, pool pgxQuerier, tenantID string) (string, bool, error) {
	var state string
	err := pool.QueryRow(ctx, `
		SELECT state FROM seed_runs
		WHERE tenant_id = $1::uuid
		ORDER BY updated_at DESC, started_at DESC
		LIMIT 1`, tenantID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read latest seed_run: %w", err)
	}
	return state, true, nil
}

func hasVerifiedRun(ctx context.Context, pool pgxQuerier, tenantID string) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM seed_runs
		WHERE tenant_id = $1::uuid AND state = 'verified'`, tenantID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count verified seed_runs: %w", err)
	}
	return n > 0, nil
}

type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
