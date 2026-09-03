package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestCreateFeedPurchaseEmitsReachedEventInTheSameTransaction pins the toxin trigger
// (maintainer decisions 2026-08-25 and 2026-09-03): a committed purchase that ALREADY
// REACHED leaves ONE pending procurement.feed_purchase.reached outbox message whose
// payload carries the load context the toxin consumer denormalizes -- and the envelope
// passes the outbox validator, which a fake outbox would never prove -- while an exact
// idempotent replay emits no second message. A load still on the road emits nothing
// here; its arrival is covered by TestFeedPurchaseDeliveryPostgresPaths.
func TestCreateFeedPurchaseEmitsReachedEventInTheSameTransaction(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 10*time.Second)
	seedFeedPurchaseFixture(t, ctx, pool)

	write := feedWrite()
	write.ReachedOn = "2026-08-23"
	created, err := repo.CreateFeedPurchase(ctx, testTenant, write, "", "toxin-outbox-1")
	if err != nil {
		t.Fatalf("create purchase: %v", err)
	}

	var payloadRaw []byte
	var status string
	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1 AND event_type = 'procurement.feed_purchase.reached'`, testTenant).Scan(&count); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox messages = %d, want 1", count)
	}
	if err := pool.QueryRow(ctx, `
SELECT payload -> 'payload', status FROM outbox_messages
WHERE tenant_id = $1 AND event_type = 'procurement.feed_purchase.reached'`, testTenant).Scan(&payloadRaw, &status); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if status != "pending" {
		t.Fatalf("outbox status = %q", status)
	}
	var payload struct {
		FeedPurchaseID string  `json:"feed_purchase_id"`
		FarmLabel      string  `json:"farm_label"`
		FeedItemKey    string  `json:"feed_item_key"`
		FeedItemLabel  string  `json:"feed_item_label"`
		Vendor         string  `json:"vendor"`
		BatchNo        int     `json:"batch_no"`
		PurchaseDate   string  `json:"purchase_date"`
		ReachedOn      string  `json:"reached_on"`
		QuantityKg     float64 `json:"quantity_kg"`
	}
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.FeedPurchaseID != created.FeedPurchaseID || payload.FarmLabel != created.FarmLabel ||
		payload.FeedItemLabel != "Dry Sorghum Forage" || payload.FeedItemKey == "" ||
		payload.Vendor != created.Vendor || payload.BatchNo != created.BatchNo ||
		payload.PurchaseDate != created.PurchaseDate || payload.ReachedOn != "2026-08-23" || payload.QuantityKg != 5420 {
		t.Fatalf("payload = %+v vs created %+v", payload, created)
	}

	// An exact idempotent replay re-reads the original purchase and emits NOTHING new.
	if _, err := repo.CreateFeedPurchase(ctx, testTenant, write, "", "toxin-outbox-1"); err != nil {
		t.Fatalf("replay purchase: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1 AND event_type = 'procurement.feed_purchase.reached'`, testTenant).Scan(&count); err != nil {
		t.Fatalf("recount outbox: %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox messages after replay = %d, want still 1", count)
	}
}
