package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// 000238 seeds the per-animal rates from the pen totals the farm already has:
// grams = kg x 1000 / the pen's LIVE resident count.
//
// Four properties, and each one is a way the conversion could quietly go wrong:
//
//  1. The denominator is the LIVE herd, not the stored head_count (maintainer instruction
//     2026-09-01, "use live only, forget recorded"). This fixture makes the two DISAGREE on purpose
//     -- the row says 99, the pen holds 31 -- because that is the live situation the instruction was
//     about, and a conversion that quietly used the stale figure would re-base the pen on the spot.
//  2. The rate is TRUNCATED, never rounded to nearest. The generator rounds a pen's session quantity
//     UP to a packable 0.1 kg, so a rate a hair ABOVE exact lifts a pen's sheet by a whole notch even
//     though its population never moved. 8 kg across 31 animals is the worked case: 258.064516...
//     truncates to 258.064 (31 x that = 7999.984 g, still 8.0 kg on the sheet) where rounding to
//     nearest gives 258.065 (8000.015 g -> 4.1 kg per session instead of 4.0).
//  3. A pen with NO LIVE ANIMALS is LEFT ALONE on the legacy basis. There is no denominator, so there
//     is no rate; inventing one would be a feeding decision nobody made.
//  4. A cell already authored in the app is NOT touched, and the pass is IDEMPOTENT: migrations get
//     re-run, and a second pass must convert nothing and must not re-derive a rate from a value that
//     is no longer there.
func TestFeedExperimentBasisSeedDerivesRatesFromTheLiveHerd(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant   = "f2360000-0000-4000-8000-000000000001"
		park     = "f2360000-0000-4000-8000-000000003001"
		shed     = "f2360000-0000-4000-8000-000000004001"
		exact    = "f2360000-0000-4000-8000-000000005001"
		repeat   = "f2360000-0000-4000-8000-000000005002"
		zero     = "f2360000-0000-4000-8000-000000005003"
		noCount  = "f2360000-0000-4000-8000-000000005004"
		authored = "f2360000-0000-4000-8000-000000005005"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Feed Experiment Basis Seed', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, tenant)
	exec(`INSERT INTO locations (location_id, tenant_id, parent_location_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, NULL, 'park', 'F236-P', 'F236 Park', 'active'),
       ($3::uuid, $1::uuid, $2::uuid, 'shed', 'F236-S', 'F236 Shed', 'active')
ON CONFLICT (location_id) DO NOTHING`, tenant, park, shed)

	// PEN 1 holds 31 live animals, PEN 2 holds none, PEN 3 holds 12. The animals are what the
	// conversion divides by; the stored head_count on the rows below deliberately disagrees.
	var partyID string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES (gen_random_uuid(), 'org', 'F236 Custodian', 'active')
RETURNING party_id::text`).Scan(&partyID); err != nil {
		t.Fatalf("insert custodian party: %v", err)
	}
	exec(`INSERT INTO goats (tenant_id, species, sex, lifecycle_status, custodian_party_id, shed_id)
SELECT $1::uuid, 'goat', 'female', 'alive', $2::uuid, $3::uuid FROM generate_series(1, 31)`,
		tenant, partyID, shed)
	exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label)
SELECT $1::uuid, goat_id, $2::uuid, 'F236 Shed', '1' FROM goats WHERE tenant_id = $1::uuid`, tenant, shed)

	// The rows are written in the PRE-000238 shape on purpose: this is what a database that has run
	// 000237 and not yet 000238 actually holds. head_count carries a STALE 99 on the convertible
	// rows: if the conversion reads it instead of the herd, the derived rates come out wrong and the
	// assertions below fail.
	exec(`INSERT INTO feed_experiment_config
  (experiment_config_id, tenant_id, park_id, shed_id, partition_label, feed_item_label,
   quantity_basis, absolute_kg, grams_per_head, head_count, experiment_category, status)
VALUES
  ($4::uuid,  $1::uuid, $2::uuid, $3::uuid, '1', 'Dry Masoor Bhusa',            'absolute_kg',    8.000, NULL,     99,   'Arm A', 'active'),
  ($5::uuid,  $1::uuid, $2::uuid, $3::uuid, '1', 'Mesha Kids Sheep Concentrate','absolute_kg',    8.000, NULL,     99,   'Arm A', 'active'),
  ($6::uuid,  $1::uuid, $2::uuid, $3::uuid, '1', 'RGS Concentrate',             'absolute_kg',    0.000, NULL,     99,   'Arm A', 'active'),
  ($7::uuid,  $1::uuid, $2::uuid, $3::uuid, '2', 'Dry Masoor Bhusa',            'absolute_kg',    5.000, NULL,     10,   'Arm B', 'active'),
  ($8::uuid,  $1::uuid, $2::uuid, $3::uuid, '3', 'Dry Masoor Bhusa',            'grams_per_head', NULL,  410.000,  12,   'Arm C', 'active')`,
		tenant, park, shed, exact, repeat, zero, noCount, authored)

	raw, err := os.ReadFile("000238_feed_experiment_seed_grams_per_head.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	up := migrationUp(string(raw))
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("apply 000238: %v", err)
	}

	type row struct {
		basis  string
		kg     *string
		grams  *string
		source string
	}
	read := func(id string) row {
		t.Helper()
		var got row
		if err := pool.QueryRow(ctx, `
SELECT quantity_basis, absolute_kg::text, grams_per_head::text, source
FROM feed_experiment_config WHERE experiment_config_id = $1::uuid`, id).
			Scan(&got.basis, &got.kg, &got.grams, &got.source); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return got
	}
	wantGrams := func(id, want string) {
		t.Helper()
		got := read(id)
		if got.basis != "grams_per_head" {
			t.Fatalf("%s basis = %q, want grams_per_head", id, got.basis)
		}
		if got.grams == nil || *got.grams != want {
			t.Fatalf("%s grams_per_head = %v, want %q", id, got.grams, want)
		}
		if got.kg != nil {
			t.Fatalf("%s still carries absolute_kg %q; the pairing check exists so a row cannot hold two answers", id, *got.kg)
		}
	}

	// 8 kg across the 31 animals ACTUALLY IN THE PEN = 258.064516..., TRUNCATED. Reading the stored
	// 99 instead would give 80.808; rounding to nearest would give 258.065, which turns an unchanged
	// pen's 4.000 kg session into 4.100 kg.
	wantGrams(exact, "258.064")
	wantGrams(repeat, "258.064")
	// An authored zero is a real instruction ("this arm gets none of it") and converts to a real 0.
	wantGrams(zero, "0.000")

	// PEN 2 holds no animals, so there is no denominator and no rate: the row keeps feeding exactly
	// what it feeds today, and its stored head count of 10 is NOT used as a stand-in.
	if got := read(noCount); got.basis != "absolute_kg" || got.kg == nil || *got.kg != "5.000" {
		t.Fatalf("empty pen's row = %+v, want the untouched legacy 5.000 kg", got)
	}
	// An app authoring is not a derivation and must survive untouched.
	if got := read(authored); got.grams == nil || *got.grams != "410.000" {
		t.Fatalf("app-authored row = %+v, want its own 410.000 g", got)
	}

	// The log carries the inputs, so the conversion is auditable and the Down path exact.
	var logged int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_experiment_basis_conversions WHERE tenant_id = $1::uuid`, tenant).Scan(&logged); err != nil {
		t.Fatalf("count conversions: %v", err)
	}
	if logged != 3 {
		t.Fatalf("logged %d conversions, want 3 (the empty pen's row and the app-authored row are not conversions)", logged)
	}
	// The log records the LIVE denominator, which is the number nobody could reconstruct later.
	var loggedCount int32
	if err := pool.QueryRow(ctx, `SELECT head_count FROM feed_experiment_basis_conversions WHERE experiment_config_id = $1::uuid`, exact).Scan(&loggedCount); err != nil {
		t.Fatalf("read conversion denominator: %v", err)
	}
	if loggedCount != 31 {
		t.Fatalf("logged denominator = %d, want the live 31 rather than the stored 99", loggedCount)
	}

	// IDEMPOTENT: a re-run converts nothing and changes no value.
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("re-apply 000238: %v", err)
	}
	wantGrams(exact, "258.064")
	wantGrams(repeat, "258.064")
	if got := read(noCount); got.basis != "absolute_kg" {
		t.Fatalf("countless row moved on the second pass: %+v", got)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_experiment_basis_conversions WHERE tenant_id = $1::uuid`, tenant).Scan(&logged); err != nil {
		t.Fatalf("recount conversions: %v", err)
	}
	if logged != 3 {
		t.Fatalf("second pass logged %d conversions, want the original 3", logged)
	}
}

