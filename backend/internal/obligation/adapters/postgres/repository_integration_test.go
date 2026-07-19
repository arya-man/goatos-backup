package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const (
	tenantID   = "00000000-0000-4000-8000-000000000001"
	meshaParty = "00000000-0000-4000-8000-000000001001"
	cbePark    = "00000000-0000-4000-8000-000000003001"
	testGoatID = "10000000-0000-4000-8000-0000000000aa"
)

// seed creates a goat target + a draft protocol version + a rule + one obligation, and returns
// the obligation id. It exercises the protocol and obligation repos along the way.
func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (obligationID string) {
	t.Helper()

	// Goat target (raw insert; goats are owned by the identity module).
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`,
		testGoatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed goat: %v", err)
	}

	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.test", Name: "Test", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("seed definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("seed version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-1", Sequence: 1,
	})
	if err != nil || !applied || id == "" {
		t.Fatalf("seed obligation: id=%q applied=%v err=%v", id, applied, err)
	}
	return id
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestObligationInsertIsIdempotent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	// Re-insert with the SAME idempotency key -> replay no-op.
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: mustVersionOf(t, ctx, pool), RuleID: mustRuleOf(t, ctx, pool),
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-1", Sequence: 1,
	})
	if err != nil {
		t.Fatalf("replay insert: %v", err)
	}
	if applied || id != "" {
		t.Fatalf("expected replay no-op, got id=%q applied=%v", id, applied)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, "obl-1"); got != 1 {
		t.Fatalf("expected exactly 1 obligation row, got %d", got)
	}
	_ = obligationID
}

func TestFindNearestPlannedBatchDateUsesCompatibleComboSession(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)
	unrelated := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	compatible := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	for _, batch := range []domain.NewBatch{
		{
			TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
			Session: "combo:PPR+Blue Tongue", PlannedDate: &unrelated, Status: "planned",
			EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose",
		},
		{
			TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
			Session: "combo:FMD+HS", PlannedDate: &compatible, Status: "planned",
			EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose",
		},
	} {
		if _, err := repo.CreateBatch(ctx, batch); err != nil {
			t.Fatalf("create planned batch %s: %v", batch.Session, err)
		}
	}

	got, err := repo.FindNearestPlannedBatchDate(ctx, tenantID, versionID, ruleID, "FMD", "", cbePark,
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("find nearest planned batch: %v", err)
	}
	if got == nil || !got.Equal(compatible) {
		t.Fatalf("nearest planned batch = %v, want compatible combo date %s", got, compatible)
	}
}

func TestObligationInsertPreservesCanceledSameKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obligationID); err != nil {
		t.Fatalf("cancel seed obligation: %v", err)
	}
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-1", Sequence: 1,
	})
	if err != nil {
		t.Fatalf("replay canceled insert: %v", err)
	}
	if applied || id != "" {
		t.Fatalf("terminal replay id=%q applied=%v, want no-op", id, applied)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2 AND idempotency_key=$3 AND status='canceled'`, tenantID, obligationID, "obl-1"); got != 1 {
		t.Fatalf("expected terminal canceled obligation preserved, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND idempotency_key LIKE '%:regenerated:%'`, tenantID, obligationID); got != 0 {
		t.Fatalf("terminal replay must not create regenerated status events, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='obligation.regenerated'`, tenantID, obligationID); got != 0 {
		t.Fatalf("terminal replay must not create regenerated outbox events, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.regenerated'`, tenantID, obligationID); got != 0 {
		t.Fatalf("terminal replay must not create regenerated audit events, got %d", got)
	}
}

func TestObligationInsertCreatesFreshWorkAfterMissedWithFreshKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='missed' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obligationID); err != nil {
		t.Fatalf("mark seed obligation missed: %v", err)
	}
	nextDue := time.Date(2027, 8, 1, 0, 0, 0, 0, time.UTC)
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: nextDue, Status: "scheduled",
		IdempotencyKey: "obl-1", Sequence: 1,
	})
	if err != nil {
		t.Fatalf("replay missed insert: %v", err)
	}
	if applied || id != "" {
		t.Fatalf("missed replay id=%q applied=%v, want no-op", id, applied)
	}
	var status string
	var due time.Time
	if err := pool.QueryRow(ctx, `SELECT status, due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obligationID).Scan(&status, &due); err != nil {
		t.Fatalf("read terminal obligation: %v", err)
	}
	if status != "missed" || !due.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("status=%q due=%s, want missed at original due", status, due)
	}

	freshID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: nextDue, Status: "scheduled",
		IdempotencyKey: "obl-1-fresh-after-return", Sequence: 2,
	})
	if err != nil {
		t.Fatalf("fresh post-missed insert: %v", err)
	}
	if !applied || freshID == "" || freshID == obligationID {
		t.Fatalf("fresh insert id=%q applied=%v, want new scheduled obligation distinct from %s", freshID, applied, obligationID)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='missed'`, tenantID, testGoatID); got != 1 {
		t.Fatalf("terminal missed history rows=%d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2 AND due_at=$3 AND status='scheduled'`, tenantID, freshID, nextDue); got != 1 {
		t.Fatalf("fresh scheduled work rows=%d, want 1", got)
	}
}

