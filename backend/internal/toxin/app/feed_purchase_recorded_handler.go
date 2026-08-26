package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// Toxin task creation consumer (maintainer decision 2026-08-25). Recording a feed
// purchase emits procurement.feed_purchase.recorded from inside the purchase
// transaction's outbox; this consumer materializes the load's aflatoxin test task. The
// task is the OUTWARD consequence of the purchase, so it rides the durable event spine
// rather than a synchronous cross-module call — a purchase commits whether or not the
// toxin module is healthy, and the relay replays the event until the task exists.
//
// Idempotency: CreateTaskFromPurchase is keyed on (tenant_id, feed_purchase_id), so a
// replayed or duplicated event is a no-op, never a second task for the same load.

// eventFeedPurchaseRecorded is the procurement producer's event type.
const eventFeedPurchaseRecorded = "procurement.feed_purchase.recorded"

// feedPurchaseRecordedPayload is the subset of the purchase event this consumer reads.
type feedPurchaseRecordedPayload struct {
	FeedPurchaseID string  `json:"feed_purchase_id"`
	FarmLabel      string  `json:"farm_label"`
	FeedItemKey    string  `json:"feed_item_key"`
	FeedItemLabel  string  `json:"feed_item_label"`
	Vendor         string  `json:"vendor"`
	BatchNo        int     `json:"batch_no"`
	PurchaseDate   string  `json:"purchase_date"`
	QuantityKg     float64 `json:"quantity_kg"`
}

// FeedPurchaseRecordedHandler creates one toxin test task per recorded feed purchase.
type FeedPurchaseRecordedHandler struct {
	repo ports.Repository
	log  *slog.Logger
}

// NewFeedPurchaseRecordedHandler constructs the consumer over the toxin repository.
func NewFeedPurchaseRecordedHandler(repo ports.Repository, log *slog.Logger) *FeedPurchaseRecordedHandler {
	if log == nil {
		log = slog.Default()
	}
	return &FeedPurchaseRecordedHandler{repo: repo, log: log}
}

var _ eventbus.Handler = (*FeedPurchaseRecordedHandler)(nil)

// Register subscribes the handler to the purchase event.
func (h *FeedPurchaseRecordedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventFeedPurchaseRecorded, h)
}

// HandleEvent materializes the purchased load's test task.
func (h *FeedPurchaseRecordedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventFeedPurchaseRecorded {
		return nil
	}
	var p feedPurchaseRecordedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if strings.TrimSpace(e.TenantID) == "" || strings.TrimSpace(p.FeedPurchaseID) == "" {
		// A purchase event with no identity cannot key a task; drop it loudly rather than
		// insert an orphan the tester cannot trace to a load.
		h.log.Warn("toxin: feed purchase event missing tenant or purchase id", "event_id", e.ID)
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
