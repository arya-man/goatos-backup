package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// extractBackfillSQL pulls the executable body of the 000127 migration's Up block (everything
// after the header comments, from the first SET statement through the DO $$ ... $$ backfill,
// stopping before "-- +goose Down") straight out of the CURRENT migration file on disk. This
// proves the test exercises the real migration SQL rather than a hand-copied duplicate that can
// silently drift from the file it is supposed to guard.
func extractBackfillSQL(t *testing.T) string {
	t.Helper()
	_, body := onlyMigrationWithSuffix(t, "verification_items_partition_label")

	upMarker := "-- +goose NO TRANSACTION"
	downMarker := "-- +goose Down"

	firstNoTx := strings.Index(body, upMarker)
	if firstNoTx < 0 {
		t.Fatalf("000127 migration missing %q marker", upMarker)
	}
	downIdx := strings.Index(body, downMarker)
	if downIdx < 0 || downIdx < firstNoTx {
		t.Fatalf("000127 migration missing %q marker after Up block", downMarker)
	}

	// Skip past the "-- +goose Up" / "-- +goose NO TRANSACTION" directive lines themselves; the
	// ADD COLUMN has already run when pgtest bootstraps the template DB, so re-running it here
	// would fail on a duplicate column. Start from the backfill's own SET statements.
	upBlock := body[firstNoTx:downIdx]
	setIdx := strings.Index(upBlock, "SET lock_timeout")
	if setIdx < 0 {
		t.Fatalf("000127 migration missing backfill SET lock_timeout statement")
	}
	return upBlock[setIdx:]
}

// runBackfill re-executes the migration's own backfill SQL against pool, using a dedicated
// autocommit connection since the migration is NO TRANSACTION and its DO block issues an
// internal COMMIT after every keyset batch (which is only legal outside an explicit BEGIN).
func runBackfill(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn for backfill: %v", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("run backfill SQL: %v", err)
	}
}

func seedBackfillTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var tenantID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), $1, 'active') RETURNING tenant_id::text`,
		"partition-backfill-test-tenant",
	).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return tenantID
}

func seedBackfillShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, name string) string {
	t.Helper()
	var shedID string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', $2, 'active')
RETURNING location_id::text
`, tenantID, name).Scan(&shedID); err != nil {
		t.Fatalf("insert shed %s: %v", name, err)
	}
	return shedID
}

func seedBackfillGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, shedID string) string {
	t.Helper()
	var partyID string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES (gen_random_uuid(), 'org', 'Backfill Test Custodian', 'active')
RETURNING party_id::text
`).Scan(&partyID); err != nil {
		t.Fatalf("insert custodian party: %v", err)
	}

	var goatID string
	if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, species, sex, lifecycle_status, custodian_party_id, shed_id)
VALUES ($1::uuid, 'goat', 'female', 'alive', $2::uuid, $3::uuid)
RETURNING goat_id::text
`, tenantID, partyID, shedID).Scan(&goatID); err != nil {
		t.Fatalf("insert goat: %v", err)
	}
	return goatID
}

func seedPendingVaccinationVerificationItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, goatID, shedID, idemKey string) string {
	t.Helper()
	var itemID string
	if err := pool.QueryRow(ctx, `
INSERT INTO verification_items (
    tenant_id, vertical, module, category, source_module, source_ref_type, source_ref_id,
    shed_id, captured_at, idempotency_key
) VALUES (
    $1::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', 'vaccination_goat', $2::uuid,
    $3::uuid, now(), $4
)
RETURNING item_id::text
`, tenantID, goatID, shedID, idemKey).Scan(&itemID); err != nil {
		t.Fatalf("insert verification item: %v", err)
	}
	return itemID
}

