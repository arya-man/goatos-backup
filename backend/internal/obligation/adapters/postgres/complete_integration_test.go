package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestSM5aMarkCompleted(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool) // scheduled obligation for testGoatID
	repo := NewRepository(pool, 5*time.Second)

	ok, err := repo.MarkCompleted(ctx, tenantID, obA)
	if err != nil || !ok {
		t.Fatalf("mark completed: ok=%v err=%v", ok, err)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "completed" {
		t.Fatalf("status: want completed, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='completed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 completed event, got %d", got)
	}

	ok2, err := repo.MarkCompleted(ctx, tenantID, obA)
	if err != nil {
		t.Fatalf("re-complete: %v", err)
	}
	if ok2 {
		t.Fatalf("re-complete should be a no-op (already terminal)")
	}
}

func TestMarkMissedBeforeMaterializesCanonicalStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	obB, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-missed-terminal-control", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obB: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='completed' WHERE obligation_id=$1`, obB); err != nil {
		t.Fatalf("complete obB: %v", err)
	}
	obC, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC), Status: "in_progress",
		IdempotencyKey: "obl-missed-in-progress-control", Sequence: 2,
	})
	if err != nil || !applied {
		t.Fatalf("insert obC: applied=%v err=%v", applied, err)
	}

	n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("mark missed: %v", err)
	}
	if n != 2 {
		t.Fatalf("marked missed = %d, want 2", n)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "missed" {
		t.Fatalf("obA status: want missed, got %s", got)
	}
	if got := scanStatus(t, ctx, pool, obB); got != "completed" {
		t.Fatalf("obB status: want completed, got %s", got)
	}
	if got := scanStatus(t, ctx, pool, obC); got != "missed" {
		t.Fatalf("obC status: want missed, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 missed event, got %d", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='obligation.missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 missed outbox event, got %d", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 missed audit event, got %d", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.missed'`,
		tenantID, obC); got != 1 {
		t.Fatalf("expected 1 in-progress missed audit event, got %d", got)
	}

	replay, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("replay mark missed: %v", err)
	}
	if replay != 0 {
		t.Fatalf("replay marked missed = %d, want 0", replay)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("replay missed audit event count = %d, want 1", got)
	}
}

func TestMarkCompletedAllowsLateMissedObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	if n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100); err != nil || n != 1 {
		t.Fatalf("mark missed: n=%d err=%v", n, err)
	}

	ok, err := repo.MarkCompleted(ctx, tenantID, obA)
	if err != nil || !ok {
		t.Fatalf("late complete missed obligation: ok=%v err=%v", ok, err)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "completed" {
		t.Fatalf("status: want completed, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("missed event count = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='completed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("completed event count = %d, want 1", got)
	}
}

func TestMarkMissedBeforeSkipsDeferredObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='deferred' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obA); err != nil {
		t.Fatalf("defer obligation: %v", err)
	}
	repo := NewRepository(pool, 5*time.Second)
	n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("mark missed: %v", err)
	}
	if n != 0 {
		t.Fatalf("marked deferred obligation missed: n=%d, want 0", n)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "deferred" {
		t.Fatalf("deferred obligation status = %s, want deferred", got)
	}
}

func TestMarkMissedBeforeSkipsProcurementExcludedVaccinationWithoutPoisoningBatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	excludedObligationID := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)
	const cleanGoatID = "10000000-0000-4000-8000-0000000000bb"
	if _, err := pool.Exec(ctx, `
	INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
	VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`, cleanGoatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed clean sibling goat: %v", err)
	}
	cleanObligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: cleanGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC), Status: "in_progress",
		IdempotencyKey: "obl-missed-clean-sibling", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert clean sibling obligation: id=%q applied=%v err=%v", cleanObligationID, applied, err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET lifecycle_status='dead'
WHERE tenant_id=$1 AND goat_id=$2`, tenantID, testGoatID); err != nil {
		t.Fatalf("mark excluded goat dead: %v", err)
	}

	n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("mark missed with excluded sibling: %v", err)
	}
	if n != 1 {
		t.Fatalf("marked missed = %d, want only clean sibling", n)
	}
	if got := scanStatus(t, ctx, pool, excludedObligationID); got != "scheduled" {
		t.Fatalf("excluded obligation status = %s, want scheduled", got)
	}
	if got := scanStatus(t, ctx, pool, cleanObligationID); got != "missed" {
		t.Fatalf("clean sibling status = %s, want missed", got)
	}
}

