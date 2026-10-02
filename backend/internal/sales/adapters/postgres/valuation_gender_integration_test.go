package postgres

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"testing"
	"time"
)

func TestMotherValuationUsesFemaleRateDespiteRecordedMaleSex(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	var party string
	if err := pool.QueryRow(ctx, `INSERT INTO parties (party_type, display_name, status) VALUES ('org', 'Valuation test', 'active') RETURNING party_id::text`).Scan(&party); err != nil {
		t.Fatal(err)
	}
	const tenant = "fd100000-0000-4000-8000-000000000099"
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id,name,status) VALUES ($1,'Valuation regression','active')`, tenant); err != nil {
		t.Fatal(err)
	}
	// Species is part of the bucket (2026-10-02): a goat Mother, a goat Buck and a sheep Buck. The
	// register only admits goat or sheep (goats_species_check), so every live animal has one.
	if _, err := pool.Exec(ctx, `INSERT INTO goats (tenant_id,species,sex,lifecycle_status,management_stage,custodian_party_id) VALUES
		($1,'goat','male','alive','Mother',$2), ($1,'goat','male','alive','Buck',$2), ($1,'sheep','male','alive','Buck',$2)`, tenant, party); err != nil {
		t.Fatal(err)
	}
	// #389 folded the valuation read into GetOverview's single pgx.Batch; read it back through it.
	overview, err := NewRepository(pool, 10*time.Second).GetOverview(ctx, tenant, "")
	if err != nil {
		t.Fatal(err)
	}
	got := overview.FarmValuation
	for _, key := range []string{"adult_goat_female", "adult_goat_male", "adult_sheep_male"} {
		found := false
		for _, bucket := range got.Buckets {
			if bucket.Bucket != key {
				continue
			}
			found = true
			want := 24000.0
			if key != "adult_goat_female" {
				want = 30000
			}
			if bucket.AnimalCount != 1 || bucket.ValueRupees != want {
				t.Fatalf("%s: %+v", key, bucket)
			}
		}
		if !found {
			t.Fatalf("missing %s", key)
		}
	}
	if got.ExcludedAnimals != 0 || got.ValuedAnimals != 3 {
		t.Fatalf("every goat and sheep must be valued: valued=%d excluded=%d %+v", got.ValuedAnimals, got.ExcludedAnimals, got.NotValued)
	}
}
