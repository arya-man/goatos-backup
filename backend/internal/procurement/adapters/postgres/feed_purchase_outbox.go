package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// procurement.feed_purchase.reached (maintainer decision 2026-09-03, moving the toxin
// trigger off procurement.feed_purchase.recorded): a feed load REACHING the farm is the
// business event with an outward consequence — the toxin module owes the load an
// aflatoxin strip test, and only now is there feed to test. Recording the purchase is
// not: the truck is still on the road. The event is emitted INSIDE the transaction that
// flips the load to reached (the delivery write, or a record-purchase write that already
// carries an arrival date), on that transition ONLY, so a committed arrival always reaches
// the toxin consumer exactly once and a later correction of the arrival day or received
// weight emits nothing. Registered in context/architecture/domain-event-registry.json.
const (
	feedPurchaseReachedEventType     = "procurement.feed_purchase.reached"
	feedPurchaseReachedSchemaVersion = "1.0.0"
	feedPurchaseReachedSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	feedPurchaseReachedTopic         = "procurement.events"
)

// feedPurchaseReachedFacts is the load context the event denormalizes for the toxin task
// card. The consumer keeps these as the CATALOG's spelling, the same one the ledger stores.
type feedPurchaseReachedFacts struct {
	PurchaseID    string
	FarmLabel     string
	FeedItemKey   string
	FeedItemLabel string
	Vendor        string
	BatchNo       int
	PurchaseDate  string
	ReachedOn     string
	// StockKg is the weight the load contributes to stock at the moment it reached: the
	// received weight when it was entered with the arrival, else the buying weight.
	StockKg float64
}

// emitFeedPurchaseReached writes the arrival event into outbox_messages inside tx.
//
// idempotencyKey is the operation that caused the arrival (the record form's key, or the
// delivery write's derived key); the outbox row carries it so a replayed relay delivery is
// recognisable as the same arrival.
func emitFeedPurchaseReached(ctx context.Context, tx pgx.Tx, tenantID, actorID, idempotencyKey string, facts feedPurchaseReachedFacts) error {
	eventID, err := newUUID(ctx, tx)
	if err != nil {
		return fmt.Errorf("procurement: feed purchase event id: %w", err)
	}
	now := time.Now().UTC()
	payload := map[string]any{
		"feed_purchase_id": facts.PurchaseID,
		"farm_label":       facts.FarmLabel,
		"feed_item_key":    facts.FeedItemKey,
		"feed_item_label":  facts.FeedItemLabel,
		"vendor":           facts.Vendor,
		"batch_no":         facts.BatchNo,
		"purchase_date":    facts.PurchaseDate,
		"reached_on":       facts.ReachedOn,
		"quantity_kg":      facts.StockKg,
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     feedPurchaseReachedEventType,
		"schema_version": feedPurchaseReachedSchemaVersion,
		"schema_ref":     feedPurchaseReachedSchemaRef,
		"aggregate_type": "feed_purchase",
		"aggregate_id":   facts.PurchaseID,
		"occurred_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"recorded_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "procurement_feed_purchases",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "human",
			"actor_id":   nullableStringValue(&actorID),
			"actor_ref":  nil,
		},
		"subject_type": "feed_purchase",
		"subject_id":   facts.PurchaseID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "feed_purchase:" + facts.PurchaseID,
		}},
		"payload":  payload,
		"trace_id": "procurement-feed-purchase:" + facts.PurchaseID,
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":      actorID,
		"farm":          facts.FarmLabel,
		"business_date": biztime.BusinessDate(now),
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, status
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'feed_purchase', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, 'pending'
)`,
		tenantID,
		eventID,
		feedPurchaseReachedEventType,
		feedPurchaseReachedSchemaVersion,
		facts.PurchaseID,
		feedPurchaseReachedTopic,
		envelope,
		headers,
		idempotencyKey,
	); err != nil {
		return fmt.Errorf("procurement: outbox feed purchase reached: %w", err)
	}
	return nil
}
