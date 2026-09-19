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
	}
	visibility := map[string]any{"tenant_id": tenantID}
	if parkID != "" {
		visibility["park_id"] = parkID
	}
	var actor any
	if a := strings.TrimSpace(actorID); a != "" {
		actor = a
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     saleRecordedEventType,
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
		"idempotency_key": idempotencyKey,
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
		"payload":  payload,
		"trace_id": "sales-deal:" + dealID,
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":      actorID,
		"farm":          write.Farm,
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
		tenantID, eventID, saleRecordedEventType, saleRecordedSchemaVersion, saleRecordedAggregate, dealID,
		saleRecordedTopic, envelope, headers, saleRecordedEventType+":"+idempotencyKey,
	); err != nil {
		return fmt.Errorf("sales: outbox deal recorded: %w", err)
	}
	return nil
}
