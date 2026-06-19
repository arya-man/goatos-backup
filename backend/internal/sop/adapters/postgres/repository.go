package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	rows, err := r.pool.Query(ctx, sopSelectSQL(`
WHERE sd.tenant_id = $1::uuid
  AND ($2 = '' OR sd.status = $2)
ORDER BY sd.updated_at DESC, sd.sop_id DESC
LIMIT $3`), params.TenantID, params.Status, params.Limit)
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

func (r *Repository) ListTasks(ctx context.Context, params ports.ListTasksParams) ([]domain.TaskSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, taskSelectSQL(`
WHERE st.tenant_id = $1::uuid
  AND ($2 = '' OR st.state = $2)
  AND ($3 = '' OR st.assigned_to = $3::uuid)
  AND ($4 = '' OR st.scope_type = $4)
  AND ($5 = '' OR st.scope_id = $5::uuid)
ORDER BY st.due_at NULLS LAST, st.updated_at DESC, st.task_id DESC
LIMIT $6`), params.TenantID, params.State, params.AssignedTo, params.ScopeType, params.ScopeID, params.Limit)
	if err != nil {
		return nil, err
	}
	return scanTasks(rows)
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
	contextBytes, err := json.Marshal(nonNilMap(cmd.Body.Context))
	if err != nil {
		return domain.TaskSummary{}, err
	}
	var taskID string
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
RETURNING task_id::text`, cmd.TenantID, cmd.TaskID, cmd.Body.RowVersion, cmd.State, cmd.ActorID).Scan(&taskID)
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
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskSummary{}, err
	}
	task, _, _, err := r.GetTask(context.WithoutCancel(ctx), cmd.TenantID, taskID)
	return task, err
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
SET state = $3,
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
	keys := itemKeys(cmd.Body.Answers)
	for _, item := range keys {
		result, _ := json.Marshal(map[string]any{"accepted_at": time.Now().UTC().Format(time.RFC3339)})
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
		if err := rows.Scan(&item.TaskID, &item.TenantID, &item.SOPID, &item.SOPVersionID, &item.SOPCode, &item.TaskType, &item.Title, &item.Description, &item.State, &assignedTo, &item.ScopeType, &item.ScopeID, &item.Priority, &dueAt, &contextBytes, &item.RowVersion, &createdAt, &updatedAt); err != nil {
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

type submissionItemKey struct {
	GoatID  string
	ItemKey string
}

func itemKeys(answers map[string]any) []submissionItemKey {
	raw, ok := answers["goat_ids"].([]any)
	if !ok || len(raw) == 0 {
		return []submissionItemKey{{ItemKey: "batch"}}
	}
	out := make([]submissionItemKey, 0, len(raw))
	for i, value := range raw {
		goatID, _ := value.(string)
		if goatID == "" {
			out = append(out, submissionItemKey{ItemKey: "goat-" + time.Now().UTC().Format("150405")})
			continue
		}
		out = append(out, submissionItemKey{GoatID: goatID, ItemKey: goatID})
		if i >= 999 {
			break
		}
	}
	return out
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
