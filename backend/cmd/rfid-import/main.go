package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/vgoats/goatos/backend/internal/legacy_import"
	importpg "github.com/vgoats/goatos/backend/internal/legacy_import/adapters/postgres"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	var inputPath string
	var tenantID string
	var dryRun bool
	var batchSize int
	var startedBy string
	var sourceName string
	var policyVersion string

	flag.StringVar(&inputPath, "input", "", "Path to the RFID workbook .xlsx file")
	flag.StringVar(&tenantID, "tenant-id", "", "Tenant UUID")
	flag.BoolVar(&dryRun, "dry-run", false, "Record an import run and aggregate counts without staging rows")
	flag.IntVar(&batchSize, "batch-size", legacy_import.DefaultBatchSize, "Staging insert batch size")
	flag.StringVar(&startedBy, "started-by", "", "Optional actor UUID")
	flag.StringVar(&sourceName, "source-name", legacy_import.DefaultSourceName, "Logical source name")
	flag.StringVar(&policyVersion, "policy-version", legacy_import.DefaultPolicyVersion, "Legacy import policy version")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := platformpg.Connect(ctx, platformpg.ConfigFromEnv())
	if err != nil {
		log.Error("connect_postgres", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	var actor *string
	if startedBy != "" {
		actor = &startedBy
	}
	repo := importpg.NewRepository(pool, 5*time.Second)
	importer := legacy_import.NewImporter(repo)
	result, err := importer.ImportRFIDWorkbook(ctx, legacy_import.ImportCommand{
		InputPath:     inputPath,
		TenantID:      tenantID,
		DryRun:        dryRun,
		BatchSize:     batchSize,
		StartedBy:     actor,
		SourceName:    sourceName,
		PolicyVersion: policyVersion,
	})
	if err != nil {
		log.Error("rfid_import_failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	fmt.Printf("import_run_id=%s dry_run=%t policy_version=%s source_system=%s source_dataset=%s rows=%d inserted=%d errors=%d",
		result.ImportRunID,
		result.DryRun,
		result.PolicyVersion,
		result.SourceSystem,
		result.SourceDataset,
		result.RowCount,
		result.RowsInserted,
		result.ErrorCount,
	)
	for _, state := range sortedStates(result.StateCounts) {
		fmt.Printf(" %s=%d", state, result.StateCounts[state])
	}
	fmt.Println()
}

func sortedStates(counts map[string]int) []string {
	states := make([]string, 0, len(counts))
	for state := range counts {
		states = append(states, state)
	}
	sort.Strings(states)
	return states
}
