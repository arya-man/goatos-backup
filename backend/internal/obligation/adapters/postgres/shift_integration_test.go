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
	if err := bus.Publish(ctx, eventbus.Event{Type: oblapp.EventGoatShifted, TenantID: tenantID, Key: testGoatID, Payload: payload}); err != nil {
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
	if err := bus.Publish(ctx, eventbus.Event{Type: oblapp.EventGoatShifted, TenantID: tenantID, Key: testGoatID, Payload: payload}); err != nil {
		t.Fatalf("re-publish: %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='rescoped'`, tenantID, obA); got != 1 {
		t.Fatalf("re-shift to same scope must not add an event, got %d", got)
	}
}
