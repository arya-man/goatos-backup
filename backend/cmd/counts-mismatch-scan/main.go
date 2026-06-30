// Command counts-mismatch-scan runs the bounded Counts/Shifting mismatch scan
// used by Feed Direction gate G2 to catch stale imported or historical Base
// Count anchors that were not checked at write time.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	defaultLimit = int32(100)
	maxLimit     = int32(500)
)

type config struct {
	TenantID        string
	ParkID          string
	ShedID          string
	CountedAfter    *time.Time
	CountedBefore   time.Time
	CursorCountedAt *time.Time
	CursorAnchorID  string
	Limit           int32
	Timeout         time.Duration
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
	result, err := service.ScanCountMismatches(ctx, countsdomain.CountMismatchScanRequest{
		TenantID:        cfg.TenantID,
		ParkID:          ptrIfNotEmpty(cfg.ParkID),
		ShedID:          ptrIfNotEmpty(cfg.ShedID),
		CountedAfter:    cfg.CountedAfter,
		CountedBefore:   cfg.CountedBefore,
		CursorCountedAt: cfg.CursorCountedAt,
		CursorAnchorID:  ptrIfNotEmpty(cfg.CursorAnchorID),
		Limit:           cfg.Limit,
	})
	if err != nil {
		return err
	}
	completedAt := ""
	if result.CompletedAt != nil {
		completedAt = result.CompletedAt.UTC().Format(time.RFC3339)
	}
	fmt.Printf("counts mismatch scan run=%s tenant=%s status=%s scanned=%d exception_writes=%d investigating=%d counted_before=%s completed_at=%s limit=%d\n",
		result.RunID, result.TenantID, result.Status, result.ScannedAnchorCount, result.ExceptionWriteCount,
		result.InvestigatingAnchorCount, cfg.CountedBefore.UTC().Format(time.RFC3339), completedAt, cfg.Limit)
	if result.NextCursor != nil {
		fmt.Printf("next_cursor_counted_at=%s next_cursor_anchor_id=%s\n",
			result.NextCursor.CountedAt.UTC().Format(time.RFC3339Nano), result.NextCursor.BaseCountAnchorID)
	}
	return nil
}

func parseFlags(args []string, now func() time.Time) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("counts-mismatch-scan", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.ParkID, "park-id", getenv("GOATOS_COUNTS_PARK_ID"), "optional park/location id")
	fs.StringVar(&cfg.ShedID, "shed-id", getenv("GOATOS_COUNTS_SHED_ID"), "optional shed/location id")
	fs.StringVar(&cfg.CursorAnchorID, "cursor-anchor-id", getenv("GOATOS_COUNTS_MISMATCH_CURSOR_ANCHOR_ID"), "optional cursor base_count_anchor_id")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_COUNTS_MISMATCH_SCAN_TIMEOUT", 120*time.Second), "worker timeout")
	limitRaw := fs.String("limit", getenvDefault("GOATOS_COUNTS_MISMATCH_SCAN_LIMIT", strconv.Itoa(int(defaultLimit))), "page limit, max 500")
	countedAfterRaw := fs.String("counted-after", getenv("GOATOS_COUNTS_MISMATCH_COUNTED_AFTER"), "optional counted_at lower bound, YYYY-MM-DD or RFC3339")
	countedBeforeRaw := fs.String("counted-before", getenv("GOATOS_COUNTS_MISMATCH_COUNTED_BEFORE"), "counted_at upper bound, YYYY-MM-DD or RFC3339; default now")
	cursorCountedAtRaw := fs.String("cursor-counted-at", getenv("GOATOS_COUNTS_MISMATCH_CURSOR_COUNTED_AT"), "optional cursor counted_at RFC3339")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.ParkID = strings.TrimSpace(cfg.ParkID)
	cfg.ShedID = strings.TrimSpace(cfg.ShedID)
	cfg.CursorAnchorID = strings.TrimSpace(cfg.CursorAnchorID)
	if cfg.TenantID == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	limit, err := strconv.Atoi(strings.TrimSpace(*limitRaw))
	if err != nil || limit <= 0 || limit > int(maxLimit) {
		return config{}, errors.New("limit must be between 1 and 500")
	}
	cfg.Limit = int32(limit)
	if now == nil {
		now = time.Now
	}
	cfg.CountedBefore = now().UTC()
	if strings.TrimSpace(*countedBeforeRaw) != "" {
		parsed, err := parseDateOrInstant(strings.TrimSpace(*countedBeforeRaw))
		if err != nil {
			return config{}, fmt.Errorf("counted-before: %w", err)
		}
		cfg.CountedBefore = parsed.UTC()
	}
	if strings.TrimSpace(*countedAfterRaw) != "" {
		parsed, err := parseDateOrInstant(strings.TrimSpace(*countedAfterRaw))
		if err != nil {
			return config{}, fmt.Errorf("counted-after: %w", err)
		}
		cfg.CountedAfter = &parsed
	}
	if strings.TrimSpace(*cursorCountedAtRaw) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*cursorCountedAtRaw))
		if err != nil {
			return config{}, errors.New("cursor-counted-at must be RFC3339")
		}
		cfg.CursorCountedAt = &parsed
	}
	if (cfg.CursorCountedAt == nil) != (cfg.CursorAnchorID == "") {
		return config{}, errors.New("cursor-counted-at and cursor-anchor-id must be provided together")
	}
	return cfg, nil
}

func parseDateOrInstant(raw string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("must be YYYY-MM-DD or RFC3339")
	}
	return t.UTC(), nil
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
