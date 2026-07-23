// Command recompute-vaccination-drives is a one-time admin operation that recomputes
// existing future vaccination drive batches + operator assignments to match the CURRENT
// operator-assignment config (default operator, coverage schedules, capacity caps).
//
// Usage:
//
//	recompute-vaccination-drives -tenant <UUID> -park <UUID> [-from <date>]
//
// The from date (default today IST) controls the start of the future window to recompute.
// Batches with planned_date < from are left untouched (historical).
//
// This operation:
// 1. Acquires the per-tenant advisory lock (blocking concurrent sweepers)
// 2. Releases future planned batches and their obligations back to unbatched state
// 3. Nulls operator assignments and clears vaccination_drive_assignments
// 4. DOES NOT re-run the sweeper (caller/maintainer must run separately if needed)
//
// Requirements:
// - Must be run against an EXPLICIT database (never the shared 5433 app DB)
// - Idempotent: second run with same config produces same result
// - Clinical due dates are PRESERVED (byte-identical before/after)
// - In-progress/completed batches are LEFT UNTOUCHED
//
// Exit codes:
//
//	0 = success (future planned batches released; run obligation-sweeper to re-plan)
//	1 = argument/DB error (operator's responsibility to fix and retry)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

type config struct {
	TenantID string
	ParkID   string
	FromDate time.Time
	Timeout  time.Duration
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args, time.Now)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	// Connect to database (must be explicit, never default 5433)
	pgCfg := platformpg.ConfigFromEnv()

	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("recompute-vaccination-drives: connect to database: %w", err)
	}
	defer pool.Close()

	// Create repositories and services
	obligationRepo := obligationpg.NewRepository(pool, pgCfg.QueryTimeout)

	// Release future planned batches back to unbatched
	fmt.Printf("Releasing future planned batches (from %s onwards)...\n", cfg.FromDate.Format("2006-01-02"))
	releasedCount, err := obligationRepo.RecomputeFutureVaccinationDrives(ctx, cfg.TenantID, cfg.ParkID, cfg.FromDate)
	if err != nil {
		return fmt.Errorf("recompute-vaccination-drives: release batches: %w", err)
	}
	fmt.Printf("Released %d batches back to unbatched state.\n", releasedCount)
	fmt.Println("\nDone. Future vaccination drives have been released.")
	fmt.Println("\nTo re-batch under current operator config, run obligation-sweeper separately.")
	return nil
}

type timeNowFunc func() time.Time

func parseFlags(args []string, timeNow timeNowFunc) (config, error) {
	fs := flag.NewFlagSet("recompute-vaccination-drives", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: recompute-vaccination-drives -tenant <UUID> -park <UUID> [-from <date>]\n\n")
		fs.PrintDefaults()
	}

	tenantID := fs.String("tenant", "", "Tenant ID (required, UUID format)")
	parkID := fs.String("park", "", "Park ID (required, UUID format)")
	fromStr := fs.String("from", "", "Start of future window to recompute (YYYY-MM-DD, default: today IST)")
	timeout := fs.Duration("timeout", 5*time.Minute, "Operation timeout")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	if *tenantID == "" {
		return config{}, fmt.Errorf("recompute-vaccination-drives: -tenant is required")
	}
	if *parkID == "" {
		return config{}, fmt.Errorf("recompute-vaccination-drives: -park is required")
	}

	// Parse from date; default to today IST
	fromDate := biztime.BusinessDayStart(timeNow())
	if *fromStr != "" {
		parsed, err := time.Parse("2006-01-02", *fromStr)
		if err != nil {
			return config{}, fmt.Errorf("recompute-vaccination-drives: from date format must be YYYY-MM-DD: %w", err)
		}
		fromDate = biztime.BusinessDayStart(parsed)
	}

	return config{
		TenantID: *tenantID,
		ParkID:   *parkID,
		FromDate: fromDate,
		Timeout:  *timeout,
	}, nil
}
