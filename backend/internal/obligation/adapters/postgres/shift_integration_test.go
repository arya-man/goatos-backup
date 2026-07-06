package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const cptPark = "00000000-0000-4000-8000-000000003002"

const orderedPark = "00000000-0000-4000-8000-000000003099"

// TestSM2ShiftReScopesOpenObligations drives SM-2 (minimal): a goat.shifted event moves the goat's
// open, unbatched obligations to the new park scope and records a 'rescoped' event; completed work
// stays put; a same-scope replay is a no-op.
func TestSM2ShiftReScopesOpenObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool) // scheduled, scope park CBE, unbatched
	repo := NewRepository(pool, 5*time.Second)

	// A completed obligation in the old scope must NOT move.
	obDone, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: mustVersionOf(t, ctx, pool), RuleID: mustRuleOf(t, ctx, pool),
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-done", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed completed obligation: applied=%v err=%v", applied, err)
	}
	if ok, err := repo.MarkCompleted(ctx, tenantID, obDone); err != nil || !ok {
		t.Fatalf("mark completed: ok=%v err=%v", ok, err)
	}

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatShiftedHandler(repo).Register(bus)
	payload, _ := json.Marshal(oblapp.ShiftPayload{ScopeType: "park", ScopeID: cptPark})
	shiftedAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "60000000-0000-4000-8000-000000000201",
		Type:       oblapp.EventGoatShifted,
		TenantID:   tenantID,
		Key:        testGoatID,
		Payload:    payload,
		OccurredAt: shiftedAt,
	}); err != nil {
		t.Fatalf("publish goat.shifted: %v", err)
	}

	// Open obligation moved to CPT; completed one stays at CBE.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2 AND scope_type='park'`, obA, cptPark); got != 1 {
		t.Fatalf("obA should be re-scoped to CPT, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2`, obDone, cbePark); got != 1 {
		t.Fatalf("completed obligation must stay at CBE")
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='rescoped'`, tenantID, obA); got != 1 {
		t.Fatalf("want 1 rescoped event for obA, got %d", got)
	}

	// Idempotent: re-publishing the same shift moves nothing and writes no new event.
	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "60000000-0000-4000-8000-000000000201",
		Type:       oblapp.EventGoatShifted,
		TenantID:   tenantID,
		Key:        testGoatID,
		Payload:    payload,
		OccurredAt: shiftedAt,
	}); err != nil {
		t.Fatalf("re-publish: %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='rescoped'`, tenantID, obA); got != 1 {
		t.Fatalf("re-shift to same scope must not add an event, got %d", got)
	}
}

// TestSM2ShiftReScopesWaivedThenReopensAtCurrentShed guards the MF-2 fix: ReScopeOpenObligationsForGoat
// must move 'waived' (clinical block) work too — symmetric with SM-3 cancel — so when a goat that shifted while
// held later recovers, ReopenDeferredObligationByIdempotencyKey surfaces the obligation at the goat's
// CURRENT scope, not the stale pre-move one (otherwise SM-4 would batch the drive under the wrong shed).
func TestSM2ShiftReScopesWaivedThenReopensAtCurrentShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool) // goat + protocol version + rule; obl-1 scheduled at CBE
	repo := NewRepository(pool, 5*time.Second)

	// A held (waived), unbatched obligation at the old scope (CBE).
	obDef, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: mustVersionOf(t, ctx, pool), RuleID: mustRuleOf(t, ctx, pool),
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Status: "waived",
		IdempotencyKey: "obl-waived", Sequence: 2,
	})
	if err != nil || !applied {
		t.Fatalf("seed waived obligation: applied=%v err=%v", applied, err)
	}

	// Goat shifts to CPT while still held → the waived row must re-scope (stays waived).
	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatShiftedHandler(repo).Register(bus)
	payload, _ := json.Marshal(oblapp.ShiftPayload{ScopeType: "park", ScopeID: cptPark})
	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "60000000-0000-4000-8000-000000000202",
		Type:       oblapp.EventGoatShifted,
		TenantID:   tenantID,
		Key:        testGoatID,
		Payload:    payload,
		OccurredAt: time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.shifted: %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2 AND status='waived'`, obDef, cptPark); got != 1 {
		t.Fatalf("waived obligation should re-scope to CPT and stay waived, got %d", got)
	}

	// Goat recovers → reopen flips it to scheduled AT THE NEW shed, not stale CBE.
	if _, changed, err := repo.ReopenDeferredObligationByIdempotencyKey(ctx, tenantID, "obl-waived", time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC), nil); err != nil || !changed {
		t.Fatalf("reopen deferred: changed=%v err=%v", changed, err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2 AND status='scheduled'`, obDef, cptPark); got != 1 {
		t.Fatalf("reopened obligation must be scheduled at CPT (current shed), got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2`, obDef, cbePark); got != 0 {
		t.Fatalf("reopened obligation must NOT remain at stale CBE")
	}
}

