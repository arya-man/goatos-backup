package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Domain events (maintainer decision 2026-09-04). A raised task and a status change are
// business events with an outward consequence -- the push to the person it concerns. Each
// is emitted INSIDE the write transaction's outbox, so a committed task always announces
// itself and a rolled-back one never does. Registered in
// context/architecture/domain-event-registry.json; consumed by
// notificationbridge.LeadershipTaskNotifyConsumer.
const (
	EventTaskRaised        = "leadership_task.raised"
	EventTaskStatusChanged = "leadership_task.status_changed"

	eventSchemaVersion = "1.0.0"
	eventSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	eventTopic         = "leadership_tasks.events"
	aggregateType      = "leadership_task"
)

// EventPayload is the inner payload both events carry: everything the notifier needs to
// compose a specific message without a second read.
type EventPayload struct {
	TaskID         string `json:"task_id"`
	TaskNo         int64  `json:"task_no"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status,omitempty"`
	RaisedByUserID string `json:"raised_by_user_id"`
	RaisedByName   string `json:"raised_by_name"`
	AssigneeUserID string `json:"assignee_user_id"`
	AssigneeName   string `json:"assignee_name"`
	AttachmentCnt  int    `json:"attachment_count"`
	ChangedBy      string `json:"changed_by_user_id"`
	OccurredAt     string `json:"occurred_at"`
}

// emitEvent writes one task event into outbox_messages inside tx.
func emitEvent(ctx context.Context, tx pgx.Tx, eventType string, t domain.Task, previousStatus, actorID, idempotencyKey string, now time.Time) error {
	var eventID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&eventID); err != nil {
		return fmt.Errorf("leadership task: event id: %w", err)
	}
	now = now.UTC()
	payload := EventPayload{
		TaskID:         t.TaskID,
		TaskNo:         t.TaskNo,
		Title:          t.Title,
		Status:         t.Status,
		PreviousStatus: previousStatus,
		RaisedByUserID: t.RaisedByUserID,
		RaisedByName:   t.RaisedByName,
		AssigneeUserID: t.AssigneeUserID,
		AssigneeName:   t.AssigneeName,
		AttachmentCnt:  len(t.Attachments),
		ChangedBy:      actorID,
		OccurredAt:     now.Format(time.RFC3339),
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
			"module":  "leadership_tasks",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "human",
			"actor_id":   actorID,
			"actor_ref":  nil,
		},
		"subject_type": aggregateType,
		"subject_id":   t.TaskID,
		"visibility_scope": map[string]any{
			"tenant_id": t.TenantID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "leadership_task:" + t.TaskID,
		}},
		"payload":  payload,
		"trace_id": "leadership-task:" + t.TaskID,
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
		eventTopic, envelope, headers, eventType+":"+idempotencyKey,
	); err != nil {
		return fmt.Errorf("leadership task: outbox %s: %w", eventType, err)
	}
	return nil
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlOutbox1 = `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, status
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, 'pending'
)`
)
