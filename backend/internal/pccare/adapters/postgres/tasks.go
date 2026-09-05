package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

const (
	pcCareTaskResourceType = "pc_care_task"
	pcCareCreateIdemScope  = "pc_care.task.create"

	pcCareCreatedAction  = "pc_care.task.created"
	pcCareClosedAction   = "pc_care.task.closed"
	pcCareReopenedAction = "pc_care.task.reopened"
)

// pcCareAssignableRoles is WHO may be assigned a PC Care task: field operators and the PC
// Director (who may execute alongside their oversight, mirroring the weighing shape).
var pcCareAssignableRoles = []string{"operator", "pc_director"}

// CreateTask inserts the task + its assignees in one transaction with audit + request-level
// idempotency. A live natural-key collision returns domain.ErrTaskAlreadyPlanned.
func (r *Repository) CreateTask(ctx context.Context, p ports.CreateTaskParams) (ports.TaskRow, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	plannedDate := p.PlannedBusinessDate.Format("2006-01-02")

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: begin create task tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if err := requireShedPartitionInPark(ctx, tx, p.TenantID, p.ParkID, p.ShedID, p.PartitionLabel); err != nil {
		return ports.TaskRow{}, err
	}

	// Every named assignee must be a real, active workforce member of this tenant. Assigning a
	// task to an id nothing resolves would create work no phone ever lists. The removal
	// operators are held to the identical bar — one set-based check over the union.
	verifyUserIDs := append(append([]string{}, p.AssigneeUserIDs...), p.RemovalOperatorUserIDs...)
	var assigneeCount int
	if err := tx.QueryRow(ctx, activeMembersCountSQL,
		p.TenantID, verifyUserIDs).Scan(&assigneeCount); err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: verify assignees: %w", err)
	}
	if assigneeCount != len(distinctIDs(verifyUserIDs)) {
		return ports.TaskRow{}, ports.ErrInvalidArgument
	}
	// ...and every one of them must be scoped to THIS park (or tenant-wide). The
	// removal operator is held to the same bar as the task's own assignees: a
	// removal card handed to another park's operator is work at pens they cannot
	// reach, and nothing downstream re-checks it (the work list filters on the
	// assignee alone). Mirrors weighing's assertOperatorsScopedToPark.
	if err := assertOperatorsScopedToPark(ctx, tx, p.TenantID, p.ParkID, verifyUserIDs); err != nil {
		return ports.TaskRow{}, err
	}

	// The fingerprint gains the removal fields ONLY when the toggle rides the request, so a
	// replay of a pre-existing plain create hashes exactly as it always did, while the same key
	// re-sent with a different removal payload is a same-key/different-payload conflict.
	fingerprintParts := []string{
		p.Category, p.ParkID, p.ShedID, domain.PartitionMatchKey(p.PartitionLabel),
		plannedDate, strings.Join(p.AssigneeUserIDs, ","),
	}
	if p.FeedRemovalRequired {
		fingerprintParts = append(fingerprintParts, "feed_removal", strings.Join(p.RemovalOperatorUserIDs, ","))
	}
	fingerprint := requestFingerprint(fingerprintParts...)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, pcCareCreateIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.TaskRow{}, err
	}
	if !reservation.proceed {
		// Exact replay: return the original task, run no side effects.
		if err := tx.Commit(ctx); err != nil {
			return ports.TaskRow{}, fmt.Errorf("pccare: commit idempotent create replay: %w", err)
		}
		committed = true
		return r.GetTask(ctx, p.TenantID, reservation.resultID, nil, true)
	}

	var taskID string
	err = tx.QueryRow(ctx, `
INSERT INTO pc_care_tasks (
  tenant_id, category, park_id, shed_id, partition_label,
  planned_business_date, due_business_date, idempotency_key, created_by
) VALUES (
  $1::uuid, $2, $3::uuid, $4::uuid, nullif($5::text, ''),
  $6::date, $6::date, $7, $8::uuid
)
ON CONFLICT (tenant_id, category, park_id, shed_id, partition_key, planned_business_date)
  WHERE work_state <> 'canceled'
DO NOTHING
RETURNING task_id::text`,
		p.TenantID, p.Category, p.ParkID, p.ShedID, p.PartitionLabel,
		plannedDate, p.IdempotencyKey, p.CreatedBy).Scan(&taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.TaskRow{}, domain.ErrTaskAlreadyPlanned
	}
	if err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: insert task: %w", err)
	}

	// One set-based insert for the whole assignee list — never a statement per operator.
	if _, err := tx.Exec(ctx, `
INSERT INTO pc_care_task_assignees (tenant_id, task_id, operator_user_id)
SELECT $1::uuid, $2::uuid, unnest($3::uuid[])`,
		p.TenantID, taskID, p.AssigneeUserIDs); err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: insert task assignees: %w", err)
	}

	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "human"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       pcCareCreatedAction,
		ResourceType: pcCareTaskResourceType,
		ResourceID:   taskID,
		ScopeType:    "shed",
		ScopeID:      p.ShedID,
		AfterState: map[string]any{
			"category":              p.Category,
			"park_id":               p.ParkID,
			"shed_id":               p.ShedID,
			"partition_label":       p.PartitionLabel,
			"planned_business_date": plannedDate,
			"assignee_user_ids":     p.AssigneeUserIDs,
		},
		Metadata: map[string]any{"source": "pc-care-planner"},
		TraceID:  p.TraceID,
	}); err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: write create audit: %w", err)
	}

	// Feed & water removal precondition (maintainer decision 2026-09-03): the SAME transaction
	// that plans a tablet-in-feed deworming plans the evening-before removal — same pen, one day
	// earlier, its own operators, gates_task_id pointing back at the deworming so the midnight
	// gate can hold the deworming until the removal is submitted.
	if p.FeedRemovalRequired {
		removalDate := p.PlannedBusinessDate.AddDate(0, 0, -1).Format("2006-01-02")
		var removalTaskID string
		err = tx.QueryRow(ctx, removalTaskInsertSQL,
			p.TenantID, domain.CategoryFeedWaterRemoval, p.ParkID, p.ShedID, p.PartitionLabel,
			removalDate, taskID, p.IdempotencyKey+":fasting", p.CreatedBy).Scan(&removalTaskID)
		if errors.Is(err, pgx.ErrNoRows) {
			// A live removal task already covers this pen on the evening-before date (e.g. a
			// canceled deworming left its removal row live). The pair cannot be planned whole,
			// so the WHOLE create rolls back rather than shipping a deworming with no gate.
			return ports.TaskRow{}, domain.ErrTaskAlreadyPlanned
		}
		if err != nil {
			return ports.TaskRow{}, fmt.Errorf("pccare: insert removal task: %w", err)
		}
		if _, err := tx.Exec(ctx, removalAssigneesInsertSQL,
			p.TenantID, removalTaskID, p.RemovalOperatorUserIDs); err != nil {
			return ports.TaskRow{}, fmt.Errorf("pccare: insert removal task assignees: %w", err)
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     p.TenantID,
			ActorID:      p.ActorID,
			ActorType:    actorType,
			Action:       pcCareCreatedAction,
			ResourceType: pcCareTaskResourceType,
			ResourceID:   removalTaskID,
			ScopeType:    "shed",
			ScopeID:      p.ShedID,
			AfterState: map[string]any{
				"category":              domain.CategoryFeedWaterRemoval,
				"park_id":               p.ParkID,
				"shed_id":               p.ShedID,
				"partition_label":       p.PartitionLabel,
				"planned_business_date": removalDate,
				"assignee_user_ids":     p.RemovalOperatorUserIDs,
				"gates_task_id":         taskID,
			},
			Metadata: map[string]any{"source": "pc-care-planner"},
			TraceID:  p.TraceID,
		}); err != nil {
			return ports.TaskRow{}, fmt.Errorf("pccare: write removal create audit: %w", err)
		}
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, pcCareCreateIdemScope, p.IdempotencyKey, pcCareTaskResourceType, taskID); err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: complete create idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: commit create task: %w", err)
	}
	committed = true
	return r.GetTask(ctx, p.TenantID, taskID, nil, true)
}

