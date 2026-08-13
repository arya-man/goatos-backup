package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// TestWithdrawItemsBySourceEmitsRetraction is the P1-B regression.
//
// Creating an item publishes verification.item.pending, which the notification
// bridge consumes and turns into a push telling a verifier to review the proof.
// WithdrawItemsBySource then flips that item to 'withdrawn' with a plain UPDATE
// and NO outbox event, so every consumer that already acted on the pending
// event is left believing there is still work to do. In the first real weighing
// device run 10 items were withdrawn ~60ms after their pending event was
// published, and nothing downstream was ever told.
//
// This repo bans a producer with no consumer and vice versa: a retraction must
// be published on the same transaction as the withdrawal. verification.item.closed
// is the existing "this item is no longer decidable" event -- it already carries
// the routing fields and already has a registered consumer -- so a withdrawal
// publishes it with status/decision = 'withdrawn'.
func TestWithdrawItemsBySourceEmitsRetraction(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	sourceRefID := tenantID // any UUID; the source ref is opaque to verification.

	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID,
		Vertical: "growth",
		Module:   "weighing",
		Category: "weighing_animal",
		Source: domain.SourceRef{
			Module:  "weighing",
			RefType: "weighing_animal_observation",
			RefID:   sourceRefID,
		},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "weighing:weighing_animal:obs-1:1",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if got := countEventsFor(t, ctx, pool, tenantID, EventItemPending); got != 1 {
		t.Fatalf("pending events=%d, want 1", got)
	}

	withdrawn, err := repo.WithdrawItemsBySource(ctx, tenantID, "weighing", "weighing_animal_observation", []string{sourceRefID})
	if err != nil {
		t.Fatalf("WithdrawItemsBySource: %v", err)
	}
	if withdrawn != 1 {
		t.Fatalf("withdrawn=%d, want 1", withdrawn)
	}

	// THE BUG: the withdrawal is silent. Nothing retracts the pending event.
	if got := countEventsFor(t, ctx, pool, tenantID, EventItemClosed); got != 1 {
		t.Fatalf("verification.item.closed retraction events=%d, want exactly 1 -- a withdrawal that published nothing leaves every consumer of verification.item.pending believing the item is still decidable", got)
	}
	if got := retractionStatus(t, ctx, pool, tenantID, created.Item.ItemID); got != domain.StatusWithdrawn {
		t.Fatalf("retraction payload status=%q, want %q so a consumer can tell a retraction from a verdict", got, domain.StatusWithdrawn)
	}

	// A second withdrawal of the same already-withdrawn item changes nothing and
	// must not publish a second retraction.
	if _, err := repo.WithdrawItemsBySource(ctx, tenantID, "weighing", "weighing_animal_observation", []string{sourceRefID}); err != nil {
		t.Fatalf("replay WithdrawItemsBySource: %v", err)
	}
	if got := countEventsFor(t, ctx, pool, tenantID, EventItemClosed); got != 1 {
		t.Fatalf("retraction events after replay=%d, want 1", got)
	}
}

func countEventsFor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, eventType string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type=$2`, tenantID, eventType).Scan(&count); err != nil {
		t.Fatalf("count %s events: %v", eventType, err)
	}
	return count
}

// retractionStatus reads the status the retraction event carries in its payload,
// which is what a consumer branches on to tell a withdrawal from a verdict.
func retractionStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, itemID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(payload->'payload'->>'status', payload->>'status', '')
FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type=$2 AND aggregate_id=$3::uuid`,
		tenantID, EventItemClosed, itemID).Scan(&status); err != nil {
		t.Fatalf("read retraction payload: %v", err)
	}
	return status
}
