package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestParseFlags(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 6, 29, 8, 0, 0, 0, time.UTC) }
	cfg, err := parseFlags([]string{
		"-months-ahead", "18",
		"-timeout", "2m",
		"-as-of", "2026-12-15T00:00:00Z",
	}, now)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.MonthsAhead != 18 || cfg.Timeout != 2*time.Minute {
		t.Fatalf("cfg = %#v", cfg)
	}
	wantAsOf := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	if !cfg.AsOf.Equal(wantAsOf) {
		t.Fatalf("AsOf = %s, want %s", cfg.AsOf, wantAsOf)
	}
}

func TestParseFlagsRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "coverage too low", args: []string{"-months-ahead", "11"}, want: "months-ahead"},
		{name: "coverage too high", args: []string{"-months-ahead", "61"}, want: "months-ahead"},
		{name: "bad timeout", args: []string{"-timeout", "0s"}, want: "timeout"},
		{name: "bad as-of", args: []string{"-as-of", "today"}, want: "as-of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseFlags(tt.args, func() time.Time { return time.Date(2026, 6, 29, 8, 0, 0, 0, time.UTC) })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestEnsureCoverageCreatesFuturePartitions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	cfg := config{
		MonthsAhead: 12,
		Timeout:     time.Minute,
		AsOf:        time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC),
	}
	results, err := ensureCoverage(ctx, pool, cfg)
	if err != nil {
		t.Fatalf("ensureCoverage: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}

	for _, table := range []string{
		"public.goat_identity_events_2027_12",
		"public.audit_log_2027_12",
		"public.obligation_status_events_2027_12",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("%s was not created", table)
		}
	}

	second, err := ensureCoverage(ctx, pool, cfg)
	if err != nil {
		t.Fatalf("ensureCoverage second run: %v", err)
	}
	for _, result := range second {
		if result.CreatedPartitions != 0 {
			t.Fatalf("second run created %d partitions for %s, want 0", result.CreatedPartitions, result.ParentTable)
		}
	}
}