func TestObligationInsertDedupesSameLogicalDoseWithDifferentKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	duplicateID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-1-legacy-next-cycle-key", Sequence: 1,
	})
	if err != nil {
		t.Fatalf("insert logical duplicate: %v", err)
	}
	if applied || duplicateID != "" {
		t.Fatalf("logical duplicate applied=%v id=%q, want no-op", applied, duplicateID)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id=$1 AND protocol_version_id=$2 AND rule_id=$3 AND target_id=$4 AND due_at=$5`,
		tenantID, versionID, ruleID, testGoatID, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)); got != 1 {
		t.Fatalf("logical duplicate count=%d, want 1 existing obligation %s", got, obligationID)
	}
}

func TestBatchAttachDoesNotReuseReservedBatchAndMissedOnlyNotFinalized(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	firstObligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)
	batchDate := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	firstBatchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           cbePark,
		Session:           "morning",
		PlannedDate:       &batchDate,
		Status:            "planned",
		EstimatedTargets:  1,
		PlannedQuantity:   "1",
		QuantityUnit:      "dose",
	}, []string{firstObligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create first batch: batch=%s attached=%d err=%v", firstBatchID, attached, err)
	}
	const itemID = "d3000000-0000-4000-8000-000000000001"
	const lotID = "d3000000-0000-4000-8000-000000000002"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-BATCH', 'Batch attach vaccine', 'vaccine', 'dose')`, itemID, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 1, 'dose', DATE '2026-12-31')`, lotID, tenantID, itemID, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO inventory_stock_movements (
  tenant_id, lot_id, item_id, location_id, movement_type, quantity,
  quantity_unit, batch_id, reason, idempotency_key
) VALUES (
  $1, $2, $3, $4, 'reserve', 1, 'dose', $5, 'batch reserve', 'batch-attach-reserve'
)`, tenantID, lotID, itemID, cbePark, firstBatchID); err != nil {
		t.Fatalf("seed reserve movement: %v", err)
	}

	const secondGoatID = "10000000-0000-4000-8000-0000000000cc"
	if _, err := pool.Exec(ctx, `
	INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
	VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`, secondGoatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed second goat: %v", err)
	}
	secondObligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: secondGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: batchDate, Status: "scheduled",
		IdempotencyKey: "obl-new-after-reserved-batch", Sequence: 2,
	})
	if err != nil || !applied {
		t.Fatalf("insert second obligation: id=%q applied=%v err=%v", secondObligationID, applied, err)
	}
	secondBatchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           cbePark,
		Session:           "morning",
		PlannedDate:       &batchDate,
		Status:            "planned",
		EstimatedTargets:  1,
		PlannedQuantity:   "1",
		QuantityUnit:      "dose",
	}, []string{secondObligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create second batch: batch=%s attached=%d err=%v", secondBatchID, attached, err)
	}
	if secondBatchID == firstBatchID {
		t.Fatalf("reserved first batch was reused for second obligation")
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id=$1 AND obligation_id=$2 AND batch_id=$3::uuid`, tenantID, secondObligationID, secondBatchID); got != 1 {
		t.Fatalf("second obligation attached to new batch count=%d, want 1", got)
	}

	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = 'missed'
WHERE tenant_id=$1
  AND obligation_id IN ($2::uuid, $3::uuid)`, tenantID, firstObligationID, secondObligationID); err != nil {
		t.Fatalf("mark batches missed-only: %v", err)
	}
	pending, err := repo.ListPlannedBatchesNeedingFinalization(ctx, tenantID, versionID, true, true, nil, 100)
	if err != nil {
		t.Fatalf("list planned finalization: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("missed-only planned batches needing finalization = %+v, want none", pending)
	}
}

