package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

const (
	procurementGoatCreatedEventType     = "goat.created"
	procurementGoatCreatedSchemaVersion = "1.0.0"
	procurementGoatCreatedSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	procurementGoatCreatedTopic         = "identity.events"
)

func (r *Repository) emitAcceptedIntakeGoatCreated(ctx context.Context, tx pgx.Tx, in ports.AcceptIntake, handoff domain.PCHandoff) error {
	eventID, err := newUUID(ctx, tx)
	if err != nil {
		return fmt.Errorf("procurement: goat.created event id: %w", err)
	}
	now := time.Now().UTC()
	occurredAt := in.AcceptedAt.UTC()
	idempotencyKey := in.IdempotencyKey + ":" + handoff.GoatID
	sourceRecordID := "procurement_load:" + in.LoadID
	eventPayload, err := json.Marshal(map[string]any{
		"goat_id":                     handoff.GoatID,
		"load_id":                     in.LoadID,
		"handoff_id":                  handoff.HandoffID,
		"origin_type":                 "procured",
		"entry_date":                  biztime.BusinessDate(in.EntryDate),
		"park_id":                     in.ParkLocationID,
		"shed_id":                     handoff.ShedLocationID,
		"trusted_vaccination_history": json.RawMessage(handoff.TrustedVaccinationHistory),
		"intake_health_signal":        handoff.IntakeHealthSignal,
		"generation_status":           "queued",
	})
	if err != nil {
		return err
	}
	var recordedAt time.Time
	if err := tx.QueryRow(ctx, `
INSERT INTO goat_identity_events (
  identity_event_id, tenant_id, goat_id, event_type, event_version,
  occurred_at, recorded_at, actor_id, source_system, source_record_id,
  payload, decision_id, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, 1,
  $5::timestamptz, $6::timestamptz, nullif($7::text, '')::uuid, 'procurement_source_entry', $8,
  $9::jsonb, NULL, $10
)
RETURNING recorded_at`,
		eventID,
		in.TenantID,
		handoff.GoatID,
		procurementGoatCreatedEventType,
		occurredAt,
		now,
		stringPtrValue(in.ActorID),
		sourceRecordID,
		eventPayload,
		idempotencyKey,
	).Scan(&recordedAt); err != nil {
		return fmt.Errorf("procurement: insert goat.created identity event: %w", err)
	}

	auditAfter := map[string]any{
		"handoff":           handoff,
		"event_id":          eventID,
		"generation_status": "queued",
	}
	auditMetadata := map[string]any{
		"domain":            "procurement",
		"module":            "source_entry",
		"category":          "accepted_intake",
		"result":            "queued",
		"status":            "queued",
		"idempotency_key":   idempotencyKey,
		"operation_id":      idempotencyKey,
		"actor_id":          stringPtrValue(in.ActorID),
		"load_id":           in.LoadID,
		"handoff_id":        handoff.HandoffID,
		"identity_event_id": eventID,
		"outbox_event_id":   eventID,
		"source_record_id":  sourceRecordID,
		"generation_status": "queued",
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      stringPtrValue(in.ActorID),
		ActorType:    "human",
		Action:       procurementGoatCreatedEventType,
		ResourceType: "goat",
		ResourceID:   handoff.GoatID,
		ScopeType:    "shed",
		ScopeID:      handoff.ShedLocationID,
		AfterState:   auditAfter,
		Metadata:     auditMetadata,
	}); err != nil {
		return fmt.Errorf("procurement: audit goat.created handoff: %w", err)
	}

	envelope, err := procurementGoatCreatedEnvelope(in, handoff, eventID, occurredAt, now, eventPayload, idempotencyKey)
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":          stringPtrValue(in.ActorID),
		"load_id":           in.LoadID,
		"handoff_id":        handoff.HandoffID,
		"generation_status": "queued",
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, status
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'goat', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, 'pending'
)`,
		in.TenantID,
		eventID,
		procurementGoatCreatedEventType,
		procurementGoatCreatedSchemaVersion,
		handoff.GoatID,
		procurementGoatCreatedTopic,
		envelope,
		headers,
		idempotencyKey,
	); err != nil {
		return fmt.Errorf("procurement: outbox goat.created handoff: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE procurement_pc_handoffs
SET event_status = 'emitted', updated_at = now()
WHERE tenant_id = $1::uuid
  AND handoff_id = $2::uuid`,
		in.TenantID, handoff.HandoffID); err != nil {
		return fmt.Errorf("procurement: mark PC handoff emitted: %w", err)
	}
	_ = recordedAt
	return nil
}

func procurementGoatCreatedEnvelope(in ports.AcceptIntake, handoff domain.PCHandoff, eventID string, occurredAt, recordedAt time.Time, eventPayload []byte, idempotencyKey string) ([]byte, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(eventPayload, &payload); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     procurementGoatCreatedEventType,
		"schema_version": procurementGoatCreatedSchemaVersion,
		"schema_ref":     procurementGoatCreatedSchemaRef,
		"aggregate_type": "goat",
		"aggregate_id":   handoff.GoatID,
		"occurred_at":    occurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"recorded_at":    recordedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "procurement_source_entry",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "human",
			"actor_id":   nullableStringValue(in.ActorID),
			"actor_ref":  nil,
		},
		"subject_type": "goat",
		"subject_id":   handoff.GoatID,
		"visibility_scope": map[string]any{
			"tenant_id": in.TenantID,
			"park_id":   in.ParkLocationID,
			"shed_id":   handoff.ShedLocationID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "procurement_load:" + in.LoadID,
		}},
		"payload":  payload,
		"trace_id": "procurement-accept-intake:" + in.LoadID,
	})
}

func newUUID(ctx context.Context, tx pgx.Tx) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id)
	return id, err
}

func nullableStringValue(value *string) any {
	if strings.TrimSpace(stringPtrValue(value)) == "" {
		return nil
	}
	return stringPtrValue(value)
}
