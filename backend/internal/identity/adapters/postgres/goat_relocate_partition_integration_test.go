package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// RelocateGoatsToShedInTx partition-awareness regression suite.
//
// OperationalLocation = park + physical shed + optional partition (backend/internal/platform/oploc).
// goats.shed_id/park_id must ALWAYS name the parent physical shed, never a partition-bearing alias,
// and goat_shed_partitions carries the partition half. These tests prove that RelocateGoatsToShedInTx
// keeps both halves in sync, atomically, across every movement shape: same-shed partition move,
// cross-shed partition move, a genuinely non-partitioned move, and the two mixed transitions
// (partitioned -> non-partitioned, non-partitioned -> partitioned).

const (
	rpTenant = "00000000-0000-4000-8000-000000000001" // shared baseline tenant (see herd_register_projection_integration_test.go)
	rpParty  = "00000000-0000-4000-8000-000000001001"
	rpPark   = "00000000-0000-4000-8000-000000003001" // CBE, shared baseline park
	rpActor  = "30000000-0000-4000-8000-000000000001"
)

// relocatePartitionFixture seeds one park's worth of physical sheds -- two partitioned (Castro,
// Gandhi) and two non-partitioned (Yashoda, Old Yashoda) -- plus one INACTIVE alias location that
// must never be written as a goat's shed_id or read back as a real destination (the retired
// "Castro 1"/"Godel 1 - Part 3" alias-row shape named in the operational-location contract).
type relocatePartitionFixture struct {
	castroShed, gandhiShed, yashodaShed, oldYashodaShed, inactiveAliasShed string
}

// seedRelocatePartitionFixture seeds the sheds via gen_random_uuid() (each test runs against its
// own StartPostgres clone, so there is no cross-test id collision to avoid) and returns the ids.
func seedRelocatePartitionFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) relocatePartitionFixture {
	t.Helper()
	insertShed := func(name, status string) string {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, 'shed', $2, $3::uuid, $4)
RETURNING location_id::text`, rpTenant, name, rpPark, status).Scan(&id); err != nil {
			t.Fatalf("seed shed %s: %v", name, err)
		}
		return id
	}
	f := relocatePartitionFixture{
		castroShed:     insertShed("Castro", "active"),
		gandhiShed:     insertShed("Gandhi", "active"),
		yashodaShed:    insertShed("Yashoda", "active"),
		oldYashodaShed: insertShed("Old Yashoda", "active"),
		// An INACTIVE alias-shaped row -- exactly the retired "Castro 1" pattern the contract says
		// must never be used as a destination or referenced as a shed_id. Named to look like a
		// partition-bearing alias to make sure nothing in the write path treats a NAME match as
		// eligibility.
		inactiveAliasShed: insertShed("Castro 1", "inactive"),
	}
	return f
}

func seedRelocateGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID string) string {
	t.Helper()
	var goatID string
	if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, shed_id, breed, sex, age_band)
VALUES ($1::uuid, 'alive', 'goat', $2::uuid, $3::uuid, $4::uuid, $3::uuid, 'Boer', 'female', 'adult')
RETURNING goat_id::text`, rpTenant, rpParty, shedID, rpPark).Scan(&goatID); err != nil {
		t.Fatalf("seed goat: %v", err)
	}
	return goatID
}

