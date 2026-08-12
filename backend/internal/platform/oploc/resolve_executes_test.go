package oploc_test

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestShedScopedLocationSQLActuallyExecutes runs the canonical query against a real Postgres.
//
// WHY THIS EXISTS: resolve_test.go covers ResolveShedLocation with a FAKE row scanner and asserts
// the query TEXT contains the right column names. Both passed while the SQL itself was invalid --
// it selected sp.partition_label under a bare HAVING with no aggregate, which Postgres rejects
// with 42803. A canonical SQL constant that is never executed is not verified; it is a string.
func TestShedScopedLocationSQLActuallyExecutes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	var tenantID string
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), 'oploc exec', 'active') RETURNING tenant_id::text`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	var parkID string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_type, name, status)
VALUES ($1::uuid, 'park', 'Exec Park', 'active') RETURNING location_id::text`, tenantID).Scan(&parkID); err != nil {
		t.Fatalf("seed park: %v", err)
	}

	seedShed := func(name string) string {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, 'shed', $2, $3::uuid, 'active') RETURNING location_id::text`, tenantID, name, parkID).Scan(&id); err != nil {
			t.Fatalf("seed shed %s: %v", name, err)
		}
		return id
	}
	addPartition := func(shedID, label, normalized string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active', 'manual')`, tenantID, shedID, label, normalized); err != nil {
			t.Fatalf("seed partition %s: %v", label, err)
		}
	}

	onePartition := seedShed("Godel 1")
	addPartition(onePartition, "Part 3", "3")

	twoPartitions := seedShed("Mandela 2")
	addPartition(twoPartitions, "Part 1", "1")
	addPartition(twoPartitions, "Part 2", "2")

	noPartition := seedShed("Yashoda 2")

	for _, tc := range []struct {
		name, shedID, want string
	}{
		{"single compatibility partition does not change shed name", onePartition, "Godel 1"},
		// AGREE-OR-GO-BARE: several partitions is ambiguous at shed grain, so it goes bare.
		{"several partitions go bare", twoPartitions, "Mandela 2"},
		// An undivided shed whose NAME ends in a number stays whole.
		{"undivided shed renders whole", noPartition, "Yashoda 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc, err := oploc.ResolveShedLocation(ctx, pool.QueryRow(ctx, oploc.ShedScopedLocationSQL, tenantID, tc.shedID))
			if err != nil {
				t.Fatalf("canonical SQL failed to execute: %v", err)
			}
			if got := loc.Display(); got != tc.want {
				t.Fatalf("Display() = %q, want %q", got, tc.want)
			}
		})
	}
}