func TestCreateBatchWithObligationsSkipsObligationCanceledAfterSelection(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	batchDate := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
	UPDATE obligation_instances
	SET status = 'canceled'
	WHERE tenant_id=$1
	  AND obligation_id=$2::uuid`, tenantID, obligationID); err != nil {
		t.Fatalf("cancel selected obligation: %v", err)
	}

	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           cbePark,
		Session:           "morning",
		PlannedDate:       &batchDate,
		Status:            "planned",
		EstimatedTargets:  1,
		PlannedQuantity:   "1",
		QuantityUnit:      "dose",
	}, []string{obligationID})
	if err != nil {
		t.Fatalf("create batch with canceled obligation: %v", err)
	}
	if batchID != "" || attached != 0 {
		t.Fatalf("created batch=%q attached=%d, want no batch and no attached rows", batchID, attached)
	}
	if got := countRows(t, ctx, pool, `
	SELECT count(*)
	FROM obligation_instances
	WHERE tenant_id=$1
	  AND obligation_id=$2::uuid
	  AND status='canceled'
	  AND batch_id IS NULL`, tenantID, obligationID); got != 1 {
		t.Fatalf("canceled obligation left unattached count=%d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1`, tenantID); got != 0 {
		t.Fatalf("batch rows after canceled attach race = %d, want 0", got)
	}
}

func TestCreateBatchWithObligationsRecordsHoldOnlyForAttachedRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	firstObligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)
	secondObligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-partial-hold-canceled", Sequence: 2,
	})
	if err != nil || !applied || secondObligationID == "" {
		t.Fatalf("seed second obligation: id=%q applied=%v err=%v", secondObligationID, applied, err)
	}
	batchDate := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	holdUntil := batchDate.AddDate(0, 0, 7)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = 'canceled',
    updated_at = now()
