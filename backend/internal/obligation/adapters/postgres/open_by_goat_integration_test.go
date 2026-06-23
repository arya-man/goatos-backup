package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestListOpenByGoat checks the Goat Passport next-due read: only still-open obligations, earliest
// due first, completed ones excluded.
func TestListOpenByGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seed(t, ctx, pool) // scheduled obligation 'obl-1' for testGoatID, due 2026-08-01
	repo := NewRepository(pool, 5*time.Second)
	version, rule := mustVersionOf(t, ctx, pool), mustRuleOf(t, ctx, pool)

	mk := func(key string, due time.Time) string {
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: version, RuleID: rule,
			TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	obEarly := mk("obl-early", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	obDone := mk("obl-done", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if ok, err := repo.MarkCompleted(ctx, tenantID, obDone); err != nil || !ok {
		t.Fatalf("mark completed: ok=%v err=%v", ok, err)
	}

	open, err := repo.ListOpenByGoat(ctx, tenantID, testGoatID, 100)
	if err != nil {
		t.Fatalf("list open by goat: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("want 2 open obligations (completed excluded), got %d", len(open))
	}
	if open[0].ObligationID != obEarly {
		t.Fatalf("earliest due first: want %s, got %s", obEarly, open[0].ObligationID)
	}
	if !open[0].DueAt.Before(open[1].DueAt) {
		t.Fatalf("not ordered by due_at: %v then %v", open[0].DueAt, open[1].DueAt)
	}
}
