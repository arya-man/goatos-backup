package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// BUG-019: the park-scope chooser's option list is canonical Postgres `locations` data, so it needs a
// real-database proof, not a fake. Three things are load-bearing and all three are invisible to a
// stubbed repository: (1) a []string bound to a `$2::uuid[]` placeholder must actually encode as a
// uuid array; (2) an EMPTY narrowing list must mean "every active park" (tenant-wide actor), never
// "no parks"; (3) inactive parks and sheds must never appear as selectable parks.
func TestAuthorizedParkOptionsNarrowsByGrantAndExcludesInactiveAndNonPark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		parkA    = "71000000-0000-4000-8000-0000000000a1"
		parkB    = "71000000-0000-4000-8000-0000000000b1"
		parkGone = "71000000-0000-4000-8000-0000000000c1"
		shed     = "71000000-0000-4000-8000-0000000000d1"
		// Own tenant: the shared pgtest database already carries seeded CPT/CBE parks, and this
		// assertion is about the WHOLE returned vocabulary, not a subset of it.
		scopeTenant = "71000000-0000-4000-8000-0000000000f1"
	)
	execProjectionSQL(t, ctx, pool, "tenant", `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'T', 'active') ON CONFLICT DO NOTHING`, scopeTenant)
	execProjectionSQL(t, ctx, pool, "park B", // inserted first: proves ORDER BY name, not insertion order
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'CPT-T19', 'Channapatna', 'active')`, parkB, scopeTenant)
	execProjectionSQL(t, ctx, pool, "park A",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'CBE-T19', 'Coimbatore', 'active')`, parkA, scopeTenant)
	execProjectionSQL(t, ctx, pool, "retired park",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'OLD-T19', 'Retired Park', 'inactive')`, parkGone, scopeTenant)
	execProjectionSQL(t, ctx, pool, "shed under park B",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'S1-T19', 'Aardvark Shed', $3, 'active')`, shed, scopeTenant, parkB)

	repo := NewRepository(pool, 5*time.Second)

	// (2) empty narrowing = tenant-wide actor sees every ACTIVE park, ordered by name; the inactive
	// park and the shed are not selectable parks even though the shed sorts first alphabetically.
	all, err := repo.AuthorizedParkOptions(ctx, scopeTenant, nil)
	if err != nil {
		t.Fatalf("AuthorizedParkOptions(nil): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("tenant-wide actor must see both active parks and nothing else; got %+v", all)
	}
	if all[0].ParkID != parkB || all[0].Name != "Channapatna" || all[0].Code != "CPT-T19" {
		t.Fatalf("first option = %+v; want Channapatna (CPT-T19) first by name", all[0])
	}
	if all[1].ParkID != parkA || all[1].Name != "Coimbatore" {
		t.Fatalf("second option = %+v; want Coimbatore", all[1])
	}

	// (1)+(3) a park-scoped grant narrows the vocabulary to exactly that park.
	narrowed, err := repo.AuthorizedParkOptions(ctx, scopeTenant, []string{parkA})
	if err != nil {
		t.Fatalf("AuthorizedParkOptions([parkA]): %v", err)
	}
	if len(narrowed) != 1 || narrowed[0].ParkID != parkA {
		t.Fatalf("grant narrowing failed; got %+v", narrowed)
	}

	// A grant naming a park that is not active resolves to  selectable park, never to "all parks".
	none, err := repo.AuthorizedParkOptions(ctx, scopeTenant, []string{parkGone})
	if err != nil {
		t.Fatalf("AuthorizedParkOptions([retired]): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("an inactive park must not be selectable; got %+v", none)
	}
}
