package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// Toxin task creation consumer (maintainer decision 2026-08-25; trigger moved to the load
// REACHING the farm by maintainer decision 2026-09-03). A feed load arriving emits
// procurement.feed_purchase.reached from inside the delivery transaction's outbox; this
// consumer materializes the load's aflatoxin test task. Recording the purchase no longer
// triggers it: the truck is still on the road and there is nothing to test yet. The task is
// the OUTWARD consequence of the arrival, so it rides the durable event spine rather than a
// synchronous cross-module call — an arrival commits whether or not the toxin module is
// healthy, and the relay replays the event until the task exists.
//
// Idempotency: CreateTaskFromPurchase is keyed on (tenant_id, feed_purchase_id), so a
// replayed or duplicated event is a no-op, never a second task for the same load.

// eventFeedPurchaseReached is the procurement producer's event type.
const eventFeedPurchaseReached = "procurement.feed_purchase.reached"

// feedPurchaseReachedPayload is the subset of the arrival event this consumer reads.
// QuantityKg is the weight that reached (the received weight when entered with the
// arrival, else the buying weight) -- what the tester actually has in front of them.
type feedPurchaseReachedPayload struct {
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

// FeedPurchaseReachedHandler creates one toxin test task per feed load that reached the farm.
type FeedPurchaseReachedHandler struct {
	repo ports.Repository
	log  *slog.Logger
}

// NewFeedPurchaseReachedHandler constructs the consumer over the toxin repository.
func NewFeedPurchaseReachedHandler(repo ports.Repository, log *slog.Logger) *FeedPurchaseReachedHandler {
	if log == nil {
		log = slog.Default()
	}
	return &FeedPurchaseReachedHandler{repo: repo, log: log}
}

var _ eventbus.Handler = (*FeedPurchaseReachedHandler)(nil)

// Register subscribes the handler to the arrival event.
func (h *FeedPurchaseReachedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventFeedPurchaseReached, h)
}

// HandleEvent materializes the reached load's test task.
func (h *FeedPurchaseReachedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventFeedPurchaseReached {
		return nil
	}
	var p feedPurchaseReachedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if strings.TrimSpace(e.TenantID) == "" || strings.TrimSpace(p.FeedPurchaseID) == "" {
		// A purchase event with no identity cannot key a task; drop it loudly rather than
		// insert an orphan the tester cannot trace to a load.
		h.log.Warn("toxin: feed purchase reached event missing tenant or purchase id", "event_id", e.ID)
		return nil
	}
	return h.repo.CreateTaskFromPurchase(ctx, ports.CreateTaskParams{
		TenantID:       e.TenantID,
		FeedPurchaseID: p.FeedPurchaseID,
		FarmLabel:      p.FarmLabel,
		FeedItemKey:    p.FeedItemKey,
		FeedItemLabel:  p.FeedItemLabel,
		Vendor:         p.Vendor,
		BatchNo:        p.BatchNo,
		PurchaseDate:   p.PurchaseDate,
		QuantityKg:     p.QuantityKg,
		SourceEventID:  e.ID,
	})
}