func TestSM2ShiftDoesNotRewriteInProgressBatchHistory(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)
	batchDate := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)

	obInProgress, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: batchDate, Status: "scheduled",
		IdempotencyKey: "obl-shift-in-progress", Sequence: 3,
	})
	if err != nil || !applied {
		t.Fatalf("seed in-progress obligation: applied=%v err=%v", applied, err)
	}
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		PlannedDate: &batchDate, Status: "planned", EstimatedTargets: 1,
		PlannedQuantity: "1", QuantityUnit: "dose",
	}, []string{obInProgress})
	if err != nil || attached != 1 {
		t.Fatalf("create execution batch: batch=%s attached=%d err=%v", batchID, attached, err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches
SET status='in_progress'
WHERE tenant_id=$1 AND batch_id=$2::uuid`, tenantID, batchID); err != nil {
		t.Fatalf("mark batch in progress: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status='in_progress'
WHERE tenant_id=$1 AND obligation_id=$2::uuid`, tenantID, obInProgress); err != nil {
		t.Fatalf("mark batch in progress: %v", err)
	}

	obDone, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: batchDate.Add(24 * time.Hour), Status: "scheduled",
		IdempotencyKey: "obl-shift-completed", Sequence: 4,
	})
	if err != nil || !applied {
		t.Fatalf("seed completed obligation: applied=%v err=%v", applied, err)
	}
	if ok, err := repo.MarkCompleted(ctx, tenantID, obDone); err != nil || !ok {
		t.Fatalf("mark completed: ok=%v err=%v", ok, err)
	}

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatShiftedHandler(repo).Register(bus)
	payload, _ := json.Marshal(oblapp.ShiftPayload{ScopeType: "park", ScopeID: cptPark})
	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "60000000-0000-4000-8000-000000000203",
		Type:       oblapp.EventGoatLocationChanged,
		TenantID:   tenantID,
		Key:        testGoatID,
		Payload:    payload,
		OccurredAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.location.changed: %v", err)
	}

	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id=$1 AND obligation_id=$2::uuid AND scope_id=$3::uuid AND batch_id IS NULL AND status='scheduled'`,
		tenantID, obInProgress, cptPark); got != 1 {
		t.Fatalf("in-progress obligation must replan to destination scope without batch, got %d", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_instances
WHERE tenant_id=$1 AND obligation_id=$2::uuid AND scope_id=$3::uuid AND status='completed'`,
		tenantID, obDone, cbePark); got != 1 {
		t.Fatalf("completed obligation must stay in historical scope, got %d", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_status_events
WHERE tenant_id=$1 AND obligation_id=$2::uuid AND event_type='rescoped'`, tenantID, obInProgress); got != 1 {
		t.Fatalf("shifted in-progress obligation must record rescoped event, got %d", got)
	}
}

// TestSM2ShiftWatermarkRejectsOutOfOrderLocationChanged proves the event-driven SM-2 path is
// arrival-order safe: a newer goat.location.changed event wins even when an older event is delivered
// afterward by the transport.
func TestSM2ShiftWatermarkRejectsOutOfOrderLocationChanged(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1, $2, 'park', 'ORD', 'Ordered Shift Park', 'active')
ON CONFLICT (location_id) DO NOTHING`, orderedPark, tenantID); err != nil {
		t.Fatalf("seed ordered park: %v", err)
	}

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatShiftedHandler(repo).Register(bus)
	olderAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	newerAt := olderAt.Add(30 * time.Minute)
	olderPayload, _ := json.Marshal(oblapp.ShiftPayload{ScopeType: "park", ScopeID: cptPark})
	newerPayload, _ := json.Marshal(oblapp.ShiftPayload{ScopeType: "park", ScopeID: orderedPark})

	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "60000000-0000-4000-8000-000000000302",
		Type:       oblapp.EventGoatLocationChanged,
		TenantID:   tenantID,
		Key:        testGoatID,
		Payload:    newerPayload,
		OccurredAt: newerAt,
	}); err != nil {
		t.Fatalf("publish newer goat.location.changed: %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2 AND scope_type='park'`, obA, orderedPark); got != 1 {
		t.Fatalf("newer event should re-scope obligation to ordered park, got %d", got)
	}

	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "60000000-0000-4000-8000-000000000301",
		Type:       oblapp.EventGoatLocationChanged,
		TenantID:   tenantID,
		Key:        testGoatID,
		Payload:    olderPayload,
		OccurredAt: olderAt,
	}); err != nil {
		t.Fatalf("publish stale goat.location.changed: %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2 AND scope_type='park'`, obA, orderedPark); got != 1 {
		t.Fatalf("stale event must not rewind obligation from ordered park, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2`, obA, cptPark); got != 0 {
		t.Fatalf("stale event must not move obligation to CPT, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='rescoped'`, tenantID, obA); got != 1 {
		t.Fatalf("want only the newer event's rescoped status event, got %d", got)
	}
	var lastEventID string
	if err := pool.QueryRow(ctx, `
SELECT last_event_id
FROM obligation_goat_shift_watermarks
WHERE tenant_id=$1 AND goat_id=$2`, tenantID, testGoatID).Scan(&lastEventID); err != nil {
		t.Fatalf("read shift watermark: %v", err)
	}
	if lastEventID != "60000000-0000-4000-8000-000000000302" {
		t.Fatalf("watermark event id = %q, want newer event", lastEventID)
	}
}

func TestSM2ShiftWatermarkUsesEventIDTieBreaker(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	if _, err := pool.Exec(ctx, `
	INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
	VALUES ($1, $2, 'park', 'ORD', 'Ordered Shift Park', 'active')
	ON CONFLICT (location_id) DO NOTHING`, orderedPark, tenantID); err != nil {
		t.Fatalf("seed ordered park: %v", err)
	}

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatShiftedHandler(repo).Register(bus)
	sameAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	lowerPayload, _ := json.Marshal(oblapp.ShiftPayload{ScopeType: "park", ScopeID: cptPark})
	higherPayload, _ := json.Marshal(oblapp.ShiftPayload{ScopeType: "park", ScopeID: orderedPark})

	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "60000000-0000-4000-8000-000000000401",
		Type:       oblapp.EventGoatLocationChanged,
		TenantID:   tenantID,
		Key:        testGoatID,
		Payload:    lowerPayload,
		OccurredAt: sameAt,
	}); err != nil {
		t.Fatalf("publish lower event: %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "60000000-0000-4000-8000-000000000402",
		Type:       oblapp.EventGoatLocationChanged,
		TenantID:   tenantID,
		Key:        testGoatID,
		Payload:    higherPayload,
		OccurredAt: sameAt,
	}); err != nil {
		t.Fatalf("publish higher same-time event: %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2 AND scope_type='park'`, obA, orderedPark); got != 1 {
		t.Fatalf("higher same-time event should win by event id, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2`, obA, cptPark); got != 0 {
		t.Fatalf("lower same-time event must not remain current, got %d", got)
	}

	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "60000000-0000-4000-8000-000000000401",
		Type:       oblapp.EventGoatLocationChanged,
		TenantID:   tenantID,
		Key:        testGoatID,
		Payload:    lowerPayload,
		OccurredAt: sameAt,
	}); err != nil {
		t.Fatalf("replay lower event: %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE obligation_id=$1 AND scope_id=$2 AND scope_type='park'`, obA, orderedPark); got != 1 {
		t.Fatalf("lower replay must not rewind higher event, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='rescoped'`, tenantID, obA); got != 2 {
		t.Fatalf("want one event per accepted watermark, got %d", got)
	}
	var lastEventID string
	if err := pool.QueryRow(ctx, `
SELECT last_event_id
FROM obligation_goat_shift_watermarks
WHERE tenant_id=$1 AND goat_id=$2`, tenantID, testGoatID).Scan(&lastEventID); err != nil {
		t.Fatalf("read shift watermark: %v", err)
	}
	if lastEventID != "60000000-0000-4000-8000-000000000402" {
		t.Fatalf("watermark event id = %q, want higher same-time event", lastEventID)
	}
}