WHERE tenant_id = $1
  AND obligation_id = $2::uuid`, tenantID, secondObligationID); err != nil {
		t.Fatalf("cancel second selected obligation: %v", err)
	}

	_, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           cbePark,
		Session:           "morning",
		PlannedDate:       &batchDate,
		Status:            "planned",
		EstimatedTargets:  2,
		PlannedQuantity:   "2",
		QuantityUnit:      "dose",
		BatchingHoldUntil: &holdUntil,
	}, []string{firstObligationID, secondObligationID})
	if err != nil {
		t.Fatalf("create batch with partial attach: %v", err)
	}
	if attached != 1 {
		t.Fatalf("attached=%d, want only the uncanceled obligation attached", attached)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id = $1
  AND obligation_id = $2::uuid
  AND COALESCE(batching_hold_count, 0) = 1
  AND first_batching_hold_until IS NOT NULL`, tenantID, firstObligationID); got != 1 {
		t.Fatalf("attached obligation hold rows=%d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id = $1
  AND obligation_id = $2::uuid
  AND batch_id IS NULL
  AND COALESCE(batching_hold_count, 0) = 0
  AND first_batching_hold_until IS NULL`, tenantID, secondObligationID); got != 1 {
		t.Fatalf("unattached canceled obligation hold rows=%d, want 1 with no hold burned", got)
	}
}

func TestCreateBatchWithObligationsDoesNotBurnHoldAgainForAlreadyAttachedReplay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	firstObligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)
	const secondGoatID = "10000000-0000-4000-8000-0000000000d7"
	seedComboGoat(t, ctx, pool, secondGoatID)
	secondObligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: secondGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-hold-replay-new", Sequence: 2,
	})
	if err != nil || !applied || secondObligationID == "" {
		t.Fatalf("seed second obligation: id=%q applied=%v err=%v", secondObligationID, applied, err)
	}
	batchDate := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	holdUntil := batchDate.AddDate(0, 0, 7)
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           cbePark,
		Session:           "hold-replay",
		PlannedDate:       &batchDate,
		Status:            "planned",
		EstimatedTargets:  1,
		PlannedQuantity:   "1",
		QuantityUnit:      "dose",
		BatchingHoldUntil: &holdUntil,
	}, []string{firstObligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create first batch: batch=%s attached=%d err=%v", batchID, attached, err)
	}
	replayBatchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           cbePark,
		Session:           "hold-replay",
		PlannedDate:       &batchDate,
		Status:            "planned",
		EstimatedTargets:  2,
		PlannedQuantity:   "2",
		QuantityUnit:      "dose",
		BatchingHoldUntil: &holdUntil,
	}, []string{firstObligationID, secondObligationID})
	if err != nil || attached != 1 || replayBatchID != batchID {
		t.Fatalf("mixed replay: batch=%s attached=%d err=%v, want existing batch %s and one new attach", replayBatchID, attached, err, batchID)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id = $1
  AND obligation_id = $2::uuid
  AND COALESCE(batching_hold_count, 0) = 1`, tenantID, firstObligationID); got != 1 {
		t.Fatalf("already-attached hold count rows=%d, want still exactly one hold", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id = $1
  AND obligation_id = $2::uuid
  AND COALESCE(batching_hold_count, 0) = 1
  AND first_batching_hold_until IS NOT NULL`, tenantID, secondObligationID); got != 1 {
		t.Fatalf("newly attached hold rows=%d, want one hold recorded", got)
	}
}

func TestPlannedBatchFinalizationOneToManyPageBoundaryScheduledDateParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	firstObligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	plannedShifted := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)

	const goatB = "10000000-0000-4000-8000-0000000000b2"
	const goatCanceled = "10000000-0000-4000-8000-0000000000b3"
	const goatPage2 = "10000000-0000-4000-8000-0000000000b4"
	seedComboGoat(t, ctx, pool, goatB)
	seedComboGoat(t, ctx, pool, goatCanceled)
	seedComboGoat(t, ctx, pool, goatPage2)
	secondOpenID := insertComboObligation(t, ctx, repo, versionID, ruleID, goatB, "park", cbePark, "obl-finalization-open", due)
	canceledID := insertComboObligation(t, ctx, repo, versionID, ruleID, goatCanceled, "park", cbePark, "obl-finalization-canceled", due)
	page2ID := insertComboObligation(t, ctx, repo, versionID, ruleID, goatPage2, "park", cbePark, "obl-finalization-page2", due)

	firstBatchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           cbePark,
		Session:           "finalization-proof-a",
		PlannedDate:       &plannedShifted,
		Status:            "planned",
		EstimatedTargets:  3,
		PlannedQuantity:   "3",
		QuantityUnit:      "dose",
	}, []string{firstObligationID, secondOpenID, canceledID})
	if err != nil || attached != 3 {
		t.Fatalf("create first planned batch: batch=%s attached=%d err=%v", firstBatchID, attached, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1 AND obligation_id=$2::uuid`, tenantID, canceledID); err != nil {
		t.Fatalf("cancel attached status-matrix member: %v", err)
	}

	secondBatchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           cbePark,
		Session:           "finalization-proof-b",
		PlannedDate:       &plannedShifted,
		Status:            "planned",
		EstimatedTargets:  1,
		PlannedQuantity:   "1",
		QuantityUnit:      "dose",
	}, []string{page2ID})
	if err != nil || attached != 1 {
		t.Fatalf("create second planned batch: batch=%s attached=%d err=%v", secondBatchID, attached, err)
	}

	var after *domain.PlannedBatchFinalizationCursor
	var collected []domain.PlannedBatchFinalization
	for {
		page, err := repo.ListPlannedBatchesNeedingFinalization(ctx, tenantID, versionID, true, false, after, 1)
		if err != nil {
			t.Fatalf("list finalization page: %v", err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) != 1 {
			t.Fatalf("page size = %d, want 1", len(page))
		}
		collected = append(collected, page[0])
		after = &domain.PlannedBatchFinalizationCursor{CreatedAt: page[0].CreatedAt, BatchID: page[0].BatchID}
		if len(collected) > 3 {
			t.Fatalf("finalization pagination did not terminate: %+v", collected)
		}
	}
	if len(collected) != 2 {
		t.Fatalf("finalization rows = %+v, want two planned batches across page boundary", collected)
	}
	byBatch := map[string]domain.PlannedBatchFinalization{}
	for _, row := range collected {
		byBatch[row.BatchID] = row
		if row.ScopeType != "park" || row.ScopeID != cbePark {
			t.Fatalf("row scope = %s/%s, want park/%s", row.ScopeType, row.ScopeID, cbePark)
		}
		if row.PlannedDate == nil || !row.PlannedDate.Equal(plannedShifted) {
			t.Fatalf("row planned date = %v, want shifted scheduled date %s", row.PlannedDate, plannedShifted)
		}
	}
	if got := byBatch[firstBatchID].AttachedObligations; got != 2 {
		t.Fatalf("first batch open attached obligations = %d, want 2 (canceled row excluded)", got)
	}
	if got := byBatch[secondBatchID].AttachedObligations; got != 1 {
		t.Fatalf("second batch open attached obligations = %d, want 1", got)
	}
}

