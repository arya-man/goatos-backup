package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestCommandBoardFirstPaintHoldsOneConnectionPerEndpoint pins the first-paint connection shape
// (docs/perf/2026-09-24-stg-latency/audit/part-vaccination.md root 3).
//
// The board used to fan its six sections out over six pooled connections, and the cohort matrix
// its two statements over two, all drawing on one shared 6-slot semaphore. Admin-web fires the
// board, cohort matrix, shed-dose grid and drive picker together, so first paint queued ~10
// statements on 6 slots per instance and a 10-connection pool: stg p50 1.8 s against ~220 ms of
// clone DB time. Each endpoint now sends its statements as ONE pgx.Batch on ONE connection, so a
// cold first paint holds at most four connections and never waits on itself.
func TestCommandBoardFirstPaintHoldsOneConnectionPerEndpoint(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000b7"
	parkID := "70000000-0000-4000-8000-0000010000b7"
	shedID := "70000000-0000-4000-8000-0000020000b7"
	protocolID := "70000000-0000-4000-8000-0000060000b0"
	protocolVersionID := "70000000-0000-4000-8000-0000060000b7"
	ruleID := "70000000-0000-4000-8000-0000070000b7"
	partyID := "70000000-0000-4000-8000-00000a0000b7"
	goatID := "70000000-0000-4000-8000-0000030000b1"
	seedKPIFixtureBase(t, ctx, pool, tenantID, parkID, shedID, protocolID, protocolVersionID, ruleID, partyID, "b7")
	seedKPIGoat(t, ctx, pool, tenantID, shedID, partyID, goatID)
	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	execProjectionSQL(t, ctx, pool, "scheduled obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ('70000000-0000-4000-8000-0000080000b1', $1, $2, $3, 'goat', 'shed', $4, $5, 'scheduled', $6::timestamptz, 'batch-b1')`,
		tenantID, protocolVersionID, goatID, shedID, ruleID, asOf.Add(3*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	acquires := func(label string, fn func() error) {
		t.Helper()
		before := pool.Stat().AcquireCount()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if got := pool.Stat().AcquireCount() - before; got != 1 {
			t.Fatalf("%s acquired %d pool connections on a cold read, want exactly 1 (one pgx.Batch per endpoint)", label, got)
		}
	}
	acquires("command board", func() error {
		resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
		if err == nil && resp.KPIs.Targets != 1 {
			t.Fatalf("targets = %d, want 1", resp.KPIs.Targets)
		}
		if err == nil && len(resp.UnavailableSections) != 0 {
			t.Fatalf("unavailable sections %v, want none", resp.UnavailableSections)
		}
		return err
	})
	acquires("cohort matrix", func() error {
		_, err := repo.CommandBoardCohortMatrix(ctx, domain.CommandBoardDrilldownQuery{TenantID: tenantID, AsOf: asOf})
		return err
	})
}
