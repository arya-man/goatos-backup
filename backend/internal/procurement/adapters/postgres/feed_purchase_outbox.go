package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// procurement.feed_purchase.recorded (maintainer decision 2026-08-25): recording a feed
// load is a business event with an outward consequence — the toxin module owes the load
// an aflatoxin strip test. The event is emitted INSIDE the purchase transaction's
// outbox, so a committed purchase always reaches the toxin consumer (which creates the
// test task idempotently), and a rolled-back purchase emits nothing. Registered in
// context/architecture/domain-event-registry.json.
const (
	feedPurchaseRecordedEventType     = "procurement.feed_purchase.recorded"
	feedPurchaseRecordedSchemaVersion = "1.0.0"
	feedPurchaseRecordedSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	feedPurchaseRecordedTopic         = "procurement.events"
)

// emitFeedPurchaseRecorded writes the purchase event into outbox_messages inside tx.
func emitFeedPurchaseRecorded(ctx context.Context, tx pgx.Tx, tenantID, purchaseID, actorID, idempotencyKey string, write domain.FeedPurchaseWrite, feedItemKey, catalogLabel string, batchNo int) error {
	eventID, err := newUUID(ctx, tx)
	if err != nil {
		return fmt.Errorf("procurement: feed purchase event id: %w", err)
	}
	now := time.Now().UTC()
	// The consumer denormalizes these onto the toxin task card; keep them the CATALOG's
	// spelling, the same one the ledger row stores.
	payload := map[string]any{
		"feed_purchase_id": purchaseID,
		"farm_label":       write.FarmLabel,
		"feed_item_key":    feedItemKey,
		"feed_item_label":  catalogLabel,
		"vendor":           write.Vendor,
		"batch_no":         batchNo,
		"purchase_date":    write.PurchaseDate,
		"quantity_kg":      write.QuantityKg,
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     feedPurchaseRecordedEventType,
		"schema_version": feedPurchaseRecordedSchemaVersion,
		"schema_ref":     feedPurchaseRecordedSchemaRef,
		"aggregate_type": "feed_purchase",
		"aggregate_id":   purchaseID,
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
		"subject_id":   purchaseID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "feed_purchase:" + purchaseID,
		}},
		"payload":  payload,
		"trace_id": "procurement-feed-purchase:" + purchaseID,
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":      actorID,
		"farm":          write.FarmLabel,
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
		feedPurchaseRecordedEventType,
		feedPurchaseRecordedSchemaVersion,
		purchaseID,
		feedPurchaseRecordedTopic,
		envelope,
		headers,
		idempotencyKey,
	); err != nil {
		return fmt.Errorf("procurement: outbox feed purchase recorded: %w", err)
	}
	return nil
}
