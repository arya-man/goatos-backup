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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

type config struct {
	MonthsAhead int
	Timeout     time.Duration
	AsOf        time.Time
}

type coverageResult struct {
	ParentTable       string
	CreatedPartitions int
	CoverageThrough   time.Time
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

	results, err := ensureCoverage(ctx, pool, cfg)
	if err != nil {
		return err
	}

	total := 0
	for _, result := range results {
		total += result.CreatedPartitions
		fmt.Printf(
			"partition coverage parent=%s created=%d through=%s\n",
			result.ParentTable,
			result.CreatedPartitions,
			biztime.BusinessDate(result.CoverageThrough),
		)
	}
	fmt.Printf("partition coverage OK created=%d months_ahead=%d as_of=%s\n", total, cfg.MonthsAhead, cfg.AsOf.In(biztime.DefaultLocation()).Format(time.RFC3339))
	return nil
}

func parseFlags(args []string, now func() time.Time) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("partition-maintainer", flag.ContinueOnError)
	fs.IntVar(&cfg.MonthsAhead, "months-ahead", intEnv("GOATOS_PARTITION_MONTHS_AHEAD", 12), "minimum monthly partition coverage ahead of as-of; must be at least 12")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_PARTITION_MAINTAINER_TIMEOUT", 60*time.Second), "partition maintenance timeout")
	asOfRaw := fs.String("as-of", getenv("GOATOS_PARTITION_MAINTAINER_AS_OF"), "RFC3339 anchor time; default now")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.MonthsAhead < 12 || cfg.MonthsAhead > 60 {
		return config{}, errors.New("months-ahead must be between 12 and 60")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if now == nil {
		now = time.Now
	}
	cfg.AsOf = now().In(biztime.DefaultLocation())
	if strings.TrimSpace(*asOfRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*asOfRaw))
		if err != nil {
			return config{}, errors.New("as-of must be RFC3339")
		}
		cfg.AsOf = parsed.In(biztime.DefaultLocation())
	}
	return cfg, nil
}

func ensureCoverage(ctx context.Context, pool *pgxpool.Pool, cfg config) ([]coverageResult, error) {
	rows, err := pool.Query(ctx, `
SELECT parent_table, created_partitions, coverage_through
FROM goatos_ensure_partition_coverage($1::timestamptz, $2::int)
ORDER BY parent_table`, cfg.AsOf, cfg.MonthsAhead)
	if err != nil {
		return nil, fmt.Errorf("ensure partition coverage: %w", err)
	}
	defer rows.Close()

	results := []coverageResult{}
	for rows.Next() {
		var result coverageResult
		if err := rows.Scan(&result.ParentTable, &result.CreatedPartitions, &result.CoverageThrough); err != nil {
			return nil, fmt.Errorf("scan partition coverage: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read partition coverage: %w", err)
	}
	if len(results) == 0 {
		return nil, errors.New("partition coverage returned no parent tables")
	}
	return results, nil
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func intEnv(key string, fallback int) int {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
