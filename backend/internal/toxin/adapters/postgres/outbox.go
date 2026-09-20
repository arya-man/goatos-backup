package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
)

// procurement.toxin_test.accepted (PROCUREMENT IS SOP-DRIVEN END TO END, maintainer decision
// 2026-09-20). The toxin module announced NOTHING: its round began from a feed load reaching the
// farm and ended on the CEO's screen, and no other part of the farm could tell whether a load had
// been screened. The feed purchase's own workflow carries an aflatoxin step, and this event is
// what completes it -- the mapping the maintainer asked for, one event wide.
//
// ONLY the ACCEPT is announced, and that is the point: a rejected or Invalid round CANCELS itself
// and mints a retest in the same transaction, so the load is still owed a test and its step must
// stay open. Announcing a reject would invite a consumer to treat "we looked at it" as "it is
// done".
//
// The OUTCOME rides along because an accepted POSITIVE is a real result: v1 FLAGS the load and
// does not block feeding (docs/decisions/toxin-testing-module.md), so a consumer that needs to
// tell a clean load from a flagged one reads the outcome rather than inferring it from the
// acceptance.
const (
	toxinAcceptedEventType     = "procurement.toxin_test.accepted"
	toxinAcceptedSchemaVersion = "1.0.0"
	toxinAcceptedSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	toxinAcceptedTopic         = "procurement.events"
	toxinAggregateType         = "toxin_task"
)

// emitToxinAccepted writes the accepted-round envelope inside the verdict transaction.
func emitToxinAccepted(ctx context.Context, tx pgx.Tx, tenantID, actorID, idempotencyKey string, task domain.Task) error {
	eventID := uuid.NewString()
	now := time.Now().UTC()
	payload := map[string]any{
		"toxin_task_id":    task.TaskID,
		"feed_purchase_id": task.FeedPurchaseID,
		"round_no":         task.RoundNo,
		"outcome":          task.Outcome,
		"farm_label":       task.FarmLabel,
		"feed_item_key":    task.FeedItemKey,
		"feed_item_label":  task.FeedItemLabel,
		"vendor":           task.Vendor,
		"batch_no":         task.BatchNo,
		"accepted_at":      now.Format(time.RFC3339Nano),
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     toxinAcceptedEventType,
		"schema_version": toxinAcceptedSchemaVersion,
		"schema_ref":     toxinAcceptedSchemaRef,
		"aggregate_type": toxinAggregateType,
		"aggregate_id":   task.TaskID,
		"occurred_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"recorded_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "toxin",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "human",
			"actor_id":   nullableActor(actorID),
			"actor_ref":  nil,
		},
		"subject_type":     toxinAggregateType,
		"subject_id":       task.TaskID,
		"visibility_scope": map[string]any{"tenant_id": tenantID},
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "toxin_test_task:" + task.TaskID,
		}},
		"payload":  payload,
		"trace_id": "toxin-test:" + task.TaskID,
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":      actorID,
		"farm":          task.FarmLabel,
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
)`, tenantID, eventID, toxinAcceptedEventType, toxinAcceptedSchemaVersion, toxinAggregateType,
		task.TaskID, toxinAcceptedTopic, envelope, headers, "toxin-accepted:"+idempotencyKey); err != nil {
		return fmt.Errorf("toxin: outbox accepted: %w", err)
	}
	return nil
}

// nullableActor keeps a blank actor out of the envelope as JSON null rather than "".
func nullableActor(actorID string) any {
	if actorID == "" {
		return nil
	}
	return actorID
}
