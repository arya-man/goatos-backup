// Package postgres is the Postgres adapter for the bulk status-update kernel. It
// owns the bulk_status_job / bulk_status_job_row tables (migration 000145). It
// reads goats.row_version for enqueue-time no-clobber capture and preview state,
// but never writes the goats table — goat mutations go through the identity
// module via the worker's applier.
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

	bulkapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
)

// enqueueChunkSize bounds the number of rows per INSERT ... SELECT unnest so a
// large job is inserted in bounded, set-based statements rather than one giant
// parameter array or a per-row loop.
const enqueueChunkSize = 1000

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = 5 * time.Second
	}
	return &Repository{pool: pool, queryTimeout: queryTimeout}
}

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.queryTimeout)
}

// ReadGoatStates returns the live state for a bounded set of goat ids (read-only).
func (r *Repository) ReadGoatStates(ctx context.Context, tenantID string, goatIDs []string) (map[string]bulkapp.GoatState, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	out := make(map[string]bulkapp.GoatState, len(goatIDs))
	if len(goatIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT goat_id::text,
       lifecycle_status,
       COALESCE(reproductive_status, ''),
       COALESCE(health_status, ''),
       COALESCE(management_stage, ''),
       row_version,
       merged_into_goat_id IS NOT NULL
FROM goats
WHERE tenant_id = $1::uuid AND goat_id = ANY($2::uuid[])`, tenantID, goatIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st bulkapp.GoatState
		if err := rows.Scan(&st.GoatID, &st.LifecycleStatus, &st.ReproductiveStatus, &st.HealthStatus, &st.ManagementStage, &st.RowVersion, &st.Merged); err != nil {
			return nil, err
		}
		st.Exists = true
		out[st.GoatID] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ReproductiveStatusExists validates a target against source-backed vocabulary.
func (r *Repository) ReproductiveStatusExists(ctx context.Context, code string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	var exists bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM status_definitions
  WHERE axis = 'reproductive' AND active = true AND status_code = $1
)`, code).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// EnqueueJob inserts a job + one row per goat inside one transaction. Idempotent
// on (tenant_id, idempotency_key): an exact replay returns the original job; a
// same-key different-fingerprint replay returns ErrIdempotencyConflict.
func (r *Repository) EnqueueJob(ctx context.Context, params bulkapp.EnqueueJobParams) (bulkapp.EnqueueJobResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout*4)
	defer cancel()

	meta, err := json.Marshal(map[string]any{
		"fingerprint": params.Fingerprint,
		"row_count":   len(params.Rows),
	})
	if err != nil {
		return bulkapp.EnqueueJobResult{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return bulkapp.EnqueueJobResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var jobID string
	err = tx.QueryRow(ctx, `
INSERT INTO bulk_status_job (tenant_id, actor_id, axis, params, total_rows, state, idempotency_key)
VALUES ($1::uuid, NULLIF($2,'')::uuid, $3, $4::jsonb, $5, 'pending', $6)
ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL
DO NOTHING
RETURNING bulk_status_job_id::text`,
		params.TenantID, params.ActorID, params.Axis, meta, len(params.Rows), params.IdempotencyKey).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.replayEnqueue(ctx, tx, &committed, params)
	}
	if err != nil {
		return bulkapp.EnqueueJobResult{}, err
	}

	if err := r.insertJobRows(ctx, tx, params, jobID); err != nil {
		return bulkapp.EnqueueJobResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return bulkapp.EnqueueJobResult{}, err
	}
	committed = true
	return bulkapp.EnqueueJobResult{JobID: jobID, TotalRows: len(params.Rows), State: bulkapp.JobStatePending, Replayed: false}, nil
}

func (r *Repository) replayEnqueue(ctx context.Context, tx pgx.Tx, committed *bool, params bulkapp.EnqueueJobParams) (bulkapp.EnqueueJobResult, error) {
	var jobID, state, fingerprint string
	var totalRows int
	err := tx.QueryRow(ctx, `
SELECT bulk_status_job_id::text, state, total_rows, COALESCE(params->>'fingerprint','')
FROM bulk_status_job
WHERE tenant_id = $1::uuid AND idempotency_key = $2`,
		params.TenantID, params.IdempotencyKey).Scan(&jobID, &state, &totalRows, &fingerprint)
	if err != nil {
		return bulkapp.EnqueueJobResult{}, err
	}
	if fingerprint != params.Fingerprint {
		return bulkapp.EnqueueJobResult{}, bulkapp.ErrIdempotencyConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return bulkapp.EnqueueJobResult{}, err
	}
	*committed = true
	return bulkapp.EnqueueJobResult{JobID: jobID, TotalRows: totalRows, State: state, Replayed: true}, nil
}

// insertJobRows performs the set-based, chunked capture of expected_row_version
// by LEFT JOINing goats. A missing goat yields a NULL expected_row_version (the
// worker skips it). No goat state is mutated here.
func (r *Repository) insertJobRows(ctx context.Context, tx pgx.Tx, params bulkapp.EnqueueJobParams, jobID string) error {
	rows := params.Rows
	for start := 0; start < len(rows); start += enqueueChunkSize {
		end := start + enqueueChunkSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		goatIDs := make([]string, len(chunk))
		targets := make([]string, len(chunk))
		reasons := make([]string, len(chunk))
		for i, row := range chunk {
			goatIDs[i] = row.GoatID
			targets[i] = row.Target
			reasons[i] = row.Reason
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO bulk_status_job_row (
  tenant_id, job_id, goat_id, axis, target, reason, expected_row_version, row_state
)
SELECT $1::uuid, $2::uuid, u.goat_id::uuid, $3, u.target, NULLIF(u.reason, ''), g.row_version, 'pending'
FROM unnest($4::text[], $5::text[], $6::text[]) AS u(goat_id, target, reason)
LEFT JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = u.goat_id::uuid
ON CONFLICT (job_id, goat_id, axis) DO NOTHING`,
			params.TenantID, jobID, params.Axis, goatIDs, targets, reasons); err != nil {
			return err
		}
	}
	return nil
}

// ClaimRows locks a batch of claimable rows with FOR UPDATE SKIP LOCKED.
func (r *Repository) ClaimRows(ctx context.Context, tenantID, jobID string, limit, leaseSeconds int) ([]bulkapp.ClaimedRow, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
WITH claimable AS (
  SELECT bulk_status_job_row_id
  FROM bulk_status_job_row
  WHERE tenant_id = $1::uuid AND job_id = $2::uuid
    AND (
      row_state IN ('pending', 'retry')
      OR ($4 > 0 AND row_state = 'claimed' AND claimed_at IS NOT NULL AND claimed_at < now() - make_interval(secs => $4))
    )
  ORDER BY created_at
  FOR UPDATE SKIP LOCKED
  LIMIT $3
)
UPDATE bulk_status_job_row r
SET row_state = 'claimed', claimed_at = now(), updated_at = now()
FROM claimable c
JOIN bulk_status_job j ON j.bulk_status_job_id = $2::uuid AND j.tenant_id = $1::uuid
WHERE r.bulk_status_job_row_id = c.bulk_status_job_row_id
RETURNING r.bulk_status_job_row_id::text, r.goat_id::text, COALESCE(j.actor_id::text, ''), r.axis, r.target, COALESCE(r.reason, ''), r.expected_row_version, r.retry_count`,
		tenantID, jobID, limit, leaseSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]bulkapp.ClaimedRow, 0, limit)
	for rows.Next() {
		var claimed bulkapp.ClaimedRow
		var expected pgtype.Int8
		if err := rows.Scan(&claimed.RowID, &claimed.GoatID, &claimed.ActorID, &claimed.Axis, &claimed.Target, &claimed.Reason, &expected, &claimed.RetryCount); err != nil {
			return nil, err
		}
		if expected.Valid {
			v := int(expected.Int64)
			claimed.ExpectedRowVersion = &v
		}
		out = append(out, claimed)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) MarkRowApplied(ctx context.Context, tenantID, rowID, eventID string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
UPDATE bulk_status_job_row
SET row_state = 'applied', event_id = NULLIF($3, '')::uuid, applied_at = now(), failure_reason = NULL, updated_at = now()
WHERE tenant_id = $1::uuid AND bulk_status_job_row_id = $2::uuid`, tenantID, rowID, eventID)
	return err
}

func (r *Repository) MarkRowSkipped(ctx context.Context, tenantID, rowID, reason string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
UPDATE bulk_status_job_row
SET row_state = 'skipped', failure_reason = NULLIF($3, ''), updated_at = now()
WHERE tenant_id = $1::uuid AND bulk_status_job_row_id = $2::uuid`, tenantID, rowID, reason)
	return err
}

func (r *Repository) MarkRowError(ctx context.Context, tenantID, rowID, reason string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
UPDATE bulk_status_job_row
SET row_state = 'error', failure_reason = NULLIF($3, ''), updated_at = now()
WHERE tenant_id = $1::uuid AND bulk_status_job_row_id = $2::uuid`, tenantID, rowID, reason)
	return err
}

// MarkRowRetry increments retry_count and parks the row as 'error' once the cap
// is reached, otherwise sets it back to 'retry' so it is re-claimed.
func (r *Repository) MarkRowRetry(ctx context.Context, tenantID, rowID, reason string, maxRetries int) (bulkapp.RowOutcome, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if maxRetries <= 0 {
		maxRetries = 1
	}
	var state string
	err := r.pool.QueryRow(ctx, `
UPDATE bulk_status_job_row
SET retry_count = retry_count + 1,
    row_state = CASE WHEN retry_count + 1 >= $4 THEN 'error' ELSE 'retry' END,
    failure_reason = NULLIF($3, ''),
    updated_at = now()
WHERE tenant_id = $1::uuid AND bulk_status_job_row_id = $2::uuid
RETURNING row_state`, tenantID, rowID, reason, maxRetries).Scan(&state)
	if err != nil {
		return "", err
	}
	if state == "error" {
		return bulkapp.OutcomeError, nil
	}
	return bulkapp.OutcomeRetry, nil
}

// RefreshJobCounts recomputes the ledger rollup and advances job state.
func (r *Repository) RefreshJobCounts(ctx context.Context, tenantID, jobID string) (bulkapp.JobCounts, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	var counts bulkapp.JobCounts
	err := r.pool.QueryRow(ctx, `
WITH c AS (
  SELECT
    count(*) FILTER (WHERE row_state = 'applied') AS applied,
    count(*) FILTER (WHERE row_state = 'skipped') AS skipped,
    count(*) FILTER (WHERE row_state = 'error') AS failed,
    count(*) FILTER (WHERE row_state IN ('pending', 'retry', 'claimed')) AS remaining
  FROM bulk_status_job_row
  WHERE tenant_id = $1::uuid AND job_id = $2::uuid
)
UPDATE bulk_status_job j
SET applied_rows = c.applied,
    skipped_rows = c.skipped,
    failed_rows = c.failed,
    state = CASE
      WHEN j.state = 'canceled' THEN j.state
      WHEN c.remaining > 0 THEN 'running'
      WHEN c.failed > 0 THEN 'failed'
      ELSE 'completed'
    END,
    updated_at = now()
FROM c
WHERE j.tenant_id = $1::uuid AND j.bulk_status_job_id = $2::uuid
RETURNING j.total_rows, j.applied_rows, j.skipped_rows, j.failed_rows, c.remaining, j.state`,
		tenantID, jobID).Scan(&counts.Total, &counts.Applied, &counts.Skipped, &counts.Failed, &counts.Remaining, &counts.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return bulkapp.JobCounts{}, fmt.Errorf("bulk status job %s not found for tenant %s", jobID, tenantID)
	}
	if err != nil {
		return bulkapp.JobCounts{}, err
	}
	return counts, nil
}

// BumpJobCounts applies a per-pass delta to the job rollup counters instead of a
// full COUNT over every row. RunUntilDrained calls this each pass with the pass's
// terminal tallies, so keeping counters warm is O(1) per pass rather than
// O(rows). The authoritative full recount + terminal-state transition still
// happens once at drain end via RefreshJobCounts, which reconciles any drift.
func (r *Repository) BumpJobCounts(ctx context.Context, tenantID, jobID string, appliedDelta, skippedDelta, failedDelta int) error {
	if appliedDelta == 0 && skippedDelta == 0 && failedDelta == 0 {
		return nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
UPDATE bulk_status_job
SET applied_rows = applied_rows + $3,
    skipped_rows = skipped_rows + $4,
    failed_rows = failed_rows + $5,
    state = CASE WHEN state = 'canceled' THEN state ELSE 'running' END,
    updated_at = now()
WHERE tenant_id = $1::uuid AND bulk_status_job_id = $2::uuid`,
		tenantID, jobID, appliedDelta, skippedDelta, failedDelta)
	return err
}

