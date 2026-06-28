// Package postgres implements the vaccination Repository over generated sqlc queries.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	vaccinationdb "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

const defaultQueryTimeout = 3 * time.Second

// Repository is the Postgres-backed vaccination repository.
type Repository struct {
	pool         *pgxpool.Pool
	queries      *vaccinationdb.Queries
	queryTimeout time.Duration
}

// NewRepository builds a Repository bound to a pgx pool.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, queries: vaccinationdb.New(pool), queryTimeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.queryTimeout)
}

// Ping checks pool connectivity.
func (r *Repository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.pool.Ping(ctx)
}

// StartGenerationRun creates or returns the durable status row for an existing-cohort generation
// pass. The idempotency key prevents replayed publish hooks from launching duplicate herd scans.
func (r *Repository) StartGenerationRun(ctx context.Context, in domain.GenerationRunInput) (domain.GenerationRun, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if in.StartedAt.IsZero() {
		in.StartedAt = time.Now().UTC()
	}
	runContext, _ := json.Marshal(map[string]string{"request_hash": in.RequestHash})
	rows, err := r.pool.Query(ctx, `
INSERT INTO vaccination_generation_runs (
  tenant_id, protocol_version_id, trigger_type, trigger_ref, status,
  started_at, idempotency_key, context
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'running', $5::timestamptz, $6, $7::jsonb
)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET status = 'running',
    started_at = CASE
      WHEN vaccination_generation_runs.status = 'running' THEN EXCLUDED.started_at
      ELSE vaccination_generation_runs.started_at
    END,
    context = CASE
      WHEN COALESCE(vaccination_generation_runs.context->>'request_hash', '') = ''
        THEN EXCLUDED.context
      ELSE vaccination_generation_runs.context
    END,
    completed_at = NULL,
    generated_count = 0,
    deferred_count = 0,
    skipped_no_due_date_count = 0,
    suppressed_trusted_history_count = 0,
    cursor_goat_id = NULL,
    last_error = NULL,
    updated_at = now(),
    row_version = vaccination_generation_runs.row_version + 1
WHERE (
    vaccination_generation_runs.status = 'failed'
    OR (
      vaccination_generation_runs.status = 'running'
      -- Staleness is measured against WALL-CLOCK now(), never the caller's business asOf ($5): on
      -- at-least-once redelivery of a crashed publish event asOf == started_at, so an asOf-based
      -- window could never elapse and a dead 1M-goat run would wedge forever. A live long run bumps
      -- updated_at via HeartbeatGenerationRun every page, so GREATEST(started_at, updated_at) stays
      -- fresh (not reclaimed); a crashed run with no heartbeat is reclaimable after 15 real minutes.
      AND GREATEST(vaccination_generation_runs.started_at, vaccination_generation_runs.updated_at) < now() - interval '15 minutes'
    )
  )
  AND (
    COALESCE(vaccination_generation_runs.context->>'request_hash', '') = ''
    OR COALESCE(vaccination_generation_runs.context->>'request_hash', '') = $8
  )
RETURNING run_id::text, tenant_id::text, protocol_version_id::text, trigger_type,
          COALESCE(trigger_ref, ''), status, started_at, completed_at,
          generated_count, deferred_count, skipped_no_due_date_count,
          suppressed_trusted_history_count, COALESCE(cursor_goat_id::text, ''),
          COALESCE(last_error, ''), idempotency_key, COALESCE(context->>'request_hash', '')`,
		in.TenantID, in.ProtocolVersionID, in.TriggerType, in.TriggerRef, in.StartedAt, in.IdempotencyKey, string(runContext), in.RequestHash)
	if err != nil {
		return domain.GenerationRun{}, false, fmt.Errorf("vaccination: start generation run: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		run, scanErr := scanGenerationRun(rows)
		return run, true, scanErr
	}
	if err := rows.Err(); err != nil {
		return domain.GenerationRun{}, false, fmt.Errorf("vaccination: start generation run rows: %w", err)
	}
	run, err := r.getGenerationRunByKey(ctx, in.TenantID, in.IdempotencyKey)
	if err == nil && in.RequestHash != "" && run.RequestHash != "" && run.RequestHash != in.RequestHash {
		return domain.GenerationRun{}, false, ports.ErrIdempotencyConflict
	}
	return run, false, err
}

// FinishGenerationRun records the terminal counts and error, if any. Empty lastError means success.
func (r *Repository) FinishGenerationRun(ctx context.Context, tenantID, runID string, result domain.GenerateResult, cursorGoatID, lastError string, completedAt time.Time) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	status := "completed"
	if lastError != "" {
		status = "failed"
	}
	_, err := r.pool.Exec(ctx, `
UPDATE vaccination_generation_runs
SET status = $3,
    completed_at = $4::timestamptz,
    generated_count = $5,
    deferred_count = $6,
    skipped_no_due_date_count = $7,
    suppressed_trusted_history_count = $8,
    cursor_goat_id = nullif($9::text, '')::uuid,
    last_error = nullif($10, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND run_id = $2::uuid`,
		tenantID, runID, status, completedAt, result.Generated, result.Deferred,
		result.SkippedNoDueDate, result.SuppressedByTrustedHistory, cursorGoatID, lastError)
	if err != nil {
		return fmt.Errorf("vaccination: finish generation run: %w", err)
	}
	return nil
}

