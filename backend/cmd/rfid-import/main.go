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
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	var inputPath string
	var sheetName string
	var tenantID string
	var dryRun bool
	var batchSize int
	var startedBy string
	var sourceName string
	var policyVersion string
	var sourceType string
	var discover bool
	var anomalyReport bool
	var importRunID string
	var outputDir string
	var includeSensitive bool

	flag.StringVar(&inputPath, "input", "", "Path to the RFID workbook .xlsx file")
	flag.StringVar(&sheetName, "sheet", "", "Optional workbook sheet name, for example Combined")
	flag.StringVar(&tenantID, "tenant-id", "", "Tenant UUID")
	flag.BoolVar(&dryRun, "dry-run", false, "Record an import run and aggregate counts without staging rows")
	flag.IntVar(&batchSize, "batch-size", legacy_import.DefaultBatchSize, "Staging insert batch size")
	flag.StringVar(&startedBy, "started-by", "", "Optional actor UUID")
	flag.StringVar(&sourceName, "source-name", legacy_import.DefaultSourceName, "Logical source name")
	flag.StringVar(&policyVersion, "policy-version", legacy_import.DefaultPolicyVersion, "Legacy import policy version")
	flag.StringVar(&sourceType, "source-type", legacy_import.SourceTypeLocalXLSX, "Source type: local_xlsx or google_sheet")
	flag.BoolVar(&discover, "discover", false, "Classify the source workbook without staging rows")
	flag.BoolVar(&anomalyReport, "anomaly-report", false, "Write a sanitized anomaly report for an import run")
	flag.StringVar(&importRunID, "import-run-id", "", "Import run UUID for --anomaly-report")
	flag.StringVar(&outputDir, "output-dir", ".codex-goatos-render/import-reports", "Output directory for local reports")
	flag.BoolVar(&includeSensitive, "include-sensitive", false, "Include raw RFID/old tag values in local report output")
	flag.Parse()

	log := observability.New(observability.Config{Service: "rfid-import"})
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if discover {
		if err := runDiscovery(inputPath, sheetName, sourceType); err != nil {
			log.Error("rfid_source_discovery_failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		return
	}

	cfg := platformpg.ConfigFromEnv()
	if err := localtarget.ValidateLocalDatabaseTarget("rfid-import", os.Getenv("GOATOS_ENV"), cfg.DatabaseURL, "local", "dev"); err != nil {
		log.Error("unsafe_database_target", slog.String("error", err.Error()))
		os.Exit(1)
	}
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		log.Error("connect_postgres", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	if anomalyReport {
		if err := writeAnomalyReport(ctx, importpg.NewRepository(pool, 5*time.Second), anomalyReportOptions{
			tenantID:         tenantID,
			importRunID:      importRunID,
			sourceType:       sourceType,
			sourceLabel:      sourceName,
			sheetName:        sheetName,
			outputDir:        outputDir,
			includeSensitive: includeSensitive,
		}); err != nil {
			log.Error("rfid_anomaly_report_failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		return
	}

	if sourceType != legacy_import.SourceTypeLocalXLSX {
		log.Error("unsupported_source_type", slog.String("source_type", sourceType), slog.String("next_step", legacy_import.GoogleSheetDiscoverySkipped().RecommendedNextStep))
		os.Exit(1)
	}

	var actor *string
	if startedBy != "" {
		actor = &startedBy
	}
	repo := importpg.NewRepository(pool, 5*time.Second)
	importer := legacy_import.NewImporter(repo)
	result, err := importer.ImportRFIDWorkbook(ctx, legacy_import.ImportCommand{
		InputPath:     inputPath,
		SheetName:     sheetName,
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

func runDiscovery(inputPath, sheetName, sourceType string) error {
	switch sourceType {
	case legacy_import.SourceTypeGoogleSheet:
		printDiscovery(legacy_import.GoogleSheetDiscoverySkipped())
		return nil
	case legacy_import.SourceTypeLocalXLSX, "":
		data, err := os.ReadFile(inputPath)
		if err != nil {
			return fmt.Errorf("read input workbook failed")
		}
		result, err := legacy_import.DiscoverXLSXSource(data, sheetName)
		if err != nil {
			return err
		}
		printDiscovery(result)
		return nil
	default:
		return fmt.Errorf("unsupported source_type %q", sourceType)
	}
}

func printDiscovery(result legacy_import.SourceDiscoveryResult) {
	fmt.Printf("source_type=%s classification=%s importable=%t google_sheet_export_built=%t",
		result.SourceType,
		result.Classification,
		result.Importable,
		result.GoogleSheetExportBuilt,
	)
	if result.TargetSheet != "" {
		fmt.Printf(" target_sheet=%q", result.TargetSheet)
	}
	if len(result.SheetNames) > 0 {
		fmt.Printf(" sheets=%q", result.SheetNames)
	}
	if len(result.MissingRequiredHeaders) > 0 {
		fmt.Printf(" missing_required_headers=%q", result.MissingRequiredHeaders)
	}
	if result.RecommendedNextStep != "" {
		fmt.Printf(" next_step=%q", result.RecommendedNextStep)
	}
	fmt.Println()
}

type anomalyReportOptions struct {
	tenantID         string
	importRunID      string
	sourceType       string
	sourceLabel      string
	sheetName        string
	outputDir        string
	includeSensitive bool
}

func writeAnomalyReport(ctx context.Context, repo *importpg.Repository, opts anomalyReportOptions) error {
	if opts.tenantID == "" {
		return fmt.Errorf("tenant-id is required for anomaly report")
	}
	if opts.importRunID == "" {
		return fmt.Errorf("import-run-id is required for anomaly report")
	}
	if opts.sourceType == "" {
		opts.sourceType = legacy_import.SourceTypeLocalXLSX
	}
	rows, err := repo.ListAnomalyReportRows(ctx, opts.tenantID, opts.importRunID)
	if err != nil {
		return err
	}
	reportOptions := legacy_import.AnomalyReportOptions{
		ImportRunID:      opts.importRunID,
		SourceType:       opts.sourceType,
		SourceLabel:      opts.sourceLabel,
		SheetName:        opts.sheetName,
		IncludeSensitive: opts.includeSensitive,
	}
	report := legacy_import.BuildAnomalyReport(rows, reportOptions)
	detailsPath, summaryPath, groupsPath, reviewerDir, err := legacy_import.WriteAnomalyReportCSV(opts.outputDir, reportOptions, report)
	if err != nil {
		return err
	}
	fmt.Printf("anomaly_report_details=%s anomaly_report_summary=%s anomaly_report_groups=%s reviewer_csv_dir=%s anomaly_rows=%d reason_codes=%d grouped_rows=%d\n", detailsPath, summaryPath, groupsPath, reviewerDir, len(report.Details), len(report.Summary), len(report.Groups))
	return nil
}

func sortedStates(counts map[string]int) []string {
	states := make([]string, 0, len(counts))
	for state := range counts {
		states = append(states, state)
	}
	sort.Strings(states)
	return states
}
