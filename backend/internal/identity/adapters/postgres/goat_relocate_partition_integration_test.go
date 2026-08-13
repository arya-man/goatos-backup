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
// OperationalLocation = real shed residence (backend/internal/platform/oploc). When a named shed
// is partitioned, goats.shed_id names the exact partition shed and goats.shed_group_id names the
// parent/group shed. goat_shed_partitions carries the group+partition bridge. These tests prove that
// RelocateGoatsToShedInTx keeps the exact residence and group bridge in sync, atomically, across
// every movement shape: same-shed partition move, cross-shed partition move, non-partitioned move,
// and the two mixed transitions
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
	castroPart1Pen, castroPart2Pen, gandhiPart3Pen                         string
}

// seedRelocatePartitionFixture seeds the sheds via gen_random_uuid() (each test runs against its
// own StartPostgres clone, so there is no cross-test id collision to avoid) and returns the ids.
func seedRelocatePartitionFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) relocatePartitionFixture {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, status)
VALUES ($1::uuid, 'k2', 'K2', 'active')
ON CONFLICT (tenant_id, stage_code) DO UPDATE SET status='active'`, rpTenant); err != nil {
		t.Fatalf("seed animal stage lookup: %v", err)
	}
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
	f.castroPart1Pen = seedPartitionOperationalLocation(t, ctx, pool, f.castroShed, "1", "Castro 1")
	f.castroPart2Pen = seedPartitionOperationalLocation(t, ctx, pool, f.castroShed, "2", "Castro 2")
	f.gandhiPart3Pen = seedPartitionOperationalLocation(t, ctx, pool, f.gandhiShed, "3", "Gandhi 3")
	return f
}

func seedPartitionOperationalLocation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, partitionLabel, partitionShedName string) string {
	t.Helper()
	var partitionShedID string
	if err := pool.QueryRow(ctx, `
	INSERT INTO locations (tenant_id, location_type, name, parent_location_id, status)
	SELECT $1::uuid, 'shed', $2, parent_location_id, 'active'
	FROM locations
	WHERE tenant_id = $1::uuid AND location_id = $3::uuid
	RETURNING location_id::text`, rpTenant, partitionShedName, shedID).Scan(&partitionShedID); err != nil {
		t.Fatalf("seed partition shed %s: %v", partitionShedName, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (
  tenant_id, shed_id, partition_label, normalized_label, status, source, operational_location_id
) VALUES (
  $1::uuid, $2::uuid, $3, regexp_replace(lower(btrim($3)), '^part[[:space:]]+', ''), 'active', 'manual', $4::uuid
	)`, rpTenant, shedID, partitionLabel, partitionShedID); err != nil {
		t.Fatalf("seed shed partition %s: %v", partitionShedName, err)
	}
	return partitionShedID
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
	var exactShedID string
	if err := pool.QueryRow(ctx, `
SELECT operational_location_id::text
FROM shed_partitions
WHERE tenant_id = $1::uuid
  AND shed_id = $2::uuid
  AND partition_label = $3
  AND status = 'active'`,
		rpTenant, shedID, partitionLabel).Scan(&exactShedID); err != nil {
		t.Fatalf("resolve partition exact shed %s: %v", partitionLabel, err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET shed_id = $3::uuid,
    current_location_id = $3::uuid,
    shed_group_id = $4::uuid
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		rpTenant, goatID, exactShedID, shedID); err != nil {
		t.Fatalf("move seeded goat to exact partition shed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)`,
		rpTenant, goatID, shedID, partitionLabel, sourceShedName); err != nil {
		t.Fatalf("seed goat_shed_partitions for %s: %v", goatID, err)
	}
}

func readGoatShedAndPartition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) (shedID, partitionLabel string, hasPartitionRow bool) {
	t.Helper()
	var currentShed, shedGroup string
	if err := pool.QueryRow(ctx, `SELECT shed_id::text, COALESCE(shed_group_id::text, '') FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		rpTenant, goatID).Scan(&currentShed, &shedGroup); err != nil {
		t.Fatalf("read goat shed_id: %v", err)
	}
	var gspShed, label string
	err := pool.QueryRow(ctx, `SELECT shed_id::text, partition_label FROM goat_shed_partitions WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		rpTenant, goatID).Scan(&gspShed, &label)
	if err != nil {
		return currentShed, "", false
	}
	if label == "whole" {
		if shedGroup != "" {
			t.Fatalf("goats.shed_group_id=%s for whole-shed goat %s, want empty", shedGroup, goatID)
		}
		if gspShed != currentShed {
			t.Fatalf("whole goat_shed_partitions.shed_id (%s) disagrees with exact goats.shed_id (%s) for goat %s", gspShed, currentShed, goatID)
		}
	} else if gspShed != shedGroup {
		t.Fatalf("goat_shed_partitions.shed_id (%s) disagrees with goats.shed_group_id (%s) for goat %s -- group bridge must move atomically", gspShed, shedGroup, goatID)
	}
	return currentShed, label, true
}