// The conversion joins a GROUPED census to the cells it updates, and these are the four ways that
// shape goes wrong. They are one test because they share a fixture: two parks that both hold a shed
// called Castro with a pen '1', one of them carrying many cells, many animals and both statuses.
//
//   - ONE-TO-MANY: a pen has many cells and many animals. The census must be grouped ONCE per pen, so
//     every cell of a pen divides by the same number and no animal is counted per cell. A correlated
//     count would still pass; a census joined at the wrong grain (per goat, say) would multiply the
//     denominator by the pen's own population and silently shrink every rate.
//   - PARK SCOPE: Castro '1' exists in BOTH parks with different populations. The census is keyed on
//     (tenant, shed, pen) and the shed id is what keeps them apart -- keying on the NAME, which this
//     repo has done wrong before, would merge two parks' animals into one denominator.
//   - STATUS BUCKETS: a RETIRED row converts too. Retired cells are kept precisely so a pen can be
//     restored without re-keying them, so leaving them on the old basis would hand back absolute kg
//     to a pen the farm later puts back on the experiment.
//   - NO PAGE BOUNDARY: a migration is one set-based statement. Every convertible row of every park
//     moves in the same pass; a partial application would leave a pen half on each basis, which is
//     the one state the feed sheet cannot read consistently.
func TestFeedExperimentBasisSeedOneToManyParkScopeStatusBucketsAndPageBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant   = "f2370000-0000-4000-8000-000000000001"
		parkA    = "f2370000-0000-4000-8000-000000003001"
		parkB    = "f2370000-0000-4000-8000-000000003002"
		shedA    = "f2370000-0000-4000-8000-000000004001"
		shedB    = "f2370000-0000-4000-8000-000000004002"
		cellA1   = "f2370000-0000-4000-8000-000000005001"
		cellA2   = "f2370000-0000-4000-8000-000000005002"
		cellA3   = "f2370000-0000-4000-8000-000000005003"
		retiredA = "f2370000-0000-4000-8000-000000005004"
		cellB1   = "f2370000-0000-4000-8000-000000005005"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Feed Experiment Basis Grain', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, tenant)
	// TWO PARKS, each with a shed named Castro holding a pen '1'. Same names, different animals.
	exec(`INSERT INTO locations (location_id, tenant_id, parent_location_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, NULL, 'park', 'F237-PA', 'F237 Park A', 'active'),
       ($3::uuid, $1::uuid, NULL, 'park', 'F237-PB', 'F237 Park B', 'active'),
       ($4::uuid, $1::uuid, $2::uuid, 'shed', 'F237-SA', 'Castro', 'active'),
       ($5::uuid, $1::uuid, $3::uuid, 'shed', 'F237-SB', 'Castro', 'active')
ON CONFLICT (location_id) DO NOTHING`, tenant, parkA, parkB, shedA, shedB)

	var partyID string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES (gen_random_uuid(), 'org', 'F237 Custodian', 'active')
