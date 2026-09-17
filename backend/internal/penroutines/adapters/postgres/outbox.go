package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// Domain events (maintainer instruction 2026-09-16, docs/decisions/pen-routines.md). A task
// coming into existence, a task being submitted and a task being verified are the business
// facts of this module; each is emitted INSIDE the write transaction's outbox so a committed
// row always announces itself and a rolled-back one never does. Registered in
// context/architecture/domain-event-registry.json. pen_routine.submitted is consumed by the
// verification enqueue handler (penroutines/app.PendingVerificationHandler) -- it is what turns
// the answers and captures into a verifier item; pen_routine.verified is the audit/event-spine
// fact of the verdict. The day's push is NOT driven by these events: it is one digest per
// routine per due date, queued by the kernel stage once the routine's notify time has passed
// (the FeedLowStockNotifier shape), because one push per pen would be ten pushes for one
// morning's round.
const (
	EventRoutineCreated   = "pen_routine.created"
	EventRoutineSubmitted = "pen_routine.submitted"
	EventRoutineVerified  = "pen_routine.verified"

	eventSchemaVersion = "1.0.0"
	eventSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	eventTopic         = "pen_routines.events"
	aggregateType      = "pen_routine_task"
	producerModule     = "pen_routines"
)

// EventPayload is the inner payload every task event carries.
type EventPayload struct {
	TaskID         string `json:"task_id"`
	RoutineID      string `json:"routine_id"`
	RoutineVersion int    `json:"routine_version"`
	RoutineName    string `json:"routine_name"`
	ReviewKind     string `json:"review_kind"`
	ParkID         string `json:"park_id"`
	ParkName       string `json:"park_name"`
	// ScopeKind: all_pens / selected_pens / park. A whole-park task carries shed_id,
	// partition_label and pen_label as "".
	ScopeKind      string             `json:"scope_kind"`
	ShedID         string             `json:"shed_id"`
	PartitionLabel string             `json:"partition_label"`
	PenLabel       string             `json:"pen_label"`
	TriggerKinds   []string           `json:"trigger_kinds"`
	SourceDate     string             `json:"source_business_date"`
	PlannedDate    string             `json:"planned_business_date"`
	DueDate        string             `json:"due_business_date"`
	WorkState      string             `json:"work_state"`
	Status         string             `json:"status"`
	Proofs         []domain.ProofItem `json:"proof_refs"`
	AnswerRows     []domain.AnswerRow `json:"answer_rows"`
	SubmittedBy    string             `json:"submitted_by,omitempty"`
	VerifiedBy     string             `json:"verified_by,omitempty"`
	RowVersion     int                `json:"row_version"`
	ChangedBy      string             `json:"changed_by_user_id"`
	OccurredAt     string             `json:"occurred_at"`
}

// emitEvent writes one task event into outbox_messages inside tx. The event id is
// deterministic on (type, tenant, idempotency key) so a retried write never double-announces.
// actorType is one of the envelope's closed actor vocabulary: "human" for the assignee's
// submit, "system_rule" for the materializer and the verdict consumer. The envelope is
// validated by the relay against contracts/jsonschema/domain-event-envelope.schema.json before
// publish, and a rejected envelope is a SILENT drop, so the integration test validates every
// emitted envelope against the same schema.
func emitEvent(ctx context.Context, tx pgx.Tx, eventType string, t domain.Task, actorID, actorType, idempotencyKey string, now time.Time) error {
	now = now.UTC()
	eventID := platformoutbox.DeterministicUUID(eventType + ":" + t.TenantID + ":" + idempotencyKey)
	proofs := t.Proofs
	if proofs == nil {
		proofs = []domain.ProofItem{}
	}
	kinds := domain.SortWorkKinds(t.TriggerKinds)
	if kinds == nil {
		kinds = []string{}
	}
	payload := EventPayload{
		TaskID:         t.TaskID,
		RoutineID:      t.RoutineID,
		RoutineVersion: t.RoutineVersion,
		RoutineName:    t.RoutineName,
		ReviewKind:     t.ReviewKind,
		ParkID:         t.ParkID,
		ParkName:       t.ParkName,
		ScopeKind:      t.ScopeKind,
		ShedID:         t.ShedID,
		PartitionLabel: t.Partition,
		PenLabel:       t.PenLabel,
		TriggerKinds:   kinds,
		SourceDate:     t.SourceDate,
		PlannedDate:    t.PlannedDate,
		DueDate:        t.DueDate,
		WorkState:      t.WorkState,
		Status:         t.Status,
		Proofs:         proofs,
		AnswerRows:     domain.AnswerRows(t),
		SubmittedBy:    t.SubmittedBy,
		VerifiedBy:     t.VerifiedBy,
		RowVersion:     t.RowVersion,
		ChangedBy:      actorID,
		OccurredAt:     now.Format(time.RFC3339),
	}
	// The envelope's visibility scope types shed_id as a uuid or null; a whole-park task has none.
	var shedRef any
	if t.ShedID != "" {
		shedRef = t.ShedID
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
			"module":  producerModule,
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
			"shed_id":   shedRef,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "pen_routine_task:" + t.TaskID,
		}},
		"payload":  payload,
		"trace_id": "pen-routine:" + t.TaskID,
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
		eventTopic, envelope, headers, eventType+":"+idempotencyKey, "pen-routine:"+t.TaskID,
	); err != nil {
		return fmt.Errorf("pen routine: outbox %s: %w", eventType, err)
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
