package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

const defaultQueryTimeout = 3 * time.Second

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

func (r *Repository) ListSOPs(ctx context.Context, params ports.ListSOPsParams) ([]domain.SOPDefinition, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var cursorUpdatedAt any
	var cursorSOPID any
	if params.Cursor != nil {
		cursorUpdatedAt = params.Cursor.UpdatedAt
		cursorSOPID = params.Cursor.SOPID
	}
	rows, err := r.pool.Query(ctx, sopSelectSQL(`
WHERE sd.tenant_id = $1::uuid
  AND ($2 = '' OR sd.status = $2)
  AND ($3 = '' OR sd.code LIKE $3 || '%')
  AND ($4 = '' OR sd.code ILIKE '%' || $4 || '%' OR sd.name ILIKE '%' || $4 || '%' OR sd.description ILIKE '%' || $4 || '%')
  AND ($5::timestamptz IS NULL OR (sd.updated_at, sd.sop_id) < ($5::timestamptz, $6::uuid))
ORDER BY sd.updated_at DESC, sd.sop_id DESC
LIMIT $7`), params.TenantID, params.Status, params.CodePrefix, params.Search, cursorUpdatedAt, cursorSOPID, params.Limit)
	if err != nil {
		return nil, err
	}
	return scanSOPs(rows)
}

func (r *Repository) CreateSOP(ctx context.Context, cmd ports.CreateSOPCommand) (domain.SOPDefinition, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.SOPDefinition{}, err
	}
	defer rollback(ctx, tx)
	var sopID string
	err = tx.QueryRow(ctx, `
INSERT INTO sop_definitions (tenant_id, code, name, description, status, created_by)
VALUES ($1::uuid, $2, $3, $4, 'draft', $5::uuid)
RETURNING sop_id::text`, cmd.TenantID, cmd.Body.Code, cmd.Body.Name, cmd.Body.Description, cmd.ActorID).Scan(&sopID)
	if err != nil {
		return domain.SOPDefinition{}, mapWriteErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "sop.create", "sop_definition", sopID, nil); err != nil {
		return domain.SOPDefinition{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SOPDefinition{}, err
	}
	sop, _, err := r.GetSOP(context.WithoutCancel(ctx), cmd.TenantID, sopID)
	return sop, err
}

func (r *Repository) GetSOP(ctx context.Context, tenantID, sopID string) (domain.SOPDefinition, *domain.SOPVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sopSelectSQL(`
WHERE sd.tenant_id = $1::uuid
  AND sd.sop_id = $2::uuid
LIMIT 1`), tenantID, sopID)
	if err != nil {
		return domain.SOPDefinition{}, nil, err
	}
	items, err := scanSOPs(rows)
	if err != nil {
		return domain.SOPDefinition{}, nil, err
	}
	if len(items) == 0 {
		return domain.SOPDefinition{}, nil, ports.ErrNotFound
	}
	version, err := r.latestVersion(ctx, tenantID, sopID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return items[0], nil, nil
		}
		return domain.SOPDefinition{}, nil, err
	}
	return items[0], &version, nil
}

func (r *Repository) CreateVersion(ctx context.Context, cmd ports.CreateVersionCommand) (domain.SOPVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	defer rollback(ctx, tx)
	formDSL, err := json.Marshal(nonNilMap(cmd.Body.FormDSL))
	if err != nil {
		return domain.SOPVersion{}, err
	}
	proofPolicy, err := json.Marshal(nonNilMap(cmd.Body.ProofPolicy))
	if err != nil {
		return domain.SOPVersion{}, err
	}
	compatibility, err := json.Marshal(nonNilMap(cmd.Body.Compatibility))
	if err != nil {
		return domain.SOPVersion{}, err
	}
	report, err := json.Marshal(cmd.Report)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	var versionID string
	err = tx.QueryRow(ctx, `
INSERT INTO sop_versions (
  tenant_id, sop_id, version, version_label, status, form_dsl,
  proof_policy, compatibility, validation_report, created_by
)
SELECT
  $1::uuid,
  $2::uuid,
  COALESCE(max(version), 0) + 1,
  $3,
  'draft',
  $4::jsonb,
  $5::jsonb,
  $6::jsonb,
  $7::jsonb,
  $8::uuid
FROM sop_versions
WHERE tenant_id = $1::uuid
  AND sop_id = $2::uuid
RETURNING sop_version_id::text`,
		cmd.TenantID, cmd.SOPID, cmd.Body.VersionLabel, formDSL, proofPolicy, compatibility, report, cmd.ActorID).Scan(&versionID)
	if err != nil {
		return domain.SOPVersion{}, mapWriteErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "sop.version.create", "sop_version", versionID, nil); err != nil {
		return domain.SOPVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SOPVersion{}, err
	}
	return r.GetVersion(context.WithoutCancel(ctx), cmd.TenantID, cmd.SOPID, versionID)
}

