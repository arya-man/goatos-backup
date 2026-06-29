package postgres

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
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
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id, current_location_id, park_id)
		 VALUES ($1, $2, 'alive', 'clean', $3, $4, $4)`,
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
		TriggerType: "birth_age", Repeat: "none", CatchUp: "phc_approval",
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
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id, current_location_id, park_id)
VALUES ($1, $2, 'alive', 'clean', $3, $4, $4)`, secondGoatID, tenantID, meshaParty, cbePark); err != nil {
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
	pending, err := repo.ListPlannedBatchesNeedingFinalization(ctx, tenantID, versionID, true, true, 100)
	if err != nil {
		t.Fatalf("list planned finalization: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("missed-only planned batches needing finalization = %+v, want none", pending)
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
		OccurredAt: time.Now().UTC(), Payload: []byte(`{"k":1}`),
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
				OccurredAt: time.Now().UTC(), Payload: []byte(`{}`),
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