// HeartbeatGenerationRun bumps updated_at on a running generation run so a long (1M-goat) pass is not
// reclaimed/duplicated by the stale-run detector in StartGenerationRun. Best-effort liveness: callers
// ignore the error rather than abort a multi-minute run on a transient blip.
func (r *Repository) HeartbeatGenerationRun(ctx context.Context, tenantID, runID string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
UPDATE vaccination_generation_runs
SET updated_at = now()
WHERE tenant_id = $1::uuid AND run_id = $2::uuid AND status = 'running'`, tenantID, runID)
	if err != nil {
		return fmt.Errorf("vaccination: heartbeat generation run: %w", err)
	}
	return nil
}

func (r *Repository) getGenerationRunByKey(ctx context.Context, tenantID, key string) (domain.GenerationRun, error) {
	var run domain.GenerationRun
	err := r.pool.QueryRow(ctx, `
SELECT run_id::text, tenant_id::text, protocol_version_id::text, trigger_type,
       COALESCE(trigger_ref, ''), status, started_at, completed_at,
       generated_count, deferred_count, skipped_no_due_date_count,
       suppressed_trusted_history_count, COALESCE(cursor_goat_id::text, ''),
       COALESCE(last_error, ''), idempotency_key, COALESCE(context->>'request_hash', '')
FROM vaccination_generation_runs
WHERE tenant_id = $1::uuid AND idempotency_key = $2`, tenantID, key).Scan(
		&run.RunID,
		&run.TenantID,
		&run.ProtocolVersionID,
		&run.TriggerType,
		&run.TriggerRef,
		&run.Status,
		&run.StartedAt,
		&run.CompletedAt,
		&run.Generated,
		&run.Deferred,
		&run.SkippedNoDueDate,
		&run.SuppressedByTrustedHistory,
		&run.CursorGoatID,
		&run.LastError,
		&run.IdempotencyKey,
		&run.RequestHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GenerationRun{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.GenerationRun{}, fmt.Errorf("vaccination: get generation run: %w", err)
	}
	return run, nil
}

type generationRunScanner interface {
	Scan(dest ...any) error
}

func scanGenerationRun(row generationRunScanner) (domain.GenerationRun, error) {
	var run domain.GenerationRun
	if err := row.Scan(
		&run.RunID,
		&run.TenantID,
		&run.ProtocolVersionID,
		&run.TriggerType,
		&run.TriggerRef,
		&run.Status,
		&run.StartedAt,
		&run.CompletedAt,
		&run.Generated,
		&run.Deferred,
		&run.SkippedNoDueDate,
		&run.SuppressedByTrustedHistory,
		&run.CursorGoatID,
		&run.LastError,
		&run.IdempotencyKey,
		&run.RequestHash,
	); err != nil {
		return domain.GenerationRun{}, fmt.Errorf("vaccination: scan generation run: %w", err)
	}
	return run, nil
}

// RecordCompletion appends an idempotent dose-administered record.
func (r *Repository) RecordCompletion(ctx context.Context, in domain.NewCompletion) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	obligation, err := pgconv.UUID(in.ObligationID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: obligation id: %w", err)
	}
	goat, err := pgconv.UUID(in.GoatID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: goat id: %w", err)
	}
	doseML, err := pgconv.Numeric(in.DoseMlGiven)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: dose_ml_given: %w", err)
	}
	status := in.Status
	if status == "" {
		status = "recorded"
	}
	id, err := r.queries.RecordVaccinationCompletion(ctx, vaccinationdb.RecordVaccinationCompletionParams{
		TenantID:                 tenant,
		ObligationID:             obligation,
		BatchID:                  pgconv.NullableUUID(in.BatchID),
		GoatID:                   goat,
		SopSubmissionItemID:      pgconv.NullableUUID(in.SopSubmissionItemID),
		VaccineInventoryLotID:    pgconv.NullableUUID(in.VaccineInventoryLotID),
		Doses:                    pgconv.Int4(in.Doses),
		DoseMlGiven:              doseML,
		RouteSite:                pgconv.Text(in.RouteSite),
		AdverseReaction:          in.AdverseReaction,
		AdverseReactionProblemID: pgconv.NullableUUID(in.AdverseReactionProblemID),
		ColdChainVerified:        in.ColdChainVerified,
		AdministeredAt:           pgconv.Timestamptz(in.AdministeredAt),
		Status:                   status,
		WithdrawalUntilDate:      pgconv.Date(in.WithdrawalUntilDate),
		RecordedBy:               pgconv.NullableUUID(in.RecordedBy),
		IdempotencyKey:           in.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil // already recorded for this idempotency key
	}
	if err != nil {
		return "", false, fmt.Errorf("vaccination: record completion: %w", err)
	}
	return id, true, nil
}

// AcceptCompletion marks a recorded completion accepted (SM-5).
func (r *Repository) AcceptCompletion(ctx context.Context, tenantID, completionID string, verifiedBy *string, withdrawalUntil *time.Time) (domain.AcceptedCompletion, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: completion id: %w", err)
	}
	rows, err := r.queries.AcceptVaccinationCompletion(ctx, vaccinationdb.AcceptVaccinationCompletionParams{
		VerifiedBy:          pgconv.NullableUUID(verifiedBy),
		WithdrawalUntilDate: pgconv.Date(withdrawalUntil),
		TenantID:            tenant,
		CompletionID:        cid,
	})
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: accept completion: %w", err)
	}
	if len(rows) == 0 {
		return domain.AcceptedCompletion{}, false, nil // already accepted/rejected → no-op
	}
	row := rows[0]
	return domain.AcceptedCompletion{
		CompletionID:   completionID,
		Status:         "accepted",
		ObligationID:   row.ObligationID,
		GoatID:         row.GoatID,
		BatchID:        row.BatchID,
		LotID:          row.VaccineInventoryLotID,
		Doses:          row.Doses,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

// GetRecordedCompletion returns the SM-5 context before a recorded completion is accepted.
func (r *Repository) GetRecordedCompletion(ctx context.Context, tenantID, completionID string) (domain.AcceptedCompletion, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: completion id: %w", err)
	}
	row, err := r.queries.GetRecordedVaccinationCompletion(ctx, vaccinationdb.GetRecordedVaccinationCompletionParams{
		TenantID:     tenant,
		CompletionID: cid,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptedCompletion{}, false, nil
	}
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: get recorded completion: %w", err)
	}
	return domain.AcceptedCompletion{
		CompletionID:   completionID,
		Status:         "recorded",
		ObligationID:   row.ObligationID,
		GoatID:         row.GoatID,
		BatchID:        row.BatchID,
		LotID:          row.VaccineInventoryLotID,
		Doses:          row.Doses,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

// GetAcceptableCompletion returns context for a recorded or already-accepted completion so SM-5
// retries can resume idempotent side effects after a partial failure.
func (r *Repository) GetAcceptableCompletion(ctx context.Context, tenantID, completionID string) (domain.AcceptedCompletion, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: completion id: %w", err)
	}
	row, err := r.queries.GetAcceptableVaccinationCompletion(ctx, vaccinationdb.GetAcceptableVaccinationCompletionParams{
		TenantID:     tenant,
		CompletionID: cid,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptedCompletion{}, false, nil
	}
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: get acceptable completion: %w", err)
	}
	return domain.AcceptedCompletion{
		CompletionID:   row.CompletionID,
		Status:         row.Status,
		ObligationID:   row.ObligationID,
		GoatID:         row.GoatID,
		BatchID:        row.BatchID,
		LotID:          row.VaccineInventoryLotID,
		Doses:          row.Doses,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

// GetAcceptableCompletionByIdempotency returns context for a recorded or already-accepted direct
// completion keyed by idempotency key.
func (r *Repository) GetAcceptableCompletionByIdempotency(ctx context.Context, tenantID, idempotencyKey string) (domain.AcceptedCompletion, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	row, err := r.queries.GetAcceptableVaccinationCompletionByIdempotency(ctx, vaccinationdb.GetAcceptableVaccinationCompletionByIdempotencyParams{
		TenantID:       tenant,
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptedCompletion{}, false, nil
	}
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: get acceptable completion by idempotency: %w", err)
	}
	return domain.AcceptedCompletion{
		CompletionID:   row.CompletionID,
		Status:         row.Status,
		ObligationID:   row.ObligationID,
		GoatID:         row.GoatID,
		BatchID:        row.BatchID,
		LotID:          row.VaccineInventoryLotID,
		Doses:          row.Doses,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

// RejectCompletion marks a recorded completion rejected (SM-5 rework). applied is false on replay.
func (r *Repository) RejectCompletion(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return false, fmt.Errorf("vaccination: completion id: %w", err)
	}
	n, err := r.queries.RejectVaccinationCompletion(ctx, vaccinationdb.RejectVaccinationCompletionParams{
		VerifiedBy:      pgconv.NullableUUID(verifiedBy),
		RejectionReason: pgconv.Text(reason),
		TenantID:        tenant,
		CompletionID:    cid,
	})
	if err != nil {
		return false, fmt.Errorf("vaccination: reject completion: %w", err)
	}
	return n == 1, nil
}

// ListRecordedCompletionsByTask returns the still-recorded completion ids captured under a SOP
// task's submissions (the verify fan-out source).
func (r *Repository) ListRecordedCompletionsByTask(ctx context.Context, tenantID, taskID string) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	task, err := pgconv.UUID(taskID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: task id: %w", err)
	}
	ids, err := r.queries.ListRecordedCompletionsByTask(ctx, vaccinationdb.ListRecordedCompletionsByTaskParams{
		TenantID: tenant, TaskID: task,
	})
	if err != nil {
		return nil, fmt.Errorf("vaccination: list recorded completions by task: %w", err)
	}
	return ids, nil
}

// RecordCompletionsFromSubmission records one completion per per-goat SOP submission item for a
// vaccination task. The matching obligation comes from the generic obligation batch/task linkage.
func (r *Repository) RecordCompletionsFromSubmission(ctx context.Context, tenantID, taskID, submissionID, recordedBy string) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	task, err := pgconv.UUID(taskID)
	if err != nil {
		return 0, fmt.Errorf("vaccination: task id: %w", err)
	}
	submission, err := pgconv.UUID(submissionID)
	if err != nil {
		return 0, fmt.Errorf("vaccination: submission id: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
INSERT INTO vaccination_completions (
  tenant_id,
  obligation_id,
  batch_id,
  goat_id,
  sop_submission_item_id,
  vaccine_inventory_lot_id,
  doses,
  dose_ml_given,
  route_site,
  adverse_reaction,
  cold_chain_verified,
  administered_at,
  status,
  recorded_by,
  idempotency_key
)
SELECT
  si.tenant_id,
  oi.obligation_id,
  oi.batch_id,
  si.goat_id,
  si.item_id,
  nullif(ss.answers ->> 'vaccine_lot_id', '')::uuid,
  COALESCE(nullif(ss.answers ->> 'doses', '')::int, 1),
  nullif(ss.answers ->> 'dose_ml_given', '')::numeric,
  nullif(ss.answers ->> 'route_site', ''),
  COALESCE((ss.answers ->> 'adverse_reaction')::boolean, false),
  COALESCE((ss.answers ->> 'cold_chain_verified')::boolean, false),
  COALESCE(nullif(ss.answers ->> 'administered_at', '')::timestamptz, ss.submitted_at),
  'recorded',
  nullif($4, '')::uuid,
  'vaccination:sop_submission_item:' || si.item_id::text
FROM sop_submission_items si
JOIN sop_submissions ss
  ON ss.tenant_id = si.tenant_id
 AND ss.submission_id = si.submission_id
JOIN sop_tasks st
  ON st.tenant_id = si.tenant_id
 AND st.task_id = si.task_id
JOIN sop_definitions sd
  ON sd.tenant_id = st.tenant_id
 AND sd.sop_id = st.sop_id
LEFT JOIN obligation_batches ob
  ON ob.tenant_id = st.tenant_id
 AND ob.sop_task_id = st.task_id
JOIN obligation_instances oi
  ON oi.tenant_id = si.tenant_id
 AND oi.target_type = 'goat'
 AND oi.target_id = si.goat_id
 AND (
      oi.sop_task_id = st.task_id
      OR (ob.batch_id IS NOT NULL AND oi.batch_id = ob.batch_id)
 )
WHERE si.tenant_id = $1
  AND si.task_id = $2
  AND si.submission_id = $3
  AND si.goat_id IS NOT NULL
  AND si.state IN ('accepted', 'needs_review')
  AND oi.status NOT IN ('completed', 'waived', 'canceled', 'superseded')
  AND (
    oi.batch_id IS NULL
    OR (
      nullif(ss.answers ->> 'vaccine_lot_id', '') IS NOT NULL
      AND COALESCE((ss.answers ->> 'cold_chain_verified')::boolean, false)
    )
  )
  AND (
    sd.code IN ('vaccination.drive', 'vaccination.session')
    OR st.task_type IN ('vaccination', 'vaccination_drive', 'vaccination_session')
  )
ON CONFLICT DO NOTHING
RETURNING completion_id::text`,
		tenant, task, submission, recordedBy)
	if err != nil {
		return 0, fmt.Errorf("vaccination: record completions from submission: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("vaccination: record completions from submission rows: %w", err)
	}
	eligibleItems, materializedItems, err := r.submissionFanoutCounts(ctx, tenant, task, submission)
	if err != nil {
		return 0, err
	}
	if materializedItems < eligibleItems {
		return materializedItems, fmt.Errorf("vaccination: materialized %d of %d eligible submission items", materializedItems, eligibleItems)
	}
	if materializedItems > 0 {
		return materializedItems, nil
	}
	return count, nil
}

