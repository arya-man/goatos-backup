package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// Domain events (maintainer decisions 2026-09-07 and 2026-09-12). A visit coming into
// existence, a visit being submitted and a visit being verified are the business facts of this
// module; each is emitted INSIDE the write transaction's outbox so a committed row always
// announces itself and a rolled-back one never does. Registered in
// context/architecture/domain-event-registry.json. pen_visit.submitted is consumed by the
// verification enqueue handler (penvisits/app.PendingVerificationHandler) -- it is what turns
// the visit video into a verifier item; pen_visit.verified is what the parent-closure
// consumer reads. The morning push is NOT driven by these events: it is one digest per park
// per day, queued by the materializer stage with a business-date key (the FeedLowStockNotifier
// shape), because one push per pen would be ten pushes for one morning's round.
const (
	EventVisitCreated   = "pen_visit.created"
	EventVisitSubmitted = "pen_visit.submitted"
	EventVisitVerified  = "pen_visit.verified"

	eventSchemaVersion = "1.0.0"
	eventSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	eventTopic         = "pen_visits.events"
	aggregateType      = "pen_visit_task"
)

// EventPayload is the inner payload both events carry.
type EventPayload struct {
	TaskID         string   `json:"task_id"`
	ParkID         string   `json:"park_id"`
	ParkName       string   `json:"park_name"`
	ShedID         string   `json:"shed_id"`
	PartitionLabel string   `json:"partition_label"`
	PenLabel       string   `json:"pen_label"`
	Reasons        []string `json:"reasons"`
	SourceDate     string   `json:"source_business_date"`
	DueDate        string   `json:"due_business_date"`
	WorkState      string   `json:"work_state"`
	Status         string   `json:"status"`
	// Sources are the parents this visit closes, as "<kind>:<ref id>".
	Sources    []string `json:"sources"`
	ProofRef   string   `json:"proof_ref,omitempty"`
	RowVersion int      `json:"row_version"`
	ChangedBy  string   `json:"changed_by_user_id"`
	OccurredAt string   `json:"occurred_at"`
}

// emitEvent writes one visit event into outbox_messages inside tx. The event id is
// deterministic on (type, tenant, idempotency key) so a retried write never double-announces.
// actorType is one of the envelope's closed actor vocabulary: "human" for the park head's
// submit, "system_rule" for the materializer. The envelope is validated by the relay against
// contracts/jsonschema/domain-event-envelope.schema.json before publish, and a rejected
// envelope is a SILENT drop (status failed, no consumer ever sees it), so the integration
// test validates every emitted envelope against the same schema.
func emitEvent(ctx context.Context, tx pgx.Tx, eventType string, t domain.Task, actorID, actorType, idempotencyKey string, now time.Time) error {
	now = now.UTC()
	eventID := platformoutbox.DeterministicUUID(eventType + ":" + t.TenantID + ":" + idempotencyKey)
	proof := ""
	if t.ProofRef != nil {
		proof = *t.ProofRef
	}
	sources := make([]string, 0, len(t.Sources))
	for _, src := range t.Sources {
		sources = append(sources, src.Kind+":"+src.RefID)
	}
	payload := EventPayload{
		TaskID:         t.TaskID,
		ParkID:         t.ParkID,
		ParkName:       t.ParkName,
		ShedID:         t.ShedID,
		PartitionLabel: t.Partition,
		PenLabel:       t.PenLabel,
		Reasons:        domain.SortReasons(t.Reasons),
		SourceDate:     t.SourceDate,
		DueDate:        t.DueDate,
		WorkState:      t.WorkState,
		Status:         t.Status,
		Sources:        sources,
		ProofRef:       proof,
		RowVersion:     t.RowVersion,
		ChangedBy:      actorID,
		OccurredAt:     now.Format(time.RFC3339),
	}
	var actorRef any
	if actorID != "" {
		actorRef = actorID
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": eventSchemaVersion,
		"schema_ref":     eventSchemaRef,
		"aggregate_type": aggregateType,
		"aggregate_id":   t.TaskID,
		"occurred_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"recorded_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "pen_visits",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": actorType,
			"actor_id":   actorRef,
			"actor_ref":  nil,
		},
		"subject_type": aggregateType,
		"subject_id":   t.TaskID,
		"visibility_scope": map[string]any{
			"tenant_id": t.TenantID,
			"park_id":   t.ParkID,
			"shed_id":   t.ShedID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "pen_visit_task:" + t.TaskID,
		}},
		"payload":  payload,
		"trace_id": "pen-visit:" + t.TaskID,
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":      actorID,
		"business_date": biztime.BusinessDate(now),
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sqlOutbox1,
		t.TenantID, eventID, eventType, eventSchemaVersion, aggregateType, t.TaskID,
		eventTopic, envelope, headers, eventType+":"+idempotencyKey, "pen-visit:"+t.TaskID,
	); err != nil {
		return fmt.Errorf("pen visit: outbox %s: %w", eventType, err)
	}
	return nil
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlOutbox1 = `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, $11, 'pending', now()
)
ON CONFLICT DO NOTHING`
)
