package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The phone's Add-birth breed list honours the Configuration register (audit 2026-09-26): a breed
// the farm archived is not offered even while older animals still carry it, a carried breed the
// register has never heard of is still offered, and a name the farm keeps under two species is two
// options so the phone's species filter picks the right one.
func TestBirthBreedListHonoursArchivedBreedsAndSpecies(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedCountsScope(t, ctx, pool)
	seedCustodianParty(t, ctx, pool)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	// Live herd: an archived breed (Jamunapari), a legacy text breed (Local) and a breed kept under
	// both species (Deccani).
	exec(`INSERT INTO goats (goat_id, tenant_id, species, breed, sex, lifecycle_status, custodian_party_id, park_id, shed_id)
VALUES ('00000000-0000-4000-8000-00000000b001', $1::uuid, 'goat', 'Jamunapari', 'female', 'alive', $3::uuid, $2::uuid, $4::uuid),
       ('00000000-0000-4000-8000-00000000b002', $1::uuid, 'goat', 'Local', 'female', 'alive', $3::uuid, $2::uuid, $4::uuid),
       ('00000000-0000-4000-8000-00000000b003', $1::uuid, 'sheep', 'Deccani', 'male', 'alive', $3::uuid, $2::uuid, $4::uuid)`,
		countsTenant, countsPark, countsCustodian, countsShedA)
	exec(`INSERT INTO breeds (tenant_id, species, canonical_name, status) VALUES
($1::uuid, 'goat', 'Jamunapari', 'inactive'),
($1::uuid, 'goat', 'Boer', 'active'),
($1::uuid, 'goat', 'Deccani', 'active'),
($1::uuid, 'sheep', 'Deccani', 'active'),
($1::uuid, 'goat', 'Retired unused', 'inactive')`, countsTenant)

	got, err := NewRepository(pool, 10*time.Second).ActiveBreeds(ctx, countsTenant)
	if err != nil {
		t.Fatalf("active breeds: %v", err)
	}
	offered := map[string]bool{}
	for _, b := range got {
		offered[b.Species+"/"+b.Label] = true
	}
	for _, want := range []string{"goat/Boer", "goat/Local", "goat/Deccani", "sheep/Deccani"} {
		if !offered[want] {
			t.Errorf("%s must be offered, got %v", want, offered)
		}
	}
	for _, never := range []string{"goat/Jamunapari", "goat/Retired unused"} {
		if offered[never] {
			t.Errorf("%s is archived on Configuration and must not be offered, got %v", never, offered)
		}
	}
	if len(got) != 4 {
		t.Errorf("want exactly 4 options, got %d: %v", len(got), got)
	}
}