func (r *Repository) submissionFanoutCounts(ctx context.Context, tenant, task, submission pgtype.UUID) (eligibleItems, materializedItems int, err error) {
	err = r.pool.QueryRow(ctx, `
	SELECT count(si.item_id)::int,
	       count(DISTINCT vc.sop_submission_item_id)::int
	FROM sop_submission_items si
	LEFT JOIN vaccination_completions vc
	  ON vc.tenant_id = si.tenant_id
	 AND vc.sop_submission_item_id = si.item_id
	WHERE si.tenant_id = $1
	  AND si.task_id = $2
	  AND si.submission_id = $3
	  AND si.goat_id IS NOT NULL
	  AND si.state IN ('accepted', 'needs_review')`,
		tenant, task, submission).Scan(&eligibleItems, &materializedItems)
	if err != nil {
		return 0, 0, fmt.Errorf("vaccination: count submission fanout rows: %w", err)
	}
	return eligibleItems, materializedItems, nil
}

// ListRecordedCompletions returns completions awaiting review (status='recorded'), earliest
// administered first (the Verification queue).
func (r *Repository) ListRecordedCompletions(ctx context.Context, tenantID, parkID string, limit int32) ([]domain.RecordedCompletion, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	var park pgtype.UUID
	if parkID != "" {
		park, err = pgconv.UUID(parkID)
		if err != nil {
			return nil, fmt.Errorf("vaccination: park id: %w", err)
		}
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.queries.ListRecordedCompletions(ctx, vaccinationdb.ListRecordedCompletionsParams{
		TenantID: tenant, ParkID: park, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("vaccination: list recorded completions: %w", err)
	}
	out := make([]domain.RecordedCompletion, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.RecordedCompletion{
			CompletionID:   row.CompletionID,
			ObligationID:   row.ObligationID,
			GoatID:         row.GoatID,
			BatchID:        row.BatchID,
			AdministeredAt: row.AdministeredAt.Time,
			Doses:          row.Doses,
			RouteSite:      row.RouteSite,
		})
	}
	return out, nil
}

// ListCompletionsByGoat returns a goat's vaccination history (most recent first).
func (r *Repository) ListCompletionsByGoat(ctx context.Context, tenantID, goatID string, limit int32) ([]domain.CompletionHistoryItem, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: goat id: %w", err)
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.queries.ListVaccinationCompletionsByGoat(ctx, vaccinationdb.ListVaccinationCompletionsByGoatParams{
		TenantID: tenant, GoatID: goat, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("vaccination: list completions: %w", err)
	}
	out := make([]domain.CompletionHistoryItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.CompletionHistoryItem{
			CompletionID:        row.CompletionID,
			ObligationID:        row.ObligationID,
			BatchID:             row.BatchID,
			AdministeredAt:      row.AdministeredAt.Time,
			Status:              row.Status,
			Doses:               row.Doses,
			RouteSite:           row.RouteSite,
			AdverseReaction:     row.AdverseReaction,
			WithdrawalUntilDate: pgconv.DateValue(row.WithdrawalUntilDate),
		})
	}
	return out, nil
}

// GetLastAcceptedForGoat returns the most recent accepted administration (found=false when none).
func (r *Repository) GetLastAcceptedForGoat(ctx context.Context, tenantID, goatID string) (domain.LastAccepted, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.LastAccepted{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return domain.LastAccepted{}, false, fmt.Errorf("vaccination: goat id: %w", err)
	}
	row, err := r.queries.GetLastAcceptedCompletionForGoat(ctx, vaccinationdb.GetLastAcceptedCompletionForGoatParams{TenantID: tenant, GoatID: goat})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LastAccepted{}, false, nil
	}
	if err != nil {
		return domain.LastAccepted{}, false, fmt.Errorf("vaccination: last accepted: %w", err)
	}
	return domain.LastAccepted{
		CompletionID:   row.CompletionID,
		ObligationID:   row.ObligationID,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

func (r *Repository) eligParams(f domain.ImpactFilter) (vaccinationdb.CountEligibleGoatsParams, error) {
	tenant, err := pgconv.UUID(f.TenantID)
	if err != nil {
		return vaccinationdb.CountEligibleGoatsParams{}, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	return vaccinationdb.CountEligibleGoatsParams{
		TenantID: tenant,
		Stage:    f.Stage,
		Sex:      f.Sex,
		Breed:    f.Breed,
		Health:   f.Health,
		ParkID:   pgconv.NullableUUID(f.ParkID),
	}, nil
}

// CountEligibleGoats counts alive goats matching the eligibility filter.
func (r *Repository) CountEligibleGoats(ctx context.Context, f domain.ImpactFilter) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	p, err := r.eligParams(f)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.CountEligibleGoats(ctx, p)
	if err != nil {
		return 0, fmt.Errorf("vaccination: count eligible: %w", err)
	}
	return n, nil
}

// CountCatchupGoats counts eligible goats with a prior accepted completion.
func (r *Repository) CountCatchupGoats(ctx context.Context, f domain.ImpactFilter) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	p, err := r.eligParams(f)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.CountCatchupGoats(ctx, vaccinationdb.CountCatchupGoatsParams(p))
	if err != nil {
		return 0, fmt.Errorf("vaccination: count catchup: %w", err)
	}
	return n, nil
}

// CountEligibleShedScopes counts distinct sheds holding eligible goats (≈ drive batches).
func (r *Repository) CountEligibleShedScopes(ctx context.Context, f domain.ImpactFilter) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	p, err := r.eligParams(f)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.CountEligibleShedScopes(ctx, vaccinationdb.CountEligibleShedScopesParams(p))
	if err != nil {
		return 0, fmt.Errorf("vaccination: count shed scopes: %w", err)
	}
	return n, nil
}

