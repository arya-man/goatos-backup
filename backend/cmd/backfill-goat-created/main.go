// Command backfill-goat-created is the MANUAL repair entry point for a lost
// `goat.created` event. It shares its implementation with the SCHEDULED
// reconciliation stage that runs inside the kernel worker
// (backend/internal/kernelstages.GoatCreatedRecoveryStage) — see
// backend/internal/identity/goatcreatedrecovery. Automatic recovery does not
// depend on an operator running this command (BUG-016).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/goatcreatedrecovery"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("backfill-goat-created", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", os.Getenv("GOATOS_TENANT_ID"), "tenant id")
	goatID := fs.String("goat-id", "", "optional goat id to backfill exactly one existing goat")
	limit := fs.Int("limit", 500, "maximum goats to backfill")
	dryRun := fs.Bool("dry-run", false, "list candidate count without writing")
	timeout := fs.Duration("timeout", 60*time.Second, "backfill timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenantID == "" {
		return fmt.Errorf("tenant-id is required")
	}
	if *limit < 1 || *limit > 5000 {
		return fmt.Errorf("limit must be between 1 and 5000")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	candidates, err := goatcreatedrecovery.ScanCandidates(ctx, pool, *tenantID, *goatID, *limit)
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Printf("goat.created backfill candidates=%d\n", len(candidates))
		return nil
	}
	applied, err := goatcreatedrecovery.Recover(ctx, pool, candidates)
	if err != nil {
		return err
	}
	fmt.Printf("goat.created backfill applied=%d candidates=%d\n", applied, len(candidates))
	return nil
}