func TestStatusEventReserveBeforeInsertDedup(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	ev := domain.NewStatusEvent{
		TenantID: tenantID, ObligationID: obligationID, EventType: "became_due",
		OccurredAt: time.Now().In(biztime.DefaultLocation()), Payload: []byte(`{"k":1}`),
		IdempotencyKey: "evt-1", Scope: "obligation.status_event", RequestHash: "h1",
	}

	id1, applied1, err := repo.RecordStatusEvent(ctx, ev)
	if err != nil || !applied1 || id1 == "" {
		t.Fatalf("first record: id=%q applied=%v err=%v", id1, applied1, err)
	}
	id2, applied2, err := repo.RecordStatusEvent(ctx, ev)
	if err != nil {
		t.Fatalf("retry record: %v", err)
	}
	if applied2 || id2 != "" {
		t.Fatalf("expected retry dedup (applied=false), got id=%q applied=%v", id2, applied2)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, "evt-1"); got != 1 {
		t.Fatalf("expected exactly 1 status event, got %d", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM idempotency_keys
WHERE tenant_id=$1
  AND idempotency_key=$2
  AND expires_at IS NOT NULL
  AND expires_at > first_seen_at`, tenantID, "evt-1"); got != 1 {
		t.Fatalf("expected expiring idempotency key, got %d", got)
	}
}

func TestMarkCompletedEmitsVaccinationCompletedOutbox(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	completed, err := repo.MarkCompleted(ctx, tenantID, obligationID)
	if err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if !completed {
		t.Fatal("MarkCompleted completed=false, want true")
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid AND status='completed'`, tenantID, obligationID); got != 1 {
		t.Fatalf("completed obligation count = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_status_events
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid AND event_type='completed'`, tenantID, obligationID); got != 1 {
		t.Fatalf("completed status event count = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM idempotency_keys
WHERE tenant_id=$1::uuid
  AND idempotency_key=$2
  AND scope='obligation.mark_completed'
  AND status='completed'
  AND result_type='obligation_status_event'`, tenantID, obligationID+":completed"); got != 1 {
		t.Fatalf("completed idempotency key count = %d, want 1", got)
	}

	var raw []byte
	if err := pool.QueryRow(ctx, `
SELECT payload
FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND event_type='vaccination.completed'
  AND aggregate_type='obligation_instance'
  AND aggregate_id=$2::uuid`, tenantID, obligationID).Scan(&raw); err != nil {
		t.Fatalf("query vaccination completed outbox: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode vaccination completed outbox: %v", err)
	}
	if envelope["event_type"] != vaccinationCompletedEventType || envelope["aggregate_id"] != obligationID {
		t.Fatalf("vaccination completed envelope = %#v", envelope)
	}
	payload, ok := envelope["payload"].(map[string]any)
	if !ok || payload["obligation_id"] != obligationID || payload["status"] != "completed" {
		t.Fatalf("vaccination completed payload = %#v", envelope["payload"])
	}

	completed, err = repo.MarkCompleted(ctx, tenantID, obligationID)
	if err != nil {
		t.Fatalf("MarkCompleted replay: %v", err)
	}
	if completed {
		t.Fatal("MarkCompleted replay completed=true, want false")
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='vaccination.completed' AND aggregate_id=$2::uuid`, tenantID, obligationID); got != 1 {
		t.Fatalf("vaccination completed outbox count after replay = %d, want 1", got)
	}
}

func TestStatusEventConcurrentDedup(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	appliedCount := 0
	errs := make([]error, 0)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, applied, err := repo.RecordStatusEvent(ctx, domain.NewStatusEvent{
				TenantID: tenantID, ObligationID: obligationID, EventType: "became_due",
				OccurredAt: time.Now().In(biztime.DefaultLocation()), Payload: []byte(`{}`),
				IdempotencyKey: "evt-concurrent", Scope: "obligation.status_event", RequestHash: "h",
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if applied {
				appliedCount++
			}
		}()
	}
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("concurrent record errors: %v", errs)
	}
	if appliedCount != 1 {
		t.Fatalf("expected exactly 1 applied insert under concurrency, got %d", appliedCount)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, "evt-concurrent"); got != 1 {
		t.Fatalf("expected exactly 1 status event row under concurrency, got %d", got)
	}
}

// TestNextSuccessorSuffixBounded is the R50-011 fix: find the next free successor
// suffix in one bounded query, never O(N) round trips for large collision histories.
func TestNextSuccessorSuffixBounded(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	baseKey := "goat:test:cancel"

	// Test 1: No successors exist yet, should return 1
	suffix, err := repo.NextSuccessorSuffix(ctx, tenantID, baseKey)
	if err != nil {
		t.Fatalf("next suffix (empty): %v", err)
	}
	if suffix != 1 {
		t.Fatalf("next suffix with no history = %d, want 1", suffix)
	}

	// Test 2: Simulate 40 prior successors (collision history)
	// Normally this would be O(40) round trips with attempt-by-attempt probing.
	// With the bounded query, it's ONE query.
	for i := 1; i <= 40; i++ {
		key := fmt.Sprintf("%s:successor:%02d", baseKey, i)
		_, err := pool.Exec(ctx, `
INSERT INTO idempotency_keys (tenant_id, idempotency_key, scope, request_hash, status, result_type, result_id)
VALUES ($1, $2, 'obligation.status_event', 'hash', 'started', NULL, NULL)`, tenantID, key)
		if err != nil {
			t.Fatalf("insert successor %d: %v", i, err)
		}
	}

	// Query next suffix — should return 41 (one query, not 41 round trips)
	suffix, err = repo.NextSuccessorSuffix(ctx, tenantID, baseKey)
	if err != nil {
		t.Fatalf("next suffix (40 history): %v", err)
	}
	if suffix != 41 {
		t.Fatalf("next suffix with 40 successors = %d, want 41", suffix)
	}

	// Test 3: Prove it's bounded (max 2000 per query)
	// Insert more successors, confirm we find the next one
	for i := 41; i <= 50; i++ {
		key := fmt.Sprintf("%s:successor:%02d", baseKey, i)
		_, _ = pool.Exec(ctx, `
INSERT INTO idempotency_keys (tenant_id, idempotency_key, scope, request_hash, status, result_type, result_id)
VALUES ($1, $2, 'obligation.status_event', 'hash', 'started', NULL, NULL)
ON CONFLICT DO NOTHING`, tenantID, key)
	}
	suffix, _ = repo.NextSuccessorSuffix(ctx, tenantID, baseKey)
	if suffix < 1 || suffix > 2000 {
		t.Fatalf("successor suffix out of bounds: %d (should be 1-2000)", suffix)
	}
	if suffix != 51 {
		t.Fatalf("next suffix after 50 = %d, want 51", suffix)
	}

	// Test 4 (R50-011 3-digit fix): past 99 the suffix is 3 digits ("100", "105"). A fixed
	// 2-char SUBSTRING read capped MAX at 99 and returned a colliding value, so successor
	// creation past 99 failed. Insert up to 105 and assert the next free suffix is 106.
	for i := 51; i <= 105; i++ {
		key := fmt.Sprintf("%s:successor:%02d", baseKey, i)
		if _, err := pool.Exec(ctx, `
INSERT INTO idempotency_keys (tenant_id, idempotency_key, scope, request_hash, status, result_type, result_id)
VALUES ($1, $2, 'obligation.status_event', 'hash', 'started', NULL, NULL)
ON CONFLICT DO NOTHING`, tenantID, key); err != nil {
			t.Fatalf("insert successor %d: %v", i, err)
		}
	}
	suffix, err = repo.NextSuccessorSuffix(ctx, tenantID, baseKey)
	if err != nil {
		t.Fatalf("next suffix (105 history): %v", err)
	}
	if suffix != 106 {
		t.Fatalf("next suffix after 105 (3-digit) = %d, want 106", suffix)
	}
}

// mustVersionOf / mustRuleOf re-read the seeded ids for the replay test.
func mustVersionOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `SELECT protocol_version_id::text FROM protocol_versions WHERE tenant_id=$1 ORDER BY created_at LIMIT 1`, tenantID).Scan(&id); err != nil {
		t.Fatalf("read version: %v", err)
	}
	return id
}

func mustRuleOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `SELECT rule_id::text FROM protocol_rules WHERE tenant_id=$1 ORDER BY created_at LIMIT 1`, tenantID).Scan(&id); err != nil {
		t.Fatalf("read rule: %v", err)
	}
	return id
}
