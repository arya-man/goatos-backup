// Command repair-obligation-duplicates retires duplicate open obligations and labels the
// survivors with the cause that produced them. The repair itself lives in
// internal/obligation/repair, where it can be tested against a real database.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/repair"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"

	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, timeout, err := parseFlags(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "repair-obligation-duplicates"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

	pool, err := platformpg.Connect(ctx, platformpg.ConfigFromEnv())
	if err != nil {
		return err
	}
	defer pool.Close()

	got, err := repair.Run(ctx, pool, obligationpg.NewRepository(pool, 30*time.Second), cfg)
	if err != nil {
		return err
	}
	mode := "dry_run"
	if cfg.Apply {
		mode = "applied"
	}
	fmt.Printf(
		"repair-obligation-duplicates %s mode=%s tenant=%s groups_examined=%d duplicates_retired=%d cycles_labelled=%d skipped_already_valid=%d skipped_no_anchor=%d skipped_not_repeat=%d\n",
		mode, cfg.Mode, cfg.TenantID,
		got.GroupsExamined, got.DuplicatesRetired, got.CyclesLabelled,
		got.SkippedAlreadyValid, got.SkippedNoAnchor, got.SkippedNotRepeat,
	)
	return nil
}

func parseFlags(args []string) (repair.Config, time.Duration, error) {
	fs := flag.NewFlagSet("repair-obligation-duplicates", flag.ContinueOnError)
	var cfg repair.Config
	var timeout time.Duration
	fs.StringVar(&cfg.TenantID, "tenant", "", "tenant id to repair (required)")
	fs.StringVar(&cfg.Mode, "mode", "repeat", strings.Join(repair.Modes(), "|")+": which duplicate class to repair")
	fs.IntVar(&cfg.Limit, "limit", 500, "maximum duplicate groups to process in one run")
	fs.DurationVar(&timeout, "timeout", 10*time.Minute, "overall run timeout")
	// Applying is opt-in. A repair job that mutates by default is one mistyped flag away from
	// retiring live work.
	var dryRun bool
	fs.BoolVar(&dryRun, "dry-run", true, "report what would change without changing it")
	if err := fs.Parse(args); err != nil {
		return repair.Config{}, 0, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return repair.Config{}, 0, errors.New("-tenant is required")
	}
	if !repair.ValidMode(cfg.Mode) {
		return repair.Config{}, 0, fmt.Errorf("unknown -mode %q: want one of %s", cfg.Mode, strings.Join(repair.Modes(), ", "))
	}
	cfg.Apply = !dryRun
	if cfg.Limit <= 0 {
		return repair.Config{}, 0, errors.New("-limit must be positive")
	}
	return cfg, timeout, nil
}
