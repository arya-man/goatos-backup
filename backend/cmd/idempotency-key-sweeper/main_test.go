package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const testTenantID = "00000000-0000-4000-8000-000000000001"

func TestParseFlags(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC) }
	cfg, err := parseFlags([]string{
		"-tenant-id", testTenantID,
		"-limit", "250",
		"-before", "2026-06-27T00:00:00Z",
		"-dry-run",
	}, now)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.TenantID != testTenantID || cfg.Limit != 250 || !cfg.DryRun {
		t.Fatalf("cfg = %#v", cfg)
	}
	wantBefore := time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)
	if !cfg.Before.Equal(wantBefore) {
		t.Fatalf("Before = %s, want %s", cfg.Before, wantBefore)
	}
}

func TestParseFlagsRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "limit too low", args: []string{"-limit", "0"}, want: "limit"},
		{name: "limit too high", args: []string{"-limit", "5001"}, want: "limit"},
		{name: "bad before", args: []string{"-before", "today"}, want: "before"},
		{name: "bad tenant", args: []string{"-tenant-id", "not-a-uuid"}, want: "tenant-id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseFlags(tt.args, func() time.Time { return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC) })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestSweepExpiredDeletesOnlyExpiredRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	before := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	rows := []struct {
		key       string
		expiresAt any
	}{
		{key: "expired", expiresAt: before.Add(-time.Hour)},
		{key: "future", expiresAt: before.Add(time.Hour)},
		{key: "no-expiry", expiresAt: nil},
	}
	for _, row := range rows {
		if _, err := pool.Exec(ctx, `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status, expires_at)
VALUES ($1, $2::uuid, 'test', $3, 'started', $4::timestamptz)`, row.key, testTenantID, "hash:"+row.key, row.expiresAt); err != nil {
			t.Fatalf("insert %s: %v", row.key, err)
		}
	}

	dryRunCount, err := sweepExpired(ctx, pool, config{TenantID: testTenantID, Limit: 10, Before: before, DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dryRunCount != 1 {
		t.Fatalf("dry run count = %d, want 1", dryRunCount)
	}
	if got := countKeys(t, ctx, pool); got != 3 {
		t.Fatalf("dry run deleted rows, count = %d", got)
	}

	deleted, err := sweepExpired(ctx, pool, config{TenantID: testTenantID, Limit: 10, Before: before})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if got := countKeys(t, ctx, pool); got != 2 {
		t.Fatalf("remaining keys = %d, want 2", got)
	}
	for _, key := range []string{"future", "no-expiry"} {
		if got := countKey(t, ctx, pool, key); got != 1 {
			t.Fatalf("key %s count = %d, want 1", key, got)
		}
	}
}

func countKeys(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM idempotency_keys`).Scan(&count); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	return count
}

func countKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM idempotency_keys WHERE idempotency_key = $1`, key).Scan(&count); err != nil {
		t.Fatalf("count key %s: %v", key, err)
	}
	return count
}
