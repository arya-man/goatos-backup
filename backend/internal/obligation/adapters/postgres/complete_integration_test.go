package postgres

import (
	"context"
	"testing"
	"time"

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