// TestBackfillPartitionLabelRequiresShedMatch_RealPostgres is the GOS-PR31-1 regression: a goat
// that moved sheds after its verification item was created must NOT have the item stapled with
// the goat's CURRENT shed's partition. The item keeps the OLD shed_id it was raised against, so
// only a goat_shed_partitions row for that SAME shed may supply partition_label.
func TestBackfillPartitionLabelRequiresShedMatch_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	backfillSQL := extractBackfillSQL(t)

	tenantID := seedBackfillTenant(t, ctx, pool)
	oldShedID := seedBackfillShed(t, ctx, pool, tenantID, "Old Shed")
	newShedID := seedBackfillShed(t, ctx, pool, tenantID, "New Shed")

	// Goat's CURRENT shed is newShedID; the goat_shed_partitions row (source of "current"
	// partition truth) reports the current shed and partition.
	goatID := seedBackfillGoat(t, ctx, pool, tenantID, newShedID)
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '5', 'New Shed 5')
`, tenantID, goatID, newShedID); err != nil {
		t.Fatalf("insert goat_shed_partitions: %v", err)
	}

	// The verification item was raised while the goat was still in oldShedID, and it was never
	// backfilled (partition_label NULL). This models a pending item on a goat that has since
	// moved.
	movedItemID := seedPendingVaccinationVerificationItem(t, ctx, pool, tenantID, goatID, oldShedID, "backfill-test:moved-goat")

	// A second item, raised against the goat's CURRENT shed, should legitimately receive the
	// current partition -- proving the fix does not just make the join match nothing.
	currentShedGoatID := seedBackfillGoat(t, ctx, pool, tenantID, newShedID)
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '7', 'New Shed 7')
`, tenantID, currentShedGoatID, newShedID); err != nil {
		t.Fatalf("insert goat_shed_partitions (unmoved goat): %v", err)
	}
	unmovedItemID := seedPendingVaccinationVerificationItem(t, ctx, pool, tenantID, currentShedGoatID, newShedID, "backfill-test:unmoved-goat")

	runBackfill(t, ctx, pool, backfillSQL)

	var movedLabel *string
	if err := pool.QueryRow(ctx, `SELECT partition_label FROM verification_items WHERE item_id = $1::uuid`, movedItemID).Scan(&movedLabel); err != nil {
		t.Fatalf("read moved item: %v", err)
	}
	if movedLabel != nil {
		t.Fatalf("moved-goat verification item got partition_label = %q, want NULL (shed mismatch must not backfill)", *movedLabel)
	}

	var unmovedLabel *string
	if err := pool.QueryRow(ctx, `SELECT partition_label FROM verification_items WHERE item_id = $1::uuid`, unmovedItemID).Scan(&unmovedLabel); err != nil {
		t.Fatalf("read unmoved item: %v", err)
	}
	if unmovedLabel == nil || *unmovedLabel != "7" {
		t.Fatalf("unmoved-goat verification item got partition_label = %v, want \"7\"", unmovedLabel)
	}
}

// TestBackfillPartitionLabelKeysetBatchesAndCommits_RealPostgres is the GOS-PR31-2 regression:
// the migration must actually batch (process rows in bounded chunks, not a single unbounded
// UPDATE) and each batch must independently COMMIT rather than deferring everything to one
// giant transaction. It seeds enough rows to force multiple keyset batches at the migration's
// documented batch_size (500) and asserts every eligible row still converges correctly.
func TestBackfillPartitionLabelKeysetBatchesAndCommits_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	backfillSQL := extractBackfillSQL(t)
	if !strings.Contains(backfillSQL, "batch_size") || !strings.Contains(backfillSQL, "LIMIT batch_size") {
		t.Fatalf("000127 backfill no longer keyset-batches (missing batch_size/LIMIT) -- comment and code must agree")
	}
	if !strings.Contains(backfillSQL, "COMMIT;") {
		t.Fatalf("000127 backfill no longer commits per batch -- a single unbounded transaction can hold broad locks / hit statement_timeout")
	}

	tenantID := seedBackfillTenant(t, ctx, pool)
	shedID := seedBackfillShed(t, ctx, pool, tenantID, "Batch Shed")

	const rowCount = 1200 // > 2x the migration's batch_size=500, forcing >=3 batches
	itemIDs := make([]string, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		goatID := seedBackfillGoat(t, ctx, pool, tenantID, shedID)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '9', 'Batch Shed 9')
`, tenantID, goatID, shedID); err != nil {
			t.Fatalf("insert goat_shed_partitions[%d]: %v", i, err)
		}
		itemID := seedPendingVaccinationVerificationItem(t, ctx, pool, tenantID, goatID, shedID, "backfill-test:batch:"+goatID)
		itemIDs = append(itemIDs, itemID)
	}

	start := time.Now()
	runBackfill(t, ctx, pool, backfillSQL)
	elapsed := time.Since(start)
	t.Logf("backfill of %d rows across keyset batches took %s", rowCount, elapsed)

	var backfilledCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM verification_items
WHERE tenant_id = $1::uuid AND source_module = 'vaccination' AND partition_label = '9'
`, tenantID).Scan(&backfilledCount); err != nil {
		t.Fatalf("count backfilled rows: %v", err)
	}
	if backfilledCount != rowCount {
		t.Fatalf("backfilled rows = %d, want %d (some rows left behind by keyset batching)", backfilledCount, rowCount)
	}
}
