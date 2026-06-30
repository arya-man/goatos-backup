// Command counts-projection-recompute runs the bounded Counts/Shifting projection
// recompute path used by Feed Direction gate G2. It is scheduler-facing and
// tenant/park/date scoped; it must not become a full-herd scan or a Feed
// generation shortcut.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	horizonBoth           = "both"
	horizonCountAsOf      = "count_as_of"
	horizonFeedTargetDate = "feed_target_date"
	defaultContract       = "counts-shifting-v1"
	defaultGeneratedBy    = "counts-projection-recompute"
)

type config struct {
	TenantID              string
	ParkID                string
	Horizon               string
	TargetDate            time.Time
	AsOf                  time.Time
	SourceContractVersion string
	GeneratedBy           string
	TraceID               string
	Timeout               time.Duration
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args, time.Now)
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

	service := countsapp.NewService(countspg.NewRepository(pool, pgCfg.QueryTimeout))
	for _, horizon := range horizons(cfg.Horizon) {
		req := countsdomain.ProjectionRecomputeRequest{
			TenantID:              cfg.TenantID,
			ParkID:                cfg.ParkID,
			Horizon:               horizon,
			TargetDate:            cfg.TargetDate,
			AsOf:                  cfg.AsOf,
			SourceContractVersion: cfg.SourceContractVersion,
			GeneratedBy:           cfg.GeneratedBy,
			TraceID:               ptrIfNotEmpty(cfg.TraceID),
		}
		if horizon == horizonCountAsOf && cfg.TargetDate.IsZero() {
			req.TargetDate = cfg.AsOf
		}
		result, err := service.RecomputeProjectionSnapshotWithResult(ctx, req)
		if err != nil {
			return fmt.Errorf("recompute %s: %w", horizon, err)
		}
		fmt.Printf("counts projection recomputed horizon=%s snapshot=%s status=%s rows=%d exceptions=%d target_date=%s as_of=%s\n",
			result.Horizon, result.SnapshotID, result.ProjectionStatus, result.RowCount, result.ExceptionCount,
			result.TargetDate.Format("2006-01-02"), result.AsOf.UTC().Format(time.RFC3339))
	}
	return nil
}

func parseFlags(args []string, now func() time.Time) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("counts-projection-recompute", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.ParkID, "park-id", getenv("GOATOS_COUNTS_PARK_ID"), "park/location id")
	fs.StringVar(&cfg.Horizon, "horizon", horizonBoth, "projection horizon: both, count_as_of, or feed_target_date")
	fs.StringVar(&cfg.SourceContractVersion, "source-contract-version", getenvDefault("GOATOS_COUNTS_SOURCE_CONTRACT_VERSION", defaultContract), "Counts/Shifting source contract version")
	fs.StringVar(&cfg.GeneratedBy, "generated-by", getenvDefault("GOATOS_COUNTS_PROJECTION_GENERATED_BY", defaultGeneratedBy), "projection generated_by value")
	fs.StringVar(&cfg.TraceID, "trace-id", getenv("GOATOS_TRACE_ID"), "optional trace id")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_COUNTS_PROJECTION_TIMEOUT", 120*time.Second), "worker timeout")
	targetDateRaw := fs.String("target-date", getenv("GOATOS_COUNTS_TARGET_DATE"), "target date as YYYY-MM-DD or RFC3339; required for feed_target_date")
	asOfRaw := fs.String("as-of", getenv("GOATOS_COUNTS_AS_OF"), "as-of instant as RFC3339; default now")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.ParkID = strings.TrimSpace(cfg.ParkID)
	cfg.Horizon = strings.TrimSpace(cfg.Horizon)
	cfg.SourceContractVersion = strings.TrimSpace(cfg.SourceContractVersion)
	cfg.GeneratedBy = strings.TrimSpace(cfg.GeneratedBy)
	cfg.TraceID = strings.TrimSpace(cfg.TraceID)
	if cfg.TenantID == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if cfg.ParkID == "" {
		return config{}, errors.New("park-id is required")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.SourceContractVersion == "" {
		return config{}, errors.New("source-contract-version is required")
	}
	if cfg.GeneratedBy == "" {
		return config{}, errors.New("generated-by is required")
	}
	if cfg.Horizon != horizonBoth && cfg.Horizon != horizonCountAsOf && cfg.Horizon != horizonFeedTargetDate {
		return config{}, errors.New("horizon must be both, count_as_of, or feed_target_date")
	}
	if now == nil {
		now = time.Now
	}
	cfg.AsOf = now().UTC()
	if strings.TrimSpace(*asOfRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*asOfRaw))
		if err != nil {
			return config{}, errors.New("as-of must be RFC3339")
		}
		cfg.AsOf = parsed.UTC()
	}
	if strings.TrimSpace(*targetDateRaw) != "" {
		parsed, err := parseDateOrInstant(strings.TrimSpace(*targetDateRaw))
		if err != nil {
			return config{}, err
		}
		cfg.TargetDate = dateOnly(parsed)
	}
	if (cfg.Horizon == horizonBoth || cfg.Horizon == horizonFeedTargetDate) && cfg.TargetDate.IsZero() {
		return config{}, errors.New("target-date is required for feed_target_date")
	}
	return cfg, nil
}

func horizons(horizon string) []string {
	if horizon == horizonBoth {
		return []string{horizonCountAsOf, horizonFeedTargetDate}
	}
	return []string{horizon}
}

func parseDateOrInstant(raw string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, errors.New("target-date must be YYYY-MM-DD or RFC3339")
	}
	return t.UTC(), nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
}

func ptrIfNotEmpty(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func getenvDefault(key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	raw := getenv(name)
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
