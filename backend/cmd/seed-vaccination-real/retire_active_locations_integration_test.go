package main

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestRetireNonSourceLocationsSkipsParkWithActiveChildSheds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := detUUID("tenant", "retire-active-child-test")
	parkID := detUUID("park", tenantID, "RetireActiveChildPark")
	shedID := detUUID("shed", tenantID, "RetireActiveChildPark", "RetireActiveChildShed")

	mustExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'retire-active-child-test','active') ON CONFLICT (tenant_id) DO NOTHING`, tenantID)
	mustExec(t, ctx, pool, `
		INSERT INTO locations (location_id, tenant_id, location_type, location_code, name,
			country, state_region, timezone, status, updated_at)
		VALUES ($1,$2,'park','PACT','Active Child Park', 'IN','Tamil Nadu','Asia/Kolkata','active',now())
		ON CONFLICT (tenant_id, location_code) DO UPDATE SET status='active', updated_at=now()`, parkID, tenantID)
	mustExec(t, ctx, pool, `
		INSERT INTO locations (location_id, tenant_id, location_type, location_code, name,
			parent_location_id, country, state_region, timezone, status, updated_at)
		VALUES ($1,$2,'shed','PACT-SHED','Retire Active Shed',$3,'IN','Tamil Nadu','Asia/Kolkata','active',now())
		ON CONFLICT (tenant_id, location_code) DO UPDATE SET status='active', updated_at=now()`, shedID, tenantID, parkID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	retired, err := retireActiveNonSourceLocations(ctx, tx, tenantID, []string{"OTHER"}, nil)
	if err != nil {
		t.Fatalf("retire non-source: %v", err)
	}
	if retired != 1 {
		t.Fatalf("retired = %d, want 1 (child shed retired, park skipped)", retired)
	}

	var parkStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM locations WHERE tenant_id=$1 AND location_id=$2`, tenantID, parkID).Scan(&parkStatus); err != nil {
		t.Fatalf("query park status: %v", err)
	}
	if parkStatus != "active" {
		t.Fatalf("park status = %q, want active", parkStatus)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
