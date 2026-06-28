package postgres

import (
	"context"
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
