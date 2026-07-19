package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestForwardCompatDedupKeepsEarliest guards the R50-015 P0 fix in migration
// 000004_r50_forward_compat_concurrent_indexes.sql. The dedup that runs before the widened
// UNIQUE INDEX must keep the EARLIEST (tenant_id, idempotency_key) row per group and delete every
// later duplicate. The original SQL used NOT EXISTS, which selected the earliest row itself for
// deletion and left the later duplicates — so 3 dupes left 2 and the CREATE UNIQUE INDEX
// CONCURRENTLY still failed with 42P10. The migration-apply harness starts from a CLEAN db, so it
// never exercises this path.
//
// The dedup predicate is exercised on a structural clone of outbox_messages (the real table has
// FK/trigger machinery that rejects synthetic rows); what is under test is the keep-earliest SQL
// logic itself — deleting the wrong row is what caused the 42P10.
func TestForwardCompatDedupKeepsEarliest(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
CREATE TEMP TABLE dedup_probe (
  outbox_id uuid PRIMARY KEY,
  tenant_id uuid NOT NULL,
  event_type text NOT NULL,
  idempotency_key text,
  created_at timestamptz NOT NULL
)`); err != nil {
		t.Fatalf("create temp: %v", err)
	}

	tenant := uuid.NewString()
	key := "verification:item:dupe-1"
	base := time.Now().Add(-1 * time.Hour)
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	// Three duplicates of the same (tenant, idempotency_key), ascending created_at. ids[0]=earliest.
	for i, id := range ids {
		if _, err := pool.Exec(ctx, `
INSERT INTO dedup_probe (outbox_id, tenant_id, event_type, idempotency_key, created_at)
VALUES ($1, $2, 'verification.item.closed', $3, $4)`,
			id, tenant, key, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("insert dupe %d: %v", i, err)
		}
	}
	// A non-covered event type with the same key must NEVER be touched.
	other := uuid.NewString()
	if _, err := pool.Exec(ctx, `
INSERT INTO dedup_probe (outbox_id, tenant_id, event_type, idempotency_key, created_at)
VALUES ($1, $2, 'some.other.event', $3, $4)`, other, tenant, key, base); err != nil {
		t.Fatalf("insert non-covered: %v", err)
	}

	// The corrected dedup (mirrors migration 000004): a row is a duplicate-to-delete iff an EARLIER
	// covered row EXISTS; the earliest is kept.
	if _, err := pool.Exec(ctx, `
WITH duplicate_rows AS (
  SELECT dupe.outbox_id
  FROM dedup_probe dupe
  WHERE dupe.idempotency_key IS NOT NULL
    AND dupe.created_at > now() - '7 days'::interval
    AND dupe.event_type = ANY (ARRAY['verification.item.pending','verification.verdict.approved','verification.verdict.rework','verification.item.closed'])
    AND EXISTS (
      SELECT 1 FROM dedup_probe keep
      WHERE keep.tenant_id = dupe.tenant_id AND keep.idempotency_key = dupe.idempotency_key
        AND keep.idempotency_key IS NOT NULL
        AND keep.event_type = ANY (ARRAY['verification.item.pending','verification.verdict.approved','verification.verdict.rework','verification.item.closed'])
        AND (keep.created_at, keep.outbox_id) < (dupe.created_at, dupe.outbox_id)
    )
  LIMIT 10000
)
DELETE FROM dedup_probe WHERE outbox_id IN (SELECT outbox_id FROM duplicate_rows)`); err != nil {
		t.Fatalf("dedup: %v", err)
	}

	var covered int
	var survivorID string
	if err := pool.QueryRow(ctx, `SELECT count(*), max(outbox_id::text) FROM dedup_probe WHERE tenant_id=$1 AND idempotency_key=$2 AND event_type='verification.item.closed'`, tenant, key).Scan(&covered, &survivorID); err != nil {
		t.Fatalf("count covered: %v", err)
	}
	if covered != 1 {
		t.Fatalf("after dedup %d covered rows remain, want exactly 1 (the earliest) — a UNIQUE index would fail otherwise", covered)
	}
	if survivorID != ids[0] {
		t.Fatalf("survivor = %s, want the earliest %s", survivorID, ids[0])
	}
	var otherCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dedup_probe WHERE event_type='some.other.event'`).Scan(&otherCount); err != nil {
		t.Fatalf("count other: %v", err)
	}
	if otherCount != 1 {
		t.Fatalf("non-covered event was touched (%d remain, want 1)", otherCount)
	}
}
