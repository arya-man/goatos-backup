package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Proofs for the operator-facing single-animal shifting reads, run against the REAL schema.
//
// These exist because the fake-backed handler/service tests cannot see SQL. A fake repository
// returns whatever a test stocked it with, so it will happily "prove" a grouping the query does not
// actually produce, a column that does not exist, or a predicate that silently drops rows -- the
// same class of blind spot documented at the top of approval_relocate_integration_test.go, where a
// fake passed while production returned 500 on every shifting approval. Everything asserted below
// is asserted through the query the handler really runs.

const (
	// A SECOND park, so the repeated-shed-name case is reproducible. The fixture park (countsPark)
	// plus this one mirror the live two-park topology.
	destSecondPark = "00000000-0000-4000-8000-000000003002"
	// Two sheds that DELIBERATELY share the name "Castro 1" while sitting under different parks --
	// the exact live condition that makes a flat shed list ambiguous.
	destCastroCPT = "00000000-0000-4000-8000-000000004101"
	destCastroCBE = "00000000-0000-4000-8000-000000004102"
	// An inactive shed and a retired park, to prove the filters actually filter.
	destInactiveShed = "00000000-0000-4000-8000-000000004103"
	destRetiredPark  = "00000000-0000-4000-8000-000000003003"
	// A park with no sheds at all, to prove it still appears (LEFT JOIN, not INNER).
	destEmptyPark = "00000000-0000-4000-8000-000000003004"
)

func seedDestinationTopology(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	// retired_at is set in the INSERT rather than by a follow-up UPDATE: the schema's
	// locations_seeded_scope_guard_update_trg blocks UPDATEs of location_type/status/retired_at on
	// the seeded CBE/CPT/HF scope ("requires approved migration plan"), which is itself a useful
	// signal that retirement is a governed operation rather than an ad-hoc column write.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, retired_at)
VALUES
  ($2::uuid, $1::uuid, 'park', 'CBE',      'Coimbatore',   'active',   NULL),
  ($3::uuid, $1::uuid, 'park', 'EMPTY',    'Empty Park',   'active',   NULL),
  ($4::uuid, $1::uuid, 'park', 'RETIRED',  'Retired Park', 'inactive', now())
ON CONFLICT (location_id) DO NOTHING`,
		countsTenant, destSecondPark, destEmptyPark, destRetiredPark); err != nil {
		t.Fatalf("seed destination parks: %v", err)
	}
	// Two sheds sharing the name "Castro 1" under DIFFERENT parks, plus an inactive shed under the
	// fixture park.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($3::uuid, $1::uuid, 'shed', 'CPT-CASTRO1', 'Castro 1',      $2::uuid, 'active'),
  ($5::uuid, $1::uuid, 'shed', 'CBE-CASTRO1', 'Castro 1',      $4::uuid, 'active'),
  ($6::uuid, $1::uuid, 'shed', 'CPT-OLD',     'Retired Shed',  $2::uuid, 'inactive')
ON CONFLICT (location_id) DO NOTHING`,
		countsTenant, countsPark, destCastroCPT, destSecondPark, destCastroCBE, destInactiveShed); err != nil {
		t.Fatalf("seed destination sheds: %v", err)
	}
}

// TestShiftingDestinationCatalogGroupsRepeatedShedNamesByPark is the disambiguation proof against
// real SQL.
//
// Two sheds genuinely named "Castro 1" exist under different parks. The catalog must place each
// under its OWN park and keep their ids distinct, because the name alone cannot tell an operator
// (or a bug report) which shed an animal went to.
func TestShiftingDestinationCatalogGroupsRepeatedShedNamesByPark(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedDestinationTopology(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	catalog, err := repo.ShiftingDestinationCatalog(ctx, countsTenant)
	if err != nil {
		t.Fatalf("ShiftingDestinationCatalog: %v", err)
	}
	if len(catalog.ManagementStages) == 0 {
		t.Fatal("active management-stage vocabulary must be returned with the destination catalog")
	}
	for _, stage := range catalog.ManagementStages {
		if stage == "ICU" || stage == "Quarantine" {
			t.Fatalf("clinical stage %q must not be offered by a movement form", stage)
		}
	}

	byID := map[string]struct {
		name  string
		sheds map[string]string // shed id -> name
	}{}
	names := make([]string, 0, len(catalog.Parks))
	for _, park := range catalog.Parks {
		sheds := map[string]string{}
		for _, shed := range park.Sheds {
			sheds[shed.ShedID] = shed.Name
		}
		byID[park.ParkID] = struct {
			name  string
			sheds map[string]string
		}{name: park.Name, sheds: sheds}
		names = append(names, park.Name)
	}

	// Both Castro 1 rows must be present, each under its own park.
	cpt, ok := byID[countsPark]
	if !ok {
		t.Fatalf("fixture park %s missing from catalog", countsPark)
	}
	cbe, ok := byID[destSecondPark]
	if !ok {
		t.Fatalf("Coimbatore missing from catalog")
	}
	if cpt.sheds[destCastroCPT] != "Castro 1" {
		t.Fatalf("CPT Castro 1 = %q, want it under park %s", cpt.sheds[destCastroCPT], countsPark)
	}
	if cbe.sheds[destCastroCBE] != "Castro 1" {
		t.Fatalf("CBE Castro 1 = %q, want it under park %s", cbe.sheds[destCastroCBE], destSecondPark)
	}
	// The cross-wiring check: neither park may contain the OTHER park's shed. A join on name, or a
	// parent predicate dropped from the ON clause, would surface exactly here.
	if _, wrong := cpt.sheds[destCastroCBE]; wrong {
		t.Fatal("Coimbatore's Castro 1 leaked into the other park -- animals would move to the wrong farm")
	}
	if _, wrong := cbe.sheds[destCastroCPT]; wrong {
		t.Fatal("the fixture park's Castro 1 leaked into Coimbatore")
	}

	// Filters must actually filter.
	if _, present := cpt.sheds[destInactiveShed]; present {
		t.Fatal("an inactive shed must not be offered as a destination")
	}
	if _, present := byID[destRetiredPark]; present {
		t.Fatal("a retired/inactive park must not be offered as a destination")
	}

	// A park with no sheds must still appear: the shed-side filters live in the LEFT JOIN's ON
	// clause precisely so a newly-created park is not invisible. Moving them into WHERE would
	// silently make this an inner join, and this assertion is what catches that.
	empty, ok := byID[destEmptyPark]
	if !ok {
		t.Fatal("a park with no active sheds must still appear in the catalog")
	}
	if len(empty.sheds) != 0 {
		t.Fatalf("empty park has %d sheds, want 0", len(empty.sheds))
	}

	// Parks come back ordered by name so the dropdown is stable between calls.
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("parks are not name-ordered: %v", names)
		}
	}
}