// distinctIDs de-duplicates an id list (the create's union assignee-existence check compares a
// DISTINCT count against it).
func distinctIDs(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// CancelTask flips work_state -> canceled. Only an unsubmitted task can be canceled; a locked
// (pending_verification) or terminal task is a stale-guarded no-op.

// removalTaskInsertSQL / removalAssigneesInsertSQL are the linked
// feed_water_removal create (maintainer decision 2026-09-03) — package-level so
// query-plan tests and the scale guard can reach them.
// assertOperatorsScopedToPark refuses any user id in ids whose active scope grants
// neither the tenant nor this park. One set-based read over the whole list, never
// a query per operator.
func assertOperatorsScopedToPark(ctx context.Context, tx pgx.Tx, tenantID, parkID string, ids []string) error {
	distinct := distinctIDs(ids)
	if len(distinct) == 0 {
		return nil
	}
	var offending int
	if err := tx.QueryRow(ctx, operatorsOutsideParkCountSQL, tenantID, parkID, distinct).Scan(&offending); err != nil {
		return fmt.Errorf("pccare: verify assignee park scope: %w", err)
	}
	if offending > 0 {
		return ports.ErrOperatorOutsidePark
	}
	return nil
}

// operatorsOutsideParkCountSQL counts the named users with NO active grant that is
// tenant-wide or names this park.
const operatorsOutsideParkCountSQL = `
SELECT count(*)::int
FROM unnest($3::uuid[]) AS u(user_id)
WHERE NOT EXISTS (
  SELECT 1 FROM user_scope_grants g
  WHERE g.tenant_id = $1::uuid
    AND g.user_id = u.user_id
    AND g.status = 'active'
    AND (g.scope_type = 'tenant' OR (g.scope_type = 'park' AND g.scope_id = $2::uuid))
)`

// activeMembersCountSQL verifies every named assignee (task + removal
// operators, one set-based read) resolves to an active workforce member.
const activeMembersCountSQL = `
SELECT count(DISTINCT m.user_id)::int
FROM workforce_members m
WHERE m.tenant_id = $1::uuid AND m.status = 'active' AND m.user_id = ANY($2::uuid[])`

const removalTaskInsertSQL = `
INSERT INTO pc_care_tasks (
  tenant_id, category, park_id, shed_id, partition_label,
  planned_business_date, due_business_date, gates_task_id, idempotency_key, created_by
) VALUES (
  $1::uuid, $2, $3::uuid, $4::uuid, nullif($5::text, ''),
  $6::date, $6::date, $7::uuid, $8, $9::uuid
)
ON CONFLICT (tenant_id, category, park_id, shed_id, partition_key, planned_business_date)
  WHERE work_state <> 'canceled'
DO NOTHING
RETURNING task_id::text`

const removalAssigneesInsertSQL = `
INSERT INTO pc_care_task_assignees (tenant_id, task_id, operator_user_id)
SELECT $1::uuid, $2::uuid, unnest($3::uuid[])`

// CloseTask ends one task's work (maintainer decision 2026-09-05, RETIRING the cancel verb
// this replaces): work_state -> closed, stamped with who closed it and why.
//
// PC Care is now on par with weighing, which has never had a cancel: "exactly two verbs —
// CLOSE a task, or REOPEN it if it is already closed. There is no third verb." The
// difference is not cosmetic. CANCEL erased a plan, and because the natural key excludes
// canceled rows the pen-day became re-plannable as though nothing had ever been planned
// there. CLOSE records what happened — planned, then ended without being done — and the
// pen-day STAYS TAKEN.
//
// THE CLOSE GATE IS UNCONDITIONAL, exactly as weighing's is (ledger D-5). A task whose
// evidence is awaiting a verdict cannot close, and there is no caller-supplied way past
// that: if a task will not close, the answer is to RESOLVE the verification — get the
// verdict — never to add a path around the gate. An already-terminal task is an idempotent
// no-op, so a retried tap after a network blip reports success rather than an error.
func (r *Repository) CloseTask(ctx context.Context, p ports.CloseTaskParams) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	reason := strings.TrimSpace(p.Reason)
	if reason == "" {
		return domain.ErrCloseReasonRequired
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("pccare: begin close tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// The gate is read under the row lock, so a verdict landing mid-close cannot slip past it.
	var status, workState, shedID, parkID string
	err = tx.QueryRow(ctx, `
SELECT status, work_state, coalesce(shed_id::text, ''), park_id::text
FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND task_id = $2::uuid
FOR UPDATE`, p.TenantID, p.TaskID).Scan(&status, &workState, &shedID, &parkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("pccare: lock task for close: %w", err)
	}
	if status == domain.StatusPendingVerification {
		return domain.ErrVerificationPending
	}
	if workState != domain.WorkStateScheduled && workState != domain.WorkStateDelayed {
		// Already closed, completed or canceled: settled work, reported as an accepted no-op.
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		committed = true
		return nil
	}

	if _, err := tx.Exec(ctx, `
UPDATE pc_care_tasks
SET work_state = 'closed', terminal_at = now(), closed_by = $3::uuid, close_reason = $4,
    updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		p.TenantID, p.TaskID, p.ClosedBy, reason); err != nil {
		return fmt.Errorf("pccare: close task: %w", err)
	}

	// CLOSE CASCADES TO THE LINKED REMOVAL, as cancel did before it (maintainer decision
	// 2026-09-03): a closed deworming's evening feed & water removal serves nothing, and
	// leaving it live would put a crew out to empty pens for work nobody will do. Only a
	// not-yet-submitted removal is closed — submitted evidence is history, and its verdict
	// still belongs to the verifier. This is the LEGACY 1:1 pair only; a ROUND's removal card
	// gates the round's other pens too, so it is closed by CloseRound rather than by any one
	// pen's close.
	if _, err := tx.Exec(ctx, `
UPDATE pc_care_tasks
SET work_state = 'closed', terminal_at = now(), closed_by = $3::uuid, close_reason = $4,
    updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND gates_task_id = $2::uuid
  AND work_state IN ('scheduled', 'delayed')
  AND status IN ('open', 'rework')
  AND submitted_at IS NULL`, p.TenantID, p.TaskID, p.ClosedBy, reason); err != nil {
		return fmt.Errorf("pccare: close linked removal task: %w", err)
	}

	if err := recordTaskLifecycleAudit(ctx, tx, p.TenantID, p.ActorID, pcCareClosedAction, p.TaskID, shedID, parkID, map[string]any{
		"work_state":   domain.WorkStateClosed,
		"close_reason": reason,
	}, p.TraceID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pccare: commit close task: %w", err)
	}
	committed = true
	return nil
}

// ReopenTask undoes a close: work_state closed -> scheduled, clearing the close stamp. The
// due date is left alone — an overdue reopened task is carried by the ordinary roll-forward
// sweep, the same path every other late task takes.
//
// Only a CLOSED task reopens. A completed task is accepted work and is never reopened this
// way, and a canceled one is pre-retirement history. Refusing beats silently doing nothing:
// a planner who reopened the wrong task is owed the correction.
func (r *Repository) ReopenTask(ctx context.Context, p ports.ReopenTaskParams) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("pccare: begin reopen tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var shedID, parkID string
	err = tx.QueryRow(ctx, `
UPDATE pc_care_tasks
SET work_state = 'scheduled', terminal_at = NULL, closed_by = NULL, close_reason = NULL,
    updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND work_state = 'closed'
RETURNING coalesce(shed_id::text, ''), park_id::text`, p.TenantID, p.TaskID).Scan(&shedID, &parkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotClosed
	}
	if err != nil {
		return fmt.Errorf("pccare: reopen task: %w", err)
	}
	if err := recordTaskLifecycleAudit(ctx, tx, p.TenantID, p.ActorID, pcCareReopenedAction, p.TaskID, shedID, parkID, map[string]any{
		"work_state": domain.WorkStateScheduled,
	}, p.TraceID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pccare: commit reopen task: %w", err)
	}
	committed = true
	return nil
}

// recordTaskLifecycleAudit writes the close/reopen audit row. A per-vaccine stock task has
// no shed, so its audit scope is the park.
func recordTaskLifecycleAudit(ctx context.Context, tx pgx.Tx, tenantID, actorID, action, taskID, shedID, parkID string, after map[string]any, traceID string) error {
	scopeType, scopeID := "shed", shedID
	if shedID == "" {
		scopeType, scopeID = "park", parkID
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       action,
		ResourceType: pcCareTaskResourceType,
		ResourceID:   taskID,
		ScopeType:    scopeType,
		ScopeID:      scopeID,
		AfterState:   after,
		Metadata:     map[string]any{"source": "pc-care-planner"},
		TraceID:      traceID,
	}); err != nil {
		return fmt.Errorf("pccare: write %s audit: %w", action, err)
	}
	return nil
}

// taskSelectColumns is the ONE column list every task read scans, so a new column cannot be
// added to one read and missed in another (scan-count discipline).
const taskSelectColumns = `
  t.task_id::text,
  t.category,
  t.park_id::text,
  park.name,
  coalesce(t.shed_id::text, ''),
  coalesce(shed.name, ''),
  coalesce(t.partition_label, ''),
  coalesce(t.vaccine_label, ''),
  t.planned_business_date::text,
  t.due_business_date::text,
  t.work_state,
  t.status,
  coalesce(t.rework_reason, ''),
  coalesce(t.close_reason, ''),
  t.row_version,
  coalesce(t.submitted_by::text, ''),
  t.submitted_at,
  coalesce(assignees.user_ids, ARRAY[]::text[]),
  coalesce(assignees.names, ARRAY[]::text[]),
  coalesce(animals.animal_count, 0),
  coalesce(requirements.items, '[]'::jsonb),
  coalesce(task_proofs.items, '[]'::jsonb)`

// taskFromJoins is the FROM/JOIN block matching taskSelectColumns. The assignee and animal
// sides are PRE-AGGREGATED to exactly one row per task before joining, so they cannot multiply
// task rows (grain proof: producer unique per (tenant_id, task_id); consumer matches on the
// task PK; join cardinality 1:0..1).
const taskFromJoins = `
FROM pc_care_tasks t
JOIN locations park ON park.tenant_id = t.tenant_id AND park.location_id = t.park_id
LEFT JOIN locations shed ON shed.tenant_id = t.tenant_id AND shed.location_id = t.shed_id
LEFT JOIN LATERAL (
  SELECT array_agg(a.operator_user_id::text ORDER BY m.display_name, a.operator_user_id) AS user_ids,
         array_agg(coalesce(m.display_name, '') ORDER BY m.display_name, a.operator_user_id) AS names
  FROM pc_care_task_assignees a
  LEFT JOIN workforce_members m
    ON m.tenant_id = a.tenant_id AND m.user_id = a.operator_user_id AND m.status = 'active'
  WHERE a.tenant_id = t.tenant_id AND a.task_id = t.task_id
) assignees ON true
LEFT JOIN LATERAL (
  SELECT count(*)::int AS animal_count
  FROM pc_care_task_animals an
  WHERE an.tenant_id = t.tenant_id AND an.task_id = t.task_id
) animals ON true
LEFT JOIN LATERAL (
  SELECT jsonb_agg(
           jsonb_build_object(
             'vaccine_label', r.vaccine_label,
             'required_doses', r.required_doses,
             'source_batch_ids', (
               SELECT coalesce(jsonb_agg(b::text ORDER BY b::text), '[]'::jsonb)
               FROM unnest(r.source_batch_ids) AS b
             )
           )
           ORDER BY r.vaccine_label
         ) AS items
  FROM pc_care_task_inventory_requirements r
  WHERE r.tenant_id = t.tenant_id AND r.task_id = t.task_id
) requirements ON true
LEFT JOIN LATERAL (
  SELECT jsonb_agg(
           jsonb_build_object(
             'slot_key', p.slot_key,
             'proof_ref', p.proof_ref,
             'captured_by', p.captured_by::text,
             'captured_by_name', coalesce(m.display_name, ''),
             'captured_at', p.captured_at
           )
           ORDER BY p.slot_key
         ) AS items
  FROM pc_care_task_proofs p
  LEFT JOIN workforce_members m
    ON m.tenant_id = p.tenant_id AND m.user_id = p.captured_by AND m.status = 'active'
  WHERE p.tenant_id = t.tenant_id AND p.task_id = t.task_id
) task_proofs ON true`

func scanTaskRow(row pgx.Row) (ports.TaskRow, error) {
	var t ports.TaskRow
	var submittedAt *time.Time
	var requirementsJSON []byte
	var taskProofsJSON []byte
	if err := row.Scan(
		&t.TaskID, &t.Category, &t.ParkID, &t.ParkName, &t.ShedID, &t.ShedName,
		&t.PartitionLabel, &t.VaccineLabel, &t.PlannedBusinessDate, &t.DueBusinessDate,
		&t.WorkState, &t.Status, &t.ReworkReason, &t.CloseReason, &t.RowVersion,
		&t.SubmittedBy, &submittedAt, &t.AssigneeUserIDs, &t.AssigneeNames, &t.AnimalCount,
		&requirementsJSON, &taskProofsJSON,
	); err != nil {
		return ports.TaskRow{}, err
	}
	if len(requirementsJSON) > 0 {
		var raw []struct {
			VaccineLabel   string   `json:"vaccine_label"`
			RequiredDoses  int32    `json:"required_doses"`
			SourceBatchIDs []string `json:"source_batch_ids"`
		}
		if err := json.Unmarshal(requirementsJSON, &raw); err != nil {
			return ports.TaskRow{}, fmt.Errorf("pccare: decode inventory requirements: %w", err)
		}
		t.InventoryRequirements = make([]ports.InventoryRequirement, 0, len(raw))
		for _, item := range raw {
			t.InventoryRequirements = append(t.InventoryRequirements, ports.InventoryRequirement{
				VaccineLabel: item.VaccineLabel, RequiredDoses: item.RequiredDoses, SourceBatchIDs: item.SourceBatchIDs,
			})
		}
	}
	if len(taskProofsJSON) > 0 {
		var raw []struct {
			SlotKey        string    `json:"slot_key"`
			ProofRef       string    `json:"proof_ref"`
			CapturedBy     string    `json:"captured_by"`
			CapturedByName string    `json:"captured_by_name"`
			CapturedAt     time.Time `json:"captured_at"`
		}
		if err := json.Unmarshal(taskProofsJSON, &raw); err != nil {
			return ports.TaskRow{}, fmt.Errorf("pccare: decode task proofs: %w", err)
		}
		t.TaskProofs = make([]ports.TaskProofRow, 0, len(raw))
		for _, item := range raw {
			t.TaskProofs = append(t.TaskProofs, ports.TaskProofRow{
				SlotKey: item.SlotKey, ProofRef: item.ProofRef, CapturedBy: item.CapturedBy,
				CapturedByName: item.CapturedByName, CapturedAt: item.CapturedAt,
			})
		}
	}
	t.SubmittedAt = submittedAt
	return t, nil
}

// GetTask reads one task clamped to the authorized parks; outside scope reads as not-found.
func (r *Repository) GetTask(ctx context.Context, tenantID, taskID string, authorizedParkIDs []string, tenantWide bool) (ports.TaskRow, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	// scale-guard:ignore: single-task read by primary key with two 1:0..1 lateral aggregates bounded by one task's assignees/animals.
	row := r.pool.QueryRow(ctx, `
SELECT`+taskSelectColumns+taskFromJoins+`
WHERE t.tenant_id = $1::uuid AND t.task_id = $2::uuid
  AND ($3::bool OR t.park_id = ANY($4::uuid[]))`,
		tenantID, taskID, tenantWide, authorizedParkIDs)
	t, err := scanTaskRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.TaskRow{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: get task: %w", err)
	}
	return t, nil
}

// ListTasks serves the monitor list and (with AssigneeUserID) the operator worklist: one
// bounded keyset page of one due date's tasks, park-clamped.
//
// projection-review: membership=pc_care_tasks rows for ONE due business date within the
// caller's authorized parks (optionally one park / one category / one assignee);
// group_key=task_id (the page rows ARE the grain); join_cardinality=locations 1:1 by PK,
// assignees/animals pre-aggregated 1:0..1 laterals; pagination=bounded keyset over the
// park's task list for one day (bounded by pens x categories, never herd size); scope=tenant_id
// + park clamp.
func (r *Repository) ListTasks(ctx context.Context, q ports.ListTasksQuery) (ports.TaskPage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	limit := q.Limit
	if limit <= 0 {
		limit = 25
	}

	afterPark, afterShed, afterPartition, afterCategory, afterTask, err := decodeTaskCursor(q.Cursor)
	if err != nil {
		return ports.TaskPage{}, ports.ErrInvalidArgument
	}

	// scale-guard:ignore: one bounded page of one due-day's tasks, covered by pc_care_tasks_serving_idx (tenant_id, park_id, due_business_date, work_state); bounded keyset over the park's pen catalog x categories, never by herd size.
	//
	// Every optional uuid parameter is nullif-guarded: a bare `$n::uuid` of '' raises 22P02 at
	// PLAN time when the planner folds the row-comparison keyset arm, even though the `$n = ''`
	// guard short-circuits at execution (first-page reads 500'd on STG, 2026-08-22).
	listTasksPageSQL := `
	WHERE t.tenant_id = $1::uuid
	  AND (
	        (NOT $14::bool AND t.due_business_date = $2::date)
	        OR ($14::bool AND (
	             (t.due_business_date = $2::date)
	             OR (t.due_business_date < $2::date AND t.work_state IN ('scheduled', 'delayed'))
	        ))
	      )
  AND t.work_state <> 'canceled'
	  -- Evening visibility (maintainer decision 2026-09-03), the shiftingActionsVisibleSQL shape:
	  -- a feed & water removal card surfaces on the list only from 20:00 IST of its due day —
	  -- the work is "tonight, after the animals' last feed", so an all-day card would invite
	  -- removing feed at 9 AM. $15 carries the CALLER's clock (deterministic in tests); the IST
	  -- wall-clock comparison mirrors counts' actions lead-time predicate. Other categories pass
	  -- through untouched, and detail/get-by-id reads never apply this — visibility narrows the
	  -- LIST, not the record. Computed predicate over a page already narrowed by
	  -- pc_care_tasks_serving_idx to one park-day, so the extra work is bounded by that page.
	  AND (
	        t.category <> 'feed_water_removal'
	        OR ($15::timestamptz AT TIME ZONE 'Asia/Kolkata') >= (t.due_business_date + TIME '20:00')
	      )
  AND ($3::bool OR t.park_id = ANY($4::uuid[]))
  AND ($5::text = '' OR t.park_id = nullif($5::text, '')::uuid)
  -- The Deworming TAB also lists the linked feed & water removal cards
  -- (maintainer decision 2026-09-03, "cards are separate, each in each
  -- category"): the removal is deworming's own evening precondition and the
  -- pc_care bar stays locked to its four tabs, so the removal card surfaces
  -- under Deworming rather than growing a fifth tab. Every other category
  -- filter is exact.
  AND ($6::text = '' OR t.category = $6
       OR ($6::text = 'deworming' AND t.category = 'feed_water_removal'))
	  AND ($7::text = '' OR EXISTS (
	        SELECT 1 FROM pc_care_task_assignees mine
	        WHERE mine.tenant_id = t.tenant_id AND mine.task_id = t.task_id
	          AND mine.operator_user_id = nullif($7::text, '')::uuid))
	  AND (
	        $9::text = ''
	        OR (park.name, coalesce(shed.name, coalesce(t.vaccine_label, '')), t.partition_key, t.category, t.task_id)
	           > ($9::text, $10::text, $11::text, $12::text, nullif($13::text, '')::uuid)
	      )
	ORDER BY park.name, coalesce(shed.name, coalesce(t.vaccine_label, '')), t.partition_key, t.category, t.task_id
	LIMIT $8`
	now := q.Now
	if now.IsZero() {
		// Defensive fallback for internal callers that never set a clock; the service always
		// fills Now from its injectable clock.
		now = time.Now()
	}
	rows, err := r.pool.Query(ctx, "SELECT"+taskSelectColumns+taskFromJoins+listTasksPageSQL,
		q.TenantID, q.DueBusinessDate, q.TenantWide, q.AuthorizedParkIDs,
		q.ParkID, q.Category, q.AssigneeUserID, limit+1,
		afterPark, afterShed, afterPartition, afterCategory, afterTask, q.CurrentOrCarry,
		now)
	if err != nil {
		return ports.TaskPage{}, fmt.Errorf("pccare: list tasks: %w", err)
	}
	defer rows.Close()

	items := make([]ports.TaskRow, 0, limit)
	var last ports.TaskRow
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return ports.TaskPage{}, fmt.Errorf("pccare: scan task row: %w", err)
		}
		items = append(items, t)
		last = t
	}
	if err := rows.Err(); err != nil {
		return ports.TaskPage{}, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
		last = items[len(items)-1]
	}
	nextCursor := ""
	if hasMore {
		nextCursor = encodeTaskCursor(last)
	}
	return ports.TaskPage{Items: items, NextCursor: nextCursor}, nil
}

type taskCursor struct {
	ParkName     string `json:"p"`
	ShedName     string `json:"s"`
	PartitionKey string `json:"k"`
	Category     string `json:"c"`
	TaskID       string `json:"t"`
}

func encodeTaskCursor(t ports.TaskRow) string {
	sortShed := t.ShedName
	if sortShed == "" {
		sortShed = t.VaccineLabel
	}
	raw, err := json.Marshal(taskCursor{
		ParkName:     t.ParkName,
		ShedName:     sortShed,
		PartitionKey: domain.PartitionMatchKey(t.PartitionLabel),
		Category:     t.Category,
		TaskID:       t.TaskID,
	})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeTaskCursor(cursor string) (parkName, shedName, partitionKey, category, taskID string, err error) {
	if strings.TrimSpace(cursor) == "" {
		return "", "", "", "", "", nil
	}
	raw, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
	if decodeErr != nil {
		return "", "", "", "", "", decodeErr
	}
	var c taskCursor
	if unmarshalErr := json.Unmarshal(raw, &c); unmarshalErr != nil {
		return "", "", "", "", "", unmarshalErr
	}
	if c.ParkName == "" || c.ShedName == "" || c.Category == "" || c.TaskID == "" {
		return "", "", "", "", "", fmt.Errorf("incomplete task cursor")
	}
	if !uuidutil.IsUUIDString(c.TaskID) {
		return "", "", "", "", "", fmt.Errorf("invalid task cursor id")
	}
	return c.ParkName, c.ShedName, c.PartitionKey, c.Category, c.TaskID, nil
}

// IsAssignee reports whether userID is named on the task.
func (r *Repository) IsAssignee(ctx context.Context, tenantID, taskID, userID string) (bool, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	var ok bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM pc_care_task_assignees
  WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND operator_user_id = $3::uuid
)`, tenantID, taskID, userID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("pccare: check assignee: %w", err)
	}
	return ok, nil
}

// PlannerCatalog returns the tenant's active parks and its assignable operators (weighing
// PlannerCatalog clone, without weighing's per-park task decorations — pen availability is
// answered by PlannerParkSheds).
func (r *Repository) PlannerCatalog(ctx context.Context, tenantID string) (ports.PlannerCatalog, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	out := ports.PlannerCatalog{Parks: []ports.PlannerPark{}, Operators: []ports.PlannerOperator{}}

	// scale-guard:ignore: parks of one tenant (single digits), hard-capped; covered by the locations tenant/type/status index.
	rows, err := r.pool.Query(ctx, `
SELECT park.location_id::text, park.name
FROM locations park
WHERE park.tenant_id = $1::uuid
  AND park.location_type = 'park'
  AND park.status = 'active'
  AND park.retired_at IS NULL
ORDER BY park.display_order, park.name, park.location_id
LIMIT 50`, tenantID)
	if err != nil {
		return ports.PlannerCatalog{}, fmt.Errorf("pccare: planner parks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var park ports.PlannerPark
		if err := rows.Scan(&park.ParkID, &park.ParkName); err != nil {
			return ports.PlannerCatalog{}, err
		}
		out.Parks = append(out.Parks, park)
	}
	if err := rows.Err(); err != nil {
		return ports.PlannerCatalog{}, err
	}

	// WHO belongs here is "may be assigned PC Care work": membership follows the assignable
	// roles (weighing operator-picker clone, including the empty-park_ids = cross-park rule).
	// scale-guard:ignore: the assignable field roster of one tenant, hard-capped at 200; grants are pre-aggregated per member.
	operatorRows, err := r.pool.Query(ctx, `
SELECT m.user_id::text, m.display_name,
       COALESCE(
         (SELECT array_agg(DISTINCT g2.scope_id::text)
          FROM user_scope_grants g2
          WHERE g2.tenant_id = m.tenant_id
            AND g2.user_id = m.user_id
            AND g2.status = 'active'
            AND g2.scope_type = 'park'
            AND NOT EXISTS (
              SELECT 1 FROM user_scope_grants g3
              WHERE g3.tenant_id = m.tenant_id AND g3.user_id = m.user_id
                AND g3.status = 'active' AND g3.scope_type = 'tenant'
            )),
         ARRAY[]::text[]
       ) AS park_ids
FROM workforce_members m
WHERE m.tenant_id = $1::uuid
  AND m.status = 'active'
  AND m.user_id IS NOT NULL
  AND (
    m.primary_role_hint = ANY($2::text[])
    OR EXISTS (
      SELECT 1 FROM user_scope_grants g
      WHERE g.tenant_id = m.tenant_id
        AND g.user_id = m.user_id
        AND g.status = 'active'
        AND g.role = ANY($2::text[])
    )
  )
ORDER BY m.display_name, m.user_id
LIMIT 200`, tenantID, pcCareAssignableRoles)
	if err != nil {
		return ports.PlannerCatalog{}, fmt.Errorf("pccare: planner operators: %w", err)
	}
	defer operatorRows.Close()
	for operatorRows.Next() {
		var op ports.PlannerOperator
		if err := operatorRows.Scan(&op.UserID, &op.DisplayName, &op.ParkIDs); err != nil {
			return ports.PlannerCatalog{}, err
		}
		out.Operators = append(out.Operators, op)
	}
	return out, operatorRows.Err()
}

// plannerShedCursor encodes/decodes the keyset cursor (shed name, shed id, partition key) —
// the exact ORDER BY tuple, so the walk resumes where the page stopped.
func encodePlannerShedCursor(name, shedID, partitionKey string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name + "\x1f" + shedID + "\x1f" + partitionKey))
}

func decodePlannerShedCursor(cursor string) (name, shedID, partitionKey string, err error) {
	if strings.TrimSpace(cursor) == "" {
		return "", "", "", nil
	}
	raw, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
	if decodeErr != nil {
		return "", "", "", ports.ErrInvalidArgument
	}
	parts := strings.Split(string(raw), "\x1f")
	if len(parts) != 3 {
		return "", "", "", ports.ErrInvalidArgument
	}
	return parts[0], parts[1], parts[2], nil
}

// PlannerParkSheds pages ONE park's pens (shed x catalog partition; an undivided shed is one
// 'whole' row), each decorated with any existing live task for the chosen category+date so the
// wizard greys taken pens. The pen catalog is shed_partitions — never a per-goat table.
func (r *Repository) PlannerParkSheds(ctx context.Context, tenantID, parkID, category, plannedBusinessDate, cursor string, limit int) (ports.PlannerParkSheds, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	afterName, afterShed, afterPartition, err := decodePlannerShedCursor(cursor)
	if err != nil {
		return ports.PlannerParkSheds{}, err
	}
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	// projection-review: membership=the active pens of ONE park (locations sheds x active
	// shed_partitions, an undivided shed contributing one 'whole' row); group_key=(shed_id,
	// partition_key); join_cardinality=existing task side is 0..1 per pen by
	// pc_care_tasks_natural_uq (tenant, category, park, shed, partition_key, planned date,
	// live), joined on that exact key; pagination=keyset on the ORDER BY tuple (shed name,
	// shed_id, partition_key); scope=tenant_id + park_id + category + planned date.
	// scale-guard:ignore: one keyset page of ONE park's pen catalog (physical infrastructure, never herd-sized); the existing-task join hits pc_care_tasks_natural_uq.
	rows, err := r.pool.Query(ctx, `
WITH pens AS (
  SELECT shed.location_id AS shed_id, shed.name AS shed_name,
         COALESCE(NULLIF(BTRIM(sp.partition_label), ''), '') AS partition_label,
         COALESCE(NULLIF(LOWER(BTRIM(sp.partition_label)), ''), 'whole') AS partition_key
  FROM locations shed
  LEFT JOIN shed_partitions sp
    ON sp.tenant_id = shed.tenant_id AND sp.shed_id = shed.location_id AND sp.status = 'active'
   AND COALESCE(NULLIF(BTRIM(sp.partition_label), ''), 'whole') <> 'whole'
  WHERE shed.tenant_id = $1::uuid
    AND shed.parent_location_id = $2::uuid
    AND shed.location_type = 'shed'
    AND shed.status = 'active'
    AND shed.retired_at IS NULL
    -- Legacy partition-alias suppression (shared rule): the farm's pens exist twice in
    -- locations — the canonical parent shed + shed_partitions catalog row ("Castro" + "1"),
    -- and an old still-active shed row literally named "Castro 1". Without this every pen
    -- lists twice in the picker ("Castro 1, Castro 1, Castro 2, Castro 2 …").
    AND `+oploc.PartitionAliasExclusionSQL("shed")+`
)
SELECT p.shed_id::text, p.shed_name, p.partition_label,
       COALESCE(t.task_id::text, '')
FROM pens p
LEFT JOIN pc_care_tasks t
  ON t.tenant_id = $1::uuid
 AND t.category = $3
 AND t.park_id = $2::uuid
 AND t.shed_id = p.shed_id
 AND t.partition_key = p.partition_key
 AND t.planned_business_date = $4::date
 AND t.work_state <> 'canceled'
WHERE ($5::text = '' OR (p.shed_name, p.shed_id::text, p.partition_key) > ($5, $6, $7))
ORDER BY p.shed_name, p.shed_id::text, p.partition_key
LIMIT $8`,
		tenantID, parkID, category, plannedBusinessDate,
		afterName, afterShed, afterPartition, limit+1)
	if err != nil {
		return ports.PlannerParkSheds{}, fmt.Errorf("pccare: planner park sheds: %w", err)
	}
	defer rows.Close()

	out := ports.PlannerParkSheds{Sheds: []ports.PlannerShed{}}
	type cursorParts struct{ name, shedID, partitionKey string }
	var last cursorParts
	for rows.Next() {
		var shed ports.PlannerShed
		if err := rows.Scan(&shed.ShedID, &shed.ShedName, &shed.PartitionLabel, &shed.ExistingTaskID); err != nil {
			return ports.PlannerParkSheds{}, err
		}
		out.Sheds = append(out.Sheds, shed)
		last = cursorParts{shed.ShedName, shed.ShedID, domain.PartitionMatchKey(shed.PartitionLabel)}
	}
	if err := rows.Err(); err != nil {
		return ports.PlannerParkSheds{}, err
	}
	if len(out.Sheds) > limit {
		out.Sheds = out.Sheds[:limit]
		trimmed := out.Sheds[len(out.Sheds)-1]
		last = cursorParts{trimmed.ShedName, trimmed.ShedID, domain.PartitionMatchKey(trimmed.PartitionLabel)}
		out.NextCursor = encodePlannerShedCursor(last.name, last.shedID, last.partitionKey)
	}
	return out, nil
}