func seedRelocateGoatPartition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shedID, partitionLabel, sourceShedName string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)`,
		rpTenant, goatID, shedID, partitionLabel, sourceShedName); err != nil {
		t.Fatalf("seed goat_shed_partitions for %s: %v", goatID, err)
	}
}

func readGoatShedAndPartition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) (shedID, partitionLabel string, hasPartitionRow bool) {
	t.Helper()
	var currentShed string
	if err := pool.QueryRow(ctx, `SELECT shed_id::text FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		rpTenant, goatID).Scan(&currentShed); err != nil {
		t.Fatalf("read goat shed_id: %v", err)
	}
	var gspShed, label string
	err := pool.QueryRow(ctx, `SELECT shed_id::text, partition_label FROM goat_shed_partitions WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		rpTenant, goatID).Scan(&gspShed, &label)
	if err != nil {
		return currentShed, "", false
	}
	if gspShed != currentShed {
		t.Fatalf("goat_shed_partitions.shed_id (%s) disagrees with goats.shed_id (%s) for goat %s -- the two must move atomically together", gspShed, currentShed, goatID)
	}
	return currentShed, label, true
}

func relocate(t *testing.T, ctx context.Context, repo *Repository, cmd ports.RelocateGoatsCommand) ports.RelocateGoatsResult {
	t.Helper()
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := repo.RelocateGoatsToShedInTx(ctx, tx, cmd)
	if err != nil {
		t.Fatalf("RelocateGoatsToShedInTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit relocate tx: %v", err)
	}
	return result
}

func strp(s string) *string { return &s }

// TestRelocateGoatsToShedInTxSameShedPartitionMove: Castro Part 1 -> Castro Part 2. The physical
// shed does not change; only the partition does.
func TestRelocateGoatsToShedInTxSameShedPartitionMove(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	goatID := seedRelocateGoat(t, ctx, pool, f.castroShed)
	seedRelocateGoatPartition(t, ctx, pool, goatID, f.castroShed, "1", "Castro 1")

	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.castroShed),
		ToParkID: rpPark, ToShedID: f.castroShed,
		DestinationPartitionLabel: strp("2"), DestinationShedName: "Castro",
		OccurredAt: time.Now(), OutboxIdempotencyPrefix: "test-same-shed-partition-move",
	})

	shedID, partition, ok := readGoatShedAndPartition(t, ctx, pool, goatID)
	if !ok {
		t.Fatalf("expected a goat_shed_partitions row after the move")
	}
	if shedID != f.castroShed {
		t.Fatalf("goats.shed_id = %s, want the parent shed %s unchanged (same-shed partition move)", shedID, f.castroShed)
	}
	if shedID == f.inactiveAliasShed {
		t.Fatalf("goats.shed_id resolved to the inactive alias location, must never happen")
	}
	if partition != "2" {
		t.Fatalf("goat_shed_partitions.partition_label = %q, want \"2\"", partition)
	}
}

// A move into a clinical KID pen stamps that pen's tag AND saves the animal's milk band, in the
// same statement, so the band can never be lost in between.
//
// Stamping 'ICU-Kid' overwrites management_stage, which is where the milk ladder lives -- and a kid
// in ICU still drinks milk. milk_cohort (000166) is what Milk Preparation falls back to, so if the
// stamp landed without the save, the animal would silently stop being prepared for. The return leg
// clears it: once a real band is back in management_stage, a leftover milk_cohort would outlive its
// own truth.
func TestRelocateIntoClinicalKidPenStampsTagAndSavesMilkBand(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	seedRelocateStageVocabulary(t, ctx, pool)

	goatID := seedRelocateGoat(t, ctx, pool, f.castroShed)
	if _, err := pool.Exec(ctx, `UPDATE goats SET management_stage='K2', age_band='kid' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		rpTenant, goatID); err != nil {
		t.Fatalf("seed K2 stage: %v", err)
	}

	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.castroShed),
		ToParkID: rpPark, ToShedID: f.gandhiShed,
		DestinationTag: "ICU-Kid", DestinationShedName: "Gandhi",
		OccurredAt: time.Now(), OutboxIdempotencyPrefix: "test-clinical-kid-pen-in",
	})

	stage, band, milk := readStageBandAndMilkCohort(t, ctx, pool, goatID)
	if stage != "ICU-Kid" {
		t.Fatalf("management_stage = %q after a move into a clinical kid pen, want it stamped as %q", stage, "ICU-Kid")
	}
	if milk != "K2" {
		t.Fatalf("milk_cohort = %q, want %q -- the band must be saved in the same statement that overwrites it", milk, "K2")
	}
	// A clinical tag carries no age_band, so the animal's existing kid classification survives.
	if band != "kid" {
		t.Fatalf("age_band = %q, want it untouched at %q -- a clinical pen must not reclassify the animal", band, "kid")
	}

	// Return leg: back onto a real milk band. The band is live in management_stage again, so the
	// saved copy must be cleared rather than left to go stale.
	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.gandhiShed),
		ToParkID: rpPark, ToShedID: f.yashodaShed,
		DestinationTag: "K3", DestinationShedName: "Yashoda",
		OccurredAt: time.Now(), OutboxIdempotencyPrefix: "test-clinical-kid-pen-out",
	})

	stage, _, milk = readStageBandAndMilkCohort(t, ctx, pool, goatID)
	if stage != "K3" || milk != "" {
		t.Fatalf("after the return leg stage=%q milk_cohort=%q, want K3 with the saved band cleared", stage, milk)
	}
}

