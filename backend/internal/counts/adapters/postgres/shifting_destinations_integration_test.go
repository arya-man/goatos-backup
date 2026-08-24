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
	// Parent shed + partition catalog used to prove old same-park partition aliases are suppressed.
	destCastroParentCPT = "00000000-0000-4000-8000-000000004104"
	// The OTHER alias spelling: a parent "Mandela 1" whose catalog pen is labelled "Part 3", beside
	// the legacy location literally named "Mandela 1 - Part 3". Both render "Mandela 1 - Part 3",
	// so the picker showed the same pen twice under two different shed ids.
	destMandelaParentCPT = "00000000-0000-4000-8000-000000004105"
	destMandelaPartCPT   = "00000000-0000-4000-8000-000000004106"
)

func seedDestinationTopology(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	// The catalog carries the ACTIVE management-stage vocabulary alongside the destinations, read
	// from animal_stage_lookup. Without it the movement form has no cohort to move an animal into,
	// so the fixture seeds the vocabulary it asserts on -- including the two clinical stages, which
	// the picker must strip.
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order, status)
VALUES ($1::uuid, 'K2', 'K2', 1, 'active'),
       ($1::uuid, 'Mother', 'Mother', 2, 'active'),
       ($1::uuid, 'ICU', 'ICU', 3, 'active'),
       ($1::uuid, 'Quarantine', 'Quarantine', 4, 'active')
