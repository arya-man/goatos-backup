package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

func TestReleaseBatchReconcileRemaindersReleasesOnlyRecordedExcess(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const tenantID = "00000000-0000-4000-8000-000000000001"
	const locationID = "00000000-0000-4000-8000-000000003001"
	const itemID = "d1000000-0000-4000-8000-000000000001"
	const lotID = "d1000000-0000-4000-8000-000000000002"
	const batchID = "d1000000-0000-4000-8000-000000000003"

	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.reconcile", Name: "Reconcile", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-REC', 'Reconcile vaccine', 'vaccine', 'dose')`, itemID, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 2, 'dose', DATE '2026-12-31')`, lotID, tenantID, itemID, locationID); err != nil {
		t.Fatalf("seed stock: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (
  batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status,
  estimated_targets, context
) VALUES (
  $1, $2, $3, 'park', $4, 'planned', 1,
  '{"defer_repair":{"state":"stock_reconcile_required","release_qty":1,"reason":"test"}}'::jsonb
)`, batchID, tenantID, versionID, locationID); err != nil {
		t.Fatalf("seed batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO inventory_stock_movements (
  tenant_id, lot_id, item_id, location_id, movement_type, quantity,
  quantity_unit, batch_id, reason, idempotency_key
) VALUES (
  $1, $2, $3, $4, 'reserve', 2, 'dose', $5, 'batch reserve', 'reconcile-reserve'
)`, tenantID, lotID, itemID, locationID, batchID); err != nil {
		t.Fatalf("seed reserve movement: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	summary, err := repo.ReleaseBatchReconcileRemainders(ctx, tenantID, 10)
	if err != nil {
		t.Fatalf("ReleaseBatchReconcileRemainders: %v", err)
	}
	if summary.Batches != 1 || summary.Movements != 1 || summary.Released != 1 {
		t.Fatalf("summary=%+v, want batches=1 movements=1 released=1", summary)
	}
	if got := scanInventoryText(t, ctx, pool, `SELECT quantity_reserved::text FROM inventory_stock WHERE tenant_id=$1 AND stock_id=$2`, tenantID, lotID); got != "1" {
		t.Fatalf("quantity_reserved=%q, want 1", got)
	}
	if got := scanInventoryText(t, ctx, pool, `
SELECT context #>> '{defer_repair,state}'
FROM obligation_batches
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "stock_reconciled" {
		t.Fatalf("repair state=%q, want stock_reconciled", got)
	}
	if got := scanInventoryText(t, ctx, pool, `
SELECT count(*)::text
FROM inventory_stock_movements
WHERE tenant_id=$1 AND batch_id=$2 AND movement_type='release'`, tenantID, batchID); got != "1" {
		t.Fatalf("release movements=%q, want 1", got)
	}

	replay, err := repo.ReleaseBatchReconcileRemainders(ctx, tenantID, 10)
	if err != nil {
		t.Fatalf("ReleaseBatchReconcileRemainders replay: %v", err)
	}
	if replay.Batches != 0 || replay.Movements != 0 || replay.Released != 0 {
		t.Fatalf("replay summary=%+v, want zero", replay)
	}

	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches
SET context = context || '{"shift_repair":{"state":"stock_reconcile_required","release_qty":1,"reason":"second_repair_same_lot"}}'::jsonb,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); err != nil {
		t.Fatalf("mark second repair: %v", err)
	}
	second, err := repo.ReleaseBatchReconcileRemainders(ctx, tenantID, 10)
	if err != nil {
		t.Fatalf("ReleaseBatchReconcileRemainders second repair: %v", err)
	}
	if second.Batches != 1 || second.Movements != 1 || second.Released != 1 {
		t.Fatalf("second summary=%+v, want batches=1 movements=1 released=1", second)
	}
	if got := scanInventoryText(t, ctx, pool, `SELECT quantity_reserved::text FROM inventory_stock WHERE tenant_id=$1 AND stock_id=$2`, tenantID, lotID); got != "0" {
		t.Fatalf("quantity_reserved after second repair=%q, want 0", got)
	}
	if got := scanInventoryText(t, ctx, pool, `
SELECT count(*)::text
FROM inventory_stock_movements
WHERE tenant_id=$1 AND batch_id=$2 AND movement_type='release'`, tenantID, batchID); got != "2" {
		t.Fatalf("release movements after second repair=%q, want 2", got)
	}
}

func scanInventoryText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var out string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
		t.Fatalf("scan text: %v", err)
	}
	return out
}
