package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const adminUITestTenant = "00000000-0000-4000-8000-000000000001"

func TestConfigFamilyRevisionLedgerBumpsAndFeedsBootstrapInputs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	before, err := repo.LoadContractFamilies(ctx, adminUITestTenant)
	if err != nil {
		t.Fatalf("LoadContractFamilies(before): %v", err)
	}
	if _, ok := before.RevisionInputs["admin-ui:locations"]; ok {
		t.Fatalf("locations revision should not be pre-populated before a config-family write: %#v", before.RevisionInputs)
	}

	bumpLocationRevision(t, ctx, pool)
	firstRevision := configFamilyRevision(t, ctx, pool, "locations")
	if firstRevision != 1 {
		t.Fatalf("first locations revision = %d, want 1", firstRevision)
	}
	assertConfigChangedOutbox(t, ctx, pool, "locations")

	after, err := repo.LoadContractFamilies(ctx, adminUITestTenant)
	if err != nil {
		t.Fatalf("LoadContractFamilies(after): %v", err)
	}
	if after.RevisionInputs["admin-ui:locations"] == "" {
		t.Fatalf("repository did not expose admin-ui locations revision input: %#v", after.RevisionInputs)
	}

	bumpLocationRevision(t, ctx, pool)
	secondRevision := configFamilyRevision(t, ctx, pool, "locations")
	if secondRevision <= firstRevision {
		t.Fatalf("locations revision did not increment: first=%d second=%d", firstRevision, secondRevision)
	}
}

func TestConfigFamilyRevisionCoalescesWithinTransaction(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	for i := 0; i < 3; i++ {
		bumpConfigFamilyInTransaction(t, ctx, pool, "locations", 4)

		revision := configFamilyRevision(t, ctx, pool, "locations")
		wantRevision := int64(i + 1)
		if revision != wantRevision {
			t.Fatalf("locations revision after coalesced tx %d = %d, want %d", i+1, revision, wantRevision)
		}

		count, idempotencyKey, coalescedCount := configChangedOutboxCountAndKey(t, ctx, pool, "locations", revision)
		if count != 1 {
			t.Fatalf("config.changed rows for locations revision %d = %d, want 1", revision, count)
		}
		wantKey := fmt.Sprintf("admin-ui-config:%s:locations:%d", adminUITestTenant, revision)
		if idempotencyKey != wantKey {
			t.Fatalf("config.changed idempotency key = %q, want %q", idempotencyKey, wantKey)
		}
		if coalescedCount != 4 {
			t.Fatalf("coalesced bump count = %d, want 4", coalescedCount)
		}
	}
}

func bumpLocationRevision(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tag, err := pool.Exec(ctx, `
UPDATE locations
SET updated_at = now()
WHERE location_id = (
  SELECT location_id
  FROM locations
  WHERE tenant_id = $1::uuid
    AND location_type = 'park'
    AND status = 'active'
  ORDER BY display_order, name, location_id
  LIMIT 1
)`, adminUITestTenant)
	if err != nil {
		t.Fatalf("bump location revision: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("bump location affected %d rows, want 1", tag.RowsAffected())
	}
}

func bumpConfigFamilyInTransaction(t *testing.T, ctx context.Context, pool *pgxpool.Pool, familyKey string, bumps int) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin config family transaction: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	for i := 0; i < bumps; i++ {
		if _, err := tx.Exec(ctx, `
	SELECT admin_ui_bump_config_family(
	  $1::uuid,
	  $2,
	  NULL,
	  'test.coalesce',
	  jsonb_build_object('test_bump', $3::int)
	)`, adminUITestTenant, familyKey, i+1); err != nil {
			t.Fatalf("queue config family bump %d: %v", i+1, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit config family transaction: %v", err)
	}
	committed = true
}

func configFamilyRevision(t *testing.T, ctx context.Context, pool *pgxpool.Pool, familyKey string) int64 {
	t.Helper()
	var revision int64
	if err := pool.QueryRow(ctx, `
SELECT revision
FROM admin_ui_config_family_revisions
WHERE tenant_id = $1::uuid
  AND family_key = $2`, adminUITestTenant, familyKey).Scan(&revision); err != nil {
		t.Fatalf("read family revision %s: %v", familyKey, err)
	}
	return revision
}

func configChangedOutboxCountAndKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, familyKey string, revision int64) (int, string, int) {
	t.Helper()
	var count int
	var idempotencyKey string
	var coalescedCount int
	if err := pool.QueryRow(ctx, `
	SELECT
	  count(*),
	  COALESCE(min(idempotency_key), ''),
	  COALESCE(max((payload->'metadata'->>'coalesced_change_count')::int), 0)
	FROM outbox_messages
	WHERE tenant_id = $1::uuid
	  AND event_type = 'config.changed'
	  AND payload->>'family_key' = $2
	  AND (payload->>'revision')::bigint = $3`, adminUITestTenant, familyKey, revision).Scan(&count, &idempotencyKey, &coalescedCount); err != nil {
		t.Fatalf("count config.changed outbox rows for family %s revision %d: %v", familyKey, revision, err)
	}
	return count, idempotencyKey, coalescedCount
}

func assertConfigChangedOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool, familyKey string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND event_type = 'config.changed'
  AND payload->>'family_key' = $2`, adminUITestTenant, familyKey).Scan(&count); err != nil {
		t.Fatalf("count config.changed outbox rows: %v", err)
	}
	if count == 0 {
		t.Fatalf("no config.changed outbox row emitted for family %s", familyKey)
	}
}
