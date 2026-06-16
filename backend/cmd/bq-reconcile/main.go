package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/legacy_import/bqreconcile"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	log := observability.New(observability.Config{Service: "bq-reconcile"})
	if err := run(os.Args[1:]); err != nil {
		log.Error("BQ reconciliation failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var tenantID string
	var importRunID string
	var eventsPath string
	var locationsPath string
	var backfillMissing bool
	var candidatesCSV string
	var reportDir string
	var execute bool
	var timeout time.Duration
	var traceID string
	fs := flag.NewFlagSet("bq-reconcile", flag.ContinueOnError)
	fs.StringVar(&tenantID, "tenant-id", "", "tenant UUID to reconcile")
	fs.StringVar(&importRunID, "import-run-id", "", "optional import run UUID for BQ-backed blank-gender staging fixes")
	fs.StringVar(&eventsPath, "events-json", "", "BQ event export JSON array or JSONL file")
	fs.StringVar(&locationsPath, "locations-json", "", "optional BQ latest-location export JSON array or JSONL file")
	fs.BoolVar(&backfillMissing, "backfill-missing", false, "blocked unless a deterministic per-goat current BQ identity export is added; event-history exports cannot safely create missing passports")
	fs.StringVar(&candidatesCSV, "backfill-candidates-csv", "", "deterministic safe old-tag passport backfill: explicit candidate CSV path. This is the ONLY path that creates old-tag-only passports; event-only exports never create goats.")
	fs.StringVar(&reportDir, "report-dir", "", "optional directory for backfilled/skipped CSV artifacts")
	fs.BoolVar(&execute, "execute", false, "apply planned updates; default is dry-run")
	fs.DurationVar(&timeout, "timeout", 10*time.Minute, "command timeout")
	fs.StringVar(&traceID, "trace-id", "", "optional audit trace id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(traceID) == "" {
		traceID = "bq-reconcile:" + time.Now().UTC().Format("20060102T150405Z")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cfg := platformpg.ConfigFromEnv()

	// Gate EVERY --execute path (event-history reconcile AND candidate backfill)
	// to a local/dev LOCAL database, BEFORE connecting. Both mutate `goats`, and
	// the only counter-refresh tool (rebuild-identity-counters) is itself
	// local/dev-only, so allowing stg/prod here would leave dashboard counters
	// stale with no committed rebuild path. The candidate backfill additionally
	// emits no goat_identity_events/outbox rows. Promotion beyond local/dev needs
	// a production-safe counter refresh (and event egress) first.
	if execute {
		cmdLabel := "bq-reconcile --execute"
		if strings.TrimSpace(candidatesCSV) != "" {
			cmdLabel = "bq-reconcile --backfill-candidates-csv --execute"
		}
		if err := localtarget.ValidateLocalDatabaseTarget(
			cmdLabel, os.Getenv("GOATOS_ENV"), cfg.DatabaseURL, "local", "dev",
		); err != nil {
			return err
		}
	}

	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	opts := bqreconcile.Options{
		TenantID:          tenantID,
		ImportRunID:       importRunID,
		EventsPath:        eventsPath,
		LocationsPath:     locationsPath,
		BackfillMissing:   backfillMissing,
		CandidatesCSVPath: candidatesCSV,
		ReportDir:         reportDir,
		Execute:           execute,
		TraceID:           traceID,
	}

	// Deterministic explicit old-tag passport backfill is a distinct, fail-closed
	// path (env/local-DB gated above). It is mutually exclusive with the
	// event-history reconcile path.
	if strings.TrimSpace(candidatesCSV) != "" {
		result, err := bqreconcile.RunCandidateBackfill(ctx, pool, opts)
		if err != nil {
			return err
		}
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if execute {
			fmt.Fprintln(os.Stderr, "Old-tag passport backfill applied (local/dev only). It bypasses the goat_identity_events stream, so run rebuild-identity-counters (NOT the incremental updater) before trusting dashboard counts.")
		} else {
			fmt.Fprintln(os.Stderr, "dry-run only; rerun with --execute (local/dev) to apply, then run rebuild-identity-counters.")
		}
		return nil
	}

	result, err := bqreconcile.Run(ctx, pool, opts)
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	if execute {
		fmt.Fprintln(os.Stderr, "BQ reconciliation applied. Rebuild identity counters before trusting dashboard counts.")
	} else {
		fmt.Fprintln(os.Stderr, "dry-run only; rerun with --execute to apply. Rebuild identity counters after execute.")
	}
	return nil
}