func (r *Repository) GetVersion(ctx context.Context, tenantID, sopID, versionID string) (domain.SOPVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, versionSelectSQL(`
WHERE sv.tenant_id = $1::uuid
  AND sv.sop_id = $2::uuid
  AND sv.sop_version_id = $3::uuid
LIMIT 1`), tenantID, sopID, versionID)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	items, err := scanVersions(rows)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	if len(items) == 0 {
		return domain.SOPVersion{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) GetVersionByID(ctx context.Context, tenantID, versionID string) (domain.SOPVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, versionSelectSQL(`
WHERE sv.tenant_id = $1::uuid
  AND sv.sop_version_id = $2::uuid
LIMIT 1`), tenantID, versionID)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	items, err := scanVersions(rows)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	if len(items) == 0 {
		return domain.SOPVersion{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) GetPublishedVersionByCode(ctx context.Context, tenantID, sopCode string) (domain.SOPVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, versionSelectSQL(`
JOIN sop_definitions sd_filter
  ON sd_filter.tenant_id = sv.tenant_id
 AND sd_filter.sop_id = sv.sop_id
WHERE sv.tenant_id = $1::uuid
  AND sd_filter.code = $2
  AND sv.status = 'published'
LIMIT 1`), tenantID, sopCode)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	items, err := scanVersions(rows)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	if len(items) == 0 {
		return domain.SOPVersion{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) PublishVersion(ctx context.Context, cmd ports.VersionCommand) (domain.SOPVersion, error) {
	return r.setVersionStatus(ctx, cmd, "published")
}

func (r *Repository) RetireVersion(ctx context.Context, cmd ports.VersionCommand) (domain.SOPVersion, error) {
	return r.setVersionStatus(ctx, cmd, "retired")
}

func (r *Repository) ListTasks(ctx context.Context, params ports.ListTasksParams) (ports.TaskListPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	const filters = `
WHERE st.tenant_id = $1::uuid
  AND ($2 = '' OR st.state = $2)
  AND ($3 = '' OR st.assigned_to = $3::uuid)
  AND ($4 = '' OR st.scope_type = $4)
	  AND ($5 = '' OR st.scope_id = $5::uuid)`
	if !params.AppView {
		rows, err := r.pool.Query(ctx, taskSelectSQL(filters+`
ORDER BY st.due_at NULLS LAST, st.updated_at DESC, st.task_id DESC
LIMIT $6`), params.TenantID, params.State, params.AssignedTo, params.ScopeType, params.ScopeID, params.Limit)
		if err != nil {
			return ports.TaskListPage{}, err
		}
		items, err := scanTasks(rows)
		return ports.TaskListPage{Items: items}, err
	}

	var total int64
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM sop_tasks st`+filters,
		params.TenantID, params.State, params.AssignedTo, params.ScopeType, params.ScopeID).Scan(&total)
	if err != nil {
		return ports.TaskListPage{}, err
	}
	var cursorTaskID any
	var cursorDueAt any
	if params.Cursor != nil {
		cursorTaskID = params.Cursor.TaskID
		cursorDueAt = params.Cursor.DueAt
	}
	rows, err := r.pool.Query(ctx, taskSelectSQL(filters+`
  AND (
    $6::uuid IS NULL
    OR (
      $7::timestamptz IS NOT NULL
      AND (
        st.due_at > $7::timestamptz
        OR st.due_at IS NULL
        OR (st.due_at = $7::timestamptz AND st.task_id > $6::uuid)
      )
    )
    OR (
      $7::timestamptz IS NULL
      AND st.due_at IS NULL
      AND st.task_id > $6::uuid
    )
  )
ORDER BY st.due_at ASC NULLS LAST, st.task_id ASC
LIMIT $8`), params.TenantID, params.State, params.AssignedTo, params.ScopeType, params.ScopeID, cursorTaskID, cursorDueAt, params.Limit)
	if err != nil {
		return ports.TaskListPage{}, err
	}
	items, err := scanTasks(rows)
	if err != nil {
		return ports.TaskListPage{}, err
	}
	return ports.TaskListPage{Items: items, Total: total}, nil
}

func (r *Repository) CreateTask(ctx context.Context, cmd ports.CreateTaskCommand) (domain.TaskSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.TaskSummary{}, err
	}
	defer rollback(ctx, tx)
	version, err := r.resolveTaskVersion(ctx, tx, cmd)
	if err != nil {
		return domain.TaskSummary{}, err
	}
	contextMap := nonNilMap(cmd.Body.Context)
	batchID := obligationBatchIDFromContext(contextMap)
	if batchID != "" {
		if task, found, err := r.getTaskByObligationBatchID(ctx, tx, cmd.TenantID, batchID); err != nil {
			return domain.TaskSummary{}, err
		} else if found {
			return task, nil
		}
	}
	contextBytes, err := json.Marshal(contextMap)
	if err != nil {
		return domain.TaskSummary{}, err
	}
	var taskID string
	if batchID != "" {
		if _, err := tx.Exec(ctx, "SAVEPOINT sop_task_insert"); err != nil {
			return domain.TaskSummary{}, err
		}
	}
	err = tx.QueryRow(ctx, `
INSERT INTO sop_tasks (
  tenant_id, sop_id, sop_version_id, task_type, title, description,
  state, assigned_to, scope_type, scope_id, priority, due_at, context, created_by
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, $5, $6,
  CASE WHEN nullif($7, '')::uuid IS NULL THEN 'queued' ELSE 'assigned' END,
  nullif($7, '')::uuid, $8, $9::uuid, $10, nullif($11, '')::timestamptz, $12::jsonb, $13::uuid
)
RETURNING task_id::text`,
		cmd.TenantID,
		version.SOPID,
		version.SOPVersionID,
		cmd.Body.TaskType,
		cmd.Body.Title,
		cmd.Body.Description,
		ptrValue(cmd.Body.AssignedTo),
		cmd.Body.ScopeType,
		cmd.Body.ScopeID,
		cmd.Body.Priority,
		ptrValue(cmd.Body.DueAt),
		contextBytes,
		cmd.ActorID,
	).Scan(&taskID)
	if err != nil {
		if batchID != "" && uniqueViolationOn(err, "sop_tasks_obligation_batch_id_unique_idx") {
			if _, rollbackErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sop_task_insert"); rollbackErr != nil {
				return domain.TaskSummary{}, rollbackErr
			}
			task, found, lookupErr := r.getTaskByObligationBatchID(ctx, tx, cmd.TenantID, batchID)
			if lookupErr != nil {
				return domain.TaskSummary{}, lookupErr
			}
			if found {
				return task, nil
			}
		}
		return domain.TaskSummary{}, mapWriteErr(err)
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "sop.task.create", "sop_task", taskID, map[string]any{"sop_version_id": version.SOPVersionID}); err != nil {
		return domain.TaskSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskSummary{}, err
	}
	task, _, _, err := r.GetTask(context.WithoutCancel(ctx), cmd.TenantID, taskID)
	return task, err
}

func (r *Repository) GetTask(ctx context.Context, tenantID, taskID string) (domain.TaskSummary, *domain.SOPVersion, []domain.SubmissionSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, taskSelectSQL(`
WHERE st.tenant_id = $1::uuid
  AND st.task_id = $2::uuid
LIMIT 1`), tenantID, taskID)
	if err != nil {
		return domain.TaskSummary{}, nil, nil, err
	}
	tasks, err := scanTasks(rows)
	if err != nil {
		return domain.TaskSummary{}, nil, nil, err
	}
	if len(tasks) == 0 {
		return domain.TaskSummary{}, nil, nil, ports.ErrNotFound
	}
	version, err := r.GetVersionByID(ctx, tenantID, tasks[0].SOPVersionID)
	if err != nil {
		return domain.TaskSummary{}, nil, nil, err
	}
	submissions, err := r.listSubmissions(ctx, tenantID, taskID)
	if err != nil {
		return domain.TaskSummary{}, nil, nil, err
	}
	return tasks[0], &version, submissions, nil
}

func (r *Repository) getTaskByObligationBatchID(ctx context.Context, tx pgx.Tx, tenantID, batchID string) (domain.TaskSummary, bool, error) {
	rows, err := tx.Query(ctx, taskSelectSQL(`
WHERE st.tenant_id = $1::uuid
  AND st.context ->> 'obligation_batch_id' = $2
ORDER BY st.created_at ASC, st.task_id ASC
LIMIT 1`), tenantID, batchID)
	if err != nil {
		return domain.TaskSummary{}, false, err
	}
	tasks, err := scanTasks(rows)
	if err != nil {
		return domain.TaskSummary{}, false, err
	}
	if len(tasks) == 0 {
		return domain.TaskSummary{}, false, nil
	}
	return tasks[0], true, nil
}

// CreateTasksForBatches creates multiple SOP tasks in a single batch transaction,
// avoiding N+1 by using one INSERT ... SELECT query with UNNEST.
// Returns a map of obligation_batch_id -> task_id.
func (r *Repository) CreateTasksForBatches(ctx context.Context, tenantID, sopVersionID, actorID string, tasks []domain.BatchTaskRequest) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if len(tasks) == 0 {
		return map[string]string{}, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, tx)

	// Check if version exists and is accessible
	version, err := r.getVersionForTaskCreation(ctx, tx, tenantID, sopVersionID)
	if err != nil {
		return nil, err
	}

	// Prepare batch data for UNNEST
	batchIDs := make([]string, len(tasks))
	taskTypes := make([]string, len(tasks))
	titles := make([]string, len(tasks))
	scopes := make([]string, len(tasks))
	scopeIDs := make([]string, len(tasks))
	contextJSONs := make([]string, len(tasks))

	for i, t := range tasks {
		batchIDs[i] = t.BatchID
		taskTypes[i] = t.TaskType
		titles[i] = t.Title
		scopes[i] = t.ScopeType
		scopeIDs[i] = t.ScopeID
		contextBytes, _ := json.Marshal(map[string]any{
			"created_by":          "obligation-sweeper",
			"obligation_batch_id": t.BatchID,
		})
		contextJSONs[i] = string(contextBytes)
	}

	// Insert tasks and their audit rows in one set-based statement. The conflict
	// target exactly matches migration 000101's partial expression index
	// (context ->> text plus predicate); retries therefore create neither duplicate
	// tasks nor duplicate create-audit rows.
	var createdCount int
	err = tx.QueryRow(ctx, `
WITH input AS (
  SELECT *
  FROM UNNEST($5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[]) AS t(
    batch_id, task_type, title, scope_type, scope_id, context
  )
),
inserted AS (
  INSERT INTO sop_tasks (
  tenant_id, sop_id, sop_version_id, task_type, title, description,
  state, scope_type, scope_id, priority, context, created_by
  )
  SELECT
    $1::uuid, $2::uuid, $3::uuid, input.task_type, input.title, '',
    'queued', input.scope_type, input.scope_id::uuid, 'normal', input.context::jsonb, $4::uuid
  FROM input
  ON CONFLICT (tenant_id, (context ->> 'obligation_batch_id'))
    WHERE context ? 'obligation_batch_id'
  DO NOTHING
  RETURNING task_id, context ->> 'obligation_batch_id' AS batch_id
),
audited AS (
  INSERT INTO audit_log (
    tenant_id, actor_id, actor_type, action, resource_type, resource_id, metadata
  )
  SELECT
    $1::uuid, $4::uuid, 'human', 'sop.task.create', 'sop_task', inserted.task_id,
    jsonb_build_object('sop_version_id', $3::text, 'obligation_batch_id', inserted.batch_id)
  FROM inserted
  RETURNING resource_id
)
SELECT COUNT(*)::int FROM audited
`,
		tenantID,
		version.SOPID,
		sopVersionID,
		actorID,
		batchIDs,
		taskTypes,
		titles,
		scopes,
		scopeIDs,
		contextJSONs,
	).Scan(&createdCount)
	if err != nil {
		return nil, err
	}

	// Resolve both newly inserted and pre-existing idempotent rows in one bounded
	// query so a replay returns the same complete batch->task mapping.
	rows, err := tx.Query(ctx, `
SELECT task_id::text, context ->> 'obligation_batch_id' AS batch_id
FROM sop_tasks
WHERE tenant_id = $1::uuid
  AND context ->> 'obligation_batch_id' = ANY($2::text[])
ORDER BY context ->> 'obligation_batch_id'`, tenantID, batchIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string, len(tasks))
	for rows.Next() {
		var taskID, batchID string
		if err := rows.Scan(&taskID, &batchID); err != nil {
			return nil, err
		}
		result[batchID] = taskID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(result) != len(tasks) {
		return nil, fmt.Errorf("sop: bulk task create resolved %d of %d batch ids (created %d)", len(result), len(tasks), createdCount)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// getVersionForTaskCreation is a helper to resolve SOP version within a transaction
func (r *Repository) getVersionForTaskCreation(ctx context.Context, tx pgx.Tx, tenantID, versionID string) (domain.SOPVersion, error) {
	rows, err := tx.Query(ctx, versionSelectSQL(`
WHERE sv.tenant_id = $1::uuid
  AND sv.sop_version_id = $2::uuid
  AND sv.status = 'published'
LIMIT 1`), tenantID, versionID)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	defer rows.Close()

	versions, err := scanVersions(rows)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	if len(versions) == 0 {
		return domain.SOPVersion{}, ports.ErrNotFound
	}
	return versions[0], nil
}

func (r *Repository) AssignTask(ctx context.Context, cmd ports.AssignTaskCommand) (domain.TaskSummary, error) {
	return r.updateTaskState(ctx, cmd.TenantID, cmd.ActorID, cmd.TaskID, cmd.Body.RowVersion, "assigned", &cmd.Body.AssignedTo, "sop.task.assign", map[string]any{"reason": cmd.Body.Reason})
}

func (r *Repository) ReviewTask(ctx context.Context, cmd ports.ReviewTaskCommand) (domain.TaskSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.TaskSummary{}, err
	}
	defer rollback(ctx, tx)
	var taskID string
	var taskRowVersion int
	err = tx.QueryRow(ctx, `
UPDATE sop_tasks
SET state = $4,
    verified_by = $5::uuid,
    verified_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND task_id = $2::uuid
  AND row_version = $3
RETURNING task_id::text, row_version`, cmd.TenantID, cmd.TaskID, cmd.Body.RowVersion, cmd.State, cmd.ActorID).Scan(&taskID, &taskRowVersion)
	if err != nil {
		return domain.TaskSummary{}, mapUpdateErr(err)
	}
	if cmd.State == "accepted" {
		if err := insertMovementForLatestSubmission(ctx, tx, cmd.TenantID, taskID); err != nil {
			return domain.TaskSummary{}, err
		}
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "sop.task."+cmd.State, "sop_task", taskID, map[string]any{"reason": cmd.Body.Reason}); err != nil {
		return domain.TaskSummary{}, err
	}
	if cmd.State == "accepted" || cmd.State == "rework_requested" {
		if err := insertReviewFanout(ctx, tx, ports.ReviewFanoutStatusCommand{
			TenantID:       cmd.TenantID,
			TaskID:         taskID,
			TaskRowVersion: taskRowVersion,
			Outcome:        cmd.State,
			ActorID:        cmd.ActorID,
			Reason:         cmd.Body.Reason,
			Status:         "pending",
		}); err != nil {
			return domain.TaskSummary{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskSummary{}, err
	}
	task, _, _, err := r.GetTask(context.WithoutCancel(ctx), cmd.TenantID, taskID)
	return task, err
}

func (r *Repository) RecordReviewFanoutStatus(ctx context.Context, cmd ports.ReviewFanoutStatusCommand) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
INSERT INTO sop_task_review_fanouts (
  tenant_id, task_id, task_row_version, outcome, status, requested_by, reason,
  attempt_count, last_error, completed_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, nullif($6, '')::uuid, $7,
	  CASE WHEN $5 IN ('completed', 'failed', 'superseded') THEN 1 ELSE 0 END,
  $8,
	  CASE WHEN $5 IN ('completed', 'superseded') THEN now() ELSE NULL END
	)
	ON CONFLICT (tenant_id, task_id, task_row_version, outcome) DO UPDATE
	SET status = EXCLUDED.status,
	    requested_by = COALESCE(EXCLUDED.requested_by, sop_task_review_fanouts.requested_by),
	    reason = COALESCE(NULLIF(EXCLUDED.reason, ''), sop_task_review_fanouts.reason),
	    attempt_count = CASE
	      WHEN EXCLUDED.status IN ('completed', 'failed', 'superseded') THEN sop_task_review_fanouts.attempt_count + 1
	      ELSE sop_task_review_fanouts.attempt_count
	    END,
	    last_error = EXCLUDED.last_error,
	    completed_at = CASE WHEN EXCLUDED.status IN ('completed', 'superseded') THEN now() ELSE NULL END,
	    updated_at = now()`,
		cmd.TenantID,
		cmd.TaskID,
		cmd.TaskRowVersion,
		cmd.Outcome,
		cmd.Status,
		cmd.ActorID,
		cmd.Reason,
		cmd.LastError,
	)
	return err
}

func (r *Repository) ListPendingReviewFanouts(ctx context.Context, tenantID string, limit int) ([]ports.ReviewFanoutAttempt, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
SELECT tenant_id::text,
       task_id::text,
       task_row_version,
       outcome,
       COALESCE(requested_by::text, '')::text,
       reason
FROM sop_task_review_fanouts
WHERE tenant_id = $1::uuid
  AND status IN ('pending', 'failed')
ORDER BY updated_at ASC, review_fanout_id ASC
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ports.ReviewFanoutAttempt
	for rows.Next() {
		var attempt ports.ReviewFanoutAttempt
		if err := rows.Scan(
			&attempt.TenantID,
			&attempt.TaskID,
			&attempt.TaskRowVersion,
			&attempt.Outcome,
			&attempt.ActorID,
			&attempt.Reason,
		); err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func insertReviewFanout(ctx context.Context, tx pgx.Tx, cmd ports.ReviewFanoutStatusCommand) error {
	_, err := tx.Exec(ctx, `
INSERT INTO sop_task_review_fanouts (
  tenant_id, task_id, task_row_version, outcome, status, requested_by, reason
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, nullif($6, '')::uuid, $7
)
ON CONFLICT (tenant_id, task_id, task_row_version, outcome) DO UPDATE
SET status = 'pending',
    requested_by = COALESCE(EXCLUDED.requested_by, sop_task_review_fanouts.requested_by),
    reason = EXCLUDED.reason,
    last_error = '',
    updated_at = now()`,
		cmd.TenantID,
		cmd.TaskID,
		cmd.TaskRowVersion,
		cmd.Outcome,
		cmd.Status,
		cmd.ActorID,
		cmd.Reason,
	)
	return err
}

func (r *Repository) RecordSubmissionFanoutStatus(ctx context.Context, cmd ports.SubmissionFanoutStatusCommand) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
INSERT INTO sop_task_submission_fanouts (
  tenant_id, task_id, submission_id, status, requested_by,
  attempt_count, last_error, completed_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, nullif($5, '')::uuid,
  CASE WHEN $4 IN ('completed', 'failed', 'skipped') THEN 1 ELSE 0 END,
  $6,
  CASE WHEN $4 IN ('completed', 'skipped') THEN now() ELSE NULL END
)
ON CONFLICT (tenant_id, submission_id) DO UPDATE
SET status = EXCLUDED.status,
    requested_by = COALESCE(EXCLUDED.requested_by, sop_task_submission_fanouts.requested_by),
    attempt_count = CASE
      WHEN EXCLUDED.status IN ('completed', 'failed', 'skipped') THEN sop_task_submission_fanouts.attempt_count + 1
      ELSE sop_task_submission_fanouts.attempt_count
    END,
    last_error = EXCLUDED.last_error,
    completed_at = CASE WHEN EXCLUDED.status IN ('completed', 'skipped') THEN now() ELSE NULL END,
    updated_at = now()`,
		cmd.TenantID,
		cmd.TaskID,
		cmd.SubmissionID,
		cmd.Status,
		cmd.ActorID,
		cmd.LastError,
	)
	return err
}

func (r *Repository) ListPendingSubmissionFanouts(ctx context.Context, tenantID string, limit int) ([]ports.SubmissionFanoutAttempt, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
SELECT tenant_id::text,
       task_id::text,
       submission_id::text,
       COALESCE(requested_by::text, '')::text
FROM sop_task_submission_fanouts
WHERE tenant_id = $1::uuid
  AND status IN ('pending', 'failed')
ORDER BY updated_at ASC, submission_fanout_id ASC
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ports.SubmissionFanoutAttempt
	for rows.Next() {
		var attempt ports.SubmissionFanoutAttempt
		if err := rows.Scan(
			&attempt.TenantID,
			&attempt.TaskID,
			&attempt.SubmissionID,
			&attempt.ActorID,
		); err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ListAgedFailedSubmissionFanouts(ctx context.Context, params ports.ListAgedFailedSubmissionFanoutsParams) ([]domain.FailedSubmissionFanout, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if params.Limit <= 0 || params.Limit > 100 {
		params.Limit = 50
	}
	if params.Now.IsZero() {
		params.Now = time.Now().UTC()
	}
	rows, err := r.pool.Query(ctx, `
SELECT f.task_id::text,
       f.submission_id::text,
       sd.code,
       st.task_type,
       st.state,
       ss.submitted_at,
       f.updated_at,
       GREATEST(0, floor(EXTRACT(EPOCH FROM ($3::timestamptz - f.updated_at)))::bigint),
       f.attempt_count,
       f.last_error,
       count(si.item_id)::int,
       count(DISTINCT vc.sop_submission_item_id)::int
FROM sop_task_submission_fanouts f
JOIN sop_tasks st
  ON st.tenant_id = f.tenant_id
 AND st.task_id = f.task_id
JOIN sop_definitions sd
  ON sd.tenant_id = st.tenant_id
 AND sd.sop_id = st.sop_id
JOIN sop_submissions ss
  ON ss.tenant_id = f.tenant_id
 AND ss.submission_id = f.submission_id
LEFT JOIN sop_submission_items si
  ON si.tenant_id = f.tenant_id
 AND si.task_id = f.task_id
 AND si.submission_id = f.submission_id
 AND si.goat_id IS NOT NULL
 AND si.state IN ('accepted', 'needs_review')
LEFT JOIN vaccination_completions vc
  ON vc.tenant_id = si.tenant_id
 AND vc.sop_submission_item_id = si.item_id
 AND vc.status <> 'reversed'
WHERE f.tenant_id = $1::uuid
  AND f.status = 'failed'
  AND f.updated_at <= $2::timestamptz
  AND (
    sd.code IN ('vaccination.drive', 'vaccination.session')
    OR st.task_type IN ('vaccination', 'vaccination_drive', 'vaccination_session')
  )
-- projection-review: membership=failed sop_task_submission_fanouts for vaccination tasks older than the requested cutoff; group_key=submission_fanout_id (one row per failed fanout); join_cardinality=sop_tasks/sop_definitions/sop_submissions are keyed 1:1, sop_submission_items is 1:N but counted at item grain, vaccination_completions is 0:N and counted DISTINCT by sop_submission_item_id so completion retries cannot fan out counts; pagination=oldest failed fanouts ordered by updated_at/submission_fanout_id with repository cap <=100 after grouping, independent of any UI page; scope=tenant plus vaccination sop code/task type and failed fanout status, with eligible item status limited to accepted/needs_review
GROUP BY f.submission_fanout_id, f.task_id, f.submission_id, sd.code, st.task_type, st.state, ss.submitted_at, f.updated_at, f.attempt_count, f.last_error
ORDER BY f.updated_at ASC, f.submission_fanout_id ASC
LIMIT $4`, params.TenantID, params.UpdatedBefore, params.Now, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FailedSubmissionFanout{}
	for rows.Next() {
		var item domain.FailedSubmissionFanout
		var submittedAt, fanoutUpdatedAt time.Time
		if err := rows.Scan(
			&item.TaskID,
			&item.SubmissionID,
			&item.SOPCode,
			&item.TaskType,
			&item.TaskState,
			&submittedAt,
			&fanoutUpdatedAt,
			&item.AgeSeconds,
			&item.AttemptCount,
			&item.LastError,
			&item.EligibleSubmissionItems,
			&item.MaterializedCompletionCount,
		); err != nil {
			return nil, err
		}
		item.SubmittedAt = submittedAt.UTC().Format(time.RFC3339)
		item.FanoutUpdatedAt = fanoutUpdatedAt.UTC().Format(time.RFC3339)
		item.MissingCompletionCount = item.EligibleSubmissionItems - item.MaterializedCompletionCount
		if item.MissingCompletionCount < 0 {
			item.MissingCompletionCount = 0
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) RecordScanCapture(ctx context.Context, cmd ports.RecordScanCaptureCommand) (domain.ScanCaptureSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	normalized := normalizeScanTag(cmd.Body.Tag)
	if normalized == "" {
		return domain.ScanCaptureSummary{}, ports.ErrInvalidFilter
	}
	var item domain.ScanCaptureSummary
	var capturedAt time.Time
	err := r.pool.QueryRow(ctx, `
INSERT INTO sop_task_scan_captures (
  tenant_id, task_id, field_key, tag, normalized_tag, goat_id, obligation_id, captured_by, idempotency_key, captured_at
)
SELECT
  $1::uuid, $2::uuid, $3, $4, $5, nullif($6, '')::uuid, nullif($7, '')::uuid, $8::uuid, $9,
  COALESCE(to_timestamp(NULLIF($10::bigint, 0)::double precision / 1000.0), now())
WHERE NOT EXISTS (
  SELECT 1
  FROM obligation_instances oi
  JOIN obligation_batches ob ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
  LEFT JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id AND oi.target_type = 'goat'
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
  LEFT JOIN LATERAL (
    SELECT (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
    FROM vaccination_drive_assignments assignment
    WHERE assignment.tenant_id = oi.tenant_id
      AND assignment.batch_id = oi.batch_id
      AND assignment.shed_id = g.shed_id
      AND (
        assignment.partition_label = 'whole'
        OR regexp_replace(lower(btrim(assignment.partition_label)), '^part[[:space:]]+', '')
         = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
      )
      AND (
        cardinality(assignment.vaccine_rule_ids) = 0
        OR assignment.vaccine_rule_ids @> ARRAY[oi.rule_id]
      )
    ORDER BY assignment.planned_date ASC,
             assignment.partition_label ASC,
             assignment.operator_id ASC NULLS LAST,
             assignment.assignment_id ASC
    LIMIT 1
  ) vda ON true
  WHERE oi.tenant_id = $1::uuid
    AND oi.obligation_id = nullif($7, '')::uuid
    AND oi.status = 'scheduled'
    AND COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) > now()
)
ON CONFLICT (tenant_id, task_id, field_key, normalized_tag) DO UPDATE
SET updated_at = now()
RETURNING capture_id::text, task_id::text, field_key, tag, COALESCE(goat_id::text, ''), COALESCE(obligation_id::text, ''), captured_at`,
		cmd.TenantID,
		cmd.TaskID,
		cmd.Body.FieldKey,
		cmd.Body.Tag,
		normalized,
		cmd.Body.GoatID,
		cmd.Body.ObligationID,
		cmd.ActorID,
		cmd.IdempotencyKey,
		int64Value(cmd.Body.CapturedAtMs),
	).Scan(&item.CaptureID, &item.TaskID, &item.FieldKey, &item.Tag, &item.GoatID, &item.ObligationID, &capturedAt)
	if err != nil {
		return domain.ScanCaptureSummary{}, mapWriteErr(err)
	}
	item.CapturedAt = capturedAt.UTC().Format(time.RFC3339Nano)
	return item, nil
}

func (r *Repository) ListScanCaptures(ctx context.Context, tenantID, taskID string) ([]domain.ScanCaptureSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT capture_id::text, task_id::text, field_key, tag, COALESCE(goat_id::text, ''), COALESCE(obligation_id::text, ''), captured_at
FROM sop_task_scan_captures
WHERE tenant_id = $1::uuid
  AND task_id = $2::uuid
ORDER BY captured_at ASC, capture_id ASC
LIMIT 2000`, tenantID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ScanCaptureSummary{}
	for rows.Next() {
		var item domain.ScanCaptureSummary
		var capturedAt time.Time
		if err := rows.Scan(&item.CaptureID, &item.TaskID, &item.FieldKey, &item.Tag, &item.GoatID, &item.ObligationID, &capturedAt); err != nil {
			return nil, err
		}
		item.CapturedAt = capturedAt.UTC().Format(time.RFC3339Nano)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) RecordScanAttempt(ctx context.Context, cmd ports.RecordScanAttemptCommand) (domain.ScanAttemptSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	normalized := normalizeScanTag(cmd.Body.NormalizedTag)
	if normalized == "" {
		normalized = normalizeScanTag(cmd.Body.Tag)
	}
	if normalized == "" {
		return domain.ScanAttemptSummary{}, ports.ErrInvalidFilter
	}
	tagRole := strings.TrimSpace(cmd.Body.TagRole)
	if tagRole == "" {
		tagRole = "unknown"
	}
	var item domain.ScanAttemptSummary
	var capturedAt time.Time
	err := r.pool.QueryRow(ctx, `
INSERT INTO sop_task_scan_attempts (
  tenant_id, task_id, field_key, tag, normalized_tag, goat_id, obligation_id,
  outcome, tag_role, reason, captured_by, idempotency_key, captured_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, nullif($6, '')::uuid, nullif($7, '')::uuid,
  $8, $9, nullif($10, ''), $11::uuid, $12,
  COALESCE(to_timestamp(NULLIF($13::bigint, 0)::double precision / 1000.0), now())
)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET updated_at = now()
RETURNING attempt_id::text, task_id::text, field_key, tag, COALESCE(goat_id::text, ''),
          COALESCE(obligation_id::text, ''), outcome, tag_role, COALESCE(reason, ''), captured_at`,
		cmd.TenantID,
		cmd.TaskID,
		cmd.Body.FieldKey,
		cmd.Body.Tag,
		normalized,
		cmd.Body.GoatID,
		cmd.Body.ObligationID,
		cmd.Body.Outcome,
		tagRole,
		cmd.Body.Reason,
		cmd.ActorID,
		cmd.IdempotencyKey,
		int64Value(cmd.Body.CapturedAtMs),
	).Scan(
		&item.AttemptID,
		&item.TaskID,
		&item.FieldKey,
		&item.Tag,
		&item.GoatID,
		&item.ObligationID,
		&item.Outcome,
		&item.TagRole,
		&item.Reason,
		&capturedAt,
	)
	if err != nil {
		return domain.ScanAttemptSummary{}, mapWriteErr(err)
	}
	item.CapturedAt = capturedAt.UTC().Format(time.RFC3339Nano)
	return item, nil
}

func (r *Repository) ShedCompletionReadiness(ctx context.Context, tenantID, taskID, proofSubject, shedID string, minProofs, maxProofs int) (ports.ShedCompletionReadiness, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if proofSubject == "" {
		proofSubject = "goat"
	}
	if minProofs <= 0 {
		minProofs = 1
	}
	if maxProofs <= 0 {
		maxProofs = 5
	}
	var expected, handled, proofReady int64
	err := r.pool.QueryRow(ctx, `
WITH batch AS (
  SELECT ob.batch_id
  FROM obligation_batches ob
  WHERE ob.tenant_id = $1::uuid
    AND ob.sop_task_id = $2::uuid
),
task_scope AS (
  SELECT scope_type, scope_id
  FROM sop_tasks
  WHERE tenant_id = $1::uuid
    AND task_id = $2::uuid
),
target_shed AS (
  SELECT CASE
    WHEN nullif($4, '')::uuid IS NOT NULL THEN nullif($4, '')::uuid
    WHEN ts.scope_type = 'shed' THEN ts.scope_id
    ELSE NULL::uuid
  END AS shed_id
  FROM task_scope ts
),
eligible AS (
  SELECT oi.obligation_id, oi.target_id AS goat_id, g.shed_id
  FROM obligation_instances oi
  JOIN obligation_batches ob ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
  JOIN batch b ON b.batch_id = oi.batch_id
  JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
  LEFT JOIN LATERAL (
    SELECT (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
    FROM vaccination_drive_assignments assignment
    WHERE assignment.tenant_id = oi.tenant_id
      AND assignment.batch_id = oi.batch_id
      AND assignment.shed_id = g.shed_id
      AND (
        assignment.partition_label = 'whole'
        OR regexp_replace(lower(btrim(assignment.partition_label)), '^part[[:space:]]+', '')
         = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
      )
      AND (
        cardinality(assignment.vaccine_rule_ids) = 0
        OR assignment.vaccine_rule_ids @> ARRAY[oi.rule_id]
      )
    ORDER BY assignment.planned_date ASC,
             assignment.partition_label ASC,
             assignment.operator_id ASC NULLS LAST,
             assignment.assignment_id ASC
    LIMIT 1
  ) vda ON true
  CROSS JOIN target_shed target
  WHERE oi.tenant_id = $1::uuid
    AND oi.status NOT IN ('completed', 'waived', 'canceled', 'superseded')
    AND (target.shed_id IS NULL OR g.shed_id = target.shed_id)
    AND COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) <= now()
),
expected AS (
  SELECT count(*) AS n
  FROM eligible
),
handled AS (
  SELECT count(DISTINCT c.goat_id) AS n
  FROM sop_task_scan_captures c
  JOIN eligible e
    ON e.obligation_id = c.obligation_id
    OR (c.obligation_id IS NULL AND e.goat_id = c.goat_id)
  WHERE c.tenant_id = $1::uuid
    AND c.task_id = $2::uuid
    AND c.field_key IN ('goat_ids', '__scan_roster__')
    AND c.goat_id IS NOT NULL
),
proofed_goat AS (
  SELECT count(DISTINCT subject_id) AS n
  FROM proof_artifacts p
  JOIN eligible e ON e.goat_id = p.subject_id
  WHERE p.tenant_id = $1::uuid
    AND p.scope_type = 'task'
    AND p.scope_id = $2::uuid
    AND p.subject_type = 'goat'
    AND p.subject_id IS NOT NULL
    AND p.upload_state = 'completed'
),
proofed_shed AS (
  SELECT count(*) AS n
  FROM proof_artifacts p
  JOIN target_shed target ON target.shed_id IS NOT NULL AND p.scope_id = target.shed_id
  WHERE p.tenant_id = $1::uuid
    AND p.scope_type = 'shed'
    AND p.subject_type = 'shed'
    AND (p.subject_id IS NULL OR p.subject_id = p.scope_id)
    AND p.upload_state = 'completed'
)
SELECT COALESCE((SELECT n FROM expected), 0),
       COALESCE((SELECT n FROM handled), 0),
       CASE WHEN $3 = 'shed'
         THEN COALESCE((SELECT n FROM proofed_shed), 0)
         ELSE COALESCE((SELECT n FROM proofed_goat), 0)
       END`,
		tenantID,
		taskID,
		proofSubject,
		shedID,
	).Scan(&expected, &handled, &proofReady)
	if err != nil {
		return ports.ShedCompletionReadiness{}, err
	}
	if expected <= 0 {
		return ports.ShedCompletionReadiness{Enabled: false, Reason: "no animals found for this shed"}, nil
	}
	if handled != expected {
		if handled < expected {
			return ports.ShedCompletionReadiness{Enabled: false, Reason: fmt.Sprintf("%d animals still need scanning", expected-handled)}, nil
		}
		return ports.ShedCompletionReadiness{Enabled: false, Reason: "scanned animals do not match this shed"}, nil
	}
	if proofSubject == "shed" {
		if proofReady < int64(minProofs) {
			return ports.ShedCompletionReadiness{Enabled: false, Reason: fmt.Sprintf("%d shed video(s) still need proof", int64(minProofs)-proofReady)}, nil
		}
		if proofReady > int64(maxProofs) {
			return ports.ShedCompletionReadiness{Enabled: false, Reason: fmt.Sprintf("at most %d shed video(s) can be submitted", maxProofs)}, nil
		}
		return ports.ShedCompletionReadiness{Enabled: true}, nil
	}
	if proofReady != expected {
		if proofReady < expected {
			return ports.ShedCompletionReadiness{Enabled: false, Reason: fmt.Sprintf("%d animals still need proof", expected-proofReady)}, nil
		}
		return ports.ShedCompletionReadiness{Enabled: false, Reason: "proof records do not match this shed"}, nil
	}
	return ports.ShedCompletionReadiness{Enabled: true}, nil
}

func (r *Repository) CompletedTaskProofRefs(ctx context.Context, tenantID, taskID, proofSubject, shedID string) ([]domain.ProofReference, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if proofSubject == "" {
		proofSubject = "goat"
	}
	rows, err := r.pool.Query(ctx, `
WITH task_scope AS (
  SELECT scope_type, scope_id
  FROM sop_tasks
  WHERE tenant_id = $1::uuid
    AND task_id = $2::uuid
),
submitted_proofs AS (
  SELECT DISTINCT proof.ref ->> 'proof_id' AS proof_id
  FROM sop_submissions ss
  CROSS JOIN LATERAL jsonb_array_elements(ss.proof_refs) AS proof(ref)
  WHERE ss.tenant_id = $1::uuid
    AND ss.task_id = $2::uuid
    AND ss.state IN ('submitted', 'needs_review', 'accepted', 'rejected', 'voided')
)
SELECT proof_id::text,
       proof_type,
       subject_type,
       COALESCE(subject_id::text, ''),
       upload_state,
       metadata::text
FROM proof_artifacts p
LEFT JOIN task_scope ts ON true
WHERE p.tenant_id = $1::uuid
  AND p.subject_type = $3
  AND (
    ($3 = 'shed'
      AND p.scope_type = 'shed'
      AND (
        (nullif($4, '')::uuid IS NOT NULL AND p.scope_id = nullif($4, '')::uuid)
        OR (nullif($4, '')::uuid IS NULL AND ts.scope_type = 'shed' AND p.scope_id = ts.scope_id)
        OR (nullif($4, '')::uuid IS NULL AND ts.scope_type <> 'shed'
          AND NOT EXISTS (SELECT 1 FROM submitted_proofs sp WHERE sp.proof_id = p.proof_id::text))
      )
      AND (p.subject_id IS NULL OR p.subject_id = p.scope_id))
    OR
    ($3 <> 'shed'
      AND p.scope_type = 'task'
      AND p.scope_id = $2::uuid
      AND ($3 <> 'goat' OR p.subject_id IS NOT NULL))
  )
  AND p.upload_state = 'completed'
ORDER BY created_at, proof_id`,
		tenantID,
		taskID,
		proofSubject,
		shedID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProofReference{}
	for rows.Next() {
		var ref domain.ProofReference
		var subjectID string
		var metadataRaw string
		if err := rows.Scan(&ref.ProofID, &ref.ProofType, &ref.SubjectType, &subjectID, &ref.UploadState, &metadataRaw); err != nil {
			return nil, err
		}
		if subjectID != "" {
			ref.SubjectID = &subjectID
		}
		ref.Metadata = decodeMap([]byte(metadataRaw))
		out = append(out, ref)
	}
	return out, rows.Err()
}

func insertSubmissionFanout(ctx context.Context, tx pgx.Tx, cmd ports.SubmitTaskCommand, submissionID string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO sop_task_submission_fanouts (
  tenant_id, task_id, submission_id, status, requested_by
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'pending', nullif($4, '')::uuid
)
ON CONFLICT (tenant_id, submission_id) DO UPDATE
SET status = 'pending',
    requested_by = COALESCE(EXCLUDED.requested_by, sop_task_submission_fanouts.requested_by),
    last_error = '',
    updated_at = now()`,
		cmd.TenantID,
		cmd.TaskID,
		submissionID,
		cmd.ActorID,
	)
	return err
}

func (r *Repository) SubmitTask(ctx context.Context, cmd ports.SubmitTaskCommand) (domain.SubmissionSummary, domain.TaskSummary, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	defer rollback(ctx, tx)
	existing, replay, err := r.existingSubmission(ctx, tx, cmd)
	if err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	if replay {
		task, _, _, err := r.GetTask(ctx, cmd.TenantID, existing.TaskID)
		return existing, task, true, err
	}
	var currentState string
	if err := tx.QueryRow(ctx, `
SELECT state
FROM sop_tasks
WHERE tenant_id = $1::uuid
  AND task_id = $2::uuid`,
		cmd.TenantID,
		cmd.TaskID,
	).Scan(&currentState); err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, mapUpdateErr(err)
	}
	if currentState == "accepted" {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, ports.ErrConflict
	}
	answers, err := json.Marshal(nonNilMap(cmd.Body.Answers))
	if err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	proofRefs, err := json.Marshal(cmd.Body.ProofRefs)
	if err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	report, err := json.Marshal(cmd.Report)
	if err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	var submissionID string
	err = tx.QueryRow(ctx, `
INSERT INTO sop_submissions (
  tenant_id, task_id, sop_version_id, submitted_by, idempotency_key,
  answers, proof_refs, state, validation_report, accepted_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5,
  $6::jsonb, $7::jsonb, $8, $9::jsonb,
  CASE WHEN $8 = 'accepted' THEN now() ELSE NULL END
)
RETURNING submission_id::text`,
		cmd.TenantID,
		cmd.TaskID,
		cmd.Body.SOPVersionID,
		cmd.ActorID,
		cmd.Body.IdempotencyKey,
		answers,
		proofRefs,
		cmd.TaskState,
		report,
	).Scan(&submissionID)
	if err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, mapWriteErr(err)
	}
	if err := insertSubmissionItems(ctx, tx, cmd, submissionID); err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	var taskID string
	err = tx.QueryRow(ctx, `
UPDATE sop_tasks
SET state = CASE
        WHEN state = 'accepted' THEN state
        WHEN $3 <> 'needs_review' THEN $3
        WHEN NOT EXISTS (
            SELECT 1
            FROM obligation_batches ob
            WHERE ob.tenant_id = sop_tasks.tenant_id
              AND ob.sop_task_id = sop_tasks.task_id
        ) THEN $3
        WHEN NOT EXISTS (
            SELECT 1
            FROM obligation_instances oi
            JOIN obligation_batches ob
              ON ob.tenant_id = oi.tenant_id
             AND ob.batch_id = oi.batch_id
            WHERE oi.tenant_id = sop_tasks.tenant_id
              AND ob.sop_task_id = sop_tasks.task_id
              AND oi.target_type = 'goat'
              AND oi.status NOT IN ('completed', 'waived', 'canceled', 'superseded')
              AND NOT EXISTS (
                  SELECT 1
                  FROM sop_submission_items si
                  WHERE si.tenant_id = oi.tenant_id
                    AND si.task_id = sop_tasks.task_id
                    AND si.goat_id = oi.target_id
                    AND si.state IN ('accepted', 'needs_review')
              )
        ) THEN $3
        WHEN state IN ('queued', 'assigned') THEN 'in_progress'
        ELSE state
    END,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND task_id = $2::uuid
  AND state IN ('queued', 'assigned', 'in_progress', 'rework_requested', 'needs_review')
RETURNING task_id::text`, cmd.TenantID, cmd.TaskID, cmd.TaskState).Scan(&taskID)
	if err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, mapUpdateErr(err)
	}
	if cmd.TaskState == "accepted" {
		payload, _ := json.Marshal(nonNilMap(cmd.MovementPayload))
		_, err = tx.Exec(ctx, `
	INSERT INTO movement_commands (tenant_id, task_id, submission_id, command_type, state, payload)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shifting.apply', 'accepted', $4::jsonb)
ON CONFLICT (tenant_id, submission_id, command_type) DO NOTHING`, cmd.TenantID, cmd.TaskID, submissionID, payload)
		if err != nil {
			return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
		}
	}
	if cmd.SubmissionFanoutRequired {
		if err := insertSubmissionFanout(ctx, tx, cmd, submissionID); err != nil {
			return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
		}
	}
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "sop.submission.create", "sop_submission", submissionID, map[string]any{"task_id": cmd.TaskID, "state": cmd.TaskState}); err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	submissions, err := r.listSubmissions(context.WithoutCancel(ctx), cmd.TenantID, cmd.TaskID)
	if err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	task, _, _, err := r.GetTask(context.WithoutCancel(ctx), cmd.TenantID, cmd.TaskID)
	if err != nil {
		return domain.SubmissionSummary{}, domain.TaskSummary{}, false, err
	}
	for _, submission := range submissions {
		if submission.SubmissionID == submissionID {
			return submission, task, false, nil
		}
	}
	return domain.SubmissionSummary{}, domain.TaskSummary{}, false, ports.ErrNotFound
}

// AcceptSubmissionItemVerification projects one closed per-goat verification item into the SOP
// aggregate. Locking the submission and task serializes concurrent final-goat events, so exactly
// one caller performs the needs_review -> accepted roll-up. Replays are read-only successes.
func (r *Repository) AcceptSubmissionItemVerification(ctx context.Context, tenantID, submissionID, goatID, actorID string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var taskID, submissionState string
	err = tx.QueryRow(ctx, `
SELECT ss.task_id::text, ss.state
FROM sop_submissions ss
JOIN sop_tasks st
  ON st.tenant_id = ss.tenant_id
 AND st.task_id = ss.task_id
WHERE ss.tenant_id = $1::uuid
  AND ss.submission_id = $2::uuid
FOR UPDATE OF ss, st`, tenantID, submissionID).Scan(&taskID, &submissionState)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if submissionState != "needs_review" && submissionState != "accepted" {
		return ports.ErrConflict
	}

	rows, err := tx.Query(ctx, `
UPDATE sop_submission_items
SET state = 'accepted',
    result = result || jsonb_build_object(
      'verification_closed_at', now(),
      'verification_closed_by', $4::text
    )
WHERE tenant_id = $1::uuid
  AND submission_id = $2::uuid
  AND goat_id = $3::uuid
  AND state = 'needs_review'
RETURNING item_id::text`, tenantID, submissionID, goatID, actorID)
	if err != nil {
		return err
	}
	updatedItemIDs := make([]string, 0, 1)
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			rows.Close()
			return err
		}
		updatedItemIDs = append(updatedItemIDs, itemID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	var matchingItems int
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM sop_submission_items
WHERE tenant_id = $1::uuid
  AND submission_id = $2::uuid
  AND goat_id = $3::uuid
  AND state = 'accepted'`, tenantID, submissionID, goatID).Scan(&matchingItems); err != nil {
		return err
	}
	if matchingItems == 0 {
		return ports.ErrConflict
	}
	for _, itemID := range updatedItemIDs {
		if err := insertAudit(ctx, tx, tenantID, actorID, "sop.submission_item.accepted", "sop_submission_item", itemID, map[string]any{
			"submission_id": submissionID,
			"goat_id":       goatID,
		}); err != nil {
			return err
		}
	}

	var remainingItems int
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM sop_submission_items
WHERE tenant_id = $1::uuid
  AND submission_id = $2::uuid
  AND state NOT IN ('accepted', 'skipped')`, tenantID, submissionID).Scan(&remainingItems); err != nil {
		return err
	}
	if remainingItems > 0 {
		return tx.Commit(ctx)
	}

	var acceptedSubmissionID string
	err = tx.QueryRow(ctx, `
UPDATE sop_submissions
SET state = 'accepted',
    accepted_at = COALESCE(accepted_at, now()),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND submission_id = $2::uuid
  AND state = 'needs_review'
RETURNING submission_id::text`, tenantID, submissionID).Scan(&acceptedSubmissionID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if acceptedSubmissionID != "" {
		if err := insertAudit(ctx, tx, tenantID, actorID, "sop.submission.accepted", "sop_submission", acceptedSubmissionID, map[string]any{"task_id": taskID}); err != nil {
			return err
		}
	}

	var acceptedTaskID string
	err = tx.QueryRow(ctx, `
UPDATE sop_tasks st
SET state = 'accepted',
    verified_by = $3::uuid,
    verified_at = COALESCE(verified_at, now()),
    updated_at = now(),
    row_version = row_version + 1
WHERE st.tenant_id = $1::uuid
  AND st.task_id = $2::uuid
  AND st.state = 'needs_review'
  AND NOT EXISTS (
    SELECT 1
    FROM sop_submissions newer
    JOIN sop_submissions current
      ON current.tenant_id = newer.tenant_id
     AND current.submission_id = $4::uuid
    WHERE newer.tenant_id = st.tenant_id
      AND newer.task_id = st.task_id
      AND (newer.submitted_at, newer.submission_id) > (current.submitted_at, current.submission_id)
  )
RETURNING st.task_id::text`, tenantID, taskID, actorID, submissionID).Scan(&acceptedTaskID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if acceptedTaskID != "" {
		if err := insertAudit(ctx, tx, tenantID, actorID, "sop.task.accepted", "sop_task", acceptedTaskID, map[string]any{"submission_id": submissionID}); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *Repository) setVersionStatus(ctx context.Context, cmd ports.VersionCommand, status string) (domain.SOPVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	defer rollback(ctx, tx)
	if status == "published" {
		_, err = tx.Exec(ctx, `
UPDATE sop_versions
SET status = 'retired', retired_at = COALESCE(retired_at, now()), updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND sop_id = $2::uuid
  AND status = 'published'
  AND sop_version_id <> $3::uuid`, cmd.TenantID, cmd.SOPID, cmd.SOPVersionID)
		if err != nil {
			return domain.SOPVersion{}, err
		}
	}
	var versionID string
	if status == "published" {
		err = tx.QueryRow(ctx, `
UPDATE sop_versions
SET status = 'published',
    published_by = $5::uuid,
    published_at = COALESCE(published_at, now()),
    retired_at = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND sop_id = $2::uuid
  AND sop_version_id = $3::uuid
  AND row_version = $4
RETURNING sop_version_id::text`, cmd.TenantID, cmd.SOPID, cmd.SOPVersionID, cmd.RowVersion, cmd.ActorID).Scan(&versionID)
	} else {
		err = tx.QueryRow(ctx, `
UPDATE sop_versions
SET status = 'retired',
    retired_at = COALESCE(retired_at, now()),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND sop_id = $2::uuid
  AND sop_version_id = $3::uuid
  AND row_version = $4
RETURNING sop_version_id::text`, cmd.TenantID, cmd.SOPID, cmd.SOPVersionID, cmd.RowVersion).Scan(&versionID)
	}
	if err != nil {
		return domain.SOPVersion{}, mapUpdateErr(err)
	}
	definitionStatus := "active"
	if status == "retired" {
		definitionStatus = "retired"
	}
	_, _ = tx.Exec(ctx, `UPDATE sop_definitions SET status = $3, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1::uuid AND sop_id = $2::uuid`, cmd.TenantID, cmd.SOPID, definitionStatus)
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "sop.version."+status, "sop_version", versionID, nil); err != nil {
		return domain.SOPVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SOPVersion{}, err
	}
	return r.GetVersion(context.WithoutCancel(ctx), cmd.TenantID, cmd.SOPID, versionID)
}

func (r *Repository) updateTaskState(ctx context.Context, tenantID, actorID, taskID string, rowVersion int, state string, assignedTo *string, action string, metadata map[string]any) (domain.TaskSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.TaskSummary{}, err
	}
	defer rollback(ctx, tx)
	var updatedID string
	err = tx.QueryRow(ctx, `
UPDATE sop_tasks
SET state = $4,
    assigned_to = COALESCE(nullif($5, '')::uuid, assigned_to),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND task_id = $2::uuid
  AND row_version = $3
RETURNING task_id::text`, tenantID, taskID, rowVersion, state, ptrValue(assignedTo)).Scan(&updatedID)
	if err != nil {
		return domain.TaskSummary{}, mapUpdateErr(err)
	}
	if err := insertAudit(ctx, tx, tenantID, actorID, action, "sop_task", updatedID, metadata); err != nil {
		return domain.TaskSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskSummary{}, err
	}
	task, _, _, err := r.GetTask(context.WithoutCancel(ctx), tenantID, updatedID)
	return task, err
}

func (r *Repository) latestVersion(ctx context.Context, tenantID, sopID string) (domain.SOPVersion, error) {
	rows, err := r.pool.Query(ctx, versionSelectSQL(`
WHERE sv.tenant_id = $1::uuid
  AND sv.sop_id = $2::uuid
ORDER BY sv.version DESC
LIMIT 1`), tenantID, sopID)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	items, err := scanVersions(rows)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	if len(items) == 0 {
		return domain.SOPVersion{}, ports.ErrNotFound
	}
	return items[0], nil
}

// latestVersionsForSQL selects the single highest-version row per sop_id in one pass via DISTINCT ON.
// Column order is identical to versionSelectSQL so scanVersions decodes it unchanged. It cannot reuse
// versionSelectSQL because DISTINCT ON must lead the SELECT list.
const latestVersionsForSQL = `
SELECT DISTINCT ON (sv.sop_id)
  sv.sop_version_id::text,
  sv.tenant_id::text,
  sv.sop_id::text,
  sd.code,
  sv.version,
  sv.version_label,
  sv.status,
  sv.form_dsl,
  sv.proof_policy,
  sv.compatibility,
  sv.validation_report,
  sv.published_at,
  sv.retired_at,
  sv.row_version,
  sv.created_at,
  sv.updated_at
FROM sop_versions sv
JOIN sop_definitions sd
  ON sd.tenant_id = sv.tenant_id
 AND sd.sop_id = sv.sop_id
WHERE sv.tenant_id = $1::uuid
  AND sv.sop_id = ANY($2::uuid[])
ORDER BY sv.sop_id, sv.version DESC`

func (r *Repository) LatestVersionsFor(ctx context.Context, tenantID string, sopIDs []string) (map[string]domain.SOPVersion, error) {
	if len(sopIDs) == 0 {
		return map[string]domain.SOPVersion{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, latestVersionsForSQL, tenantID, sopIDs)
	if err != nil {
		return nil, err
	}
	items, err := scanVersions(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string]domain.SOPVersion, len(items))
	for _, v := range items {
		out[v.SOPID] = v
	}
	return out, nil
}

func (r *Repository) resolveTaskVersion(ctx context.Context, tx pgx.Tx, cmd ports.CreateTaskCommand) (domain.SOPVersion, error) {
	if cmd.Body.SOPVersionID != nil && *cmd.Body.SOPVersionID != "" {
		rows, err := tx.Query(ctx, versionSelectSQL(`
WHERE sv.tenant_id = $1::uuid
  AND sv.sop_version_id = $2::uuid
  AND sv.status = 'published'
LIMIT 1`), cmd.TenantID, *cmd.Body.SOPVersionID)
		if err != nil {
			return domain.SOPVersion{}, err
		}
		items, err := scanVersions(rows)
		if err != nil {
			return domain.SOPVersion{}, err
		}
		if len(items) == 0 {
			return domain.SOPVersion{}, ports.ErrNotFound
		}
		return items[0], nil
	}
	rows, err := tx.Query(ctx, versionSelectSQL(`
WHERE sv.tenant_id = $1::uuid
  AND sd.code = $2
  AND sv.status = 'published'
LIMIT 1`), cmd.TenantID, cmd.Body.SOPCode)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	items, err := scanVersions(rows)
	if err != nil {
		return domain.SOPVersion{}, err
	}
	if len(items) == 0 {
		return domain.SOPVersion{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) existingSubmission(ctx context.Context, tx pgx.Tx, cmd ports.SubmitTaskCommand) (domain.SubmissionSummary, bool, error) {
	var submissionID, taskID, answersRaw, proofRaw string
	err := tx.QueryRow(ctx, `
SELECT submission_id::text, task_id::text, answers::text, proof_refs::text
FROM sop_submissions
WHERE tenant_id = $1::uuid AND idempotency_key = $2
LIMIT 1`, cmd.TenantID, cmd.Body.IdempotencyKey).Scan(&submissionID, &taskID, &answersRaw, &proofRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SubmissionSummary{}, false, nil
	}
	if err != nil {
		return domain.SubmissionSummary{}, false, err
	}
	answers, _ := json.Marshal(nonNilMap(cmd.Body.Answers))
	proof, _ := json.Marshal(cmd.Body.ProofRefs)
	if taskID != cmd.TaskID || !jsonEqual([]byte(answersRaw), answers) || !jsonEqual([]byte(proofRaw), proof) {
		return domain.SubmissionSummary{}, false, ports.ErrIdempotencyConflict
	}
	submissions, err := r.listSubmissions(ctx, cmd.TenantID, taskID)
	if err != nil {
		return domain.SubmissionSummary{}, false, err
	}
	for _, item := range submissions {
		if item.SubmissionID == submissionID {
			return item, true, nil
		}
	}
	return domain.SubmissionSummary{}, false, ports.ErrNotFound
}

func insertSubmissionItems(ctx context.Context, tx pgx.Tx, cmd ports.SubmitTaskCommand, submissionID string) error {
	keys := cmd.SubmissionItems
	if len(keys) == 0 {
		keys = itemKeys(cmd.Body.Answers)
	}
	for _, item := range keys {
		resultMap := map[string]any{"accepted_at": time.Now().UTC().Format(time.RFC3339)}
		if administeredAt := strings.TrimSpace(item.AdministeredAt); administeredAt != "" {
			resultMap["administered_at"] = administeredAt
		}
		result, _ := json.Marshal(resultMap)
		_, err := tx.Exec(ctx, `
INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state, result)
VALUES ($1::uuid, $2::uuid, $3::uuid, nullif($4, '')::uuid, $5, $6, $7::jsonb)`,
			cmd.TenantID, submissionID, cmd.TaskID, item.GoatID, item.ItemKey, cmd.ItemState, result)
		if err != nil {
			return err
		}
	}
	return nil
}

func insertMovementForLatestSubmission(ctx context.Context, tx pgx.Tx, tenantID, taskID string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO movement_commands (tenant_id, task_id, submission_id, command_type, state, payload)
SELECT tenant_id, task_id, submission_id, 'shifting.apply', 'accepted',
       jsonb_build_object(
         'task_id', task_id::text,
         'sop_version_id', sop_version_id::text,
         'goat_ids', answers -> 'goat_ids',
         'source_location_id', answers -> 'source_location_id',
         'destination_location_id', answers -> 'destination_location_id',
         'destination_count', answers -> 'destination_count',
         'category', answers -> 'category',
         'priority', answers -> 'priority'
       )
FROM sop_submissions
WHERE tenant_id = $1::uuid
  AND task_id = $2::uuid
  AND state IN ('submitted', 'needs_review', 'accepted')
ORDER BY submitted_at DESC
LIMIT 1
ON CONFLICT (tenant_id, submission_id, command_type) DO NOTHING`, tenantID, taskID)
	return err
}

func (r *Repository) listSubmissions(ctx context.Context, tenantID, taskID string) ([]domain.SubmissionSummary, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
  submission_id::text,
  task_id::text,
  sop_version_id::text,
  submitted_by::text,
  idempotency_key,
  answers,
  proof_refs,
  state,
  validation_report,
  submitted_at,
  accepted_at,
  row_version
FROM sop_submissions
WHERE tenant_id = $1::uuid
  AND task_id = $2::uuid
ORDER BY submitted_at DESC`, tenantID, taskID)
	if err != nil {
		return nil, err
	}
	items, err := scanSubmissions(rows)
	if err != nil {
		return nil, err
	}
	for i := range items {
		childRows, err := r.pool.Query(ctx, `
SELECT item_id::text, goat_id::text, item_key, state, result
FROM sop_submission_items
WHERE tenant_id = $1::uuid
  AND submission_id = $2::uuid
ORDER BY created_at, item_id`, tenantID, items[i].SubmissionID)
		if err != nil {
			return nil, err
		}
		items[i].Items, err = scanSubmissionItems(childRows)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func sopSelectSQL(where string) string {
	return `
SELECT
  sd.sop_id::text,
  sd.tenant_id::text,
  sd.code,
  sd.name,
  sd.description,
  sd.status,
  (
    SELECT active.sop_version_id::text
    FROM sop_versions active
    WHERE active.tenant_id = sd.tenant_id
      AND active.sop_id = sd.sop_id
      AND active.status = 'published'
    LIMIT 1
  ),
  (
    SELECT count(*)::int
    FROM sop_versions sv
    WHERE sv.tenant_id = sd.tenant_id
      AND sv.sop_id = sd.sop_id
  ),
  sd.row_version,
  sd.created_at,
  sd.updated_at
FROM sop_definitions sd
` + where
}

func versionSelectSQL(where string) string {
	return `
SELECT
  sv.sop_version_id::text,
  sv.tenant_id::text,
  sv.sop_id::text,
  sd.code,
  sv.version,
  sv.version_label,
  sv.status,
  sv.form_dsl,
  sv.proof_policy,
  sv.compatibility,
  sv.validation_report,
  sv.published_at,
  sv.retired_at,
  sv.row_version,
  sv.created_at,
  sv.updated_at
FROM sop_versions sv
JOIN sop_definitions sd
  ON sd.tenant_id = sv.tenant_id
 AND sd.sop_id = sv.sop_id
` + where
}

func taskSelectSQL(where string) string {
	return `
SELECT
  st.task_id::text,
  st.tenant_id::text,
  st.sop_id::text,
  st.sop_version_id::text,
  sd.code,
  st.task_type,
  st.title,
  st.description,
  st.state,
  st.assigned_to::text,
  st.scope_type,
  st.scope_id::text,
  COALESCE(scope_location.name, ''),
  st.priority,
  st.due_at,
  st.context,
  st.row_version,
  st.created_at,
  st.updated_at
FROM sop_tasks st
JOIN sop_definitions sd
  ON sd.tenant_id = st.tenant_id
 AND sd.sop_id = st.sop_id
LEFT JOIN locations scope_location
  ON scope_location.tenant_id = st.tenant_id
 AND scope_location.location_id = st.scope_id
` + where
}

func scanSOPs(rows pgx.Rows) ([]domain.SOPDefinition, error) {
	defer rows.Close()
	items := []domain.SOPDefinition{}
	for rows.Next() {
		var item domain.SOPDefinition
		var active pgtype.Text
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&item.SOPID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.Status, &active, &item.VersionCount, &item.RowVersion, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.ActiveVersionID = textPtr(active)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanVersions(rows pgx.Rows) ([]domain.SOPVersion, error) {
	defer rows.Close()
	items := []domain.SOPVersion{}
	for rows.Next() {
		var item domain.SOPVersion
		var formDSL, proofPolicy, compatibility, report []byte
		var publishedAt, retiredAt pgtype.Timestamptz
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&item.SOPVersionID, &item.TenantID, &item.SOPID, &item.SOPCode, &item.Version, &item.VersionLabel, &item.Status, &formDSL, &proofPolicy, &compatibility, &report, &publishedAt, &retiredAt, &item.RowVersion, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.FormDSL = decodeMap(formDSL)
		item.ProofPolicy = decodeMap(proofPolicy)
		item.Compatibility = decodeMap(compatibility)
		item.ValidationReport = decodeReport(report)
		item.PublishedAt = timePtr(publishedAt)
		item.RetiredAt = timePtr(retiredAt)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanTasks(rows pgx.Rows) ([]domain.TaskSummary, error) {
	defer rows.Close()
	items := []domain.TaskSummary{}
	for rows.Next() {
		var item domain.TaskSummary
		var assignedTo pgtype.Text
		var dueAt pgtype.Timestamptz
		var contextBytes []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&item.TaskID, &item.TenantID, &item.SOPID, &item.SOPVersionID, &item.SOPCode, &item.TaskType, &item.Title, &item.Description, &item.State, &assignedTo, &item.ScopeType, &item.ScopeID, &item.ScopeLabel, &item.Priority, &dueAt, &contextBytes, &item.RowVersion, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.AssignedTo = textPtr(assignedTo)
		item.DueAt = timePtr(dueAt)
		item.Context = decodeMap(contextBytes)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanSubmissions(rows pgx.Rows) ([]domain.SubmissionSummary, error) {
	defer rows.Close()
	items := []domain.SubmissionSummary{}
	for rows.Next() {
		var item domain.SubmissionSummary
		var answers, proofRefs, report []byte
		var submittedAt time.Time
		var acceptedAt pgtype.Timestamptz
		if err := rows.Scan(&item.SubmissionID, &item.TaskID, &item.SOPVersionID, &item.SubmittedBy, &item.IdempotencyKey, &answers, &proofRefs, &item.State, &report, &submittedAt, &acceptedAt, &item.RowVersion); err != nil {
			return nil, err
		}
		item.Answers = decodeMap(answers)
		item.ProofRefs = decodeProofRefs(proofRefs)
		item.ValidationReport = decodeReport(report)
		item.SubmittedAt = submittedAt.UTC().Format(time.RFC3339)
		item.AcceptedAt = timePtr(acceptedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanSubmissionItems(rows pgx.Rows) ([]domain.SubmissionItem, error) {
	defer rows.Close()
	items := []domain.SubmissionItem{}
	for rows.Next() {
		var item domain.SubmissionItem
		var goatID pgtype.Text
		var result []byte
		if err := rows.Scan(&item.ItemID, &goatID, &item.ItemKey, &item.State, &result); err != nil {
			return nil, err
		}
		item.GoatID = textPtr(goatID)
		item.Result = decodeMap(result)
		items = append(items, item)
	}
	return items, rows.Err()
}

func insertAudit(ctx context.Context, tx pgx.Tx, tenantID, actorID, action, resourceType, resourceID string, metadata map[string]any) error {
	metadataBytes, err := json.Marshal(nonNilMap(metadata))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id, actor_id, actor_type, action, resource_type, resource_id, metadata
) VALUES (
  $1::uuid, $2::uuid, 'human', $3, $4, $5::uuid, $6::jsonb
)`, tenantID, actorID, action, resourceType, resourceID, metadataBytes)
	return err
}

func itemKeys(answers map[string]any) []ports.SubmissionItemInput {
	raw, ok := answers["goat_ids"].([]any)
	if !ok || len(raw) == 0 {
		return []ports.SubmissionItemInput{{ItemKey: "batch"}}
	}
	out := make([]ports.SubmissionItemInput, 0, len(raw))
	for i, value := range raw {
		goatID, _ := value.(string)
		if goatID == "" {
			out = append(out, ports.SubmissionItemInput{ItemKey: "goat-" + time.Now().UTC().Format("150405")})
			continue
		}
		out = append(out, ports.SubmissionItemInput{GoatID: goatID, ItemKey: goatID})
		if i >= 999 {
			break
		}
	}
	return out
}

func int64Value(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func normalizeScanTag(tag string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(tag) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func decodeMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func decodeReport(raw []byte) domain.ValidationReport {
	var out domain.ValidationReport
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return domain.ValidationReport{Valid: false, Errors: []domain.ValidationIssue{}, Warnings: []domain.ValidationIssue{}}
	}
	return out
}

func decodeProofRefs(raw []byte) []domain.ProofReference {
	var out []domain.ProofReference
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return []domain.ProofReference{}
	}
	return out
}

func textPtr(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	value := v.String
	return &value
}

func timePtr(v pgtype.Timestamptz) *string {
	if !v.Valid {
		return nil
	}
	value := v.Time.UTC().Format(time.RFC3339)
	return &value
}

func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func nonNilMap(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return v
}

func obligationBatchIDFromContext(v map[string]any) string {
	raw, ok := v["obligation_batch_id"]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func uniqueViolationOn(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraintName
}

func jsonEqual(left, right []byte) bool {
	var l, r any
	if json.Unmarshal(left, &l) != nil || json.Unmarshal(right, &r) != nil {
		return bytes.Equal(left, right)
	}
	lb, _ := json.Marshal(l)
	rb, _ := json.Marshal(r)
	return bytes.Equal(lb, rb)
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func mapWriteErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ports.ErrConflict
		case "23503", "23514", "22P02":
			return ports.ErrConflict
		}
	}
	return err
}

func mapUpdateErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrConflict
	}
	return mapWriteErr(err)
}
