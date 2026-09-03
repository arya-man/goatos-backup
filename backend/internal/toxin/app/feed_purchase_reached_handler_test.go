package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

type creatingRepo struct {
	fakeRepo
	created []ports.CreateTaskParams
}

func (c *creatingRepo) CreateTaskFromPurchase(_ context.Context, p ports.CreateTaskParams) error {
	c.created = append(c.created, p)
	return nil
}

// The event registry's proof for procurement.feed_purchase.reached → toxin: the
// consumer subscribes on the bus, filters to its own event type, denormalizes the load
// context onto the round-1 task, and drops identity-less events loudly rather than
// inserting an orphan. The trigger is the ARRIVAL (maintainer decision 2026-09-03): a
// procurement.feed_purchase.recorded event no longer exists, so a load still on the road
// has no task -- pinned below by publishing the retired type and expecting nothing.
func TestFeedPurchaseReachedCreatesTheLoadsTask(t *testing.T) {
	repo := &creatingRepo{}
	bus := eventbus.NewInProcessBus()
	NewFeedPurchaseReachedHandler(repo, nil).Register(bus)

	payload, _ := json.Marshal(map[string]any{
		"feed_purchase_id": "11111111-0000-4000-8000-000000000001",
		"farm_label":       "CPT",
		"feed_item_key":    "dry_sorghum_forage",
		"feed_item_label":  "Dry Sorghum Forage",
		"vendor":           "Siddi Srilekha",
		"batch_no":         7,
		"purchase_date":    "2026-08-25",
		"reached_on":       "2026-08-28",
		"quantity_kg":      1960.0,
	})
	err := bus.Publish(context.Background(), eventbus.Event{
		ID:       "evt-1",
		Type:     "procurement.feed_purchase.reached",
		TenantID: "22222222-0000-4000-8000-000000000002",
		Payload:  payload,
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d tasks, want 1", len(repo.created))
	}
	got := repo.created[0]
	if got.FeedPurchaseID != "11111111-0000-4000-8000-000000000001" ||
		got.TenantID != "22222222-0000-4000-8000-000000000002" ||
		got.FarmLabel != "CPT" || got.FeedItemLabel != "Dry Sorghum Forage" ||
		got.Vendor != "Siddi Srilekha" || got.BatchNo != 7 ||
		got.PurchaseDate != "2026-08-25" || got.QuantityKg != 1960 ||
		got.SourceEventID != "evt-1" {
		t.Fatalf("created params = %+v", got)
	}

	// A different module's event passes through untouched.
	if err := bus.Publish(context.Background(), eventbus.Event{Type: "goat.created", TenantID: "t"}); err != nil {
		t.Fatalf("unrelated publish: %v", err)
	}
	// The RETIRED recording event creates nothing: a purchase that has not reached owes no test.
	if err := bus.Publish(context.Background(), eventbus.Event{
		ID: "evt-old", Type: "procurement.feed_purchase.recorded", TenantID: "22222222-0000-4000-8000-000000000002",
		Payload: payload,
	}); err != nil {
		t.Fatalf("retired-type publish: %v", err)
	}
	// An identity-less purchase event is dropped, never inserted as an orphan.
	if err := bus.Publish(context.Background(), eventbus.Event{
		ID: "evt-2", Type: "procurement.feed_purchase.reached", TenantID: "t",
		Payload: []byte(`{"farm_label":"CPT"}`),
	}); err != nil {
		t.Fatalf("identity-less publish: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d tasks after noise, want still 1", len(repo.created))
	}
}
