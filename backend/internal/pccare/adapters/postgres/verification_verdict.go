package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// PC Care verdict write path. ApplyVerifiedTask is the ONLY producer of pc_care.task.completed
// (registered in context/architecture/domain-event-registry.json; the literal event type below
// plus the outbox_messages INSERT are what the domain-event-architecture guard matches).
const (
	pcCareTaskCompletedEventType     = "pc_care.task.completed"
	pcCareTaskCompletedSchemaVersion = "1.0.0"
	pcCareTaskCompletedSchemaRef     = "domain-event-envelope.v1"
	pcCareTaskCompletedTopic         = "pc_care.events"
	pcCareTaskCompletedAggregateType = "pc_care_task"

	pcCareCompletedAction      = "pc_care.task.completed"
	pcCareReworkAction         = "pc_care.task.rework"
	pcCarePenVisitClosedAction = "pc_care.task.pen_visit_verified"
)

// ApplyVerifiedTask flips an approved task 'pending_verification' -> 'completed' on the GATE
// column, stamps verified_by/at, and emits pc_care.task.completed. Runs from the
// verification.verdict.approved consumer, never from a phone.
//
// THE KERNEL CLOCK CLOSES ONLY WITH THE PEN VISIT (maintainer decision 2026-09-12). A pen
// task -- one of the five pen categories, with a shed -- owes a next-day visit, and that
// visit's video goes to the verifier too; the maintainer's rule is that the task closes
// "when all these videos are verified". So work_state flips to 'completed' here ONLY when no
// visit is owed (a shed-less task, or a category that raises none) or when the linked visit is
// ALREADY verified (the verifier reviewed the visit before the work's own clips). Otherwise the
// visit's approval closes the task through PenVisitVerified below; either order converges.
//
// IDEMPOTENT: a re-delivered verdict on an already-completed row, or a verdict for a row no
// longer pending (bounced to rework), returns false with no side effects.
func (r *Repository) ApplyVerifiedTask(ctx context.Context, p ports.ApplyVerifiedTaskParams) (bool, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("pccare: begin apply verdict tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var (
		status, category, parkID, shedID, plannedDate string
	)
	err = tx.QueryRow(ctx, `
SELECT status, category, park_id::text, coalesce(shed_id::text, ''), planned_business_date::text
FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND task_id = $2::uuid
FOR UPDATE`, p.TenantID, p.TaskID).Scan(&status, &category, &parkID, &shedID, &plannedDate)
	if errors.Is(err, pgx.ErrNoRows) {
		// No such row for this tenant: a stale/foreign verdict. Ignore.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("pccare: lock task for verdict: %w", err)
	}
	if status != domain.StatusPendingVerification {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}

	tag, err := tx.Exec(ctx, `
UPDATE pc_care_tasks t
SET status = 'completed',
    work_state = CASE WHEN owes.visit THEN t.work_state ELSE 'completed' END,
    terminal_at = CASE WHEN owes.visit THEN t.terminal_at ELSE now() END,
    verified_by = nullif($3::text, '')::uuid,
    verified_at = now(),
    updated_at = now(),
    row_version = t.row_version + 1
FROM (
  SELECT (
    ($4::text = ANY($5::text[]))
    AND EXISTS (SELECT 1 FROM pc_care_tasks x WHERE x.tenant_id = $1::uuid AND x.task_id = $2::uuid AND x.shed_id IS NOT NULL)
    AND NOT EXISTS (
      SELECT 1 FROM pen_visit_task_sources s
      JOIN pen_visit_tasks v ON v.tenant_id = s.tenant_id AND v.task_id = s.task_id
      WHERE s.tenant_id = $1::uuid AND s.source_kind = 'pc_care_task' AND s.source_ref_id = $2::uuid
        AND v.status = 'completed')
  ) AS visit
) owes
WHERE t.tenant_id = $1::uuid AND t.task_id = $2::uuid AND t.status = 'pending_verification'`,
		p.TenantID, p.TaskID, p.VerifiedBy, category, domain.PenVisitCategories)
	if err != nil {
		return false, fmt.Errorf("pccare: apply verified task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}

	if err := insertPCCareTaskCompletedOutbox(ctx, tx, pcCareTaskCompletedOutbox{
		TenantID:            p.TenantID,
		TaskID:              p.TaskID,
		Category:            category,
		ParkID:              parkID,
		ShedID:              shedID,
		PlannedBusinessDate: plannedDate,
		VerifiedBy:          strings.TrimSpace(p.VerifiedBy),
		TraceID:             p.TraceID,
	}); err != nil {
		return false, err
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      strings.TrimSpace(p.VerifiedBy),
		ActorType:    "verifier",
		Action:       pcCareCompletedAction,
		ResourceType: pcCareTaskResourceType,
		ResourceID:   p.TaskID,
		ScopeType:    verdictScopeType(shedID),
		ScopeID:      verdictScopeID(shedID, parkID),
		AfterState: map[string]any{
			"category": category,
			"park_id":  parkID,
			"shed_id":  shedID,
			"status":   domain.StatusCompleted,
		},
		Metadata: map[string]any{"source": "pc-care-verification"},
		TraceID:  p.TraceID,
	}); err != nil {
		return false, fmt.Errorf("pccare: write verdict audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("pccare: commit apply verdict: %w", err)
	}
	committed = true
	return true, nil
}

// BounceTaskForRework flips a rejected task 'pending_verification' -> 'rework' with the
// verifier's reason. Idempotent + stale-guarded: only a pending row is bounced.
func (r *Repository) BounceTaskForRework(ctx context.Context, p ports.BounceTaskParams) (bool, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	tag, err := r.pool.Exec(ctx, `
UPDATE pc_care_tasks
SET status = 'rework',
    rework_reason = nullif($3, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND status = 'pending_verification'`,
		p.TenantID, p.TaskID, strings.TrimSpace(p.Reason))
	if err != nil {
		return false, fmt.Errorf("pccare: bounce task for rework: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// pcCareTaskCompletedOutbox is the input for the pc_care.task.completed outbox envelope.
type pcCareTaskCompletedOutbox struct {
	TenantID            string
	TaskID              string
	Category            string
	ParkID              string
	ShedID              string
	PlannedBusinessDate string
	VerifiedBy          string
	TraceID             string
}

func insertPCCareTaskCompletedOutbox(ctx context.Context, tx pgx.Tx, o pcCareTaskCompletedOutbox) error {
	idempotencyKey := pcCareTaskCompletedEventType + ":" + o.TaskID
	eventID := platformoutbox.DeterministicUUID(pcCareTaskCompletedEventType + ":" + o.TenantID + ":" + o.TaskID)

	payload := map[string]any{
		"task_id":               o.TaskID,
		"category":              o.Category,
		"park_id":               o.ParkID,
		"shed_id":               o.ShedID,
		"planned_business_date": o.PlannedBusinessDate,
		"verified_by":           o.VerifiedBy,
	}
	envelope := pcCareEventEnvelope{
		EventID:        eventID,
		EventType:      pcCareTaskCompletedEventType,
		SchemaVersion:  pcCareTaskCompletedSchemaVersion,
		SchemaRef:      pcCareTaskCompletedSchemaRef,
		AggregateType:  pcCareTaskCompletedAggregateType,
		AggregateID:    o.TaskID,
		IdempotencyKey: idempotencyKey,
		TenantID:       o.TenantID,
		ParkID:         o.ParkID,
		ShedID:         o.ShedID,
		// The verifier who approved the video set is the actor.
		ActorID: o.VerifiedBy,
		// The PLANNED business day, not the verdict instant: the business fact this event
		// reports is that the pen's care work for that day is done and proved.
		OccurredAt: businessInstant(o.PlannedBusinessDate),
		Payload:    payload,
		TraceID:    o.TraceID,
	}.build()
	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("pccare: marshal outbox envelope: %w", err)
	}
	headersJSON, err := json.Marshal(map[string]any{"content_type": "application/json"})
	if err != nil {
		return fmt.Errorf("pccare: marshal outbox headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, $11, 'pending', now()
)
ON CONFLICT DO NOTHING`,
		o.TenantID, eventID, pcCareTaskCompletedEventType, pcCareTaskCompletedSchemaVersion,
		pcCareTaskCompletedAggregateType, o.TaskID, pcCareTaskCompletedTopic,
		envelopeJSON, headersJSON, idempotencyKey, o.TraceID)
	if err != nil {
		return fmt.Errorf("pccare: insert outbox: %w", err)
	}
	return nil
}

// verdictScopeType returns the audit scope for a verdict write: shed-scoped tasks audit at the
// shed; per-vaccine stock tasks (no shed) audit at the park.
func verdictScopeType(shedID string) string {
	if shedID == "" {
		return "park"
	}
	return "shed"
}

func verdictScopeID(shedID, parkID string) string {
	if shedID == "" {
		return parkID
	}
	return shedID
}

// PenVisitVerified closes the tasks whose next-day pen visit the verifier just approved
// (maintainer decision 2026-09-12): a task whose own videos are already verified (status
// 'completed') and whose kernel clock was held open for the visit flips to work_state
// 'completed'. A task whose own clips are still with the verifier, or sent back, is left for
// ApplyVerifiedTask, which sees the verified visit and closes the clock itself. IDEMPOTENT: an
// already-closed task matches nothing, so the at-least-once event consumer can replay it.
func (r *Repository) PenVisitVerified(ctx context.Context, tenantID string, taskIDs []string, visitTaskID, traceID string) error {
	if len(taskIDs) == 0 {
		return nil
	}
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("pccare: begin pen visit close tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	// scale-guard:ignore: one set-based UPDATE over the explicit task ids a single pen visit names (bounded by the pens worked in one pen on one day), keyed by primary key.
	rows, err := tx.Query(ctx, `
UPDATE pc_care_tasks
SET work_state = 'completed',
    terminal_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND task_id = ANY($2::uuid[])
  AND status = 'completed'
  AND work_state IN ('scheduled', 'delayed')
RETURNING task_id::text, category, park_id::text, coalesce(shed_id::text, '')`, tenantID, taskIDs)
	if err != nil {
		return fmt.Errorf("pccare: close on pen visit: %w", err)
	}
	type closed struct{ taskID, category, parkID, shedID string }
	var closedRows []closed
	for rows.Next() {
		var c closed
		if err := rows.Scan(&c.taskID, &c.category, &c.parkID, &c.shedID); err != nil {
			rows.Close()
			return fmt.Errorf("pccare: scan closed task: %w", err)
		}
		closedRows = append(closedRows, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	recorder := audit.NewTxRecorder(tx)
	for _, c := range closedRows {
		if err := recorder.Record(ctx, audit.Event{
			TenantID:     tenantID,
			ActorType:    "system",
			Action:       pcCarePenVisitClosedAction,
			ResourceType: pcCareTaskResourceType,
			ResourceID:   c.taskID,
			ScopeType:    verdictScopeType(c.shedID),
			ScopeID:      verdictScopeID(c.shedID, c.parkID),
			AfterState: map[string]any{
				"category":   c.category,
				"park_id":    c.parkID,
				"shed_id":    c.shedID,
				"status":     domain.StatusCompleted,
				"work_state": domain.WorkStateCompleted,
			},
			Metadata: map[string]any{"source": "pen-visit-verification", "pen_visit_task_id": visitTaskID},
			TraceID:  traceID,
		}); err != nil {
			return fmt.Errorf("pccare: write pen visit close audit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pccare: commit pen visit close: %w", err)
	}
	committed = true
	return nil
}