ON CONFLICT DO NOTHING`, countsTenant); err != nil {
		t.Fatalf("seed management-stage vocabulary: %v", err)
	}
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

// TestShiftingDestinationCatalogSuppressesSameParkPartitionAliases pins the live STG bug where the
// operator saw both "Castro 1" and "Castro 1" in the same park. The first is an old active
// partition-alias location; the second is the canonical parent shed plus shed_partitions row.
func TestShiftingDestinationCatalogSuppressesSameParkPartitionAliases(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedDestinationTopology(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	// TWO separate Exec calls, deliberately. pgx sends a parameterised Exec as a PREPARED statement,
	// and Postgres refuses more than one command in one of those ("cannot insert multiple commands
	// into a prepared statement", SQLSTATE 42601). Batched into a single string with a `;` the seed
	// fails, the test fails IN SETUP, and the assertion below never runs -- so the production change
	// it is supposed to prove would have gone in unproven.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($3::uuid, $1::uuid, 'shed', 'CPT-CASTRO', 'Castro', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`,
		countsTenant, countsPark, destCastroParentCPT); err != nil {
		t.Fatalf("seed parent Castro shed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
-- 'manual' because shed_partitions_source (migration 000112) allows only
-- goat_attested / location_alias / manual. 'manual' is the honest one for a hand-seeded pen.
VALUES ($1::uuid, $2::uuid, '1', '1', 'active', 'manual')
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label = EXCLUDED.partition_label, status = EXCLUDED.status`,
		countsTenant, destCastroParentCPT); err != nil {
		t.Fatalf("seed parent Castro partition: %v", err)
	}

	catalog, err := repo.ShiftingDestinationCatalog(ctx, countsTenant)
	if err != nil {
		t.Fatalf("ShiftingDestinationCatalog: %v", err)
	}

	var labels []string
	for _, park := range catalog.Parks {
		if park.ParkID != countsPark {
			continue
		}
		for _, shed := range park.Sheds {
			if shed.ShedID == destCastroCPT || shed.ShedID == destCastroParentCPT {
				labels = append(labels, shed.Display)
			}
		}
	}
	if len(labels) != 1 || labels[0] != "Castro 1" {
		t.Fatalf("Castro destination labels in one park = %v, want only the actual shed \"Castro 1\"", labels)
	}
}

// TestShiftingDestinationCatalogSuppressesPartSpelledPartitionAliases is the sibling of the test
// above for the OTHER alias spelling, and it is the one that was actually shipping duplicates.
//
// "Castro 1" normalizes to "castro1", so stripping the parent "castro" leaves "1" -- which equals
// the catalog's normalized_label and was suppressed. "Mandela 1 - Part 3" normalizes to
// "mandela1part3", so stripping the parent "mandela1" leaves "part3", which never equalled "3".
// Every "- Part N" alias therefore survived, and the live picker carried 75 exact-duplicate rows
// (195 rows for 120 real operational locations) with two different shed ids behind one label.
func TestShiftingDestinationCatalogSuppressesPartSpelledPartitionAliases(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedDestinationTopology(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	// Separate Execs for the same prepared-statement reason as the test above.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($3::uuid, $1::uuid, 'shed', 'CPT-MANDELA1', 'Mandela 1', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`,
		countsTenant, countsPark, destMandelaParentCPT); err != nil {
		t.Fatalf("seed parent Mandela shed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($3::uuid, $1::uuid, 'shed', 'CPT-MANDELA1-P3', 'Mandela 1 - Part 3', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`,
		countsTenant, countsPark, destMandelaPartCPT); err != nil {
		t.Fatalf("seed legacy Mandela part alias: %v", err)
	}
	// The catalog stores the HUMAN label "Part 3" against the matching key "3" -- the exact pairing
	// that made the remainder comparison fail.
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 3', '3', 'active', 'manual')
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label = EXCLUDED.partition_label, status = EXCLUDED.status`,
		countsTenant, destMandelaParentCPT); err != nil {
		t.Fatalf("seed parent Mandela partition: %v", err)
	}

	catalog, err := repo.ShiftingDestinationCatalog(ctx, countsTenant)
	if err != nil {
		t.Fatalf("ShiftingDestinationCatalog: %v", err)
	}

	var labels []string
	for _, park := range catalog.Parks {
		if park.ParkID != countsPark {
			continue
		}
		for _, shed := range park.Sheds {
			if shed.ShedID == destMandelaParentCPT || shed.ShedID == destMandelaPartCPT {
				labels = append(labels, shed.Display)
			}
		}
	}
	if len(labels) != 1 || labels[0] != "Mandela 1 - Part 3" {
		t.Fatalf("Mandela destination labels in one park = %v, want only the actual shed \"Mandela 1 - Part 3\"", labels)
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

// TestShiftingDestinationCatalogCarriesEachPensOwnConfiguredCohort pins the maintainer decision of
// 2026-08-14: a movement targets a PEN, so the catalog must report the cohort configured for that
// pen, not one derived from the whole building.
//
// Two pens of ONE shed are given different tags. Before shed_partitions carried a cohort there was
// no way to tell them apart, and the raise resolved from the shed's mixed residents -- which is
// precisely the case that produced "keep current" for a pen that is unambiguously one cohort.
//
// The shed's own profile is set to a THIRD value, so a query that fell back to shed_profiles for a
// pen row would be caught rather than passing by coincidence.
func TestShiftingDestinationCatalogCarriesEachPensOwnConfiguredCohort(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedDestinationTopology(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($3::uuid, $1::uuid, 'shed', 'CPT-CASTRO', 'Castro', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`,
		countsTenant, countsPark, destCastroParentCPT); err != nil {
		t.Fatalf("seed parent Castro shed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, sort_order, status)
VALUES ($1::uuid, 'K2', 'Milk drinking', 'kid', 1, 'active'),
       ($1::uuid, 'Mother', 'Mother', 'adult', 2, 'active'),
       ($1::uuid, 'Buck', 'Buck', 'adult', 3, 'active')
ON CONFLICT (tenant_id, stage_code) DO NOTHING`, countsTenant); err != nil {
		t.Fatalf("seed stage vocabulary: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source, animal_stage_id)
SELECT $1::uuid, $2::uuid, pen.label, pen.normalized, 'active', 'manual', a.animal_stage_id
FROM (VALUES ('1', '1', 'K2'), ('2', '2', 'Mother')) AS pen(label, normalized, stage)
JOIN animal_stage_lookup a ON a.tenant_id = $1::uuid AND a.stage_code = pen.stage`,
		countsTenant, destCastroParentCPT); err != nil {
		t.Fatalf("seed pens with cohorts: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, row_version)
SELECT $2::uuid, $1::uuid, a.animal_stage_id, 1
FROM animal_stage_lookup a WHERE a.tenant_id = $1::uuid AND a.stage_code = 'Buck'
ON CONFLICT (location_id) DO UPDATE SET animal_stage_id = EXCLUDED.animal_stage_id`,
		countsTenant, destCastroParentCPT); err != nil {
		t.Fatalf("seed shed profile: %v", err)
	}

	catalog, err := repo.ShiftingDestinationCatalog(ctx, countsTenant)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}

	byDisplay := map[string]string{}
	for _, park := range catalog.Parks {
		for _, shed := range park.Sheds {
			if shed.ShedID == destCastroParentCPT {
				byDisplay[shed.Display] = shed.ConfiguredStage
			}
		}
	}
	if len(byDisplay) != 2 {
		t.Fatalf("want one entry per pen, got %d: %+v", len(byDisplay), byDisplay)
	}
	for display, want := range map[string]string{"Castro 1": "K2", "Castro 2": "Mother"} {
		if got := byDisplay[display]; got != want {
			t.Fatalf("%s configured cohort = %q, want %q -- a pen must report its OWN tag, not its shed's", display, got, want)
		}
	}
	for display, got := range byDisplay {
		if got == "Buck" {
			t.Fatalf("%s fell back to the SHED profile; a pen row must never inherit it", display)
		}
	}
}
