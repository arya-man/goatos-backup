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
	pcCareCanceledAction = "pc_care.task.canceled"
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
	// task to an id nothing resolves would create work no phone ever lists.
	var assigneeCount int
	if err := tx.QueryRow(ctx, `
SELECT count(*)::int
FROM workforce_members m
WHERE m.tenant_id = $1::uuid AND m.status = 'active' AND m.user_id = ANY($2::uuid[])`,
		p.TenantID, p.AssigneeUserIDs).Scan(&assigneeCount); err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: verify assignees: %w", err)
	}
	if assigneeCount != len(p.AssigneeUserIDs) {
		return ports.TaskRow{}, ports.ErrInvalidArgument
	}

	fingerprint := requestFingerprint(
		p.Category, p.ParkID, p.ShedID, domain.PartitionMatchKey(p.PartitionLabel),
		plannedDate, strings.Join(p.AssigneeUserIDs, ","),
	)
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

	if err := completeIdempotency(ctx, tx, p.TenantID, pcCareCreateIdemScope, p.IdempotencyKey, pcCareTaskResourceType, taskID); err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: complete create idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.TaskRow{}, fmt.Errorf("pccare: commit create task: %w", err)
	}
	committed = true
	return r.GetTask(ctx, p.TenantID, taskID, nil, true)
}

// CancelTask flips work_state -> canceled. Only an unsubmitted task can be canceled; a locked
// (pending_verification) or terminal task is a stale-guarded no-op.
func (r *Repository) CancelTask(ctx context.Context, tenantID, taskID, actorID, traceID string) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("pccare: begin cancel tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var shedID string
	err = tx.QueryRow(ctx, `
UPDATE pc_care_tasks
SET work_state = 'canceled', terminal_at = now(), updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND task_id = $2::uuid
  AND work_state IN ('scheduled', 'delayed')
  AND status IN ('open', 'rework')
RETURNING shed_id::text`, tenantID, taskID).Scan(&shedID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already canceled/terminal/submitted: accepted stale cancel, no side effects.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return commitErr
		}
		committed = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("pccare: cancel task: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       pcCareCanceledAction,
		ResourceType: pcCareTaskResourceType,
		ResourceID:   taskID,
		ScopeType:    "shed",
		ScopeID:      shedID,
		AfterState:   map[string]any{"work_state": domain.WorkStateCanceled},
		Metadata:     map[string]any{"source": "pc-care-planner"},
		TraceID:      traceID,
	}); err != nil {
		return fmt.Errorf("pccare: write cancel audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pccare: commit cancel task: %w", err)
	}
	committed = true
	return nil
}

// taskSelectColumns is the ONE column list every task read scans, so a new column cannot be
// added to one read and missed in another (scan-count discipline).
const taskSelectColumns = `
  t.task_id::text,
  t.category,
  t.park_id::text,
  park.name,
  t.shed_id::text,
  shed.name,
  coalesce(t.partition_label, ''),
  t.planned_business_date::text,
  t.due_business_date::text,
  t.work_state,
  t.status,
  coalesce(t.rework_reason, ''),
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
JOIN locations shed ON shed.tenant_id = t.tenant_id AND shed.location_id = t.shed_id
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
		&t.PartitionLabel, &t.PlannedBusinessDate, &t.DueBusinessDate,
		&t.WorkState, &t.Status, &t.ReworkReason, &t.RowVersion,
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
  AND ($3::bool OR t.park_id = ANY($4::uuid[]))
  AND ($5::text = '' OR t.park_id = nullif($5::text, '')::uuid)
  AND ($6::text = '' OR t.category = $6)
	  AND ($7::text = '' OR EXISTS (
	        SELECT 1 FROM pc_care_task_assignees mine
	        WHERE mine.tenant_id = t.tenant_id AND mine.task_id = t.task_id
	          AND mine.operator_user_id = nullif($7::text, '')::uuid))
	  AND (
	        $9::text = ''
	        OR (park.name, shed.name, t.partition_key, t.category, t.task_id)
	           > ($9::text, $10::text, $11::text, $12::text, nullif($13::text, '')::uuid)
	      )
	ORDER BY park.name, shed.name, t.partition_key, t.category, t.task_id
	LIMIT $8`
	rows, err := r.pool.Query(ctx, "SELECT"+taskSelectColumns+taskFromJoins+listTasksPageSQL,
		q.TenantID, q.DueBusinessDate, q.TenantWide, q.AuthorizedParkIDs,
		q.ParkID, q.Category, q.AssigneeUserID, limit+1,
		afterPark, afterShed, afterPartition, afterCategory, afterTask, q.CurrentOrCarry)
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
	raw, err := json.Marshal(taskCursor{
		ParkName:     t.ParkName,
		ShedName:     t.ShedName,
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
