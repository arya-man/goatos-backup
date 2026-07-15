package main

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/seedrun"
)

const testTenant = "00000000-0000-4000-8000-000000000001"

// TestSeedStateQueriesReflectSeedRunTable proves the DB read helpers + the closeout/promotion gate
// behave against a real seed_runs table: a failed latest run is rejected by both boundaries, a
// verified run passes promotion, and a legacy DB with no runs passes closeout but not promotion.
func TestSeedStateQueriesReflectSeedRunTable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	assertGate := func(name, mode string, wantReject bool) {
		t.Helper()
		latest, hasRun, err := latestSeedRunState(ctx, pool, testTenant)
		if err != nil {
			t.Fatalf("%s: latest state: %v", name, err)
		}
		verified, err := hasVerifiedRun(ctx, pool, testTenant)
		if err != nil {
			t.Fatalf("%s: has verified: %v", name, err)
		}
		got := seedrun.EvaluateGate(mode, hasRun, latest, verified)
		if (got != nil) != wantReject {
			t.Fatalf("%s (mode=%s): reject=%v want=%v (latest=%q hasRun=%v verified=%v err=%v)",
				name, mode, got != nil, wantReject, latest, hasRun, verified, got)
		}
	}

	insertRun := func(id, state string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO seed_runs (seed_run_id, tenant_id, command, state) VALUES ($1,$2,'seed-vaccination-real',$3)`,
			id, testTenant, state); err != nil {
			t.Fatalf("insert run %s: %v", state, err)
		}
	}

	// No runs yet: legacy DB.
	assertGate("legacy closeout", seedrun.ModeCloseout, false)
	assertGate("legacy promotion", seedrun.ModePromotion, true)

	// A verified run: promotable.
	insertRun("11111111-0000-4000-8000-000000000001", seedrun.StateVerified)
	assertGate("verified closeout", seedrun.ModeCloseout, false)
	assertGate("verified promotion", seedrun.ModePromotion, false)

	// A later failed run becomes the latest: RESET_REQUIRED, rejected everywhere.
	if _, err := pool.Exec(ctx,
		`INSERT INTO seed_runs (seed_run_id, tenant_id, command, state, updated_at)
		 VALUES ($1,$2,'seed-vaccination-real',$3, now() + interval '1 minute')`,
		"22222222-0000-4000-8000-000000000002", testTenant, seedrun.StateFailed); err != nil {
		t.Fatalf("insert failed run: %v", err)
	}
	assertGate("failed closeout", seedrun.ModeCloseout, true)
	assertGate("failed promotion", seedrun.ModePromotion, true)
}
