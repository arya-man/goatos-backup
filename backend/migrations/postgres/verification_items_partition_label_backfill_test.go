package postgres

import (
	"context"
	"fmt"
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

	// The ADD COLUMN has already run when pgtest bootstraps the template DB (it's part of the
	// same migration file, applied once when the template is built), so re-running it here
	// would fail on a duplicate column. Extract only the backfill DO block itself -- the real
	// executable batching/commit logic under test -- prefixed with its own SET statements so
	// the lock/statement timeouts still apply exactly as they do in the real migration.
	upBlock := body[firstNoTx:downIdx]
	setIdx := strings.Index(upBlock, "SET lock_timeout")
	if setIdx < 0 {
		t.Fatalf("000127 migration missing backfill SET lock_timeout statement")
	}
	setEndMarker := "statement_timeout = '30s';"
	setEndIdx := strings.Index(upBlock, setEndMarker)
	if setEndIdx < 0 {
		t.Fatalf("000127 migration missing backfill SET statement_timeout statement")
	}
	setStatements := upBlock[setIdx : setEndIdx+len(setEndMarker)]

	doIdx := strings.Index(upBlock, "DO $$")
	if doIdx < 0 {
		t.Fatalf("000127 migration missing backfill DO $$ block")
	}
	return setStatements + "\n" + upBlock[doIdx:]
}

// runBackfill re-executes the migration's own backfill SQL against pool, using a dedicated
// autocommit connection since the migration is NO TRANSACTION and its DO block issues an
// internal COMMIT after every keyset batch (which is only legal outside an explicit BEGIN).
// Each top-level statement is sent as its OWN simple-query message: Postgres implicitly wraps
// an entire multi-statement simple-query STRING in one transaction block, which would make the
// DO block's internal per-batch COMMIT illegal ("invalid transaction termination") even on an
// otherwise-autocommit connection.
func runBackfill(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn for backfill: %v", err)
	}
	defer conn.Release()

	for _, stmt := range splitTopLevelStatements(sql) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("run backfill statement (%.60s...): %v", strings.TrimSpace(stmt), err)
		}
	}
}

