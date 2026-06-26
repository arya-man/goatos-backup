// Command generate-vaccination-obligations runs SM-1 obligation generation for published
// vaccination protocol versions against the existing in-care cohort. The :8080 API only generates
// obligations event-driven (on goat.created); after publishing a new version, the existing cohort
// has no obligations until generation is run. The obligation-sweeper only BATCHES existing
// obligations, so it reports 0 when none have been generated yet. This CLI invokes the real
// production GenerationService.GenerateForVersion (same code path as the goat.created handler),
// so obligation_instances come from the genuine generation logic, not hand-written rows.
//
// Usage:
//
//	DATABASE_URL=... go run ./cmd/generate-vaccination-obligations \
//	  -tenant-id <tenant> [-version-id <v>] [-as-of RFC3339]
//
// With no -version-id it generates for every published vaccination version.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

type config struct {
	TenantID  string
	VersionID string
	AsOf      time.Time
	Timeout   time.Duration
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
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	protocolRepo := protocolpg.NewRepository(pool, pgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, pgCfg.QueryTimeout)
	vaccinationRepo := vaccinationpg.NewRepository(pool, pgCfg.QueryTimeout)
	gen := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)

	versionIDs := []string{cfg.VersionID}
	if cfg.VersionID == "" {
		versionIDs, err = protocolRepo.ListPublishedVaccinationVersions(ctx, cfg.TenantID)
		if err != nil {
			return fmt.Errorf("list published versions: %w", err)
		}
	}
	if len(versionIDs) == 0 {
		fmt.Println("no published vaccination protocol versions to generate")
		return nil
	}

	for _, versionID := range versionIDs {
		res, err := gen.GenerateForVersion(ctx, cfg.TenantID, versionID, cfg.AsOf)
		if err != nil {
			return fmt.Errorf("generate version %s: %w", versionID, err)
		}
		fmt.Printf("generated version=%s generated=%d deferred=%d skipped_no_due_date=%d suppressed_trusted=%d\n",
			versionID, res.Generated, res.Deferred, res.SkippedNoDueDate, res.SuppressedByTrustedHistory)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("generate-vaccination-obligations", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id")
	fs.StringVar(&cfg.VersionID, "version-id", "", "protocol version id; empty generates for all published vaccination versions")
	fs.DurationVar(&cfg.Timeout, "timeout", 120*time.Second, "generation timeout")
	asOfRaw := fs.String("as-of", "", "RFC3339 as-of instant; default now")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	cfg.AsOf = time.Now().UTC()
	if strings.TrimSpace(*asOfRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*asOfRaw))
		if err != nil {
			return config{}, errors.New("as-of must be RFC3339")
		}
		cfg.AsOf = parsed.UTC()
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	return cfg, nil
}