// ListJobIDsWithClaimableRows returns tenant jobs that still have work: pending,
// retry, OR claimed rows. 'claimed' is included so a job whose LAST rows were
// claimed by a worker that then crashed is still rediscovered by the tenant-wide
// worker (RunTenant); the stale-lease gate lives in ClaimRows, and RunUntilDrained
// breaks on a zero-claim pass, so surfacing an actively-claimed (non-stale) job
// here is harmless (one no-op pass, no spin). Uses the partial claim index.
func (r *Repository) ListJobIDsWithClaimableRows(ctx context.Context, tenantID string, limit int) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
SELECT DISTINCT job_id::text
FROM bulk_status_job_row
WHERE tenant_id = $1::uuid AND row_state IN ('pending', 'retry', 'claimed')
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListJobIDsNeedingRollup returns active (pending|running) jobs whose rows are
// ALL terminal (applied|skipped|error) — i.e. there is nothing claimable left,
// but the job-level state/counters were never refreshed. This is the terminal
// crash-orphan: a worker marked the last rows terminal, then died before
// RefreshJobCounts, leaving the job stuck 'running' and invisible to the
// claimable-row discovery path. RunTenant rolls these up so a completed/failed
// job is never left hidden.
func (r *Repository) ListJobIDsNeedingRollup(ctx context.Context, tenantID string, limit int) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
SELECT j.bulk_status_job_id::text
FROM bulk_status_job j
WHERE j.tenant_id = $1::uuid
  AND j.state IN ('pending', 'running')
  AND NOT EXISTS (
    SELECT 1 FROM bulk_status_job_row r
    WHERE r.tenant_id = j.tenant_id
      AND r.job_id = j.bulk_status_job_id
      AND r.row_state IN ('pending', 'retry', 'claimed')
  )
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
