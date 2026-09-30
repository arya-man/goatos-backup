// Command backfill-kid-shift-tasks opens the litter's K0 -> K1 / K1 -> K2 shift workflow for the
// litters ALREADY on the farm (KID STAGE SHIFT TASKS, docs/decisions/kid-stage-shift-tasks.md;
// maintainer instruction 2026-09-30: "this should already reflect for animals which are present
// also"). New births open theirs from goat.created; this covers the ones born before the rule.
//
// A candidate is a recorded, non-rejected litter with a LIVE kid still on a stage before the last
// shift target (K0 or K1 by default) -- or a live FEMALE kid on a stage before the female-only
// step's target (Non-Pregnant by default, maintainer instruction 2026-10-01) -- and no litter
// workflow yet. Each is opened through the SAME
// service path a new birth uses, anchored on the kid's recorded birth moment, then judged against
// the herd register at once -- so a litter already on K1 gets its K1 step completed at the day the
// kids reached K1 and its K2 step due seven days after that. Kid and mother tracks are never
// opened here.
//
// Idempotent: a litter that already has its workflow is not a candidate, so re-running converges.
// Dry-run by default; pass -apply to write.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "backfill-kid-shift-tasks:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("backfill-kid-shift-tasks", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", os.Getenv("GOATOS_TENANT_ID"), "tenant id")
	lastTarget := fs.String("last-target-stage", "K2", "the last shift step's target; litters with a live kid on a stage before it are candidates")
	femaleTarget := fs.String("female-target-stage", "Non-Pregnant", "the female-only shift step's target; litters with a live FEMALE kid on a stage before it are candidates too (empty = none)")
	apply := fs.Bool("apply", false, "write; without it the command only lists candidates")
	pageSize := fs.Int("page-size", 200, "litters per page")
	timeout := fs.Duration("timeout", 10*time.Minute, "overall timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenantID) == "" {
		return fmt.Errorf("tenant-id is required")
	}
	stages := countsdomain.GrowthStagesBefore(*lastTarget)
	if len(stages) == 0 {
		return fmt.Errorf("%q is not a stage animals grow into", *lastTarget)
	}
	var femaleStages []string
	if strings.TrimSpace(*femaleTarget) != "" {
		if femaleStages = countsdomain.GrowthStagesBefore(*femaleTarget); len(femaleStages) == 0 {
			return fmt.Errorf("%q is not a stage animals grow into", *femaleTarget)
		}
	}
	if *pageSize < 1 || *pageSize > 1000 {
		return fmt.Errorf("page-size must be between 1 and 1000")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	cfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := taskspg.NewRepository(pool, cfg.QueryTimeout)
	svc := tasksapp.NewService(repo, observability.New(observability.Config{Service: "backfill-kid-shift-tasks"}))

	candidates, opened, after := 0, 0, ""
	for {
		// scale-guard:ignore: keyset pagination of a one-shot operator command -- one bounded page read per page, never per row
		page, err := repo.LittersOwingShift(ctx, *tenantID, stages, femaleStages, after, *pageSize)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		for _, l := range page {
			candidates++
			if !*apply {
				fmt.Printf("candidate birth_event=%s kid=%s\n", l.BirthEventID, l.KidGoatID)
				continue
			}
			// scale-guard:ignore: each litter's workflow opens in its own idempotent transaction through the production opener; one-shot backfill, bounded by the page
			if err := svc.OpenLitterWorkflowForKid(ctx, *tenantID, l.KidGoatID); err != nil {
				return fmt.Errorf("birth event %s: %w", l.BirthEventID, err)
			}
			opened++
		}
		after = page[len(page)-1].BirthEventID
	}
	fmt.Printf("kid shift backfill stages=%v female_stages=%v candidates=%d opened=%d apply=%v\n", stages, femaleStages, candidates, opened, *apply)
	return nil
}