// TestShiftingDestinationCatalogIsTenantScoped proves the catalog never leaks another tenant's
// topology into an operator's dropdown.
func TestShiftingDestinationCatalogIsTenantScoped(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedDestinationTopology(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	const otherTenant = "00000000-0000-4000-8000-0000000000ff"
	catalog, err := repo.ShiftingDestinationCatalog(ctx, otherTenant)
	if err != nil {
		t.Fatalf("ShiftingDestinationCatalog: %v", err)
	}
	if len(catalog.Parks) != 0 {
		t.Fatalf("foreign tenant saw %d parks, want 0", len(catalog.Parks))
	}
	if len(catalog.ManagementStages) != 0 {
		t.Fatalf("foreign tenant saw %d stages, want 0", len(catalog.ManagementStages))
	}
}

// TestGoatShiftingFactsDerivesBreedAndStageFromTheAnimal proves the derivation read works against
// the real goats/breeds schema -- the columns exist, the join resolves, and the breed key is
// normalized the same way the counts alias resolver normalizes it.
func TestGoatShiftingFactsDerivesBreedAndStageFromTheAnimal(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedCustodianParty(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	const goatID = "00000000-0000-4000-8000-0000000051a1"
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, species, sex, breed, age_band, management_stage,
                   lifecycle_status, custodian_party_id, park_id, shed_id, current_location_id,
                   origin_type, dob, entry_date)
VALUES ($1::uuid, $2::uuid, 'goat', 'female', 'Boer Cross', 'kid', 'K2',
        'alive', $5::uuid, $3::uuid, $4::uuid, $4::uuid, 'procured', DATE '2025-01-01', DATE '2025-01-01')
ON CONFLICT (goat_id) DO NOTHING`,
		goatID, countsTenant, countsPark, countsShedA, countsCustodian); err != nil {
		t.Fatalf("seed goat: %v", err)
	}

	facts, err := repo.GoatShiftingFacts(ctx, countsTenant, []string{goatID})
	if err != nil {
		t.Fatalf("GoatShiftingFacts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("len(facts)=%d, want 1", len(facts))
	}
	fact := facts[0]
	if fact.BreedLabel != "Boer Cross" {
		t.Fatalf("BreedLabel=%q, want %q from goats.breed", fact.BreedLabel, "Boer Cross")
	}
	// countAliasNorm semantics: lowercase, whitespace collapsed to '_'. This is what makes a
	// derived impact group onto the same projection grain as an imported cohort impact.
	if fact.BreedKey != "boer_cross" {
		t.Fatalf("BreedKey=%q, want boer_cross (countAliasNorm of the label)", fact.BreedKey)
	}
	if fact.StageTag == nil || *fact.StageTag != "K2" {
		t.Fatalf("StageTag=%v, want K2", fact.StageTag)
	}
	if fact.AgeClass == nil || *fact.AgeClass != "kid" {
		t.Fatalf("AgeClass=%v, want kid", fact.AgeClass)
	}
	if fact.Sex == nil || *fact.Sex != "female" {
		t.Fatalf("Sex=%v, want female", fact.Sex)
	}
}

// TestGoatShiftingFactsFallsBackToSpeciesWhenBreedIsUnknown pins the documented fallback.
//
// shifting_event_impacts requires a non-blank breed_key, so an animal with no breed recorded would
// otherwise make the whole movement unsubmittable -- blocking an operator from reporting a real
// physical event because of a missing attribute. The chain degrades to the species (NOT NULL), which
// is a coarser but truthful grain rather than an invented label.
func TestGoatShiftingFactsFallsBackToSpeciesWhenBreedIsUnknown(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedCustodianParty(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	const goatID = "00000000-0000-4000-8000-0000000051a2"
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, species, sex, breed, lifecycle_status, custodian_party_id,
                   park_id, shed_id, current_location_id, origin_type, dob, entry_date)
VALUES ($1::uuid, $2::uuid, 'goat', 'male', NULL, 'alive', $5::uuid,
        $3::uuid, $4::uuid, $4::uuid, 'procured', DATE '2025-01-01', DATE '2025-01-01')
ON CONFLICT (goat_id) DO NOTHING`,
		goatID, countsTenant, countsPark, countsShedA, countsCustodian); err != nil {
		t.Fatalf("seed breedless goat: %v", err)
	}

	facts, err := repo.GoatShiftingFacts(ctx, countsTenant, []string{goatID})
	if err != nil {
		t.Fatalf("GoatShiftingFacts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("len(facts)=%d, want 1", len(facts))
	}
	// Non-empty is the load-bearing part: an empty key would violate the table's CHECK downstream.
	if facts[0].BreedKey != "goat" || facts[0].BreedLabel != "goat" {
		t.Fatalf("breed=(%q,%q), want the species fallback (goat, goat)", facts[0].BreedKey, facts[0].BreedLabel)
	}
}

// TestGoatShiftingFactsKeepsExitedForEligibilityButExcludesMergedAndForeignAnimals proves the
// distinction the write path needs between a terminal goat (422) and a missing goat (404).
//
// Exited animals remain visible with their terminal facts so the service can reject them explicitly.
// Merged aliases and cross-tenant animals remain invisible and fail the write closed as not found.
func TestGoatShiftingFactsKeepsExitedForEligibilityButExcludesMergedAndForeignAnimals(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedCustodianParty(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	const (
		liveGoat   = "00000000-0000-4000-8000-0000000051b0"
		exitedGoat = "00000000-0000-4000-8000-0000000051b1"
		mergedGoat = "00000000-0000-4000-8000-0000000051b2"
	)
	for _, goatID := range []string{liveGoat, exitedGoat, mergedGoat} {
		if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, species, sex, breed, lifecycle_status, custodian_party_id,
                   park_id, shed_id, current_location_id, origin_type, dob, entry_date)
VALUES ($1::uuid, $2::uuid, 'goat', 'female', 'Sirohi', 'alive', $5::uuid,
        $3::uuid, $4::uuid, $4::uuid, 'procured', DATE '2025-01-01', DATE '2025-01-01')
ON CONFLICT (goat_id) DO NOTHING`,
			goatID, countsTenant, countsPark, countsShedA, countsCustodian); err != nil {
			t.Fatalf("seed goat %s: %v", goatID, err)
		}
	}
	if _, err := pool.Exec(ctx, `
UPDATE goats SET lifecycle_status = 'dead', exited_at = now() WHERE goat_id = $1::uuid`, exitedGoat); err != nil {
		t.Fatalf("exit goat: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE goats SET merged_into_goat_id = $2::uuid WHERE goat_id = $1::uuid`, mergedGoat, liveGoat); err != nil {
		t.Fatalf("merge goat: %v", err)
	}

	facts, err := repo.GoatShiftingFacts(ctx, countsTenant, []string{liveGoat, exitedGoat, mergedGoat})
	if err != nil {
		t.Fatalf("GoatShiftingFacts: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("facts=%+v, want live and exited animals", facts)
	}
	byID := make(map[string]struct {
		lifecycle string
		exited    bool
	}, len(facts))
	for _, fact := range facts {
		byID[fact.GoatID] = struct {
			lifecycle string
			exited    bool
		}{lifecycle: fact.LifecycleStatus, exited: fact.ExitedAt != nil}
	}
	if got := byID[liveGoat]; got.lifecycle != "alive" || got.exited {
		t.Fatalf("live fact=%+v, want lifecycle=alive and no exit stamp", got)
	}
	if got := byID[exitedGoat]; got.lifecycle != "dead" || !got.exited {
		t.Fatalf("exited fact=%+v, want lifecycle=dead with exit stamp", got)
	}
	if _, ok := byID[mergedGoat]; ok {
		t.Fatalf("merged goat %s unexpectedly resolved", mergedGoat)
	}

	// A foreign tenant's read of a real goat id must also resolve to nothing.
	foreign, err := repo.GoatShiftingFacts(ctx, "00000000-0000-4000-8000-0000000000ff", []string{liveGoat})
	if err != nil {
		t.Fatalf("GoatShiftingFacts (foreign tenant): %v", err)
	}
	if len(foreign) != 0 {
		t.Fatalf("foreign tenant resolved %d facts, want 0", len(foreign))
	}
}