// A move onto a WEANED cohort must not save a band. The animal is leaving milk, not hiding its
// place on the ladder, and a saved band would put it back into milk preparation forever.
func TestRelocateOntoWeanedCohortSavesNoMilkBand(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	seedRelocateStageVocabulary(t, ctx, pool)

	goatID := seedRelocateGoat(t, ctx, pool, f.castroShed)
	if _, err := pool.Exec(ctx, `UPDATE goats SET management_stage='K3', age_band='kid' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		rpTenant, goatID); err != nil {
		t.Fatalf("seed K3 stage: %v", err)
	}

	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.castroShed),
		ToParkID: rpPark, ToShedID: f.gandhiShed,
		DestinationTag: "F2-Male", DestinationShedName: "Gandhi",
		OccurredAt: time.Now(), OutboxIdempotencyPrefix: "test-weaned-cohort",
	})

	stage, _, milk := readStageBandAndMilkCohort(t, ctx, pool, goatID)
	if stage != "F2-Male" || milk != "" {
		t.Fatalf("stage=%q milk_cohort=%q, want F2-Male with no saved band -- a weaned kid is OFF milk", stage, milk)
	}
}

// Shifting an animal INTO K3 starts its seven-day milk clock; moving it out clears the clock; and a
// move BETWEEN K3 pens does not restart a week the animal is already partway through.
//
// The middle leg is the one worth having a test for. A partition change or a move to another K3 pen
// still writes management_stage = 'K3', so a naive "stamp the date whenever the tag is K3" would
// hand the animal a fresh seven days every time it was moved, and an animal that got shuffled
// around would never wean.
func TestRelocateIntoK3StartsTheMilkClockAndDoesNotRestartIt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	seedRelocateStageVocabulary(t, ctx, pool)

	goatID := seedRelocateGoat(t, ctx, pool, f.castroShed)
	if _, err := pool.Exec(ctx, `UPDATE goats SET management_stage='K2', age_band='kid' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		rpTenant, goatID); err != nil {
		t.Fatalf("seed K2 stage: %v", err)
	}

	// 2026-08-15 08:00 IST. Asserted as a business DATE: a feed day is a farm day in Asia/Kolkata.
	entry := time.Date(2026, 8, 15, 2, 30, 0, 0, time.UTC)
	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.castroShed),
		ToParkID: rpPark, ToShedID: f.gandhiShed,
		DestinationTag: "K3", DestinationShedName: "Gandhi",
		OccurredAt: entry, OutboxIdempotencyPrefix: "test-k3-clock-start",
	})
	if got := readK3Start(t, ctx, pool, goatID); got != "2026-08-15" {
		t.Fatalf("k3_milk_started_on = %q after entering K3, want 2026-08-15", got)
	}

	// Moved again, four days later, still K3. The clock must NOT restart.
	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.gandhiShed),
		ToParkID: rpPark, ToShedID: f.yashodaShed,
		DestinationTag: "K3", DestinationShedName: "Yashoda",
		OccurredAt: entry.AddDate(0, 0, 4), OutboxIdempotencyPrefix: "test-k3-clock-no-restart",
	})
	if got := readK3Start(t, ctx, pool, goatID); got != "2026-08-15" {
		t.Fatalf("k3_milk_started_on = %q after a K3-to-K3 move, want the original 2026-08-15 -- a move must not buy another week", got)
	}

	// Out of K3 onto a weaned cohort: the clock is cleared, so a later return starts fresh.
	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.yashodaShed),
		ToParkID: rpPark, ToShedID: f.oldYashodaShed,
		DestinationTag: "F2-Male", DestinationShedName: "Old Yashoda",
		OccurredAt: entry.AddDate(0, 0, 8), OutboxIdempotencyPrefix: "test-k3-clock-clear",
	})
	if got := readK3Start(t, ctx, pool, goatID); got != "" {
		t.Fatalf("k3_milk_started_on = %q after leaving K3, want it cleared", got)
	}
}

