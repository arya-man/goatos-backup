// kid-stage-shifting-raise runs one sweep of the kid stage ladder (maintainer decision
// 2026-08-20): K0 kids at 2 days of age and K1 kids at 7 days get a shifting RAISED
// automatically toward the park's single K1/K2 pen. A park with zero or several candidate pens
// is reported (and served to the phone as the due card) instead of guessed.
//
// The raise is the standard movement — pending event + approval request — so Park Head approval,
// operator completion with video, and verification all run unchanged. Safe to run any number of
// times per day: due kids already named in an in-flight movement are excluded, and the
// idempotency key is deterministic per (park, step, business date, goat set).
//
// Mirrors the feed-transport-issue cmd shape: one-shot, tenant-scoped, schedulable.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	if err := run(os.Args[1:], time.Now); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, now func() time.Time) error {
	fs := flag.NewFlagSet("kid-stage-shifting-raise", flag.ContinueOnError)
	tenant := fs.String("tenant-id", os.Getenv("GOATOS_TENANT_ID"), "tenant id")
	asOfRaw := fs.String("as-of", "", "RFC3339 instant (defaults to now)")
	dryRun := fs.Bool("dry-run", false, "compute and print the due groups without raising anything")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenant) == "" {
		return fmt.Errorf("tenant-id is required")
	}
	asOf := now()
	if *asOfRaw != "" {
		parsed, err := time.Parse(time.RFC3339, *asOfRaw)
		if err != nil {
			return fmt.Errorf("as-of must be RFC3339: %w", err)
		}
		asOf = parsed
	}
	asOf = asOf.In(biztime.DefaultLocation())

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	countsRepo := countspg.NewRepository(pool, cfg.QueryTimeout)
	identityRepo := identitypg.NewRepository(pool, cfg.QueryTimeout)
	raiser := countsapp.NewKidStageLadderRaiser(
		countsRepo,
		countsapp.NewService(countsRepo),
		// The identity preparer only runs at APPROVAL apply time (birth/death lifecycle prep);
		// the raise path never calls it, but the service contract wants a real one.
		countsapp.NewApprovalService(countsRepo, identityapp.NewService(identityRepo), nil),
	)

	if *dryRun {
		groups, err := raiser.DueGroups(ctx, strings.TrimSpace(*tenant), asOf)
		if err != nil {
			return err
		}
		fmt.Printf("kid-stage-shifting-raise dry-run business_date=%s groups=%d\n", biztime.BusinessDate(asOf), len(groups))
		for _, group := range groups {
			fmt.Printf("  park=%s %s->%s kids=%d candidate_pens=%d auto_raisable=%t\n",
				group.ParkID, group.FromStage, group.ToStage, len(group.Goats), len(group.Candidates), group.AutoRaisable())
		}
		return nil
	}

	result, err := raiser.AutoRaise(ctx, strings.TrimSpace(*tenant), asOf)
	if err != nil {
		return err
	}
	fmt.Printf("kid-stage-shifting-raise business_date=%s raised=%d card_groups=%d\n",
		result.BusinessDate, len(result.Raised), len(result.CardGroups))
	for _, raised := range result.Raised {
		fmt.Printf("  raised park=%s %s->%s kids=%d destination=%s event=%s approval=%s replay=%t\n",
			raised.ParkID, raised.FromStage, raised.ToStage, raised.GoatCount,
			raised.DestinationLabel, raised.ShiftingEventID, raised.ApprovalRequestID, raised.IdempotentReplay)
	}
	for _, group := range result.CardGroups {
		fmt.Printf("  card park=%s %s->%s kids=%d candidate_pens=%d (operator selects the destination)\n",
			group.ParkID, group.FromStage, group.ToStage, len(group.Goats), len(group.Candidates))
	}
	return nil
}
