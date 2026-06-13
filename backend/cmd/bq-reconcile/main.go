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
	var eventsPath string
	var locationsPath string
	var execute bool
	var timeout time.Duration
	var traceID string
	fs := flag.NewFlagSet("bq-reconcile", flag.ContinueOnError)
	fs.StringVar(&tenantID, "tenant-id", "", "tenant UUID to reconcile")
	fs.StringVar(&eventsPath, "events-json", "", "BQ event export JSON array or JSONL file")
	fs.StringVar(&locationsPath, "locations-json", "", "optional BQ latest-location export JSON array or JSONL file")
	fs.BoolVar(&execute, "execute", false, "apply planned updates; default is dry-run")
	fs.DurationVar(&timeout, "timeout", 10*time.Minute, "command timeout")
	fs.StringVar(&traceID, "trace-id", "", "optional audit trace id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if execute {
		if err := validateExecuteEnv(os.Getenv("GOATOS_ENV")); err != nil {
			return err
		}
	}
	if strings.TrimSpace(traceID) == "" {
		traceID = "bq-reconcile:" + time.Now().UTC().Format("20060102T150405Z")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	result, err := bqreconcile.Run(ctx, pool, bqreconcile.Options{
		TenantID:      tenantID,
		EventsPath:    eventsPath,
		LocationsPath: locationsPath,
		Execute:       execute,
		TraceID:       traceID,
	})
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

func validateExecuteEnv(env string) error {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "local", "dev", "stg", "stage", "prod", "production":
		return nil
	default:
		return fmt.Errorf("GOATOS_ENV must be set to local/dev/stg/prod before --execute; got %q", env)
	}
}
