package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// A park added to the tenant reaches the contract families WITH its code (the key the sales and
// feed purchase farm pickers are compiled on), and the breed register reads every species.
func TestContractFamiliesCarryANewParksCodeAndEveryBreed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
INSERT INTO locations (tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, 'park', 'HSR', 'Hosur', 'active')`, adminUITestTenant); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	fams, err := NewRepository(pool, 5*time.Second).LoadContractFamilies(ctx, adminUITestTenant)
	if err != nil {
		t.Fatalf("LoadContractFamilies: %v", err)
	}
	found := false
	for _, p := range fams.Parks {
		if p.Code == "HSR" && p.Title == "Hosur" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the new park must be in the families with its code, got %+v", fams.Parks)
	}
	if len(fams.AllBreeds) < len(fams.Breeds) {
		t.Fatalf("every-species breeds (%d) must include at least the goat breeds (%d)", len(fams.AllBreeds), len(fams.Breeds))
	}
}
