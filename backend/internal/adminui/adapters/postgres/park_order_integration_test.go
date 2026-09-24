package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The console's park list -- the top-bar park picker and every park dropdown built from the
// bootstrap -- sorts by park CODE after display_order, CBE then CPT (maintainer decision
// 2026-09-16), never by name: by name Channapatna sorts before Coimbatore, so the picker listed
// CPT first. "PKB" here has the lower id AND the alphabetically first name; only the code rule
// puts "PKA" first.
func TestContractParksSortByCodeNotName(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ('00000000-0000-4000-8000-00000000a701'::uuid, $1::uuid, 'park', 'PKB', 'Alpha farm', 'active'),
       ('00000000-0000-4000-8000-00000000a702'::uuid, $1::uuid, 'park', 'PKA', 'Zulu farm', 'active')`,
		adminUITestTenant); err != nil {
		t.Fatalf("seed parks: %v", err)
	}

	families, err := NewRepository(pool, 5*time.Second).LoadContractFamilies(ctx, adminUITestTenant)
	if err != nil {
		t.Fatalf("LoadContractFamilies: %v", err)
	}
	var ours []string
	for _, park := range families.Parks {
		if park.Label == "PKA" || park.Label == "PKB" {
			ours = append(ours, park.Label)
		}
	}
	if len(ours) != 2 || ours[0] != "PKA" || ours[1] != "PKB" {
		t.Fatalf("parks = %v, want [PKA PKB]: by code, not by name or id", ours)
	}
}