RETURNING party_id::text`).Scan(&partyID); err != nil {
		t.Fatalf("insert custodian party: %v", err)
	}
	seedPen := func(shed string, head int) {
		t.Helper()
		exec(`INSERT INTO goats (tenant_id, species, sex, lifecycle_status, custodian_party_id, shed_id)
SELECT $1::uuid, 'goat', 'female', 'alive', $2::uuid, $3::uuid FROM generate_series(1, $4::int)`,
			tenant, partyID, shed, head)
		exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label)
SELECT $1::uuid, g.goat_id, $2::uuid, 'Castro', '1'
FROM goats g
WHERE g.tenant_id = $1::uuid AND g.shed_id = $2::uuid
  AND NOT EXISTS (SELECT 1 FROM goat_shed_partitions p WHERE p.tenant_id = g.tenant_id AND p.goat_id = g.goat_id)`,
			tenant, shed)
	}
	seedPen(shedA, 20) // park A's Castro 1
	seedPen(shedB, 5)  // park B's Castro 1 -- the merge trap

	exec(`INSERT INTO feed_experiment_config
  (experiment_config_id, tenant_id, park_id, shed_id, partition_label, feed_item_label,
   quantity_basis, absolute_kg, head_count, experiment_category, status)
VALUES
  ($5::uuid, $1::uuid, $2::uuid, $3::uuid, '1', 'Dry Masoor Bhusa',             'absolute_kg', 10.000, 77, 'Arm A', 'active'),
  ($6::uuid, $1::uuid, $2::uuid, $3::uuid, '1', 'Mesha Kids Sheep Concentrate', 'absolute_kg', 20.000, 77, 'Arm A', 'active'),
  ($7::uuid, $1::uuid, $2::uuid, $3::uuid, '1', 'RGS Concentrate',              'absolute_kg',  5.000, 77, 'Arm A', 'active'),
  ($8::uuid, $1::uuid, $2::uuid, $3::uuid, '1', 'Baking Soda',                  'absolute_kg',  2.000, 77, 'Arm A', 'retired'),
  ($9::uuid, $1::uuid, $4::uuid, $10::uuid,'1', 'Dry Masoor Bhusa',             'absolute_kg', 10.000, 77, 'Arm B', 'active')`,
		tenant, parkA, shedA, parkB, cellA1, cellA2, cellA3, retiredA, cellB1, shedB)

	raw, err := os.ReadFile("000238_feed_experiment_seed_grams_per_head.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("apply 000238: %v", err)
	}

	rate := func(id string) string {
		t.Helper()
		var basis string
		var grams *string
		if err := pool.QueryRow(ctx, `
SELECT quantity_basis, grams_per_head::text FROM feed_experiment_config WHERE experiment_config_id = $1::uuid`, id).
			Scan(&basis, &grams); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if basis != "grams_per_head" || grams == nil {
			t.Fatalf("%s did not convert: basis=%q grams=%v", id, basis, grams)
		}
		return *grams
	}

	// ONE-TO-MANY: three cells of one 20-animal pen, each divided by 20 exactly once.
	if got := rate(cellA1); got != "500.000" {
		t.Fatalf("10 kg across 20 animals = %q, want 500.000 -- the census must be grouped per pen, not per cell", got)
	}
	if got := rate(cellA2); got != "1000.000" {
		t.Fatalf("20 kg across 20 animals = %q, want 1000.000", got)
	}
	if got := rate(cellA3); got != "250.000" {
		t.Fatalf("5 kg across 20 animals = %q, want 250.000", got)
	}
	// STATUS BUCKETS: the retired cell converted on the same pass and on the same denominator.
	if got := rate(retiredA); got != "100.000" {
		t.Fatalf("retired cell = %q, want 100.000 -- a retired row is kept so the pen can be restored, and must move with the rest", got)
	}
	// PARK SCOPE: park B's identically named Castro 1 divided by its OWN 5 animals, not by 25.
	if got := rate(cellB1); got != "2000.000" {
		t.Fatalf("park B's Castro 1 = %q, want 2000.000 -- keying the census on the shed NAME merges two parks' animals", got)
	}

	// NO PAGE BOUNDARY: one statement, every convertible row, both parks, both statuses.
	var left int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_experiment_config WHERE tenant_id = $1::uuid AND quantity_basis = 'absolute_kg'`, tenant).Scan(&left); err != nil {
		t.Fatalf("count unconverted: %v", err)
	}
	if left != 0 {
		t.Fatalf("%d rows still on the legacy basis; the conversion is one set-based statement and must not leave a pen half on each basis", left)
	}
}