// ListEligibleGoatsForGeneration returns a chunked page of the in-care cohort for SM-1.
func (r *Repository) ListEligibleGoatsForGeneration(ctx context.Context, f domain.ImpactFilter, afterGoatID string, limit int32) ([]domain.EligibleGoat, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(f.TenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	after := afterGoatID
	if after == "" {
		after = "00000000-0000-0000-0000-000000000000"
	}
	afterUUID, err := pgconv.UUID(after)
	if err != nil {
		return nil, fmt.Errorf("vaccination: cursor: %w", err)
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := r.queries.ListEligibleGoatsForGeneration(ctx, vaccinationdb.ListEligibleGoatsForGenerationParams{
		TenantID:    tenant,
		Stage:       f.Stage,
		Sex:         f.Sex,
		Breed:       f.Breed,
		Health:      f.Health,
		ParkID:      pgconv.NullableUUID(f.ParkID),
		AfterGoatID: afterUUID,
		RowLimit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("vaccination: list eligible goats: %w", err)
	}
	out := make([]domain.EligibleGoat, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.EligibleGoat{
			GoatID:               row.GoatID,
			DOB:                  pgconv.DateValue(row.Dob),
			EntryDate:            pgconv.DateValue(row.EntryDate),
			LifecycleStatus:      row.LifecycleStatus,
			HealthStatus:         row.HealthStatus,
			ReproductiveStatus:   row.ReproductiveStatus,
			ShedID:               row.ShedID,
			ParkID:               row.ParkID,
			Sex:                  row.Sex,
			Breed:                row.Breed,
			Stage:                row.ManagementStage,
			LocationIsQuarantine: row.LocationIsQuarantine,
			LocationIsICU:        row.LocationIsIcu,
		})
	}
	return out, nil
}

// GetGoatForGeneration loads one goat's generation fields (incl sex/breed/stage).
func (r *Repository) GetGoatForGeneration(ctx context.Context, tenantID, goatID string) (domain.EligibleGoat, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.EligibleGoat{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return domain.EligibleGoat{}, false, fmt.Errorf("vaccination: goat id: %w", err)
	}
	row, err := r.queries.GetGoatForGeneration(ctx, vaccinationdb.GetGoatForGenerationParams{TenantID: tenant, GoatID: goat})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EligibleGoat{}, false, nil
	}
	if err != nil {
		return domain.EligibleGoat{}, false, fmt.Errorf("vaccination: get goat for generation: %w", err)
	}
	return domain.EligibleGoat{
		GoatID:               row.GoatID,
		DOB:                  pgconv.DateValue(row.Dob),
		EntryDate:            pgconv.DateValue(row.EntryDate),
		LifecycleStatus:      row.LifecycleStatus,
		HealthStatus:         row.HealthStatus,
		ReproductiveStatus:   row.ReproductiveStatus,
		ShedID:               row.ShedID,
		ParkID:               row.ParkID,
		Sex:                  row.Sex,
		Breed:                row.Breed,
		Stage:                row.ManagementStage,
		LocationIsQuarantine: row.LocationIsQuarantine,
		LocationIsICU:        row.LocationIsIcu,
	}, true, nil
}

// HasTrustedCompletionEvidence returns true when prior trusted evidence already satisfies the
// matching protocol rule for this goat as of this generation run, so SM-1 must not re-issue the dose.
// Two trusted sources suppress due work:
//  1. Reviewed supplier/HF evidence (procurement_hf_vaccination_evidence, review_status='trusted').
//  2. Accepted Goat OS administrations (vaccination_completions, status='accepted') for the SAME
//     protocol (protocol_id) and dose_code — matched across protocol versions so that publishing a
//     new version of a protocol the goat already completed does not duplicate the dose (version
//     change / catch-up dedupe).
//
// Imported/rejected/conflicting/duplicate rows, future administrations, and future reviews never
// suppress due work.
func (r *Repository) HasTrustedCompletionEvidence(ctx context.Context, tenantID, goatID, protocolVersionID, ruleID, doseCode string, dueAt, generationAt time.Time) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return false, fmt.Errorf("vaccination: goat id: %w", err)
	}
	version, err := pgconv.UUID(protocolVersionID)
	if err != nil {
		return false, fmt.Errorf("vaccination: protocol version id: %w", err)
	}
	rule, err := pgconv.UUID(ruleID)
	if err != nil {
		return false, fmt.Errorf("vaccination: rule id: %w", err)
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `
SELECT (
  EXISTS (
    SELECT 1
    FROM procurement_hf_vaccination_evidence ev
    JOIN goats g
      ON g.tenant_id = ev.tenant_id
     AND g.goat_id = ev.goat_id
    LEFT JOIN procurement_load_goats plg
      ON plg.tenant_id = ev.tenant_id
     AND plg.load_id = ev.load_id
     AND plg.goat_id = ev.goat_id
    WHERE ev.tenant_id = $1
      AND ev.goat_id = $2
      AND ev.protocol_version_id = $3
      AND ev.rule_id = $4
      AND ev.dose_code = $5
      AND ev.review_status = 'trusted'
      AND ev.reviewed_at IS NOT NULL
      AND ev.administered_at <= $6::timestamptz
      AND ev.administered_at <= $7::timestamptz
      AND ev.reviewed_at <= $7::timestamptz
      AND (
        plg.intake_accepted_at IS NULL
        OR ev.administered_at <= plg.intake_accepted_at
      )
      AND (
        g.entry_date IS NULL
        OR ev.administered_at < (g.entry_date::timestamptz + interval '1 day')
      )
  )
  OR EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN obligation_instances oi
      ON oi.tenant_id = vc.tenant_id
     AND oi.obligation_id = vc.obligation_id
    JOIN protocol_rules cpr
      ON cpr.tenant_id = oi.tenant_id
     AND cpr.rule_id = oi.rule_id
    JOIN protocol_versions cpv
      ON cpv.tenant_id = oi.tenant_id
     AND cpv.protocol_version_id = oi.protocol_version_id
    WHERE vc.tenant_id = $1
      AND vc.goat_id = $2
      AND vc.status = 'accepted'
      AND cpr.dose_code = $5
      AND cpv.protocol_id = (
        SELECT protocol_id
        FROM protocol_versions
        WHERE tenant_id = $1 AND protocol_version_id = $3
      )
      AND vc.administered_at <= $6::timestamptz
      AND vc.administered_at <= $7::timestamptz
  )
)`, tenant, goat, version, rule, doseCode, dueAt, generationAt).Scan(&exists); err != nil {
		return false, fmt.Errorf("vaccination: trusted completion evidence: %w", err)
	}
	return exists, nil
}

// SumAvailableStock returns available (unreserved) quantity + earliest expiry for an item.
func (r *Repository) SumAvailableStock(ctx context.Context, tenantID, itemID string, locationID *string) (string, *time.Time, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	item, err := pgconv.UUID(itemID)
	if err != nil {
		return "", nil, fmt.Errorf("vaccination: item id: %w", err)
	}
	row, err := r.queries.SumAvailableStockForItem(ctx, vaccinationdb.SumAvailableStockForItemParams{
		TenantID:   tenant,
		ItemID:     item,
		LocationID: pgconv.NullableUUID(locationID),
	})
	if err != nil {
		return "", nil, fmt.Errorf("vaccination: sum available stock: %w", err)
	}
	return pgconv.NumericString(row.Available), pgconv.DateValue(row.EarliestExpiry), nil
}
