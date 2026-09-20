// Command seed-health-diagnosis-registers publishes the committed diagnosis rule table
// as version 1 of each animal class's AUTHORED register, so a farm starts from the
// rulebook this repo ships and edits it from /health/config thereafter.
//
// It is ADDITIVE and idempotent. A class that already has a published register is left
// exactly as it is: re-seeding a farm that has since edited its rules would retire the
// vet's work and replace it with the shipped table, which is the failure
// ReplacePublishedProtocols fails closed on for treatment cards and the same answer
// applies here. Run it as often as you like; it publishes only what is missing.
//
// Every register is validated before it is written, so a defect in the committed YAML
// or in the transcribed form stops the seed rather than publishing a rule table nobody
// can be diagnosed against.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	healthpg "github.com/vgoats/goatos/backend/internal/health/adapters/postgres"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-health-diagnosis-registers", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", os.Getenv("GOATOS_TENANT_ID"), "tenant id")
	actorID := fs.String("actor-id", os.Getenv("GOATOS_SEED_ACTOR_ID"), "workforce member id recorded as the publisher")
	timeout := fs.Duration("timeout", 60*time.Second, "seed timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenantID == "" {
		return fmt.Errorf("tenant-id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pool, err := platformpg.Connect(ctx, platformpg.ConfigFromEnv())
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := healthpg.NewRepository(pool, 30*time.Second)
	results, err := repo.SeedPublishedRegisters(ctx, *tenantID, *actorID)
	if err != nil {
		return err
	}

	for _, res := range results {
		fmt.Printf("%-14s %-10s version=%d %s\n", res.AnimalClass, res.Outcome, res.Version, res.RegisterVersionID)
		// Warnings are PRINTED rather than swallowed. Each one names a question whose
		// answer no rule reads, or a non-specific entry no answer can produce -- real
		// state in the shipped register that a vet should see rather than discover.
		for _, w := range res.Warnings {
			if !w.Fatal {
				fmt.Printf("    note  %s: %s\n", w.Path, w.Message)
			}
		}
	}
	return nil
}
