package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// sales.deal.recorded (SALES SOP, maintainer instruction 2026-09-19, docs/decisions/sales-sop.md):
// recording a sale is the business event that opens the sale's WORK -- tag the animals, load them,
// settle the money -- authored on /sales/sops and run by the tasks engine as one workflow keyed on
// the deal. The event is emitted INSIDE the transaction that inserts the sales_deals row, so a
// committed sale always reaches the opener exactly once (the outbox row's idempotency key is the
// record's own key) and a refused or rolled-back record emits nothing. The consumer is
// tasks/app.SaleRecordedWorkflowHandler, registered in eventwiring.RegisterWorkflowConsumers so
// every bus process opens it. Registered in context/architecture/domain-event-registry.json.
const (
	saleRecordedEventType     = "sales.deal.recorded"
	saleRecordedSchemaVersion = "1.0.0"
	saleRecordedSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	saleRecordedTopic         = "sales.events"
	// saleRecordedAggregate is the sales_deal aggregate the outbox validator already knows
	// (migration 000275, for goat.sale_allocated).
	saleRecordedAggregate = "sales_deal"
)

// parkIDForFarmSQL resolves the deal's farm code (CBE / CPT) to its park location so the opener
// can scope the workflow to the park whose head tags the animals. One indexed read on the
// tenant's park rows; a farm with no park row leaves the workflow tenant-scoped.
const parkIDForFarmSQL = `
SELECT l.location_id::text
FROM public.locations l
WHERE l.tenant_id = $1::uuid AND l.location_type = 'park' AND upper(l.location_code) = upper($2)
LIMIT 1`

// emitSaleRecorded writes the sales.deal.recorded envelope into outbox_messages inside tx.
func emitSaleRecorded(ctx context.Context, tx pgx.Tx, tenantID, actorID, idempotencyKey, dealID string, write domain.DealWrite) error {
	var parkID string
	if err := tx.QueryRow(ctx, parkIDForFarmSQL, tenantID, write.Farm).Scan(&parkID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("sales: resolve park for farm %q: %w", write.Farm, err)
	}
	eventID := platformoutbox.DeterministicUUID(saleRecordedEventType + ":" + tenantID + ":" + dealID)
	now := time.Now().UTC()
	animals := 0
	if write.AnimalCount != nil {
		animals = int(*write.AnimalCount)
	}
	payload := map[string]any{
		"tenant_id":     tenantID,
		"sales_deal_id": dealID,
		"sale_date":     write.SaleDate,
		"farm":          write.Farm,
		"park_id":       parkID,
		"buyer_name":    write.BuyerName,
		"animal_count":  animals,
		"product_type":  write.ProductType,
		"status":        write.Status,
		// Whether the sale has animals to tag, load and pass out (maintainer decision 2026-09-25):
		// decided HERE, from the lines being written in this transaction, so the opener never reads
		// the sale. False for a manure / feed / other-item sale and for an animal line with no head
		// count; the workflow then opens with its payment steps only.
		"has_live_animals": write.HasLiveAnimals(),
	}
	return insertSalesDealEvent(ctx, tx, salesDealEvent{
		tenantID: tenantID, actorID: actorID, dealID: dealID, parkID: parkID, farm: write.Farm,
		eventType: saleRecordedEventType, eventID: eventID, idempotencyKey: idempotencyKey,
		outboxKey: saleRecordedEventType + ":" + idempotencyKey, payload: payload, at: now,
	})
}

// salesDealEvent is one sales_deal-aggregate envelope to write into the outbox.
type salesDealEvent struct {
	tenantID, actorID, dealID, parkID, farm       string
	eventType, eventID, idempotencyKey, outboxKey string
	payload                                       map[string]any
	at                                            time.Time
}

