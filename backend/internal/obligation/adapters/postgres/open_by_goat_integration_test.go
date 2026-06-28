package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestListOpenByGoat checks the Goat Passport next-due read: still-actionable obligations, earliest
// due first, completed ones excluded.
func TestListOpenByGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seed(t, ctx, pool) // scheduled obligation 'obl-1' for testGoatID, due 2026-08-01
	repo := NewRepository(pool, 5*time.Second)
	version, rule := mustVersionOf(t, ctx, pool), mustRuleOf(t, ctx, pool)

	mk := func(key, status string, due time.Time) string {
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: version, RuleID: rule,
			TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: status, IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	obMissed := mk("obl-missed", "missed", time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))
	obEarly := mk("obl-early", "scheduled", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	obDone := mk("obl-done", "scheduled", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if ok, err := repo.MarkCompleted(ctx, tenantID, obDone); err != nil || !ok {
		t.Fatalf("mark completed: ok=%v err=%v", ok, err)
	}

	open, err := repo.ListOpenByGoat(ctx, tenantID, testGoatID, 100)
	if err != nil {
		t.Fatalf("list open by goat: %v", err)
	}
	if len(open) != 3 {
		t.Fatalf("want 3 actionable obligations (missed included, completed excluded), got %d", len(open))
	}
	if open[0].ObligationID != obMissed || open[0].Status != "missed" {
		t.Fatalf("missed obligations should stay visible and sort by due_at: first=%#v want %s", open[0], obMissed)
	}
	if open[1].ObligationID != obEarly {
		t.Fatalf("earliest due first after missed: want %s, got %s", obEarly, open[1].ObligationID)
	}
	if !open[0].DueAt.Before(open[1].DueAt) || !open[1].DueAt.Before(open[2].DueAt) {
		t.Fatalf("not ordered by due_at: %#v", open)
	}
}