func TestMarkMissedBeforeRecordsStockReconcileForReservedBatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	const itemID = "d2000000-0000-4000-8000-000000000001"
	const lotID = "d2000000-0000-4000-8000-000000000002"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-MISS', 'Missed reconcile vaccine', 'vaccine', 'dose')`, itemID, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 1, 'dose', DATE '2026-12-31')`, lotID, tenantID, itemID, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}
	batchID, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID:              tenantID,
		ProtocolVersionID:     mustVersionOf(t, ctx, pool),
		ScopeType:             "park",
		ScopeID:               cbePark,
		Status:                "planned",
		EstimatedTargets:      1,
		PlannedQuantity:       "1",
		QuantityUnit:          "dose",
		PrimaryInventoryLotID: strPtrObligation(lotID),
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, batchID, []string{obA}); err != nil || attached != 1 {
		t.Fatalf("attach obligation: attached=%d err=%v", attached, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO inventory_stock_movements (
  tenant_id, lot_id, item_id, location_id, movement_type, quantity,
  quantity_unit, batch_id, reason, idempotency_key
) VALUES (
  $1, $2, $3, $4, 'reserve', 1, 'dose', $5, 'batch reserve', 'missed-reserve'
)`, tenantID, lotID, itemID, cbePark, batchID); err != nil {
		t.Fatalf("seed reserve movement: %v", err)
	}

	n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("mark missed: %v", err)
	}
	if n != 1 {
		t.Fatalf("marked missed = %d, want 1", n)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "missed" {
		t.Fatalf("status = %s, want missed", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id=$1 AND obligation_id=$2 AND status='missed' AND batch_id IS NULL`, tenantID, obA); got != 1 {
		t.Fatalf("missed obligation batch attachment = %d, want detached for re-sweep", got)
	}
	due, err := repo.ListUnbatchedDueForVersion(ctx, tenantID, mustVersionOf(t, ctx, pool), time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("list unbatched missed: %v", err)
	}
	foundMissed := false
	for _, row := range due {
		if row.ObligationID == obA {
			foundMissed = true
			break
		}
	}
	if !foundMissed {
		t.Fatalf("missed detached obligation %s not visible for next sweep: %#v", obA, due)
	}
	if got := scanTextObligation(t, ctx, pool, `
SELECT context #>> '{missed_repair,state}'
FROM obligation_batches
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "stock_reconcile_required" {
		t.Fatalf("missed repair state = %q, want stock_reconcile_required", got)
	}
	if got := scanTextObligation(t, ctx, pool, `
SELECT context #>> '{missed_repair,release_qty}'
FROM obligation_batches
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "1" {
		t.Fatalf("missed repair release_qty = %q, want 1", got)
	}
}

func TestCancelOpenForGoatCancelsInProgress(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='in_progress' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obA); err != nil {
		t.Fatalf("set in_progress: %v", err)
	}
	repo := NewRepository(pool, 5*time.Second)
	n, err := repo.CancelOpenForGoat(ctx, tenantID, testGoatID, "dead")
	if err != nil {
		t.Fatalf("cancel open: %v", err)
	}
	if n != 1 {
		t.Fatalf("canceled = %d, want 1", n)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "canceled" {
		t.Fatalf("status: want canceled, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='canceled'`,
		tenantID, obA); got != 1 {
		t.Fatalf("canceled event count = %d, want 1", got)
	}
}

func strPtrObligation(v string) *string {
	return &v
}

func scanTextObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var out string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
		t.Fatalf("scan text: %v", err)
	}
	return out
}