// insertSalesDealEvent writes a sales_deal-aggregate envelope into outbox_messages inside tx.
func insertSalesDealEvent(ctx context.Context, tx pgx.Tx, ev salesDealEvent) error {
	tenantID, actorID, dealID, parkID, now := ev.tenantID, ev.actorID, ev.dealID, ev.parkID, ev.at
	visibility := map[string]any{"tenant_id": tenantID}
	if parkID != "" {
		visibility["park_id"] = parkID
	}
	var actor any
	if a := strings.TrimSpace(actorID); a != "" {
		actor = a
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       ev.eventID,
		"event_type":     ev.eventType,
		"schema_version": saleRecordedSchemaVersion,
		"schema_ref":     saleRecordedSchemaRef,
		"aggregate_type": saleRecordedAggregate,
		"aggregate_id":   dealID,
		"occurred_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"recorded_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "sales",
			"version": nil,
		},
		"idempotency_key": ev.idempotencyKey,
		"actor": map[string]any{
			"actor_type": "human",
			"actor_id":   actor,
			"actor_ref":  nil,
		},
		"subject_type":     saleRecordedAggregate,
		"subject_id":       dealID,
		"visibility_scope": visibility,
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "sales_deal:" + dealID,
		}},
		"payload":  ev.payload,
		"trace_id": "sales-deal:" + dealID,
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":      actorID,
		"farm":          ev.farm,
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
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, 'pending'
)`,
		tenantID, ev.eventID, ev.eventType, saleRecordedSchemaVersion, saleRecordedAggregate, dealID,
		saleRecordedTopic, envelope, headers, ev.outboxKey,
	); err != nil {
		return fmt.Errorf("sales: outbox %s: %w", ev.eventType, err)
	}
	return nil
}

// sales.deal.status_changed (maintainer decision 2026-09-25, docs/decisions/sales-sop.md -> "A
// failed sale"): a deal's lifecycle status moved. Emitted INSIDE SetDealStatus's transaction, only
// when the status actually changes, so a retried no-op change emits nothing. Two consumers:
// tasks/app.SaleStatusChangedWorkflowHandler CANCELS the sale's workflow when the deal is marked
// Deal Failed -- the work a failed sale owed (tag, load, gate pass, collect the balance) will never
// be done -- and identity/app.SaleFailedReleaseHandler RELEASES every animal tagged to the failed
// deal back into the herd, in the pen it was sold from. Deal Failed is final, so a deal emits it at
// most once. Registered in context/architecture/domain-event-registry.json.
const saleStatusChangedEventType = "sales.deal.status_changed"

// emitDealStatusChanged writes the status-change envelope. changedAt is the row's own updated_at
// from the same UPDATE, so the event id is deterministic per change and unique across changes (a
// deal failed, reopened and failed again emits three distinct events).
func emitDealStatusChanged(ctx context.Context, tx pgx.Tx, tenantID, actorID, dealID, farm, previous, status string, changedAt time.Time) error {
	var parkID string
	if err := tx.QueryRow(ctx, parkIDForFarmSQL, tenantID, farm).Scan(&parkID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("sales: resolve park for farm %q: %w", farm, err)
	}
	stamp := changedAt.UTC().Format(time.RFC3339Nano)
	key := dealID + ":" + stamp
	return insertSalesDealEvent(ctx, tx, salesDealEvent{
		tenantID: tenantID, actorID: actorID, dealID: dealID, parkID: parkID, farm: farm,
		eventType:      saleStatusChangedEventType,
		eventID:        platformoutbox.DeterministicUUID(saleStatusChangedEventType + ":" + tenantID + ":" + key),
		idempotencyKey: key,
		outboxKey:      saleStatusChangedEventType + ":" + key,
		payload: map[string]any{
			"tenant_id":       tenantID,
			"sales_deal_id":   dealID,
			"previous_status": previous,
			"status":          status,
			// Who changed it: the identity consumer records the animals' release against this
			// person (blank for a system caller; the release then names the person who tagged).
			"actor_id": strings.TrimSpace(actorID),
		},
		at: changedAt.UTC(),
	})
}