func readGoatCurrentLocation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) string {
	t.Helper()
	var currentLocationID string
	if err := pool.QueryRow(ctx, `SELECT current_location_id::text FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		rpTenant, goatID).Scan(&currentLocationID); err != nil {
		t.Fatalf("read goat current_location_id: %v", err)
	}
	return currentLocationID
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
		FromParkID: strp(rpPark), FromShedID: strp(f.castroPart1Pen),
		ToParkID: rpPark, ToShedID: f.castroShed,
		DestinationPartitionLabel: strp("2"), DestinationShedName: "Castro",
		OccurredAt: time.Now(), OutboxIdempotencyPrefix: "test-same-shed-partition-move",
	})

	shedID, partition, ok := readGoatShedAndPartition(t, ctx, pool, goatID)
	if !ok {
		t.Fatalf("expected a goat_shed_partitions row after the move")
	}
	if shedID != f.castroPart2Pen {
		t.Fatalf("goats.shed_id = %s, want exact partition shed %s (same-shed partition move)", shedID, f.castroPart2Pen)
	}
	if shedID == f.inactiveAliasShed {
		t.Fatalf("goats.shed_id resolved to the inactive alias location, must never happen")
	}
	if partition != "2" {
		t.Fatalf("goat_shed_partitions.partition_label = %q, want \"2\"", partition)
	}
	if current := readGoatCurrentLocation(t, ctx, pool, goatID); current != f.castroPart2Pen {
		t.Fatalf("current_location_id = %s, want exact partition shed %s", current, f.castroPart2Pen)
	}
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
		FromParkID: strp(rpPark), FromShedID: strp(f.castroPart1Pen),
		ToParkID: rpPark, ToShedID: f.gandhiShed,
		DestinationPartitionLabel: strp("3"), DestinationShedName: "Gandhi",
		DestinationTag: "k2",
		OccurredAt:     time.Now(), OutboxIdempotencyPrefix: "test-cross-shed-partition-move",
	})

	shedID, partition, ok := readGoatShedAndPartition(t, ctx, pool, goatID)
	if !ok {
		t.Fatalf("expected a goat_shed_partitions row after the move")
	}
	if shedID != f.gandhiPart3Pen {
		t.Fatalf("goats.shed_id = %s, want exact destination partition shed %s", shedID, f.gandhiPart3Pen)
	}
	if shedID == f.inactiveAliasShed {
		t.Fatalf("goats.shed_id resolved to the inactive alias location, must never happen")
	}
	if partition != "3" {
		t.Fatalf("goat_shed_partitions.partition_label = %q, want \"3\"", partition)
	}
	if current := readGoatCurrentLocation(t, ctx, pool, goatID); current != f.gandhiPart3Pen {
		t.Fatalf("current_location_id = %s, want exact partition shed %s", current, f.gandhiPart3Pen)
	}
	var locationEventShedID, stageIdentityShedID, stageOutboxScopeShedID, stageOutboxPayloadShedID string
	if err := pool.QueryRow(ctx, `
SELECT payload->'payload'->>'to_shed_id'
FROM outbox_messages
WHERE tenant_id=$1::uuid AND aggregate_id=$2::uuid AND event_type='goat.location.changed'
ORDER BY created_at DESC
LIMIT 1`, rpTenant, goatID).Scan(&locationEventShedID); err != nil {
		t.Fatalf("read goat.location.changed outbox: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT payload->>'current_shed_id'
FROM goat_identity_events
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND event_type='goat.stage_changed'
ORDER BY recorded_at DESC
LIMIT 1`, rpTenant, goatID).Scan(&stageIdentityShedID); err != nil {
		t.Fatalf("read goat.stage_changed identity event: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT payload->'visibility_scope'->>'shed_id', payload->'payload'->>'current_shed_id'
FROM outbox_messages
WHERE tenant_id=$1::uuid AND aggregate_id=$2::uuid AND event_type='goat.stage_changed'
ORDER BY created_at DESC
LIMIT 1`, rpTenant, goatID).Scan(&stageOutboxScopeShedID, &stageOutboxPayloadShedID); err != nil {
		t.Fatalf("read goat.stage_changed outbox: %v", err)
	}
	for label, got := range map[string]string{
		"goat.location.changed payload.to_shed_id":            locationEventShedID,
		"goat.stage_changed identity payload.current_shed_id": stageIdentityShedID,
		"goat.stage_changed outbox visibility_scope.shed_id":  stageOutboxScopeShedID,
		"goat.stage_changed outbox payload.current_shed_id":   stageOutboxPayloadShedID,
	} {
		if got != f.gandhiPart3Pen {
			t.Fatalf("%s=%s, want exact partition shed %s", label, got, f.gandhiPart3Pen)
		}
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
	if current := readGoatCurrentLocation(t, ctx, pool, goatID); current != f.oldYashodaShed {
		t.Fatalf("current_location_id = %s, want non-partitioned shed %s", current, f.oldYashodaShed)
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
		FromParkID: strp(rpPark), FromShedID: strp(f.castroPart2Pen),
		ToParkID: rpPark, ToShedID: f.yashodaShed,
		DestinationShedName: "Yashoda aa000004",
		OccurredAt:          time.Now(), OutboxIdempotencyPrefix: "test-partitioned-to-non-partitioned",
	})

	shedID, partition, ok := readGoatShedAndPartition(t, ctx, pool, goatID)
	if shedID != f.yashodaShed {
		t.Fatalf("goats.shed_id = %s, want the destination shed %s", shedID, f.yashodaShed)
	}
	if ok && partition != "whole" {
		t.Fatalf("goat_shed_partitions.partition_label = %q, want no compatibility row or the 'whole' sentinel -- Yashoda has no partition, the old \"2\" must not survive the move", partition)
	}
	if current := readGoatCurrentLocation(t, ctx, pool, goatID); current != f.yashodaShed {
		t.Fatalf("current_location_id = %s, want non-partitioned shed %s", current, f.yashodaShed)
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
	if shedID != f.castroPart1Pen {
		t.Fatalf("goats.shed_id = %s, want exact destination partition shed %s", shedID, f.castroPart1Pen)
	}
	if partition != "1" {
		t.Fatalf("goat_shed_partitions.partition_label = %q, want \"1\"", partition)
	}
	if current := readGoatCurrentLocation(t, ctx, pool, goatID); current != f.castroPart1Pen {
		t.Fatalf("current_location_id = %s, want exact partition shed %s", current, f.castroPart1Pen)
	}
}