// splitTopLevelStatements splits a small SQL script into top-level statements, treating a
// "DO $$ ... $$;" block as one indivisible statement (it contains its own internal semicolons).
func splitTopLevelStatements(sql string) []string {
	var stmts []string
	remaining := sql
	for {
		remaining = strings.TrimLeft(remaining, " \t\n")
		for strings.HasPrefix(remaining, "--") {
			if newline := strings.Index(remaining, "\n"); newline >= 0 {
				remaining = strings.TrimLeft(remaining[newline+1:], " \t\n")
				continue
			}
			remaining = ""
			break
		}
		if remaining == "" {
			break
		}
		if strings.HasPrefix(remaining, "CREATE OR REPLACE PROCEDURE") {
			start := strings.Index(remaining, "$$")
			end := -1
			if start >= 0 {
				end = strings.Index(remaining[start+2:], "$$;")
				if end >= 0 {
					end += start + 2
				}
			}
			if end < 0 {
				stmts = append(stmts, remaining)
				break
			}
			end += len("$$;")
			stmts = append(stmts, remaining[:end])
			remaining = remaining[end:]
			continue
		}
		if strings.HasPrefix(remaining, "DO $$") {
			end := strings.Index(remaining, "$$;")
			if end < 0 {
				stmts = append(stmts, remaining)
				break
			}
			end += len("$$;")
			stmts = append(stmts, remaining[:end])
			remaining = remaining[end:]
			continue
		}
		semi := strings.Index(remaining, ";")
		if semi < 0 {
			stmts = append(stmts, remaining)
			break
		}
		stmts = append(stmts, remaining[:semi+1])
		remaining = remaining[semi+1:]
	}
	return stmts
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

// TestBackfillPartitionLabelDoesNotStrandEligibleRowsBehindUnmatchedOnes_RealPostgres is the
// regression for the early-exit defect: the candidate set used to select rows purely on
// "partition_label IS NULL", including rows that can never match goat_shed_partitions (a goat
// that moved -- allowed to stay NULL by design). Those rows are re-selected at the head of
// EVERY batch, never update, and drive rows_updated to 0, which exits the loop while eligible
// rows with higher item_ids are still unprocessed. In production a handful of old moved-goat
// rows at the front would strand the rest of the verifier queue.
//
// item_id is a random uuid, so insertion order is NOT batch order. The ids are pinned
// explicitly below so the unmatched rows are guaranteed to occupy the whole first batch --
// otherwise the shape under test is only reproduced by luck.
func TestBackfillPartitionLabelDoesNotStrandEligibleRowsBehindUnmatchedOnes_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	backfillSQL := extractBackfillSQL(t)

	tenantID := seedBackfillTenant(t, ctx, pool)
	oldShedID := seedBackfillShed(t, ctx, pool, tenantID, "Stranding Old Shed")
	newShedID := seedBackfillShed(t, ctx, pool, tenantID, "Stranding New Shed")

	// 501 UNMATCHED items (> batch_size 500), pinned to the LOWEST item_ids so they fill the
	// entire first batch. Each goat lives in newShedID but its item points at oldShedID, so the
	// shed-matched join can never satisfy them.
	const unmatchedCount = 501
	for i := 0; i < unmatchedCount; i++ {
		goatID := seedBackfillGoat(t, ctx, pool, tenantID, newShedID)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2', 'Stranding New Shed 2')
`, tenantID, goatID, newShedID); err != nil {
			t.Fatalf("insert goat_shed_partitions (unmatched %d): %v", i, err)
		}
		itemID := seedPendingVaccinationVerificationItem(t, ctx, pool, tenantID, goatID, oldShedID,
			fmt.Sprintf("backfill-test:strand-unmatched-%d", i))
		if _, err := pool.Exec(ctx,
			`UPDATE verification_items SET item_id = $2::uuid WHERE item_id = $1::uuid`,
			itemID, fmt.Sprintf("00000000-0000-4000-8000-%012d", i)); err != nil {
			t.Fatalf("pin unmatched item_id %d: %v", i, err)
		}
	}

	// 3 ELIGIBLE items pinned to the HIGHEST item_ids, so they sort strictly after every
	// unmatched row. Under the old candidate set these are never reached.
	eligible := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		goatID := seedBackfillGoat(t, ctx, pool, tenantID, newShedID)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '9', 'Stranding New Shed 9')
`, tenantID, goatID, newShedID); err != nil {
			t.Fatalf("insert goat_shed_partitions (eligible %d): %v", i, err)
		}
		itemID := seedPendingVaccinationVerificationItem(t, ctx, pool, tenantID, goatID, newShedID,
			fmt.Sprintf("backfill-test:strand-eligible-%d", i))
		pinned := fmt.Sprintf("ffffffff-ffff-4fff-8fff-%012d", i)
		if _, err := pool.Exec(ctx,
			`UPDATE verification_items SET item_id = $2::uuid WHERE item_id = $1::uuid`,
			itemID, pinned); err != nil {
			t.Fatalf("pin eligible item_id %d: %v", i, err)
		}
		eligible = append(eligible, pinned)
	}

	runBackfill(t, ctx, pool, backfillSQL)

	for i, itemID := range eligible {
		var label *string
		if err := pool.QueryRow(ctx,
			`SELECT partition_label FROM verification_items WHERE item_id = $1::uuid`, itemID).Scan(&label); err != nil {
			t.Fatalf("read eligible item %d: %v", i, err)
		}
		if label == nil || *label != "9" {
			t.Fatalf("eligible item %d got partition_label = %v, want \"9\" -- %d unmatched rows ahead of it stranded the backfill",
				i, label, unmatchedCount)
		}
	}

	// The unmatched rows must still be NULL: staying NULL is correct, being skipped is not.
	var stillNull int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM verification_items
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND partition_label IS NULL
`, tenantID, oldShedID).Scan(&stillNull); err != nil {
		t.Fatalf("count unmatched: %v", err)
	}
	if stillNull != unmatchedCount {
		t.Fatalf("unmatched rows with NULL partition_label = %d, want %d", stillNull, unmatchedCount)
	}
}
