package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	reportingpg "github.com/vgoats/goatos/backend/internal/reporting/adapters/postgres"
	reportingapp "github.com/vgoats/goatos/backend/internal/reporting/app"
	"github.com/vgoats/goatos/backend/internal/reporting/domain"
	"github.com/vgoats/goatos/backend/internal/reporting/ports"
)

func main() {
	var tenantID string
	var sourceImportRunID string
	var grainsCSV string
	var timeout time.Duration
	flag.StringVar(&tenantID, "tenant-id", "", "tenant UUID to rebuild")
	flag.StringVar(&sourceImportRunID, "source-import-run-id", "", "optional legacy import run UUID to stamp on produced rows")
	flag.StringVar(&grainsCSV, "grains", "", "optional comma-separated subset of Phase 1 counter grains")
	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "command timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cfg := platformpg.ConfigFromEnv()
	if err := localtarget.ValidateLocalDatabaseTarget("rebuild-identity-counters", os.Getenv("GOATOS_ENV"), cfg.DatabaseURL, "local", "dev"); err != nil {
		fatal(err)
	}
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		fatal(err)
	}
	defer pool.Close()

	repo := reportingpg.NewRepository(pool, timeout)
	service := reportingapp.NewService(repo)
	params := ports.RebuildIdentityCountersParams{
		TenantID: tenantID,
		Grains:   parseGrains(grainsCSV),
	}
	if strings.TrimSpace(sourceImportRunID) != "" {
		value := strings.TrimSpace(sourceImportRunID)
		params.SourceImportRunID = &value
	}
	result, err := service.RebuildIdentityCounters(ctx, params)
	if err != nil {
		fatal(err)
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(out))
}

func parseGrains(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return domain.AllIdentityCounterGrains
	}
	parts := strings.Split(raw, ",")
	grains := make([]string, 0, len(parts))
	for _, part := range parts {
		grain := strings.TrimSpace(part)
		if grain != "" {
			grains = append(grains, grain)
		}
	}
	return grains
}

func fatal(err error) {
	slog.Error("rebuild identity counters failed", "error", err)
	os.Exit(1)
}
