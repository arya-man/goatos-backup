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
	var tenantID string
	var importRunID string
	var dryRun bool
	var batchSize int
	var actorID string
	var policyVersion string

	flag.StringVar(&tenantID, "tenant-id", "", "Tenant UUID")
	flag.StringVar(&importRunID, "import-run-id", "", "Completed legacy import run UUID")
	flag.BoolVar(&dryRun, "dry-run", false, "Preview applyable/review counts without mutation")
	flag.IntVar(&batchSize, "batch-size", legacy_import.DefaultBatchSize, "Apply batch size")
	flag.StringVar(&actorID, "actor-id", "", "Optional system actor UUID")
	flag.StringVar(&policyVersion, "policy-version", "", "Optional policy version guard; defaults to import run policy")
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
	if actorID != "" {
		actor = &actorID
	}
	repo := importpg.NewRepository(pool, 10*time.Second)
	applier := legacy_import.NewApplier(repo)
	result, err := applier.ApplyRFIDRows(ctx, legacy_import.ApplyCommand{
		TenantID:      tenantID,
		ImportRunID:   importRunID,
		DryRun:        dryRun,
		BatchSize:     batchSize,
		ActorID:       actor,
		PolicyVersion: policyVersion,
	})
	if err != nil {
		log.Error("rfid_apply_failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	fmt.Printf("import_run_id=%s dry_run=%t policy_version=%s source_system=%s source_dataset=%s scanned=%d applied=%d replayed=%d review=%d errors=%d skipped=%d",
		result.ImportRunID,
		result.DryRun,
		result.PolicyVersion,
		result.SourceSystem,
		result.SourceDataset,
		result.PendingScanned,
		result.AppliedCount,
		result.ReplayCount,
		result.ReviewCount,
		result.ErrorCount,
		result.SkippedCount,
	)
	for _, reason := range sortedReasons(result.ReviewReasons) {
		fmt.Printf(" review_%s=%d", reason, result.ReviewReasons[reason])
	}
	fmt.Println()
}

func sortedReasons(counts map[string]int) []string {
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return reasons
}