func readK3Start(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) string {
	t.Helper()
	var started string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(to_char(k3_milk_started_on, 'YYYY-MM-DD'), '')
FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, rpTenant, goatID).Scan(&started); err != nil {
		t.Fatalf("read k3_milk_started_on: %v", err)
	}
	return started
}

// seedRelocateStageVocabulary seeds the writable stage vocabulary this suite needs. The two
// clinical KID pens are listed active on purpose: that listing is the whole mechanism by which a
// shifting is allowed to stamp them (migration 000167), and without it the raise resolves to
// keep-current and these tests would pass for the wrong reason.
func seedRelocateStageVocabulary(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	stages := []struct{ code, ageBand string }{
		{"K1", "kid"}, {"K2", "kid"}, {"K3", "kid"},
		{"F2-Male", "kid"}, {"Mother", "adult"},
		{"ICU-Kid", ""}, {"Quarantine kids", ""},
	}
	for _, s := range stages {
		if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, status, age_band)
VALUES ($1::uuid, $2, $2, 'active', NULLIF($3::text, ''))
ON CONFLICT (tenant_id, stage_code) DO UPDATE SET status='active', age_band=EXCLUDED.age_band`,
			rpTenant, s.code, s.ageBand); err != nil {
			t.Fatalf("seed stage %s: %v", s.code, err)
		}
	}
}

func readStageBandAndMilkCohort(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) (stage, ageBand, milkCohort string) {
	t.Helper()
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(management_stage, ''), COALESCE(age_band, ''), COALESCE(milk_cohort, '')
FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, rpTenant, goatID).Scan(&stage, &ageBand, &milkCohort); err != nil {
		t.Fatalf("read goat stage/band/milk_cohort: %v", err)
	}
	return stage, ageBand, milkCohort
}

// TestRelocateGoatsToShedInTxCrossShedPartitionMove: Castro Part 1 -> Gandhi Part 3, same park.
func TestRelocateGoatsToShedInTxCrossShedPartitionMove(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	goatID := seedRelocateGoat(t, ctx, pool, f.castroShed)
	seedRelocateGoatPartition(t, ctx, pool, goatID, f.castroShed, "1", "Castro 1")

	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.castroShed),
		ToParkID: rpPark, ToShedID: f.gandhiShed,
		DestinationPartitionLabel: strp("3"), DestinationShedName: "Gandhi",
		OccurredAt: time.Now(), OutboxIdempotencyPrefix: "test-cross-shed-partition-move",
	})

	shedID, partition, ok := readGoatShedAndPartition(t, ctx, pool, goatID)
	if !ok {
		t.Fatalf("expected a goat_shed_partitions row after the move")
	}
	if shedID != f.gandhiShed {
		t.Fatalf("goats.shed_id = %s, want the destination parent shed %s", shedID, f.gandhiShed)
	}
	if shedID == f.inactiveAliasShed {
		t.Fatalf("goats.shed_id resolved to the inactive alias location, must never happen")
	}
	if partition != "3" {
		t.Fatalf("goat_shed_partitions.partition_label = %q, want \"3\"", partition)
	}
}

// TestRelocateGoatsToShedInTxNonPartitionedMove: Yashoda -> Old Yashoda, neither shed partitioned.
func TestRelocateGoatsToShedInTxNonPartitionedMove(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	goatID := seedRelocateGoat(t, ctx, pool, f.yashodaShed)
	// No goat_shed_partitions row seeded: a genuinely non-partitioned placement may have none yet.

	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.yashodaShed),
		ToParkID: rpPark, ToShedID: f.oldYashodaShed,
		DestinationShedName: "Old Yashoda",
		OccurredAt:          time.Now(), OutboxIdempotencyPrefix: "test-non-partitioned-move",
	})

	shedID, partition, ok := readGoatShedAndPartition(t, ctx, pool, goatID)
	if shedID != f.oldYashodaShed {
		t.Fatalf("goats.shed_id = %s, want the destination shed %s", shedID, f.oldYashodaShed)
	}
	if shedID == f.inactiveAliasShed {
		t.Fatalf("goats.shed_id resolved to the inactive alias location, must never happen")
	}
	// A non-partitioned destination writes the 'whole' sentinel (see oploc.WholeSentinel), which is
	// still a valid, expected row -- never a blank label and never a synthesized real partition name.
	if ok && partition != "whole" {
		t.Fatalf("goat_shed_partitions.partition_label = %q, want the 'whole' sentinel for a non-partitioned move", partition)
	}
}

// TestRelocateGoatsToShedInTxPartitionedToNonPartitioned: Castro Part 2 -> Yashoda.
func TestRelocateGoatsToShedInTxPartitionedToNonPartitioned(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	goatID := seedRelocateGoat(t, ctx, pool, f.castroShed)
	seedRelocateGoatPartition(t, ctx, pool, goatID, f.castroShed, "2", "Castro 2")

	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.castroShed),
		ToParkID: rpPark, ToShedID: f.yashodaShed,
		DestinationShedName: "Yashoda aa000004",
		OccurredAt:          time.Now(), OutboxIdempotencyPrefix: "test-partitioned-to-non-partitioned",
	})

	shedID, partition, ok := readGoatShedAndPartition(t, ctx, pool, goatID)
	if !ok {
		t.Fatalf("expected the goat's goat_shed_partitions row to be updated, not deleted")
	}
	if shedID != f.yashodaShed {
		t.Fatalf("goats.shed_id = %s, want the destination shed %s", shedID, f.yashodaShed)
	}
	if partition != "whole" {
		t.Fatalf("goat_shed_partitions.partition_label = %q, want the 'whole' sentinel -- Yashoda has no real partition, the old \"2\" must not survive the move", partition)
	}
}

// TestRelocateGoatsToShedInTxNonPartitionedToPartitioned: Yashoda -> Castro Part 1.
func TestRelocateGoatsToShedInTxNonPartitionedToPartitioned(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	goatID := seedRelocateGoat(t, ctx, pool, f.yashodaShed)

	relocate(t, ctx, repo, ports.RelocateGoatsCommand{
		TenantID: rpTenant, ActorID: rpActor, GoatIDs: []string{goatID},
		FromParkID: strp(rpPark), FromShedID: strp(f.yashodaShed),
		ToParkID: rpPark, ToShedID: f.castroShed,
		DestinationPartitionLabel: strp("1"), DestinationShedName: "Castro",
		OccurredAt: time.Now(), OutboxIdempotencyPrefix: "test-non-partitioned-to-partitioned",
	})

	shedID, partition, ok := readGoatShedAndPartition(t, ctx, pool, goatID)
	if !ok {
		t.Fatalf("expected a goat_shed_partitions row to now exist for the goat")
	}
	if shedID != f.castroShed {
		t.Fatalf("goats.shed_id = %s, want the destination shed %s", shedID, f.castroShed)
	}
	if partition != "1" {
		t.Fatalf("goat_shed_partitions.partition_label = %q, want \"1\"", partition)
	}
}
